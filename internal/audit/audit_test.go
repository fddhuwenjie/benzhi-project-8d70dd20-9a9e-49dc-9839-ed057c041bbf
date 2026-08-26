package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendReplayAndDetectTampering(t *testing.T) {
	dir := t.TempDir()
	logbook, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := logbook.Append("b", 1, "create", "a", "r1", map[string]any{"status": "draft"}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	second, err := logbook.Append("b", 2, "consent", "a", "r2", map[string]any{"status": "consented"}, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if second.Previous != first.Digest {
		t.Fatal("前序摘要错误")
	}
	if _, err = Open(dir); err != nil {
		t.Fatalf("合法日志不能回放: %v", err)
	}
	path := filepath.Join(dir, "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[20] ^= 1
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(dir); err == nil {
		t.Fatal("应检测审计篡改")
	}
}

func TestListIsBatchScopedAndPaged(t *testing.T) {
	logbook, _ := Open(t.TempDir())
	for i := int64(1); i <= 3; i++ {
		if _, err := logbook.Append("b", i, "x", "a", string(rune('a'+i)), nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	logbook.Append("other", 1, "x", "a", "other", nil, time.Now())
	events, total := logbook.List("b", 1, 1)
	if total != 3 || len(events) != 1 || events[0].Revision != 2 {
		t.Fatalf("分页错误: total=%d events=%v", total, events)
	}
}
