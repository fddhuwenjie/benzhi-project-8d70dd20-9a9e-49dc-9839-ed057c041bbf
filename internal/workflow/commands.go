package workflow

import (
	"crypto/sha256"
	"errors"
	"field-voice-archive/internal/domain"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Service) Create(cmd CreateBatchCommand) (Result, error) {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if cmd.Preflight {
		baseline, fieldErrors := domain.ValidateBatchBaseline(cmd.BatchID, cmd.Title, cmd.LanguageVariant, cmd.CollectionSite, cmd.CollectedAt, cmd.MediaSHA256, cmd.Actor, s.now())
		report := BatchPreflightReport{Valid: len(fieldErrors) == 0, Normalized: baseline, FieldErrors: fieldErrors}
		if _, invalidID := fieldErrors["batch_id"]; !invalidID {
			if exists, err := s.repo.BatchExists(baseline.BatchID); err != nil {
				return Result{}, err
			} else if exists {
				fieldErrors["batch_id"] = "编号已存在"
				report.Valid = false
			}
		}
		if _, invalidDigest := fieldErrors["media_sha256"]; !invalidDigest {
			if old, err := s.repo.FindBatchByMediaSHA256(baseline.MediaSHA256); err != nil {
				return Result{}, err
			} else if old != nil && old.BatchID != baseline.BatchID {
				report.Conflict = &BatchConflict{BatchID: old.BatchID, Status: old.Status}
				report.Valid = false
			}
		}
		return Result{Action: "create_preflight", Report: report}, nil
	}
	return s.execute(cmd.BatchID, "create", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		exists, err := s.repo.BatchExists(cmd.BatchID)
		if err != nil {
			return nil, nil, err
		}
		if exists {
			return nil, nil, errors.New("batch_id 已存在")
		}
		if old, err := s.repo.FindBatchByMediaSHA256(cmd.MediaSHA256); err != nil {
			return nil, nil, err
		} else if old != nil && old.BatchID != cmd.BatchID {
			return nil, nil, fmt.Errorf("媒体摘要冲突：已属于批次 %s", old.BatchID)
		}
		b, err := domain.NewBatch(cmd.BatchID, cmd.Title, cmd.LanguageVariant, cmd.CollectionSite, cmd.CollectedAt, cmd.MediaSHA256, cmd.Actor, s.now())
		return b, nil, err
	})
}

func (s *Service) AddConsent(batchID string, cmd AddConsentCommand) (Result, error) {
	return s.execute(batchID, "consent", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		b, err := s.loadForWrite(batchID, cmd.Meta)
		if err != nil {
			return nil, nil, err
		}
		c := domain.ConsentRecord{ConsentID: cmd.ConsentID, ParticipantCode: cmd.ParticipantCode, Scope: cmd.Scope, SignedAt: cmd.SignedAt, WithdrawalRule: cmd.WithdrawalRule, EvidenceDigest: cmd.EvidenceDigest, VerifiedBy: cmd.VerifiedBy, VerifiedAt: s.now().UTC()}
		if err = b.AddConsent(c, s.now()); err != nil {
			return nil, nil, err
		}
		return b, nil, nil
	})
}

func (s *Service) AddConsents(batchID string, cmd ConsentBatchCommand) (Result, error) {
	return s.execute(batchID, "consent", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		b, err := s.loadForWrite(batchID, cmd.Meta)
		if err != nil {
			return nil, nil, err
		}
		recs := make([]domain.ConsentRecord, len(cmd.Records))
		for i, r := range cmd.Records {
			recs[i] = domain.ConsentRecord{ConsentID: r.ConsentID, ParticipantCode: r.ParticipantCode, Scope: r.Scope, SignedAt: r.SignedAt, WithdrawalRule: r.WithdrawalRule, EvidenceDigest: r.EvidenceDigest, VerifiedBy: r.VerifiedBy, VerifiedAt: s.now().UTC()}
		}
		if err = b.AddConsents(recs, s.now()); err != nil {
			return nil, nil, err
		}
		return b, nil, nil
	})
}

