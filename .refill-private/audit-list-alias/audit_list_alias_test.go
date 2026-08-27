package audit_list_alias

import (
	"bytes"
	"field-voice-archive/internal/audit"
	"testing"
	"time"
)

// TestAuditListMutationDoesNotCorruptLog 验证分页结果归调用方所有，不能取得日志内存状态的所有权。
func TestAuditListMutationDoesNotCorruptLog(t *testing.T) {
	logbook, err := audit.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logbook.Append("batch-1", 1, "create", "collector", "req-1", map[string]any{"status": "draft"}, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	events, total := logbook.List("batch-1", 0, 10)
	if total != 1 || len(events) != 1 {
		t.Fatalf("unexpected audit page: total=%d len=%d", total, len(events))
	}
	originalDetails := append([]byte(nil), events[0].Details...)
	// 返回结果归调用方所有，修改字段或 Details 不能改写日志状态。
	events[0].Details[0] ^= 1
	events[0].Digest = "tampered"
	if err := logbook.Verify(); err != nil {
		t.Fatalf("mutating a listed event corrupted the audit log: %v", err)
	}
	fresh, _ := logbook.List("batch-1", 0, 10)
	if !bytes.Equal(fresh[0].Details, originalDetails) || fresh[0].Digest == "tampered" {
		t.Fatal("mutating a listed event leaked into subsequent audit results")
	}
}
