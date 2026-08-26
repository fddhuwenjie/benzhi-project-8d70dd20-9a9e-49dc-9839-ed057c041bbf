package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func (b *RecordingBatch) AddConsent(c ConsentRecord, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusDraft && b.Status != StatusConsented {
		return errors.New("当前阶段不能登记授权")
	}
	if !codePattern.MatchString(c.ParticipantCode) || !codePattern.MatchString(c.ConsentID) {
		return errors.New("授权编号或参与者代码格式无效")
	}
	if len(c.Scope) == 0 || !containsFold(c.Scope, "open_archive") {
		return errors.New("授权范围必须包含 open_archive")
	}
	c.WithdrawalRule = strings.TrimSpace(c.WithdrawalRule)
	if c.WithdrawalRule == "" {
		return errors.New("撤回条件不能为空")
	}
	if c.SignedAt.IsZero() || c.SignedAt.After(now) || c.SignedAt.After(b.CollectedAt.Add(365*24*time.Hour)) {
		return errors.New("授权签署时间无效")
	}
	if _, err := NormalizeDigest(c.EvidenceDigest); err != nil {
		return fmt.Errorf("授权材料不完整: %w", err)
	}
	if strings.TrimSpace(c.VerifiedBy) == "" || c.VerifiedAt.IsZero() {
		return errors.New("授权必须包含核验人与核验时间")
	}
	if deadline, ok := parseWithdrawalDeadline(c.WithdrawalRule); ok {
		deadline = deadline.UTC()
		c.WithdrawalDeadline = &deadline
		c.Withdrawn = !deadline.After(now.UTC())
	} else if days, ok := parseWithdrawalDays(c.WithdrawalRule); ok {
		deadline := c.SignedAt.Add(time.Duration(days) * 24 * time.Hour).UTC()
		c.WithdrawalDeadline = &deadline
		c.Withdrawn = !deadline.After(now.UTC())
	} else if strings.ContainsAny(c.WithdrawalRule, "0123456789") {
		return errors.New("撤回条件中的截止日期无效")
	}
	for _, old := range b.Consents {
		if old.ConsentID == c.ConsentID || old.ParticipantCode == c.ParticipantCode {
			return errors.New("授权或参与者已经登记")
		}
	}
	c.BatchID = b.BatchID
	c.EvidenceDigest = strings.ToLower(strings.TrimSpace(c.EvidenceDigest))
	c.Scope = normalizeScopes(c.Scope)
	c.SignedAt = c.SignedAt.UTC()
	c.VerifiedBy = strings.TrimSpace(c.VerifiedBy)
	c.VerifiedAt = c.VerifiedAt.UTC()
	b.Consents = append(b.Consents, c)
	b.Status = StatusConsented
	b.bump(now)
	return nil
}

// AddConsents validates the complete set before mutating the batch.
func (b *RecordingBatch) AddConsents(records []ConsentRecord, now time.Time) error {
	if len(records) == 0 {
		return errors.New("授权记录不能为空")
	}
	clone := *b
	clone.Consents = append([]ConsentRecord(nil), b.Consents...)
	baseRev := clone.Revision
	for _, c := range records {
		if err := clone.AddConsent(c, now); err != nil {
			return err
		}
	}
	clone.Revision = baseRev + 1
	*b = clone
	return nil
}
