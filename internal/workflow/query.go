package workflow

import (
	"errors"
	"field-voice-archive/internal/audit"
	"field-voice-archive/internal/domain"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Service) Preview(batchID string, marks map[string][]domain.RedactionMark) (map[string]string, error) {
	b, err := s.repo.LoadBatch(batchID)
	if err != nil {
		return nil, err
	}
	if err = b.EnsureWritable(); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, seg := range b.Segments {
		list, ok := marks[seg.SegmentID]
		if !ok {
			return nil, fmt.Errorf("片段 %s 缺少脱敏标记清单", seg.SegmentID)
		}
		text, _, err := domain.Redact(seg.RawText, list)
		if err != nil {
			return nil, err
		}
		out[seg.SegmentID] = text
	}
	return out, nil
}

func (s *Service) PreviewDetailed(batchID string, marks map[string][]domain.RedactionMark) (map[string]any, error) {
	b, err := s.repo.LoadBatch(batchID)
	if err != nil {
		return nil, err
	}
	if err = b.EnsureWritable(); err != nil {
		return nil, err
	}
	preview := map[string]any{}
	alerts := []map[string]any{}
	for _, seg := range b.Segments {
		list, ok := marks[seg.SegmentID]
		if !ok {
			return nil, fmt.Errorf("片段 %s 缺少脱敏标记清单", seg.SegmentID)
		}
		text, normalized, err := domain.Redact(seg.RawText, list)
		if err != nil {
			return nil, fmt.Errorf("片段 %s: %w", seg.SegmentID, err)
		}
		coverage := 0
		for _, m := range normalized {
			coverage += m.End - m.Start
		}
		segAlerts := domain.ScanPII(seg.RawText)
		segAlertText := []map[string]any{}
		for _, a := range segAlerts {
			alert := map[string]any{"segment_id": seg.SegmentID, "kind": a["kind"], "start": a["start"], "end": a["end"], "fragment": a["fragment"]}
			covered := false
			for _, mark := range normalized {
				if mark.Start <= a["start"].(int) && mark.End >= a["end"].(int) {
					covered = true
					break
				}
			}
			alert["covered"] = covered
			alerts = append(alerts, alert)
			segAlertText = append(segAlertText, alert)
		}
		preview[seg.SegmentID] = map[string]any{"released_text": text, "original_text": seg.RawText, "original_length": len([]rune(seg.RawText)), "released_length": len([]rune(text)), "replacement_count": len(normalized), "coverage_rate": float64(coverage) / float64(max(1, len([]rune(seg.RawText)))), "alerts": segAlertText}
	}
	return map[string]any{"preview": preview, "alerts": alerts}, nil
}

func (s *Service) Get(batchID string) (*domain.RecordingBatch, error) {
	return s.repo.LoadBatch(batchID)
}
func (s *Service) List() ([]domain.RecordingBatch, error) {
	items, err := s.repo.ListBatches()
	return items, err
}
func (s *Service) ListFiltered(filter ListFilter) (ListResult, error) {
	if filter.PageSize > 100 {
		return ListResult{}, errors.New("page_size 超出上限")
	}
	var from, to time.Time
	var err error
	if filter.CollectedFrom != "" {
		from, err = time.Parse("2006-01-02", filter.CollectedFrom)
		if err != nil {
			return ListResult{}, errors.New("collected_from 日期参数无效")
		}
	}
	if filter.CollectedTo != "" {
		to, err = time.Parse("2006-01-02", filter.CollectedTo)
		if err != nil {
			return ListResult{}, errors.New("collected_to 日期参数无效")
		}
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return ListResult{}, errors.New("日期范围无效")
	}
	items, err := s.repo.ListBatches()
	if err != nil {
		return ListResult{}, err
	}
	out := make([]domain.RecordingBatch, 0, len(items))
	counts := map[domain.Status]int{}
	for _, b := range items {
		if b.Status == domain.StatusReleased && filter.Query != "" {
			m, merr := s.repo.LoadManifest(b.BatchID)
			if merr != nil || m == nil || domain.ManifestDigest(*m) != b.PublishedManifestDigest {
				continue
			}
		}
		if filter.Status != "" && string(b.Status) != filter.Status {
			continue
		}
		if filter.LanguageVariant != "" && b.LanguageVariant != filter.LanguageVariant {
			continue
		}
		if filter.CollectionSite != "" && b.CollectionSite != filter.CollectionSite {
			continue
		}
		if filter.CollectedFrom != "" && b.CollectedAt.Format("2006-01-02") < filter.CollectedFrom {
			continue
		}
		if filter.CollectedTo != "" && b.CollectedAt.Format("2006-01-02") > filter.CollectedTo {
			continue
		}
		if filter.ReleasedOnly && b.Status != domain.StatusReleased {
			continue
		}
		if filter.Query != "" {
			q := strings.ToLower(filter.Query)
			matched := strings.Contains(strings.ToLower(b.Title), q) || strings.Contains(strings.ToLower(b.LanguageVariant), q)
			if b.Status == domain.StatusReleased {
				for _, seg := range b.Segments {
					if strings.Contains(strings.ToLower(seg.ReleasedText), q) {
						matched = true
					}
				}
			}
			if !matched {
				continue
			}
		}
		counts[b.Status]++
		out = append(out, b)
	}
	total := len(out)
	page := filter.Page
	if page < 1 {
		page = 1
	}
	size := filter.PageSize
	if size <= 0 {
		size = 50
	}
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	next := 0
	if end < total {
		next = page + 1
	}
	return ListResult{Batches: out[start:end], Total: total, StatusCounts: counts, NextPage: next}, nil
}