func (s *Service) WithdrawConsent(batchID string, cmd WithdrawConsentCommand) (Result, error) {
	return s.execute(batchID, "consent_withdraw", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		b, err := s.loadForWrite(batchID, cmd.Meta)
		if err != nil {
			return nil, nil, err
		}
		id := cmd.ParticipantCode
		if id == "" {
			id = cmd.ConsentID
		}
		reason := cmd.Reason
		if reason == "" {
			reason = cmd.WithdrawalReason
		}
		if err = b.WithdrawConsent(id, reason, s.now()); err != nil {
			return nil, nil, err
		}
		return b, nil, nil
	})
}

func (s *Service) UpdateMetadata(batchID string, cmd UpdateBatchCommand) (Result, error) {
	b, err := s.repo.LoadBatch(batchID)
	if err != nil {
		return Result{}, err
	}
	if err = b.EnsureWritable(); err != nil {
		return Result{}, err
	}
	if err = b.CheckRevision(cmd.ExpectedRevision); err != nil {
		return Result{}, err
	}
	if audited := s.audit.BatchRevision(batchID); audited != b.Revision {
		return Result{}, errors.New("批次 revision 与审计记录不一致")
	}
	return Result{}, errors.New("批次基线字段已冻结，不能修改")
}

func (s *Service) Annotate(batchID string, cmd AnnotateCommand) (Result, error) {
	if cmd.DryRun {
		b, err := s.repo.LoadBatch(batchID)
		if err != nil {
			return Result{}, err
		}
		if cmd.ExpectedRevision > 0 {
			if err = b.CheckRevision(cmd.ExpectedRevision); err != nil {
				return Result{}, err
			}
		}
		segments := append([]domain.TranscriptSegment(nil), cmd.Segments...)
		sort.Slice(segments, func(i, j int) bool { return segments[i].StartMS < segments[j].StartMS })
		stats, _, err := b.ValidateSegmentsAt(segments, s.now())
		if err != nil {
			return Result{}, err
		}
		return Result{Action: "annotate_preflight", Batch: b, Report: map[string]any{"segments": segments, "statistics": stats, "revision": b.Revision}}, nil
	}
	return s.execute(batchID, "annotate", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		b, err := s.loadForWrite(batchID, cmd.Meta)
		if err != nil {
			return nil, nil, err
		}
		if err = b.ReplaceSegments(cmd.Segments, cmd.Actor, s.now()); err != nil {
			return nil, nil, err
		}
		if err = resolveChanges(b, cmd.ResolvedChangeIDs); err != nil {
			return nil, nil, err
		}
		return b, nil, nil
	})
}

func (s *Service) Redact(batchID string, cmd RedactCommand) (Result, error) {
	return s.execute(batchID, "redact", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		b, err := s.loadForWrite(batchID, cmd.Meta)
		if err != nil {
			return nil, nil, err
		}
		if err = b.ApplyRedactions(cmd.Marks, cmd.Actor, s.now()); err != nil {
			return nil, nil, err
		}
		if err = resolveChanges(b, cmd.ResolvedChangeIDs); err != nil {
			return nil, nil, err
		}
		if !cmd.ConfirmNoPII {
			if alerts := redactionAlerts(b); len(alerts) > 0 {
				return nil, nil, fmt.Errorf("存在未确认的个人信息告警: %s", alerts[0])
			}
		} else if strings.TrimSpace(cmd.NoPIIReason) == "" && len(redactionAlerts(b)) > 0 {
			return nil, nil, errors.New("确认误报时必须提供 no_pii_reason")
		}
		return b, nil, nil
	})
}

