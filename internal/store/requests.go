package store

import (
	"errors"
	"field-voice-archive/internal/domain"
	"os"
	"path/filepath"
)

func (r *Repository) SaveRequest(rec IdempotencyRecord) error {
	if rec.RequestID == "" || rec.Fingerprint == "" || len(rec.Response) == 0 {
		return errors.New("幂等记录不完整")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.requestDir, safeName(rec.RequestID)+".json")
	if _, err := os.Stat(path); err == nil {
		return errors.New("request_id 已存在")
	}
	return atomicJSON(path, rec)
}

func (r *Repository) LoadRequest(id string) (*IdempotencyRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var rec IdempotencyRecord
	err := readJSON(filepath.Join(r.requestDir, safeName(id)+".json"), &rec)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *Repository) SaveManifest(batchID string, manifest domain.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return atomicJSON(filepath.Join(r.manifestDir, safeName(batchID)+".json"), manifest)
}

func (r *Repository) LoadManifest(batchID string) (*domain.Manifest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var m domain.Manifest
	err := readJSON(filepath.Join(r.manifestDir, safeName(batchID)+".json"), &m)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return &m, err
}
