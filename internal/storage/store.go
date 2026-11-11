// Package storage provides a single-process durable record store.
package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	ErrNotFound = errors.New("record not found")
	ErrConflict = errors.New("record version conflict")
	ErrInvalid  = errors.New("invalid storage input")
)

type Record struct {
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value,omitempty"`
	Version uint64          `json:"version"`
	Deleted bool            `json:"deleted,omitempty"`
}

// Store must be the only owner of its file. It is safe for concurrent goroutines,
// not concurrent processes. All writes persist before becoming visible in memory.
type Store struct {
	mu      sync.RWMutex
	path    string
	records map[string]Record
}

func validKey(key string) bool {
	return strings.TrimSpace(key) != "" && utf8.ValidString(key) && len(key) <= 512
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: empty path", ErrInvalid)
	}
	s := &Store{path: path, records: make(map[string]Record)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	if err := json.Unmarshal(data, &s.records); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if s.records == nil {
		return nil, fmt.Errorf("%w: null snapshot", ErrInvalid)
	}
	for key, r := range s.records {
		if !validKey(key) || r.Key != key || r.Version == 0 || (!r.Deleted && !json.Valid(r.Value)) || (r.Deleted && len(r.Value) != 0) {
			return nil, fmt.Errorf("%w: corrupt record %q", ErrInvalid, key)
		}
	}
	return s, nil
}

func clone(r Record) Record { r.Value = append(json.RawMessage(nil), r.Value...); return r }

func (s *Store) Get(key string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[key]
	if !ok || r.Deleted {
		return Record{}, ErrNotFound
	}
	return clone(r), nil
}

// Put is compare-and-swap: expected=0 creates a new key; updates require the
// current version. Tombstones retain their version and cannot be blindly reused.
func (s *Store) Put(key string, value json.RawMessage, expected uint64) (Record, error) {
	if !validKey(key) || !json.Valid(value) {
		return Record{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.records[key]
	if old.Version != expected {
		return Record{}, ErrConflict
	}
	if expected == math.MaxUint64 {
		return Record{}, fmt.Errorf("%w: version exhausted", ErrConflict)
	}
	return s.commit(Record{Key: key, Value: append(json.RawMessage(nil), value...), Version: expected + 1})
}

func (s *Store) Delete(key string, expected uint64) (Record, error) {
	if !validKey(key) {
		return Record{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.records[key]
	if !ok || old.Deleted {
		return Record{}, ErrNotFound
	}
	if old.Version != expected || expected == math.MaxUint64 {
		return Record{}, ErrConflict
	}
	return s.commit(Record{Key: key, Version: expected + 1, Deleted: true})
}

// Snapshot includes tombstones and is detached from the store's internal memory.
func (s *Store) Snapshot() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		result = append(result, clone(r))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

// Caller holds the write lock. Build a candidate snapshot so any persistence
// failure leaves the currently visible state untouched.
func (s *Store) commit(r Record) (Record, error) {
	next := make(map[string]Record, len(s.records)+1)
	for key, record := range s.records {
		next[key] = record
	}
	next[r.Key] = r
	data, err := json.Marshal(next)
	if err != nil {
		return Record{}, err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Record{}, fmt.Errorf("create data directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return Record{}, err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return Record{}, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Record{}, err
	}
	if err := f.Close(); err != nil {
		return Record{}, err
	}
	if err := os.Rename(temp, s.path); err != nil {
		return Record{}, fmt.Errorf("replace snapshot: %w", err)
	}
	s.records = next
	return clone(r), nil
}
