package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

func (l *Log) Verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.verifyOnDiskLocked()
}

func (l *Log) verifyOnDiskLocked() error {
	// 每次校验都重新读取磁盘，确保服务启动后对 JSONL 的篡改能够被发现。
	events, err := readEvents(l.path)
	if err != nil {
		return err
	}
	if err = verify(events); err != nil {
		return err
	}
	if len(events) != len(l.events) {
		return errors.New("审计日志事件数量与内存状态不一致")
	}
	for i := range events {
		if events[i].Digest != l.events[i].Digest {
			return errors.New("审计日志与内存状态不一致")
		}
	}
	return nil
}

func (l *Log) replay() error {
	events, err := readEvents(l.path)
	if err != nil {
		return err
	}
	l.events = events
	return verify(events)
}

func readEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4<<20)
	events := []Event{}
	line := 0
	for s.Scan() {
		line++
		var e Event
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("审计日志第 %d 行损坏: %w", line, err)
		}
		events = append(events, e)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func verify(events []Event) error {
	prev := ""
	var seq int64 = 1
	revisions := map[string]int64{}
	for _, e := range events {
		if e.Sequence != seq {
			return errors.New("审计事件序号不连续")
		}
		if e.Previous != prev {
			return errors.New("审计事件前序摘要断裂")
		}
		if digestEvent(e) != e.Digest {
			return errors.New("审计事件摘要校验失败")
		}
		if old := revisions[e.BatchID]; old == 0 && e.Revision != 1 {
			return errors.New("批次首个审计事件必须从 revision 1 开始")
		} else if old > 0 && e.Revision != old+1 {
			return errors.New("批次审计修订不连续")
		}
		revisions[e.BatchID] = e.Revision
		prev = e.Digest
		seq++
	}
	return nil
}

func (l *Log) BatchRevision(batchID string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.events) - 1; i >= 0; i-- {
		if l.events[i].BatchID == batchID {
			return l.events[i].Revision
		}
	}
	return 0
}

func canonical(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var generic any
	if err = json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}
func digestEvent(e Event) string {
	copy := e
	copy.Digest = ""
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func WriteEvidence(w io.Writer, m EvidenceManifest) error {
	events := append([]Event(nil), m.Events...)
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	m.Events = events
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
