package workflow

import (
	"strings"
	"testing"
	"time"

	"field-voice-archive/internal/audit"
	"field-voice-archive/internal/domain"
	"field-voice-archive/internal/store"
)

func testService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	repo, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	logbook, err := audit.Open(dir + "/audit")
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, logbook)
	service.now = func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) }
	return service
}

func createCommand(request string) CreateBatchCommand {
	return CreateBatchCommand{Meta: Meta{RequestID: request, Actor: "collector"}, BatchID: "batch-1", Title: "录音", LanguageVariant: "变体", CollectionSite: "地点", CollectedAt: "2026-08-20", MediaSHA256: strings.Repeat("a", 64)}
}

func TestIdempotentReplayAndFingerprintConflict(t *testing.T) {
	s := testService(t)
	cmd := createCommand("req-1")
	first, err := s.Create(cmd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !second.IdempotentReplay || second.Batch.Revision != first.Batch.Revision {
		t.Fatal("相同命令未幂等复用")
	}
	cmd.Title = "不同录音"
	if _, err = s.Create(cmd); err == nil {
		t.Fatal("相同 request_id 的不同载荷应冲突")
	}
}

func TestRevisionConflictDoesNotWriteAudit(t *testing.T) {
	s := testService(t)
	if _, err := s.Create(createCommand("create")); err != nil {
		t.Fatal(err)
	}
	_, err := s.AddConsent("batch-1", AddConsentCommand{Meta: Meta{RequestID: "consent", Actor: "collector", ExpectedRevision: 99}, ConsentID: "c", ParticipantCode: "speaker", Scope: []string{"open_archive"}, SignedAt: time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC), WithdrawalRule: "发布前撤回", EvidenceDigest: strings.Repeat("b", 64), VerifiedBy: "verifier"})
	if err == nil {
		t.Fatal("过期 revision 应冲突")
	}
	events, total := s.Audit().List("batch-1", 0, 10)
	if total != 1 || len(events) != 1 {
		t.Fatalf("失败命令不应写审计: %d", total)
	}
}

func TestEndToEndReleaseAndReadonly(t *testing.T) {
	s := testService(t)
	if _, err := s.Create(createCommand("create")); err != nil {
		t.Fatal(err)
	}
	signed := time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC)
	if _, err := s.AddConsent("batch-1", AddConsentCommand{Meta: Meta{RequestID: "consent", Actor: "collector", ExpectedRevision: 1}, ConsentID: "c", ParticipantCode: "speaker", Scope: []string{"open_archive"}, SignedAt: signed, WithdrawalRule: "发布前撤回", EvidenceDigest: strings.Repeat("b", 64), VerifiedBy: "verifier"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Annotate("batch-1", AnnotateCommand{Meta: Meta{RequestID: "annotate", Actor: "editor", ExpectedRevision: 2}, Segments: []domain.TranscriptSegment{{SegmentID: "seg", StartMS: 0, EndMS: 10, SpeakerCode: "speaker", RawText: "阿岚"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redact("batch-1", RedactCommand{Meta: Meta{RequestID: "redact", Actor: "editor", ExpectedRevision: 3}, Marks: map[string][]domain.RedactionMark{"seg": {{Start: 0, End: 2, Kind: "person"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Review("batch-1", ReviewCommand{Meta: Meta{RequestID: "review", Actor: "reviewer", ExpectedRevision: 4}, ReviewID: "review", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	released, err := s.Release("batch-1", ReleaseCommand{Meta: Meta{RequestID: "release", Actor: "archivist", ExpectedRevision: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if released.Batch.Status != domain.StatusReleased || released.Manifest == nil {
		t.Fatal("未生成发布清单")
	}
	if _, err := s.Annotate("batch-1", AnnotateCommand{Meta: Meta{RequestID: "late", Actor: "editor", ExpectedRevision: 6}}); err == nil {
		t.Fatal("发布后应拒绝写入")
	}
}
