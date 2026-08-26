package store

import (
	"encoding/json"
	"errors"
	"field-voice-archive/internal/domain"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (r *Repository) recoverSnapshots() error {
	entries, err := os.ReadDir(r.batchDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(r.batchDir, e.Name())
		var b domain.RecordingBatch
		if err := readJSON(path, &b); err != nil || b.ValidateSnapshot() != nil || safeName(b.BatchID)+".json" != e.Name() {
			target := filepath.Join(r.quarantine, e.Name()+".corrupt")
			if renameErr := os.Rename(path, target); renameErr != nil {
				return renameErr
			}
		}
	}
	return nil
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pending-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	keep := false
	defer func() {
		tmp.Close()
		if !keep {
			os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return err
	}
	keep = true
	return nil
}

func readJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 16<<20))
	dec.DisallowUnknownFields()
	if err = dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return errors.New("JSON 文件包含多余内容")
	}
	return nil
}

func safeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "invalid"
	}
	return b.String()
}
