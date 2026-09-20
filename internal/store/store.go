package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"smartstage/internal/model"
)

const maxConfigBytes = 4 * 1024 * 1024

type Store struct {
	mu   sync.Mutex
	dir  string
	lock *os.File
	good []byte
}

func Open(dir string) (*Store, model.Config, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, model.Config{}, err
	}
	lock, err := lockFile(filepath.Join(dir, "instance.lock"))
	if err != nil {
		return nil, model.Config{}, fmt.Errorf("configuration is in use or cannot be locked: %w", err)
	}
	s := &Store{dir: dir, lock: lock}
	f, err := os.Open(filepath.Join(dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, model.DefaultConfig(), nil
	}
	if err != nil {
		s.Close()
		return nil, model.Config{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	f.Close()
	if err == nil && len(data) > maxConfigBytes {
		err = errors.New("configuration exceeds 4 MiB")
	}
	var config model.Config
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&config)
		if err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				err = errors.New("trailing configuration data")
			}
		}
	}
	if err == nil {
		err = validate(config)
	}
	if err != nil {
		s.Close()
		return nil, model.Config{}, fmt.Errorf("configuration is corrupt or unsupported; original retained at %s; inspect state.json.bak for recovery: %w", filepath.Join(dir, "state.json"), err)
	}
	s.good = data
	return s, config, nil
}

func validate(c model.Config) error {
	if c.Schema != model.SchemaVersion {
		return fmt.Errorf("unsupported schema %d", c.Schema)
	}
	if c.PlaylistRevision == 0 || len(c.Cues) > model.MaxCues {
		return errors.New("invalid playlist revision or cue count")
	}
	ids := map[string]bool{}
	for _, cue := range c.Cues {
		if cue.ID == "" || len(cue.ID) > 128 || ids[cue.ID] || strings.TrimSpace(cue.Label) == "" || len(cue.Label) > 512 || strings.ContainsAny(cue.Label, "\x00\r\n") || !filepath.IsAbs(cue.Path) || len(cue.Path) > 32768 {
			return errors.New("invalid cue identity, label or source path")
		}
		if !model.ValidCueColor(cue.Color) {
			return errors.New("invalid cue color; use empty or #RRGGBB")
		}
		ids[cue.ID] = true
	}
	return nil
}

func (s *Store) Save(config model.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return errors.New("configuration store is closed")
	}
	if err := validate(config); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// A successful save must remain readable on the next launch, including any
	// derived native validation cache. Check before replacing the backup too.
	if len(data) > maxConfigBytes {
		return errors.New("configuration exceeds 4 MiB")
	}
	if len(s.good) > 0 {
		if err := s.write("state.json.bak", s.good); err != nil {
			return fmt.Errorf("save backup: %w", err)
		}
	}
	if err := s.write("state.json", data); err != nil {
		return fmt.Errorf("save configuration: %w", err)
	}
	s.good = data
	return nil
}

func (s *Store) write(name string, data []byte) error {
	f, err := os.CreateTemp(s.dir, ".smartstage-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceFile(temp, filepath.Join(s.dir, name))
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return nil
	}
	err := s.lock.Close()
	s.lock = nil
	return err
}
