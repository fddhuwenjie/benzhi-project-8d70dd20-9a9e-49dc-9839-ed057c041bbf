package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (b *RecordingBatch) ReplaceSegments(segments []TranscriptSegment, actor string, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusConsented && b.Status != StatusAnnotated && b.Status != StatusRejected {
		return errors.New("必须先完成授权核验")
	}
	if len(b.Consents) == 0 || len(segments) == 0 {
		return errors.New("授权和转写片段不能为空")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return errors.New("标注人不能为空")
	}
	items := append([]TranscriptSegment(nil), segments...)
	sort.Slice(items, func(i, j int) bool { return items[i].StartMS < items[j].StartMS })
	seen := map[string]bool{}
	consentedSpeakers := make(map[string]bool, len(b.Consents))
	for _, consent := range b.Consents {
		valid := !consent.Withdrawn
		if consent.WithdrawalDeadline != nil && !consent.WithdrawalDeadline.After(now.UTC()) {
			valid = false
		}
		if valid {
			consentedSpeakers[consent.ParticipantCode] = true
		}
	}
	maxDuration := int64(10 * 60 * 1000)
	oldRevisions := make(map[string]int64, len(b.Segments))
	for _, old := range b.Segments {
		oldRevisions[old.SegmentID] = old.Revision
	}
	for i := range items {
		s := &items[i]
		if !codePattern.MatchString(s.SegmentID) {
			return fmt.Errorf("片段 %s 编号格式无效", s.SegmentID)
		}
		if seen[s.SegmentID] {
			return fmt.Errorf("片段 %s 编号重复", s.SegmentID)
		}
		seen[s.SegmentID] = true
		if s.StartMS < 0 || s.EndMS <= s.StartMS {
			return fmt.Errorf("片段 %s 时间码无效", s.SegmentID)
		}
		if s.EndMS-s.StartMS > maxDuration {
			return fmt.Errorf("片段 %s 超过单段时长上限", s.SegmentID)
		}
		if i > 0 && items[i-1].EndMS > s.StartMS {
			return fmt.Errorf("片段 %s 与片段 %s 时间码重叠", items[i-1].SegmentID, s.SegmentID)
		}
		if !codePattern.MatchString(s.SpeakerCode) || hasControlRune(s.RawText) {
			return errors.New("说话人代码或原始转写无效")
		}
		if !consentedSpeakers[s.SpeakerCode] {
			return fmt.Errorf("说话人 %s 没有已核验的授权记录", s.SpeakerCode)
		}
		s.BatchID, s.AnnotatedBy, s.ReleasedText, s.RedactionMarks = b.BatchID, actor, "", nil
		s.Revision = oldRevisions[s.SegmentID] + 1
		if s.Revision <= 1 {
			s.Revision = b.Revision + 1
		}
	}
	if len(b.Segments) > 0 {
		merged := append([]TranscriptSegment(nil), b.Segments...)
		idx := map[string]int{}
		for i := range merged {
			idx[merged[i].SegmentID] = i
		}
		for _, seg := range items {
			if i, ok := idx[seg.SegmentID]; ok {
				merged[i] = seg
			} else {
				merged = append(merged, seg)
			}
		}
		sort.Slice(merged, func(i, j int) bool { return merged[i].StartMS < merged[j].StartMS })
		for i := 1; i < len(merged); i++ {
			if merged[i-1].EndMS > merged[i].StartMS {
				return fmt.Errorf("片段 %s 与片段 %s 时间码重叠", merged[i-1].SegmentID, merged[i].SegmentID)
			}
		}
		b.Segments = merged
	} else {
		b.Segments = items
	}
	b.LastEditor = actor
	b.Status = StatusAnnotated
	b.bump(now)
	return nil
}

func (b *RecordingBatch) ValidateSegments(segments []TranscriptSegment) (SegmentStatistics, []string, error) {
	return b.ValidateSegmentsAt(segments, time.Now().UTC())
}

func (b *RecordingBatch) ValidateSegmentsAt(segments []TranscriptSegment, now time.Time) (SegmentStatistics, []string, error) {
	clone := *b
	clone.Segments = append([]TranscriptSegment(nil), b.Segments...)
	if err := clone.ReplaceSegments(segments, "validator", now); err != nil {
		return SegmentStatistics{}, nil, err
	}
	return clone.SegmentStats(), nil, nil
}
