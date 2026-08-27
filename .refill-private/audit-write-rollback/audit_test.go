package auditwriterollback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"field-voice-archive/internal/audit"
	"field-voice-archive/internal/store"
	"field-voice-archive/internal/workflow"
)

func TestAuditWriteFailureDoesNotPersistSnapshot(t *testing.T) {
	dir := t.TempDir()
	repo, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	logbook, err := audit.Open(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	svc := workflow.New(repo, logbook)
	_, err = svc.Create(workflow.CreateBatchCommand{
		Meta: workflow.Meta{RequestID: "create", Actor: "collector"}, BatchID: "batch-1", Title: "录音",
		LanguageVariant: "变体", CollectionSite: "地点", CollectedAt: "2026-08-20", MediaSHA256: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(dir, "audit", "events.jsonl")
	if err := os.Remove(eventsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(eventsPath, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddConsent("batch-1", workflow.AddConsentCommand{
		Meta: workflow.Meta{RequestID: "consent", Actor: "collector", ExpectedRevision: 1}, ConsentID: "consent-1", ParticipantCode: "speaker-1",
		Scope: []string{"open_archive"}, SignedAt: time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC), WithdrawalRule: "发布前撤回",
		EvidenceDigest: strings.Repeat("b", 64), VerifiedBy: "verifier",
	})
	if err == nil {
		t.Fatal("审计写入应失败")
	}
	b, loadErr := repo.LoadBatch("batch-1")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if b.Revision != 1 || len(b.Consents) != 0 {
		t.Fatalf("审计失败后快照不应提交: revision=%d consents=%d", b.Revision, len(b.Consents))
	}
}