func (s *Service) Evidence(batchID string) (audit.EvidenceManifest, error) {
	b, err := s.repo.LoadBatch(batchID)
	if err != nil {
		return audit.EvidenceManifest{}, err
	}
	if b.Status != domain.StatusReleased || b.PublishedManifestDigest == "" {
		return audit.EvidenceManifest{}, errors.New("未发布批次不能下载证据清单")
	}
	manifest, err := s.repo.LoadManifest(batchID)
	if err != nil {
		return audit.EvidenceManifest{}, err
	}
	if manifest == nil || domain.ManifestDigest(*manifest) != b.PublishedManifestDigest {
		return audit.EvidenceManifest{}, errors.New("发布清单摘要校验失败")
	}
	evidence, err := s.audit.EvidenceFor(batchID, b.Revision, b.PublishedManifestDigest, b.MediaSHA256)
	if err != nil {
		return audit.EvidenceManifest{}, err
	}
	if len(evidence.Events) == 0 || manifest.AuditHead != evidence.Events[len(evidence.Events)-1].Previous {
		return audit.EvidenceManifest{}, errors.New("发布清单审计头不匹配")
	}
	return evidence, nil
}

func (s *Service) VerifyPublished(ids []string) []VerificationResult {
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	out := make([]VerificationResult, 0, len(ids))
	for _, id := range ids {
		vr := VerificationResult{BatchID: id}
		b, err := s.repo.LoadBatch(id)
		if err != nil {
			vr.Reason = err.Error()
			out = append(out, vr)
			continue
		}
		if b.Status != domain.StatusReleased {
			vr.Reason = "批次尚未发布"
			out = append(out, vr)
			continue
		}
		m, err := s.repo.LoadManifest(id)
		if err != nil || m == nil {
			vr.Reason = "发布清单不存在"
			out = append(out, vr)
			continue
		}
		if domain.ManifestDigest(*m) != b.PublishedManifestDigest || m.Revision != b.Revision {
			vr.Reason = "manifest_digest 或 revision 不匹配"
			out = append(out, vr)
			continue
		}
		if m.MediaSHA256 != b.MediaSHA256 {
			vr.Reason = "media_sha256 不匹配"
			out = append(out, vr)
			continue
		}
		if err = s.audit.Verify(); err != nil {
			vr.Reason = err.Error()
			out = append(out, vr)
			continue
		}
		events, _ := s.audit.List(id, 0, 100000)
		if len(events) == 0 || events[len(events)-1].Revision != b.Revision || events[len(events)-1].Previous != m.AuditHead {
			vr.Reason = "审计头或 revision 不匹配"
			out = append(out, vr)
			continue
		}
		if len(m.Segments) == 0 {
			vr.Reason = "发布清单缺少片段"
			out = append(out, vr)
			continue
		}
		vr.Passed = true
		out = append(out, vr)
	}
	return out
}
