package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"field-voice-archive/internal/domain"
)

func TestSaveLoadAndRequest(t *testing.T) {
	repo, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.NewBatch("batch", "title", "variant", "site", "2026-08-20", strings.Repeat("a", 64), "actor", time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveBatch(b); err != nil {
		t.Fatal(err)
	}
	got, err := repo.LoadBatch("batch")
	if err != nil || got.Revision != 1 {
		t.Fatalf("加载失败 %v %#v", err, got)
	}
	rec := IdempotencyRecord{RequestID: "req", Fingerprint: "fp", StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}
	if err = repo.SaveRequest(rec); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.LoadRequest("req")
	if err != nil || loaded.Fingerprint != "fp" {
		t.Fatal("幂等记录加载失败")
	}
}

func TestOpenQuarantinesCorruptSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "batches"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "batches", "broken.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "quarantine", "broken.json.corrupt")); err != nil {
		t.Fatalf("损坏记录未隔离: %v", err)
	}
}
