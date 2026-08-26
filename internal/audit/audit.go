package audit

import (
	"encoding/json"
	"errors"
	"time"
)

type Event struct {
	Sequence   int64           `json:"sequence"`
	BatchID    string          `json:"batch_id"`
	Revision   int64           `json:"revision"`
	Action     string          `json:"action"`
	Actor      string          `json:"actor"`
	RequestID  string          `json:"request_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Details    json.RawMessage `json:"details"`
	Previous   string          `json:"previous_digest"`
	Digest     string          `json:"digest"`
}

type EvidenceManifest struct {
	BatchID          string  `json:"batch_id"`
	EventCount       int     `json:"event_count"`
	HeadDigest       string  `json:"head_digest"`
	SnapshotRevision int64   `json:"snapshot_revision,omitempty"`
	ManifestDigest   string  `json:"manifest_digest,omitempty"`
	MediaSHA256      string  `json:"media_sha256,omitempty"`
	Events           []Event `json:"events"`
}

type Query struct {
	Action, Actor            string
	FromRevision, ToRevision int64
	Offset, Limit            int
}

func (l *Log) Query(batchID string, q Query) ([]Event, int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.verifyOnDiskLocked(); err != nil {
		return nil, 0, err
	}
	if q.FromRevision > 0 && q.ToRevision > 0 && q.FromRevision > q.ToRevision {
		return nil, 0, errors.New("revision 范围无效")
	}
	all := []Event{}
	for _, e := range l.events {
		if e.BatchID != batchID {
			continue
		}
		if q.Action != "" && e.Action != q.Action {
			continue
		}
		if q.Actor != "" && e.Actor != q.Actor {
			continue
		}
		if q.FromRevision > 0 && e.Revision < q.FromRevision {
			continue
		}
		if q.ToRevision > 0 && e.Revision > q.ToRevision {
			continue
		}
		all = append(all, e)
	}
	total := len(all)
	if q.Offset < 0 {
		q.Offset = 0
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50
	}
	if q.Offset > len(all) {
		q.Offset = len(all)
	}
	end := q.Offset + q.Limit
	if end > len(all) {
		end = len(all)
	}
	return append([]Event(nil), all[q.Offset:end]...), total, nil
}
