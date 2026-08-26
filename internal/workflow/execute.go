package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"field-voice-archive/internal/domain"
	"field-voice-archive/internal/store"
	"fmt"
	"sort"
	"strings"
	"sync"
)

func (s *Service) execute(batchID, action string, meta Meta, payload any, change func() (*domain.RecordingBatch, *domain.Manifest, error)) (Result, error) {
	if batchID == "" || meta.Actor == "" || meta.RequestID == "" {
		return Result{}, errors.New("batch_id、actor 和 request_id 均不能为空")
	}
	requestLock := s.requestLock(meta.RequestID)
	requestLock.Lock()
	defer requestLock.Unlock()
	lock := s.batchLock(batchID)
	lock.Lock()
	defer lock.Unlock()
	fingerprint, err := fingerprint(action, batchID, payload)
	if err != nil {
		return Result{}, err
	}
	if old, err := s.repo.LoadRequest(meta.RequestID); err != nil {
		return Result{}, err
	} else if old != nil {
		if old.Fingerprint != fingerprint {
			return Result{}, errors.New("request_id 已被不同命令使用")
		}
		var result Result
		if err = json.Unmarshal(old.Response, &result); err != nil {
			return Result{}, err
		}
		result.IdempotentReplay = true
		return result, nil
	}
	b, manifest, err := change()
	if err != nil {
		return Result{}, err
	}
	coverage, stats := b.Coverage(s.now()), b.SegmentStats()
	b.ConsentCoverage, b.SegmentStatistics = &coverage, &stats
	b.PendingRequiredChanges = b.PendingChanges()
	if err = s.repo.SaveBatch(b); err != nil {
		return Result{}, err
	}
	if manifest != nil {
		if err = s.repo.SaveManifest(batchID, *manifest); err != nil {
			return Result{}, err
		}
	}
	details := map[string]any{"status": b.Status, "manifest_digest": b.PublishedManifestDigest, "request_fingerprint": fingerprint}
	if action == "create" {
		details["baseline"] = map[string]any{"media_sha256": b.MediaSHA256, "language_variant": b.LanguageVariant, "collection_site": b.CollectionSite, "collected_at": b.CollectedAt.Format("2006-01-02")}
	}
	if action == "consent" {
		details["consent_coverage"] = b.Coverage(s.now())
		if bc, ok := payload.(ConsentBatchCommand); ok {
			ids := []string{}
			records := []map[string]any{}
			for _, r := range bc.Records {
				ids = append(ids, r.ConsentID)
				records = append(records, map[string]any{"consent_id": r.ConsentID, "participant_code": r.ParticipantCode, "scope": r.Scope, "evidence_digest": strings.ToLower(strings.TrimSpace(r.EvidenceDigest)), "verified_by": r.VerifiedBy, "result": "verified"})
			}
			sort.Strings(ids)
			details["consent_ids"] = ids
			details["records"] = records
		}
		if len(b.Consents) > 0 {
			c := b.Consents[len(b.Consents)-1]
			details["verified_by"], details["verified_at"], details["evidence_digest"] = c.VerifiedBy, c.VerifiedAt, c.EvidenceDigest
		}
	}
	if action == "consent_withdraw" {
		if wc, ok := payload.(WithdrawConsentCommand); ok {
			reason := wc.Reason
			if reason == "" {
				reason = wc.WithdrawalReason
			}
			details["participant_code"], details["consent_id"], details["reason"] = wc.ParticipantCode, wc.ConsentID, reason
		}
		details["operator"], details["withdrawn_at"] = meta.Actor, s.now().UTC()
		details["consent_coverage"] = b.Coverage(s.now())
	}
	if action == "annotate" {
		details["segment_statistics"] = b.SegmentStats()
		if ac, ok := payload.(AnnotateCommand); ok && len(ac.ResolvedChangeIDs) > 0 {
			details["resolved_change_ids"] = ac.ResolvedChangeIDs
			details["change_items"] = b.ChangeItems
		}
	}
	if action == "redact" {
		details["segment_statistics"] = b.SegmentStats()
		if rc, ok := payload.(RedactCommand); ok && rc.ConfirmNoPII {
			details["confirm_no_pii"], details["no_pii_reason"] = true, rc.NoPIIReason
		}
		if rc, ok := payload.(RedactCommand); ok && len(rc.ResolvedChangeIDs) > 0 {
			details["resolved_change_ids"] = rc.ResolvedChangeIDs
			details["change_items"] = b.ChangeItems
		}
	}
	if action == "review" {
		if rc, ok := payload.(ReviewCommand); ok {
			details["decision"] = rc.Decision
			details["reviewed_revision"] = rc.ExpectedRevision
			details["required_changes"] = rc.RequiredChanges
			details["change_items"] = b.ChangeItems
		}
	}
	if _, err = s.audit.Append(batchID, b.Revision, action, meta.Actor, meta.RequestID, details, s.now()); err != nil {
		return Result{}, err
	}
	result := Result{Action: action, Batch: b, Manifest: manifest}
	if action == "consent" {
		report := map[string]any{"coverage": b.Coverage(s.now())}
		if bc, ok := payload.(ConsentBatchCommand); ok {
			records := make([]map[string]any, 0, len(bc.Records))
			for _, r := range bc.Records {
				records = append(records, map[string]any{"consent_id": r.ConsentID, "participant_code": r.ParticipantCode, "verified": true})
			}
			report["records"] = records
		}
		result.Report = report
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	if err = s.repo.SaveRequest(store.IdempotencyRecord{RequestID: meta.RequestID, Fingerprint: fingerprint, StatusCode: 200, Response: raw}); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s *Service) loadForWrite(batchID string, meta Meta) (*domain.RecordingBatch, error) {
	b, err := s.repo.LoadBatch(batchID)
	if err != nil {
		return nil, err
	}
	if err = b.EnsureWritable(); err != nil {
		return nil, err
	}
	if err = b.CheckRevision(meta.ExpectedRevision); err != nil {
		return nil, err
	}
	if audited := s.audit.BatchRevision(batchID); audited != b.Revision {
		return nil, fmt.Errorf("批次 revision 与审计记录不一致")
	}
	return b, nil
}
func (s *Service) batchLock(id string) *sync.Mutex {
	value, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (s *Service) requestLock(id string) *sync.Mutex {
	value, _ := s.requestLocks.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}
func fingerprint(action, batchID string, payload any) (string, error) {
	raw, err := json.Marshal(struct {
		Action  string `json:"action"`
		BatchID string `json:"batch_id"`
		Payload any    `json:"payload"`
	}{action, batchID, payload})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func redactionAlerts(b *domain.RecordingBatch) []string {
	var out []string
	for _, seg := range b.Segments {
		for _, a := range domain.ScanPII(seg.RawText) {
			start, _ := a["start"].(int)
			end, _ := a["end"].(int)
			covered := false
			for _, m := range seg.RedactionMarks {
				if m.Start <= start && m.End >= end {
					covered = true
					break
				}
			}
			if !covered {
				out = append(out, fmt.Sprintf("片段 %s: %v", seg.SegmentID, a))
			}
		}
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func resolveChanges(b *domain.RecordingBatch, ids []string) error {
	for _, id := range ids {
		found := false
		for i := range b.ChangeItems {
			if b.ChangeItems[i].ID == id {
				if b.ChangeItems[i].Closed {
					return errors.New("修订项已关闭")
				}
				b.ChangeItems[i].Closed = true
				b.ChangeItems[i].ClosedRevision = b.Revision
				found = true
			}
		}
		if !found {
			return fmt.Errorf("不存在的修订项: %s", id)
		}
	}
	return nil
}
