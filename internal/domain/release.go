package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

func (b *RecordingBatch) Review(r ReviewDecision, now time.Time) error {
	if err := b.EnsureWritable(); err != nil {
		return err
	}
	if b.Status != StatusRedacted {
		return errors.New("只有完成脱敏的修订可复核")
	}
	if r.Reviewer == "" || r.Reviewer == b.LastEditor {
		return errors.New("复核人必须与编辑人职责分离")
	}
	if r.ReviewedRevision != b.Revision {
		return errors.New("复核签名必须绑定当前修订号")
	}
	if r.Decision != "approved" && r.Decision != "rejected" {
		return errors.New("复核结论必须为 approved 或 rejected")
	}
	r.RequiredChanges = normalizeStrings(r.RequiredChanges)
	if r.Decision == "rejected" && len(r.RequiredChanges) == 0 {
		return errors.New("驳回必须说明修订要求")
	}
	if r.Decision == "approved" && len(r.RequiredChanges) != 0 {
		return errors.New("批准时不能保留未解决修订要求")
	}
	if r.Decision == "approved" && len(b.PendingChanges()) > 0 {
		return errors.New("仍有未关闭的修订要求")
	}
	if !codePattern.MatchString(r.ReviewID) {
		return errors.New("review_id 格式无效")
	}
	for _, old := range b.Reviews {
		if old.ReviewID == r.ReviewID {
			return errors.New("review_id 已存在")
		}
	}
	if err := b.ValidateCoverage(now); err != nil {
		return err
	}
	r.BatchID, r.SignedAt = b.BatchID, now.UTC()
	if r.Decision == "rejected" {
		for _, text := range r.RequiredChanges {
			id := fmt.Sprintf("chg-%d-%s", r.ReviewedRevision, shortDigest(text))
			b.ChangeItems = append(b.ChangeItems, ChangeItem{ID: id, Text: text, ReviewedRevision: r.ReviewedRevision})
		}
	}
	b.Reviews = append(b.Reviews, r)
	if r.Decision == "approved" {
		b.Status = StatusApproved
	} else {
		b.Status = StatusRejected
	}
	b.bump(now)
	return nil
}

func shortDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:10]
}

func (b *RecordingBatch) BuildManifest(issuedBy, auditHead string) (Manifest, string, error) {
	if err := b.EnsureWritable(); err != nil {
		return Manifest{}, "", err
	}
	if b.Status != StatusApproved || len(b.Reviews) == 0 {
		return Manifest{}, "", errors.New("发布前必须取得独立伦理批准")
	}
	if issuedBy == "" || issuedBy == b.LastEditor || issuedBy == b.Reviews[len(b.Reviews)-1].Reviewer {
		return Manifest{}, "", errors.New("档案管理员必须独立于编辑人与复核人")
	}
	m := Manifest{BatchID: b.BatchID, Revision: b.Revision + 1, MediaSHA256: b.MediaSHA256, AuditHead: auditHead, IssuedBy: issuedBy, Review: b.Reviews[len(b.Reviews)-1]}
	for _, c := range b.Consents {
		m.ConsentDigests = append(m.ConsentDigests, c.EvidenceDigest)
	}
	sort.Strings(m.ConsentDigests)
	for _, s := range b.Segments {
		if s.ReleasedText == "" {
			return Manifest{}, "", errors.New("存在未生成脱敏文本的片段")
		}
		m.Segments = append(m.Segments, ReleasedSegment{SegmentID: s.SegmentID, StartMS: s.StartMS, EndMS: s.EndMS, SpeakerCode: s.SpeakerCode, Text: s.ReleasedText})
	}
	sort.Slice(m.Segments, func(i, j int) bool { return m.Segments[i].SegmentID < m.Segments[j].SegmentID })
	return m, ManifestDigest(m), nil
}

func ManifestDigest(m Manifest) string {
	data, _ := json.Marshal(m)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (b *RecordingBatch) Release(digest string, now time.Time) {
	b.PublishedManifestDigest = digest
	b.Status = StatusReleased
	b.bump(now)
}

func (b *RecordingBatch) ValidateSnapshot() error {
	if b.BatchID == "" || b.Revision < 1 || b.MediaSHA256 == "" {
		return errors.New("快照缺少标识、修订或媒体摘要")
	}
	if _, err := NormalizeDigest(b.MediaSHA256); err != nil {
		return err
	}
	if b.Status == StatusReleased {
		if _, err := NormalizeDigest(b.PublishedManifestDigest); err != nil {
			return errors.New("已发布快照缺少有效清单摘要")
		}
	}
	return nil
}

// PublicCopy 返回适合开放查询的副本。批次发布后不再向 API 暴露原始转写、
// 可逆前的字符位置标记或授权撤回细节，只保留发布文本和证据摘要。
func (b *RecordingBatch) PublicCopy() *RecordingBatch {
	if b == nil {
		return nil
	}
	data, _ := json.Marshal(b)
	var copy RecordingBatch
	_ = json.Unmarshal(data, &copy)
	if copy.Status == StatusReleased {
		for i := range copy.Segments {
			copy.Segments[i].RawText = ""
			copy.Segments[i].RedactionMarks = nil
		}
		for i := range copy.Consents {
			copy.Consents[i].WithdrawalRule = ""
			copy.Consents[i].WithdrawalDeadline = nil
			copy.Consents[i].Withdrawn = false
		}
	}
	return &copy
}

func (b *RecordingBatch) bump(now time.Time) { b.Revision++; b.UpdatedAt = now.UTC() }

func containsFold(values []string, wanted string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), wanted) {
			return true
		}
	}
	return false
}

func parseWithdrawalDeadline(rule string) (time.Time, bool) {
	re := regexp.MustCompile(`(20[0-9]{2}-[0-9]{2}-[0-9]{2})`)
	m := re.FindStringSubmatch(rule)
	if len(m) != 2 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", m[1])
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func parseWithdrawalDays(rule string) (int, bool) {
	re := regexp.MustCompile(`(?i)([0-9]{1,4})\s*(天|days?)`)
	m := re.FindStringSubmatch(rule)
	if len(m) != 3 {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func hasControlRune(text string) bool {
	for _, r := range text {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return true
		}
	}
	return false
}
func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeScopes(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(value)))
	}
	return normalizeStrings(normalized)
}
