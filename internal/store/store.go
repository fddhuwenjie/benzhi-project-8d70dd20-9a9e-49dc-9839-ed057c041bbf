package store

import (
	"encoding/json"
	"errors"
	"field-voice-archive/internal/domain"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type IdempotencyRecord struct {
	RequestID   string          `json:"request_id"`
	Fingerprint string          `json:"fingerprint"`
	StatusCode  int             `json:"status_code"`
	Response    json.RawMessage `json:"response"`
}

type Repository struct {
	root          string
	batchDir      string
	requestDir    string
	manifestDir   string
	quarantine    string
	manifestCache map[string]*domain.Manifest
	mu            sync.RWMutex
}

func Open(root string) (*Repository, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("数据目录不能为空")
	}
	r := &Repository{root: root, batchDir: filepath.Join(root, "batches"), requestDir: filepath.Join(root, "requests"), manifestDir: filepath.Join(root, "manifests"), quarantine: filepath.Join(root, "quarantine"), manifestCache: map[string]*domain.Manifest{}}
	for _, dir := range []string{r.batchDir, r.requestDir, r.manifestDir, r.quarantine} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	if err := r.recoverSnapshots(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Repository) Root() string { return r.root }

func (r *Repository) BatchExists(id string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, err := os.Stat(filepath.Join(r.batchDir, safeName(id)+".json"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// FindBatchByMediaSHA256 返回已占用媒体摘要的批次，用于创建时冲突检测。
func (r *Repository) FindBatchByMediaSHA256(digest string) (*domain.RecordingBatch, error) {
	digest, err := domain.NormalizeDigest(digest)
	if err != nil {
		return nil, err
	}
	items, err := r.ListBatches()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].MediaSHA256 == digest {
			return &items[i], nil
		}
	}
	return nil, nil
}

func (r *Repository) SaveBatch(batch *domain.RecordingBatch) error {
	if batch == nil {
		return errors.New("批次不能为空")
	}
	if err := batch.ValidateSnapshot(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return atomicJSON(filepath.Join(r.batchDir, safeName(batch.BatchID)+".json"), batch)
}

func (r *Repository) LoadBatch(id string) (*domain.RecordingBatch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var b domain.RecordingBatch
	if err := readJSON(filepath.Join(r.batchDir, safeName(id)+".json"), &b); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("批次 %s 不存在", id)
		}
		return nil, err
	}
	if b.BatchID != id {
		return nil, errors.New("快照标识与文件名不一致")
	}
	return &b, b.ValidateSnapshot()
}

func (r *Repository) ListBatches() ([]domain.RecordingBatch, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries, err := os.ReadDir(r.batchDir)
	if err != nil {
		return nil, err
	}
	out := make([]domain.RecordingBatch, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var b domain.RecordingBatch
		if readJSON(filepath.Join(r.batchDir, e.Name()), &b) == nil && b.ValidateSnapshot() == nil {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].BatchID < out[j].BatchID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}
