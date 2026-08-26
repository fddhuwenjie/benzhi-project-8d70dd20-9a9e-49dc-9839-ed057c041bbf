package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusConsented Status = "consented"
	StatusAnnotated Status = "annotated"
	StatusRedacted  Status = "redacted"
	StatusRejected  Status = "rejected"
	StatusApproved  Status = "approved"
	StatusReleased  Status = "released"
)

var (
	ErrReleased         = errors.New("批次已经发布，只允许读取")
	ErrRevisionConflict = errors.New("批次修订号冲突")
	shaPattern          = regexp.MustCompile(`^[a-f0-9]{64}$`)
	codePattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	phonePattern        = regexp.MustCompile(`(?:\+?\d[\d\s-]{6,}\d)`)
	emailPattern        = regexp.MustCompile(`[^\s@]+@[^\s@]+\.[^\s@]+`)
)

type RecordingBatch struct {
	BatchID                 string              `json:"batch_id"`
	Title                   string              `json:"title"`
	LanguageVariant         string              `json:"language_variant"`
	CollectionSite          string              `json:"collection_site"`
	CollectedAt             time.Time           `json:"collected_at"`
	MediaSHA256             string              `json:"media_sha256"`
	Status                  Status              `json:"status"`
	Revision                int64               `json:"revision"`
	CreatedBy               string              `json:"created_by"`
	PublishedManifestDigest string              `json:"published_manifest_digest,omitempty"`
	Consents                []ConsentRecord     `json:"consents"`
	Segments                []TranscriptSegment `json:"segments"`
	Reviews                 []ReviewDecision    `json:"reviews"`
	LastEditor              string              `json:"last_editor,omitempty"`
	CreatedAt               time.Time           `json:"created_at"`
	UpdatedAt               time.Time           `json:"updated_at"`
	ConsentCoverage         *CoverageSummary    `json:"consent_coverage,omitempty"`
	SegmentStatistics       *SegmentStatistics  `json:"segment_statistics,omitempty"`
	PendingRequiredChanges  []string            `json:"pending_required_changes,omitempty"`
	ChangeItems             []ChangeItem        `json:"change_items,omitempty"`
}

// BatchBaseline 是建档预检返回的规范化基线。它与正式建档共用校验规则，
// 但不包含修订号或任何持久化状态。
type BatchBaseline struct {
	BatchID         string `json:"batch_id"`
	Title           string `json:"title"`
	LanguageVariant string `json:"language_variant"`
	CollectionSite  string `json:"collection_site"`
	CollectedAt     string `json:"collected_at"`
	MediaSHA256     string `json:"media_sha256"`
	CreatedBy       string `json:"created_by"`
}

type CoverageSummary struct {
	Total       int `json:"total"`
	OpenArchive int `json:"open_archive"`
	Valid       int `json:"valid"`
	Withdrawn   int `json:"withdrawn"`
	Expiring    int `json:"expiring"`
}

type SegmentStatistics struct {
	Count              int                          `json:"count"`
	TotalDurationMS    int64                        `json:"total_duration_ms"`
	UntranscribedChars int                          `json:"untranscribed_chars"`
	ReleasedChars      int                          `json:"released_chars"`
	GapCount           int                          `json:"gap_count"`
	BySpeaker          map[string]SpeakerStatistics `json:"by_speaker"`
}
type SpeakerStatistics struct {
	SegmentCount       int   `json:"segment_count"`
	TotalDurationMS    int64 `json:"total_duration_ms"`
	UntranscribedChars int   `json:"untranscribed_chars"`
}

type ConsentRecord struct {
	ConsentID          string     `json:"consent_id"`
	BatchID            string     `json:"batch_id"`
	ParticipantCode    string     `json:"participant_code"`
	Scope              []string   `json:"scope"`
	SignedAt           time.Time  `json:"signed_at"`
	WithdrawalRule     string     `json:"withdrawal_rule"`
	EvidenceDigest     string     `json:"evidence_digest"`
	VerifiedBy         string     `json:"verified_by"`
	VerifiedAt         time.Time  `json:"verified_at"`
	WithdrawalDeadline *time.Time `json:"withdrawal_deadline,omitempty"`
	Withdrawn          bool       `json:"withdrawn,omitempty"`
}

type RedactionMark struct {
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Kind        string `json:"kind"`
	Replacement string `json:"replacement"`
}

type TranscriptSegment struct {
	SegmentID      string          `json:"segment_id"`
	BatchID        string          `json:"batch_id"`
	StartMS        int64           `json:"start_ms"`
	EndMS          int64           `json:"end_ms"`
	SpeakerCode    string          `json:"speaker_code"`
	RawText        string          `json:"raw_text"`
	RedactionMarks []RedactionMark `json:"redaction_marks"`
	ReleasedText   string          `json:"released_text,omitempty"`
	AnnotatedBy    string          `json:"annotated_by"`
	Revision       int64           `json:"revision"`
}

