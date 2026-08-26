package server

import (
	"encoding/json"
	"errors"
	"field-voice-archive/internal/audit"
	"field-voice-archive/internal/domain"
	"field-voice-archive/internal/workflow"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *API) HandleGetBatch(w http.ResponseWriter, r *http.Request) {
	b, err := a.workflow.Get(r.PathValue("batchID"))
	if err != nil {
		writeError(w, err)
		return
	}
	cov := b.Coverage(time.Now())
	stats := b.SegmentStats()
	copy := b.PublicCopy()
	copy.ConsentCoverage, copy.SegmentStatistics = &cov, &stats
	copy.PendingRequiredChanges = copy.PendingChanges()
	writeJSON(w, http.StatusOK, copy)
}

func (a *API) HandleUpdateBatch(w http.ResponseWriter, r *http.Request) {
	var cmd workflow.UpdateBatchCommand
	if !decode(w, r, &cmd) {
		return
	}
	result, err := a.workflow.UpdateMetadata(r.PathValue("batchID"), cmd)
	respondResult(w, result, err)
}

func (a *API) HandleCreateBatch(w http.ResponseWriter, r *http.Request) {
	var cmd workflow.CreateBatchCommand
	if !decode(w, r, &cmd) {
		return
	}
	result, err := a.workflow.Create(cmd)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if cmd.Preflight {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}
func (a *API) HandleAddConsent(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		workflow.AddConsentCommand
		Records          []workflow.AddConsentCommand `json:"records"`
		Withdrawn        bool                         `json:"withdrawn,omitempty"`
		WithdrawalReason string                       `json:"withdrawal_reason,omitempty"`
		Reason           string                       `json:"reason,omitempty"`
	}
	if !decode(w, r, &raw) {
		return
	}
	if len(raw.Records) > 0 {
		cmd := workflow.ConsentBatchCommand{Meta: raw.Meta, Records: raw.Records}
		result, err := a.workflow.AddConsents(r.PathValue("batchID"), cmd)
		respondResult(w, result, err)
		return
	}
	if raw.Withdrawn || raw.WithdrawalReason != "" || raw.Reason != "" {
		reason := raw.WithdrawalReason
		if reason == "" {
			reason = raw.Reason
		}
		cmd := workflow.WithdrawConsentCommand{Meta: raw.Meta, ParticipantCode: raw.ParticipantCode, ConsentID: raw.ConsentID, Reason: reason, WithdrawalReason: reason}
		result, err := a.workflow.WithdrawConsent(r.PathValue("batchID"), cmd)
		respondResult(w, result, err)
		return
	}
	cmd := raw.AddConsentCommand
	result, err := a.workflow.AddConsent(r.PathValue("batchID"), cmd)
	respondResult(w, result, err)
}
func (a *API) HandleWithdrawConsent(w http.ResponseWriter, r *http.Request) {
	var cmd workflow.WithdrawConsentCommand
	if !decode(w, r, &cmd) {
		return
	}
	result, err := a.workflow.WithdrawConsent(r.PathValue("batchID"), cmd)
	respondResult(w, result, err)
}

