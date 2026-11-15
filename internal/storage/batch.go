package storage

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"
)

// MaxBatchMutations bounds transaction work for both local and HTTP callers.
const MaxBatchMutations = 256

type Mutation struct {
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
	Expected uint64          `json:"expected_version"`
	Delete   bool            `json:"delete"`
}

// ValidateMutations rejects malformed or ambiguous transactions before any I/O.
func ValidateMutations(mutations []Mutation) error {
	if len(mutations) == 0 || len(mutations) > MaxBatchMutations {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(mutations))
	for _, m := range mutations {
		if !validKey(m.Key) || seen[m.Key] || m.Expected > math.MaxInt64 || (!m.Delete && !json.Valid(m.Value)) || (m.Delete && len(m.Value) > 0 && string(m.Value) != "null") {
			return ErrInvalid
		}
		seen[m.Key] = true
	}
	return nil
}

// Apply commits the entire ordered batch or rolls it back on any CAS, SQL, or
// validation error. The writer lock also serializes batches across pool owners.
func (s *Store) Apply(mutations []Mutation) ([]Record, error) {
	if err := ValidateMutations(mutations); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	result := make([]Record, 0, len(mutations))
	for _, m := range mutations {
		old, err := scan(tx.QueryRow(ctx, `SELECT key,value,version,deleted FROM records WHERE key=$1`, m.Key))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if m.Delete && (errors.Is(err, ErrNotFound) || old.Deleted) {
			return nil, ErrNotFound
		}
		if old.Version != m.Expected || m.Expected == math.MaxInt64 {
			return nil, ErrConflict
		}
		var value any
		if !m.Delete {
			value = []byte(m.Value)
		}
		record, err := scan(tx.QueryRow(ctx, `INSERT INTO records(key,value,version,deleted) VALUES($1,$2,$3,$4)
   ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,version=EXCLUDED.version,deleted=EXCLUDED.deleted
   RETURNING key,value,version,deleted`, m.Key, value, int64(m.Expected+1), m.Delete))
		if err != nil {
			return nil, inputError(err)
		}
		result = append(result, record)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