type ReviewDecision struct {
	ReviewID         string    `json:"review_id"`
	BatchID          string    `json:"batch_id"`
	Reviewer         string    `json:"reviewer"`
	Decision         string    `json:"decision"`
	Findings         string    `json:"findings"`
	RequiredChanges  []string  `json:"required_changes"`
	SignedAt         time.Time `json:"signed_at"`
	ReviewedRevision int64     `json:"reviewed_revision"`
}

type ChangeItem struct {
	ID               string `json:"id"`
	Text             string `json:"text"`
	ReviewedRevision int64  `json:"reviewed_revision"`
	Closed           bool   `json:"closed"`
	ClosedRevision   int64  `json:"closed_revision,omitempty"`
}

type Manifest struct {
	BatchID        string            `json:"batch_id"`
	Revision       int64             `json:"revision"`
	MediaSHA256    string            `json:"media_sha256"`
	ConsentDigests []string          `json:"consent_digests"`
	Segments       []ReleasedSegment `json:"segments"`
	Review         ReviewDecision    `json:"review"`
	AuditHead      string            `json:"audit_head"`
	IssuedBy       string            `json:"issued_by"`
}

type ReleasedSegment struct {
	SegmentID   string `json:"segment_id"`
	StartMS     int64  `json:"start_ms"`
	EndMS       int64  `json:"end_ms"`
	SpeakerCode string `json:"speaker_code"`
	Text        string `json:"text"`
}

func NewBatch(id, title, variant, site, collected, mediaDigest, actor string, now time.Time) (*RecordingBatch, error) {
	baseline, fieldErrors := ValidateBatchBaseline(id, title, variant, site, collected, mediaDigest, actor, now)
	if len(fieldErrors) != 0 {
		keys := make([]string, 0, len(fieldErrors))
		for key := range fieldErrors {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("%s: %s", keys[0], fieldErrors[keys[0]])
	}
	t, _ := time.Parse("2006-01-02", baseline.CollectedAt)
	now = now.UTC()
	return &RecordingBatch{BatchID: baseline.BatchID, Title: baseline.Title, LanguageVariant: baseline.LanguageVariant, CollectionSite: baseline.CollectionSite, CollectedAt: t.UTC(), MediaSHA256: baseline.MediaSHA256, Status: StatusDraft, Revision: 1, CreatedBy: baseline.CreatedBy, CreatedAt: now, UpdatedAt: now}, nil
}

// ValidateBatchBaseline 逐字段校验并返回可直接用于正式建档的规范化数据。
func ValidateBatchBaseline(id, title, variant, site, collected, mediaDigest, actor string, now time.Time) (BatchBaseline, map[string]string) {
	baseline := BatchBaseline{
		BatchID:         strings.TrimSpace(id),
		Title:           strings.TrimSpace(title),
		LanguageVariant: strings.TrimSpace(variant),
		CollectionSite:  strings.TrimSpace(site),
		CollectedAt:     strings.TrimSpace(collected),
		MediaSHA256:     strings.ToLower(strings.TrimSpace(mediaDigest)),
		CreatedBy:       strings.TrimSpace(actor),
	}
	fieldErrors := map[string]string{}
	if !codePattern.MatchString(baseline.BatchID) {
		fieldErrors["batch_id"] = "格式无效，仅允许 1 到 64 位字母、数字、点、下划线或连字符"
	}
	if baseline.Title == "" {
		fieldErrors["title"] = "不能为空"
	}
	if baseline.LanguageVariant == "" {
		fieldErrors["language_variant"] = "不能为空"
	}
	if baseline.CollectionSite == "" {
		fieldErrors["collection_site"] = "不能为空"
	}
	if baseline.CreatedBy == "" {
		fieldErrors["actor"] = "不能为空"
	}
	if t, err := time.Parse("2006-01-02", baseline.CollectedAt); err != nil || t.After(now.UTC()) {
		fieldErrors["collected_at"] = "必须是有效且非未来的 YYYY-MM-DD 日期"
	}
	if _, err := NormalizeDigest(baseline.MediaSHA256); err != nil {
		fieldErrors["media_sha256"] = err.Error()
	}
	return baseline, fieldErrors
}

func NormalizeDigest(value string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if !shaPattern.MatchString(v) {
		return "", errors.New("摘要必须是 64 位小写十六进制 SHA-256")
	}
	return v, nil
}

func (b *RecordingBatch) EnsureWritable() error {
	if b.Status == StatusReleased {
		return ErrReleased
	}
	return nil
}

func (b *RecordingBatch) CheckRevision(expected int64) error {
	if expected != b.Revision {
		return fmt.Errorf("%w: 当前为 %d，提交为 %d", ErrRevisionConflict, b.Revision, expected)
	}
	return nil
}
