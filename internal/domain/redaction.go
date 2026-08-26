package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

func (b *RecordingBatch) ApplyRedactions(marks map[string][]RedactionMark, actor string, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusAnnotated && b.Status != StatusRedacted && b.Status != StatusRejected {
		return errors.New("必须先完成转写标注")
	}
	if strings.TrimSpace(actor) == "" {
		return errors.New("脱敏编辑人不能为空")
	}
	for id := range marks {
		found := false
		for _, seg := range b.Segments {
			if seg.SegmentID == id {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("不存在的片段: %s", id)
		}
	}
	for i := range b.Segments {
		s := &b.Segments[i]
		list, ok := marks[s.SegmentID]
		if !ok {
			return fmt.Errorf("片段 %s 缺少脱敏标记清单", s.SegmentID)
		}
		released, normalized, err := Redact(s.RawText, list)
		if err != nil {
			return fmt.Errorf("片段 %s: %w", s.SegmentID, err)
		}
		s.RedactionMarks, s.ReleasedText, s.AnnotatedBy = normalized, released, actor
		s.Revision = b.Revision + 1
	}
	b.LastEditor = actor
	b.Status = StatusRedacted
	b.bump(now)
	return nil
}

func (b *RecordingBatch) Coverage(now time.Time) CoverageSummary {
	var c CoverageSummary
	c.Total = len(b.Consents)
	for _, consent := range b.Consents {
		if containsFold(consent.Scope, "open_archive") {
			c.OpenArchive++
		}
		withdrawn := consent.Withdrawn
		if consent.WithdrawalDeadline != nil {
			if !consent.WithdrawalDeadline.After(now.UTC()) {
				withdrawn = true
			} else if !withdrawn && consent.WithdrawalDeadline.Sub(now.UTC()) <= 30*24*time.Hour {
				c.Expiring++
			}
		}
		if withdrawn {
			c.Withdrawn++
		} else if containsFold(consent.Scope, "open_archive") {
			c.Valid++
		}
	}
	return c
}

func (b *RecordingBatch) SegmentStats() SegmentStatistics {
	stats := SegmentStatistics{BySpeaker: map[string]SpeakerStatistics{}}
	for _, s := range b.Segments {
		stats.Count++
		d := s.EndMS - s.StartMS
		if d > 0 {
			stats.TotalDurationMS += d
		}
		missing := 0
		for _, r := range s.RawText {
			if unicode.IsSpace(r) {
				missing++
			}
		}
		stats.ReleasedChars += len([]rune(s.ReleasedText))
		stats.UntranscribedChars += missing
		sp := stats.BySpeaker[s.SpeakerCode]
		sp.SegmentCount++
		sp.TotalDurationMS += d
		sp.UntranscribedChars += missing
		stats.BySpeaker[s.SpeakerCode] = sp
	}
	for i := 1; i < len(b.Segments); i++ {
		if b.Segments[i-1].EndMS < b.Segments[i].StartMS {
			stats.GapCount++
		}
	}
	return stats
}

func (b *RecordingBatch) PendingChanges() []string {
	if len(b.ChangeItems) > 0 {
		out := []string{}
		for _, c := range b.ChangeItems {
			if !c.Closed {
				out = append(out, c.ID)
			}
		}
		return out
	}
	for i := len(b.Reviews) - 1; i >= 0; i-- {
		if b.Reviews[i].Decision == "rejected" && b.Status != StatusApproved && b.Status != StatusReleased {
			return append([]string(nil), b.Reviews[i].RequiredChanges...)
		}
	}
	return nil
}

func (b *RecordingBatch) ValidateCoverage(now time.Time) error {
	c := b.Coverage(now)
	if c.Total == 0 || c.OpenArchive != c.Total {
		return errors.New("授权覆盖不足：每位参与者都必须包含 open_archive")
	}
	if c.Withdrawn > 0 {
		return errors.New("存在已撤回或已到期授权")
	}
	return nil
}

func (b *RecordingBatch) WithdrawConsent(identifier, reason string, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	identifier, reason = strings.TrimSpace(identifier), strings.TrimSpace(reason)
	if identifier == "" {
		return errors.New("参与者代码或授权编号不能为空")
	}
	if reason == "" {
		return errors.New("撤回原因不能为空")
	}
	for i := range b.Consents {
		if b.Consents[i].ParticipantCode == identifier || b.Consents[i].ConsentID == identifier {
			b.Consents[i].Withdrawn = true
			b.bump(now)
			return nil
		}
	}
	return fmt.Errorf("不存在的参与者代码或授权编号: %s", identifier)
}

func Redact(text string, marks []RedactionMark) (string, []RedactionMark, error) {
	runes := []rune(text)
	list := append([]RedactionMark(nil), marks...)
	sort.Slice(list, func(i, j int) bool { return list[i].Start < list[j].Start })
	last := 0
	var out strings.Builder
	for i := range list {
		m := &list[i]
		m.Kind = strings.ToLower(strings.TrimSpace(m.Kind))
		if m.Kind != "person" && m.Kind != "place" && m.Kind != "contact" && m.Kind != "other" {
			return "", nil, errors.New("个人信息类型必须是 person、place、contact 或 other")
		}
		if m.Start < last || m.End <= m.Start || m.End > len(runes) {
			return "", nil, errors.New("脱敏标记越界或互相重叠")
		}
		if m.Replacement == "" {
			m.Replacement = fmt.Sprintf("[%s-%02d]", strings.ToUpper(m.Kind), i+1)
		}
		if strings.ContainsAny(m.Replacement, "\r\n") {
			return "", nil, errors.New("替换令牌不能换行")
		}
		out.WriteString(string(runes[last:m.Start]))
		out.WriteString(m.Replacement)
		last = m.End
	}
	out.WriteString(string(runes[last:]))
	return out.String(), list, nil
}

func ScanPII(text string) []map[string]any {
	alerts := []map[string]any{}
	for _, re := range []struct {
		k string
		r *regexp.Regexp
	}{{"phone", phonePattern}, {"email", emailPattern}} {
		for _, m := range re.r.FindAllStringIndex(text, -1) {
			start, end := len([]rune(text[:m[0]])), len([]rune(text[:m[1]]))
			alerts = append(alerts, map[string]any{"kind": re.k, "start": start, "end": end, "fragment": string([]rune(text)[start:end])})
		}
	}
	return alerts
}
