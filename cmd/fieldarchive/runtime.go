package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"field-voice-archive/internal/audit"
	"field-voice-archive/internal/server"
	"field-voice-archive/internal/store"
	"field-voice-archive/internal/workflow"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func buildApp(dataDir string) (*server.API, error) {
	repo, err := store.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("打开快照存储: %w", err)
	}
	logbook, err := audit.Open(filepath.Join(dataDir, "audit"))
	if err != nil {
		return nil, fmt.Errorf("回放审计链: %w", err)
	}
	if err = logbook.Verify(); err != nil {
		return nil, err
	}
	batches, err := repo.ListBatches()
	if err != nil {
		return nil, fmt.Errorf("读取批次快照: %w", err)
	}
	for _, batch := range batches {
		if audited := logbook.BatchRevision(batch.BatchID); audited != batch.Revision {
			return nil, fmt.Errorf("批次 %s 的快照 revision %d 与审计 revision %d 不一致", batch.BatchID, batch.Revision, audited)
		}
	}
	return server.New(workflow.New(repo, logbook))
}

func runSelfCheck(cfg config) error {
	temp, err := os.MkdirTemp("", "fieldarchive-selfcheck-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	app, err := buildApp(temp)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("自检监听 %s: %w", cfg.addr, err)
	}
	httpServer := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 3 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
		<-done
	}()
	base := "http://" + listener.Addr().String()
	client := &http.Client{Timeout: 4 * time.Second}
	if err = waitReady(client, base); err != nil {
		return err
	}
	today := time.Now().UTC().Format("2006-01-02")
	signed := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	steps := []struct {
		path string
		body any
		want int
	}{
		{"/api/v1/batches", map[string]any{"request_id": "self-create", "actor": "collector-self", "batch_id": "self-check", "title": "自检录音批次", "language_variant": "自检语支", "collection_site": "回环测试点", "collected_at": today, "media_sha256": strings.Repeat("a", 64)}, http.StatusCreated},
		{"/api/v1/batches/self-check/consents", map[string]any{"request_id": "self-consent", "actor": "collector-self", "expected_revision": 1, "consent_id": "consent-self", "participant_code": "speaker-self", "scope": []string{"open_archive"}, "signed_at": signed, "withdrawal_rule": "发布前可书面撤回", "evidence_digest": strings.Repeat("b", 64), "verified_by": "verifier-self"}, http.StatusOK},
		{"/api/v1/batches/self-check/annotations", map[string]any{"request_id": "self-annotate", "actor": "editor-self", "expected_revision": 2, "segments": []map[string]any{{"segment_id": "segment-self", "start_ms": 0, "end_ms": 2800, "speaker_code": "speaker-self", "raw_text": "阿岚住在青石村"}}}, http.StatusOK},
		{"/api/v1/batches/self-check/redactions/preview", map[string]any{"marks": map[string]any{"segment-self": []map[string]any{{"start": 0, "end": 2, "kind": "person"}, {"start": 4, "end": 7, "kind": "place"}}}}, http.StatusOK},
		{"/api/v1/batches/self-check/redactions", map[string]any{"request_id": "self-redact", "actor": "editor-self", "expected_revision": 3, "marks": map[string]any{"segment-self": []map[string]any{{"start": 0, "end": 2, "kind": "person"}, {"start": 4, "end": 7, "kind": "place"}}}}, http.StatusOK},
		{"/api/v1/batches/self-check/reviews", map[string]any{"request_id": "self-review", "actor": "reviewer-self", "expected_revision": 4, "review_id": "review-self", "decision": "approved", "findings": "授权与脱敏完整", "required_changes": []string{}}, http.StatusOK},
	}
	for _, step := range steps {
		if _, err = postJSON(client, base+step.path, step.body, step.want); err != nil {
			return err
		}
	}
	preflightBody, err := postJSON(client, base+"/api/v1/batches/self-check/release", map[string]any{"request_id": "self-release-preflight", "actor": "archivist-self", "expected_revision": 5, "preflight": true}, http.StatusOK)
	if err != nil {
		return err
	}
	var preflight struct {
		Report struct {
			ManifestDigest string `json:"manifest_digest"`
			LockToken      string `json:"lock_token"`
		} `json:"report"`
	}
	if err := json.Unmarshal(preflightBody, &preflight); err != nil || preflight.Report.LockToken == "" || preflight.Report.ManifestDigest == "" {
		return errors.New("发布预检未返回有效锁和摘要")
	}
	if _, err = postJSON(client, base+"/api/v1/batches/self-check/release", map[string]any{"request_id": "self-release", "actor": "archivist-self", "expected_revision": 5, "lock_token": preflight.Report.LockToken, "expected_manifest_digest": preflight.Report.ManifestDigest}, http.StatusOK); err != nil {
		return err
	}
	for _, path := range []string{"/api/v1/batches/self-check", "/api/v1/batches/self-check/audit?limit=100", "/api/v1/batches/self-check/manifest", "/api/v1/batches/self-check/evidence"} {
		if _, err = get(client, base+path, http.StatusOK); err != nil {
			return err
		}
	}
	immutable := map[string]any{"request_id": "self-after-release", "actor": "editor-self", "expected_revision": 6, "segments": []any{}}
	if _, err = postJSON(client, base+"/api/v1/batches/self-check/annotations", immutable, http.StatusUnprocessableEntity); err != nil {
		return fmt.Errorf("只读封存校验失败: %w", err)
	}
	fmt.Println("自检通过：建档、授权、标注、脱敏预览、复核、发布、证据下载和只读封存均已通过真实 HTTP 请求验证")
	return nil
}

func waitReady(client *http.Client, base string) error {
	var last error
	for i := 0; i < 40; i++ {
		_, last = get(client, base+"/", http.StatusOK)
		if last == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("HTTP 服务未就绪: %w", last)
}
func postJSON(client *http.Client, url string, value any, want int) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(client, req, want)
}
func get(client *http.Client, url string, want int) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return do(client, req, want)
}
func do(client *http.Client, req *http.Request, want int) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != want {
		return nil, fmt.Errorf("%s %s 返回 %d，期望 %d: %s", req.Method, req.URL.Path, resp.StatusCode, want, body)
	}
	return body, nil
}
