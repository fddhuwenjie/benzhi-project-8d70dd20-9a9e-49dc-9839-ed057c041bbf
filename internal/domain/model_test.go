package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func baseBatch(t *testing.T) *RecordingBatch {
	t.Helper()
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	b, err := NewBatch("batch-1", "田野录音", "北部变体", "河谷村", "2026-08-20", strings.Repeat("a", 64), "collector", now)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func consentBatch(t *testing.T) *RecordingBatch {
	b := baseBatch(t)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	err := b.AddConsent(ConsentRecord{ConsentID: "consent-1", ParticipantCode: "speaker-1", Scope: []string{"open_archive"}, SignedAt: now.Add(-time.Hour), WithdrawalRule: "发布前撤回", EvidenceDigest: strings.Repeat("b", 64), VerifiedBy: "verifier", VerifiedAt: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func redactedBatch(t *testing.T) *RecordingBatch {
	b := consentBatch(t)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	if err := b.ReplaceSegments([]TranscriptSegment{{SegmentID: "seg-1", StartMS: 0, EndMS: 1000, SpeakerCode: "speaker-1", RawText: "阿岚在青石村"}}, "editor", now); err != nil {
		t.Fatal(err)
	}
	if err := b.ApplyRedactions(map[string][]RedactionMark{"seg-1": {{Start: 0, End: 2, Kind: "person"}, {Start: 3, End: 6, Kind: "place"}}}, "editor", now); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNewBatchValidation(t *testing.T) {
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	if _, err := NewBatch("bad/id", "x", "x", "x", "2026-08-20", strings.Repeat("a", 64), "actor", now); err == nil {
		t.Fatal("应拒绝无效编号")
	}
	if _, err := NewBatch("ok", "x", "x", "x", "2026-08-20", "no", "actor", now); err == nil {
		t.Fatal("应拒绝摘要")
	}
}

func TestSegmentsRejectOverlap(t *testing.T) {
	b := consentBatch(t)
	err := b.ReplaceSegments([]TranscriptSegment{{SegmentID: "a", StartMS: 0, EndMS: 20, SpeakerCode: "s", RawText: "a"}, {SegmentID: "b", StartMS: 10, EndMS: 30, SpeakerCode: "s", RawText: "b"}}, "editor", time.Now())
	if err == nil {
		t.Fatal("应拒绝重叠时间码")
	}
}

func TestSegmentsRequireVerifiedSpeaker(t *testing.T) {
	b := consentBatch(t)
	err := b.ReplaceSegments([]TranscriptSegment{{SegmentID: "a", StartMS: 0, EndMS: 20, SpeakerCode: "unknown", RawText: "a"}}, "editor", time.Now())
	if err == nil {
		t.Fatal("应拒绝没有授权记录的说话人")
	}
}

func TestRedactUnicodeAndDefaults(t *testing.T) {
	text, marks, err := Redact("我叫阿岚，住青石村", []RedactionMark{{Start: 2, End: 4, Kind: "person"}, {Start: 6, End: 9, Kind: "place"}})
	if err != nil {
		t.Fatal(err)
	}
	if text != "我叫[PERSON-01]，住[PLACE-02]" {
		t.Fatalf("意外预览 %q", text)
	}
	if marks[0].Replacement == "" {
		t.Fatal("未生成令牌")
	}
}

func TestReviewSeparationAndRevision(t *testing.T) {
	b := redactedBatch(t)
	err := b.Review(ReviewDecision{ReviewID: "r-1", Reviewer: "editor", Decision: "approved", ReviewedRevision: b.Revision}, time.Now())
	if err == nil {
		t.Fatal("编辑人不应复核")
	}
	err = b.Review(ReviewDecision{ReviewID: "r-1", Reviewer: "reviewer", Decision: "approved", ReviewedRevision: b.Revision - 1}, time.Now())
	if err == nil {
		t.Fatal("应拒绝过期复核")
	}
}

func TestReleaseMakesBatchImmutable(t *testing.T) {
	b := redactedBatch(t)
	now := time.Now()
	if err := b.Review(ReviewDecision{ReviewID: "r-1", Reviewer: "reviewer", Decision: "approved", ReviewedRevision: b.Revision}, now); err != nil {
		t.Fatal(err)
	}
	_, digest, err := b.BuildManifest("archivist", "head")
	if err != nil {
		t.Fatal(err)
	}
	b.Release(digest, now)
	if err := b.EnsureWritable(); !errors.Is(err, ErrReleased) {
		t.Fatalf("期望 ErrReleased，得到 %v", err)
	}
}

func TestPublicCopyHidesReversibleDataAfterRelease(t *testing.T) {
	b := redactedBatch(t)
	now := time.Now()
	if err := b.Review(ReviewDecision{ReviewID: "r-1", Reviewer: "reviewer", Decision: "approved", ReviewedRevision: b.Revision}, now); err != nil {
		t.Fatal(err)
	}
	_, digest, err := b.BuildManifest("archivist", "head")
	if err != nil {
		t.Fatal(err)
	}
	b.Release(digest, now)
	public := b.PublicCopy()
	if public.Segments[0].RawText != "" || len(public.Segments[0].RedactionMarks) != 0 || public.Consents[0].WithdrawalRule != "" {
		t.Fatal("开放副本泄露可逆信息")
	}
	if public.Segments[0].ReleasedText == "" {
		t.Fatal("开放副本缺少脱敏文本")
	}
}