func (a *API) HandleVerifyPublished(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BatchIDs []string `json:"batch_ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.BatchIDs) == 0 || len(body.BatchIDs) > 50 {
		writeError(w, errors.New("batch_ids 数量必须为 1 到 50"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": a.workflow.VerifyPublished(body.BatchIDs)})
}
func (a *API) HandleVerifyPublishedGet(w http.ResponseWriter, r *http.Request) {
	ids := r.URL.Query()["batch_id"]
	if len(ids) == 0 && r.URL.Query().Get("batch_ids") != "" {
		ids = strings.Split(r.URL.Query().Get("batch_ids"), ",")
	}
	if len(ids) == 0 || len(ids) > 50 {
		writeError(w, errors.New("batch_ids 数量必须为 1 到 50"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": a.workflow.VerifyPublished(ids)})
}
func (a *API) HandleAnnotate(w http.ResponseWriter, r *http.Request) {
	var cmd workflow.AnnotateCommand
	if !decode(w, r, &cmd) {
		return
	}
	result, err := a.workflow.Annotate(r.PathValue("batchID"), cmd)
	respondResult(w, result, err)
}
func (a *API) HandleRedact(w http.ResponseWriter, r *http.Request) {
	var cmd workflow.RedactCommand
	if !decode(w, r, &cmd) {
		return
	}
	result, err := a.workflow.Redact(r.PathValue("batchID"), cmd)
	respondResult(w, result, err)
}
func (a *API) HandleReview(w http.ResponseWriter, r *http.Request) {
	var cmd workflow.ReviewCommand
	if !decode(w, r, &cmd) {
		return
	}
	result, err := a.workflow.Review(r.PathValue("batchID"), cmd)
	respondResult(w, result, err)
}
func (a *API) HandleRelease(w http.ResponseWriter, r *http.Request) {
	var cmd workflow.ReleaseCommand
	if !decode(w, r, &cmd) {
		return
	}
	if !cmd.Preflight && (strings.TrimSpace(cmd.LockToken) == "" || strings.TrimSpace(cmd.ExpectedManifestDigest) == "") {
		writeError(w, errors.New("正式签发必须提供 lock_token 和 expected_manifest_digest"))
		return
	}
	result, err := a.workflow.Release(r.PathValue("batchID"), cmd)
	respondResult(w, result, err)
}

func (a *API) HandleRedactionPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Marks map[string][]domain.RedactionMark `json:"marks"`
	}
	if !decode(w, r, &body) {
		return
	}
	preview, err := a.workflow.PreviewDetailed(r.PathValue("batchID"), body.Marks)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (a *API) HandleAudit(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit > 100 {
		limit = 100
	}
	from, _ := strconv.ParseInt(r.URL.Query().Get("from_revision"), 10, 64)
	to, _ := strconv.ParseInt(r.URL.Query().Get("to_revision"), 10, 64)
	if err := a.workflow.Audit().Verify(); err != nil {
		writeError(w, err)
		return
	}
	events, total, err := a.workflow.Audit().Query(r.PathValue("batchID"), audit.Query{Action: r.URL.Query().Get("action"), Actor: r.URL.Query().Get("actor"), FromRevision: from, ToRevision: to, Offset: offset, Limit: limit})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "total": total, "offset": offset, "head_digest": a.workflow.Audit().Head()})
}

func (a *API) HandleEvidence(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("batchID")
	if _, err := a.workflow.Get(id); err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-evidence.json"`, safeFilename(id)))
	evidence, err := a.workflow.Evidence(id)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := writeEvidence(w, evidence); err != nil {
		writeError(w, err)
	}
}

func (a *API) HandleManifest(w http.ResponseWriter, r *http.Request) {
	m, err := a.workflow.Repository().LoadManifest(r.PathValue("batchID"))
	if err != nil {
		writeError(w, err)
		return
	}
	if m == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "批次尚未发布清单"})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func respondResult(w http.ResponseWriter, result workflow.Result, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	if result.Batch != nil && result.Batch.Status == domain.StatusReleased {
		result.Batch = result.Batch.PublicCopy()
	}
	writeJSON(w, http.StatusOK, result)
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 请求无效: " + err.Error()})
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求只能包含一个 JSON 对象"})
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	if errors.Is(err, domain.ErrRevisionConflict) || strings.Contains(err.Error(), "request_id") {
		status = http.StatusConflict
	}
	if strings.Contains(err.Error(), "不存在") {
		status = http.StatusNotFound
	}
	if strings.Contains(err.Error(), "参数") || strings.Contains(err.Error(), "日期范围") || strings.Contains(err.Error(), "page_size") {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func writeEvidence(w http.ResponseWriter, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func safeFilename(id string) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
	if name == "" {
		return "batch"
	}
	return name
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