func (s *Service) Review(batchID string, cmd ReviewCommand) (Result, error) {
	return s.execute(batchID, "review", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		b, err := s.loadForWrite(batchID, cmd.Meta)
		if err != nil {
			return nil, nil, err
		}
		r := domain.ReviewDecision{ReviewID: cmd.ReviewID, Reviewer: cmd.Actor, Decision: cmd.Decision, Findings: cmd.Findings, RequiredChanges: cmd.RequiredChanges, ReviewedRevision: cmd.ExpectedRevision}
		if err = b.Review(r, s.now()); err != nil {
			return nil, nil, err
		}
		return b, nil, nil
	})
}

func (s *Service) Release(batchID string, cmd ReleaseCommand) (Result, error) {
	if cmd.Preflight {
		if batchID == "" || cmd.Actor == "" || cmd.RequestID == "" {
			return Result{}, errors.New("batch_id、actor 和 request_id 均不能为空")
		}
		b, err := s.repo.LoadBatch(batchID)
		if err != nil {
			return Result{}, err
		}
		if err = b.CheckRevision(cmd.ExpectedRevision); err != nil {
			return Result{}, err
		}
		if err = b.ValidateCoverage(s.now()); err != nil {
			return Result{}, err
		}
		m, d, err := b.BuildManifest(cmd.Actor, s.audit.Head())
		if err != nil {
			return Result{}, err
		}
		token := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s:%s", batchID, b.Revision, d, m.AuditHead, cmd.RequestID))))
		s.preflightMu.Lock()
		expiresAt := s.now().UTC().Add(5 * time.Minute)
		s.preflights[token] = releaseLock{BatchID: batchID, Revision: b.Revision, Digest: d, AuditHead: m.AuditHead, Actor: cmd.Actor, ExpiresAt: expiresAt}
		s.preflightMu.Unlock()
		return Result{Action: "release_preflight", Batch: b, Manifest: &m, Report: map[string]any{"manifest_digest": d, "revision": b.Revision, "lock_token": token, "expires_at": expiresAt}}, nil
	}
	return s.execute(batchID, "release", cmd.Meta, cmd, func() (*domain.RecordingBatch, *domain.Manifest, error) {
		b, err := s.loadForWrite(batchID, cmd.Meta)
		if err != nil {
			return nil, nil, err
		}
		if err = b.ValidateCoverage(s.now()); err != nil {
			return nil, nil, err
		}
		auditHead := s.audit.Head()
		if cmd.LockToken != "" {
			if cmd.ExpectedManifestDigest == "" {
				return nil, nil, errors.New("必须提供 expected_manifest_digest")
			}
			if err = s.claimReleasePreflight(cmd.LockToken, batchID, b.Revision, auditHead, cmd.Actor, cmd.ExpectedManifestDigest); err != nil {
				return nil, nil, err
			}
		}
		m, digest, err := b.BuildManifest(cmd.Actor, auditHead)
		if err != nil {
			return nil, nil, err
		}
		if old, loadErr := s.repo.LoadManifest(batchID); loadErr != nil {
			return nil, nil, loadErr
		} else if old != nil {
			return nil, nil, errors.New("已存在发布清单，无法重算签发")
		}
		if cmd.ExpectedManifestDigest != "" && cmd.ExpectedManifestDigest != digest {
			return nil, nil, errors.New("清单摘要不匹配")
		}
		b.Release(digest, s.now())
		return b, &m, nil
	})
}

func (s *Service) claimReleasePreflight(token, batchID string, revision int64, auditHead, actor, expectedDigest string) error {
	s.preflightMu.Lock()
	defer s.preflightMu.Unlock()
	lk, ok := s.preflights[token]
	if !ok || lk.Consumed || lk.BatchID != batchID || lk.Revision != revision || lk.AuditHead != auditHead || lk.Actor != actor || !lk.ExpiresAt.After(s.now().UTC()) {
		return errors.New("预检锁已失效")
	}
	if expectedDigest != lk.Digest {
		return errors.New("清单摘要不匹配")
	}
	delete(s.preflights, token)
	return nil
}
