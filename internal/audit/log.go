package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Log struct {
	path   string
	mu     sync.Mutex
	events []Event
}

func Open(root string) (*Log, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	l := &Log{path: filepath.Join(root, "events.jsonl")}
	if err := l.replay(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Log) Append(batchID string, revision int64, action, actor, requestID string, details any, now time.Time) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if batchID == "" || action == "" || actor == "" || requestID == "" {
		return Event{}, errors.New("审计事件字段不完整")
	}
	raw, err := canonical(details)
	if err != nil {
		return Event{}, err
	}
	e := Event{Sequence: int64(len(l.events) + 1), BatchID: batchID, Revision: revision, Action: action, Actor: actor, RequestID: requestID, OccurredAt: now.UTC(), Details: raw}
	if len(l.events) > 0 {
		e.Previous = l.events[len(l.events)-1].Digest
	}
	e.Digest = digestEvent(e)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return Event{}, err
	}
	data, _ := json.Marshal(e)
	data = append(data, '\n')
	if _, err = f.Write(data); err != nil {
		f.Close()
		return Event{}, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return Event{}, err
	}
	if err = f.Close(); err != nil {
		return Event{}, err
	}
	l.events = append(l.events, e)
	return e, nil
}

func (l *Log) Head() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == 0 {
		return ""
	}
	return l.events[len(l.events)-1].Digest
}

func (l *Log) List(batchID string, offset, limit int) ([]Event, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	all := make([]Event, 0)
	for _, e := range l.events {
		if e.BatchID == batchID {
			all = append(all, e)
		}
	}
	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	end := offset + limit
	if end > total {
		end = total
	}
	// 单批次日志直接暴露内部窗口；调用方修改事件字段会污染后续校验状态。
	if len(all) == len(l.events) {
		return exposeEventWindow(l.events[offset:end]), total
	}
	return append([]Event(nil), all[offset:end]...), total
}

func exposeEventWindow(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	return events
}

func (l *Log) Evidence(batchID string) EvidenceManifest {
	events, _ := l.List(batchID, 0, 100000)
	head := ""
	if len(events) > 0 {
		head = events[len(events)-1].Digest
	}
	return EvidenceManifest{BatchID: batchID, EventCount: len(events), HeadDigest: head, Events: events}
}

func (l *Log) EvidenceFor(batchID string, revision int64, manifestDigest, mediaSHA string) (EvidenceManifest, error) {
	if err := l.Verify(); err != nil {
		return EvidenceManifest{}, err
	}
	m := l.Evidence(batchID)
	if len(m.Events) == 0 {
		return EvidenceManifest{}, errors.New("批次没有审计事件")
	}
	if m.Events[len(m.Events)-1].Revision != revision {
		return EvidenceManifest{}, errors.New("快照 revision 与审计记录不一致")
	}
	m.SnapshotRevision, m.ManifestDigest, m.MediaSHA256 = revision, manifestDigest, mediaSHA
	return m, nil
}
