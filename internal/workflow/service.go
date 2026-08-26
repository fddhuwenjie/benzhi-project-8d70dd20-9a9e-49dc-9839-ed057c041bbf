package workflow

import (
	"field-voice-archive/internal/audit"
	"field-voice-archive/internal/domain"
	"field-voice-archive/internal/store"
	"sync"
	"time"
)

type Service struct {
	repo         *store.Repository
	audit        *audit.Log
	now          func() time.Time
	createMu     sync.Mutex
	locks        sync.Map
	requestLocks sync.Map
	preflightMu  sync.Mutex
	preflights   map[string]releaseLock
	listCacheMu  sync.Mutex
	listCache    map[string]ListResult
}
type releaseLock struct {
	BatchID   string
	Revision  int64
	Digest    string
	AuditHead string
	Actor     string
	ExpiresAt time.Time
	Consumed  bool
}

type Meta struct {
	RequestID        string `json:"request_id"`
	Actor            string `json:"actor"`
	ExpectedRevision int64  `json:"expected_revision,omitempty"`
}

type CreateBatchCommand struct {
	Meta
	Preflight       bool   `json:"preflight,omitempty"`
	BatchID         string `json:"batch_id"`
	Title           string `json:"title"`
	LanguageVariant string `json:"language_variant"`
	CollectionSite  string `json:"collection_site"`
	CollectedAt     string `json:"collected_at"`
	MediaSHA256     string `json:"media_sha256"`
}

type BatchConflict struct {
	BatchID string        `json:"batch_id"`
	Status  domain.Status `json:"status"`
}

type BatchPreflightReport struct {
	Valid       bool                 `json:"valid"`
	Normalized  domain.BatchBaseline `json:"normalized"`
	FieldErrors map[string]string    `json:"field_errors"`
	Conflict    *BatchConflict       `json:"conflict,omitempty"`
}
type UpdateBatchCommand struct {
	Meta
	Title           string `json:"title,omitempty"`
	LanguageVariant string `json:"language_variant,omitempty"`
	CollectionSite  string `json:"collection_site,omitempty"`
	CollectedAt     string `json:"collected_at,omitempty"`
	MediaSHA256     string `json:"media_sha256,omitempty"`
}

type AddConsentCommand struct {
	Meta
	ConsentID       string    `json:"consent_id"`
	ParticipantCode string    `json:"participant_code"`
	Scope           []string  `json:"scope"`
	SignedAt        time.Time `json:"signed_at"`
	WithdrawalRule  string    `json:"withdrawal_rule"`
	EvidenceDigest  string    `json:"evidence_digest"`
	VerifiedBy      string    `json:"verified_by"`
}
type WithdrawConsentCommand struct {
	Meta
	ParticipantCode  string `json:"participant_code"`
	ConsentID        string `json:"consent_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
	WithdrawalReason string `json:"withdrawal_reason,omitempty"`
}
type ConsentBatchCommand struct {
	Meta
	Records []AddConsentCommand `json:"records"`
}

type AnnotateCommand struct {
	Meta
	Segments          []domain.TranscriptSegment `json:"segments"`
	DryRun            bool                       `json:"dry_run,omitempty"`
	ResolvedChangeIDs []string                   `json:"resolved_change_ids,omitempty"`
}
type RedactCommand struct {
	Meta
	Marks             map[string][]domain.RedactionMark `json:"marks"`
	ConfirmNoPII      bool                              `json:"confirm_no_pii,omitempty"`
	NoPIIReason       string                            `json:"no_pii_reason,omitempty"`
	ResolvedChangeIDs []string                          `json:"resolved_change_ids,omitempty"`
}
type ReviewCommand struct {
	Meta
	ReviewID        string   `json:"review_id"`
	Decision        string   `json:"decision"`
	Findings        string   `json:"findings"`
	RequiredChanges []string `json:"required_changes"`
}
type ReleaseCommand struct {
	Meta
	Preflight              bool   `json:"preflight,omitempty"`
	ExpectedManifestDigest string `json:"expected_manifest_digest,omitempty"`
	LockToken              string `json:"lock_token,omitempty"`
}

type ListFilter struct {
	Status, LanguageVariant, CollectionSite, CollectedFrom, CollectedTo, Query string
	ReleasedOnly                                                               bool
	Page, PageSize                                                             int
}

type ListResult struct {
	Batches      []domain.RecordingBatch `json:"batches"`
	Total        int                     `json:"total"`
	StatusCounts map[domain.Status]int   `json:"status_counts"`
	NextPage     int                     `json:"next_page,omitempty"`
}
type VerificationResult struct {
	BatchID string `json:"batch_id"`
	Passed  bool   `json:"passed"`
	Reason  string `json:"reason,omitempty"`
}

type Result struct {
	Action           string                 `json:"action"`
	Batch            *domain.RecordingBatch `json:"batch"`
	Manifest         *domain.Manifest       `json:"manifest,omitempty"`
	IdempotentReplay bool                   `json:"idempotent_replay"`
	Report           any                    `json:"report,omitempty"`
}

func New(repo *store.Repository, log *audit.Log) *Service {
	return &Service{repo: repo, audit: log, now: time.Now, preflights: map[string]releaseLock{}, listCache: map[string]ListResult{}}
}

func (s *Service) Repository() *store.Repository { return s.repo }
func (s *Service) Audit() *audit.Log             { return s.audit }
