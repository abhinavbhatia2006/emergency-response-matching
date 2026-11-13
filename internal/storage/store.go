// Package storage provides durable PostgreSQL records and transactional CAS.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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

// Store owns a connection pool; PostgreSQL is the sole authoritative state.
type Store struct{ pool *pgxpool.Pool }

func validKey(key string) bool {
	return strings.TrimSpace(key) != "" && utf8.ValidString(key) && len(key) <= 512 && !strings.ContainsRune(key, 0)
}

// Open accepts a PostgreSQL connection URI or libpq key/value connection string.
func Open(dsn string) (*Store, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" || (!strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") && !strings.Contains(dsn, "=")) {
		return nil, fmt.Errorf("%w: PostgreSQL DSN required", ErrInvalid)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed PostgreSQL configuration", ErrInvalid)
	}
	cfg.MaxConns = 4
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Store, error) { pool.Close(); return nil, err }
	if err := pool.Ping(ctx); err != nil {
		return fail(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fail(err)
	}
	defer rollback(tx)
	// Serialize first-time schema initialization across processes. The lock is
	// transaction-scoped; normal record mutations use the table lock below.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database() || ':' || current_schema() || ':records', 0))`); err != nil {
		rollback(tx)
		return fail(err)
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS records (
 key text PRIMARY KEY CHECK (octet_length(key) BETWEEN 1 AND 512 AND key ~ '[^[:space:]]'),
 value jsonb,
 version bigint NOT NULL CHECK (version > 0),
 deleted boolean NOT NULL DEFAULT false,
 CHECK ((deleted AND value IS NULL) OR (NOT deleted AND value IS NOT NULL))
 )`); err != nil {
		rollback(tx)
		return fail(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fail(err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func scan(row pgx.Row) (Record, error) {
	var r Record
	var version int64
	err := row.Scan(&r.Key, &r.Value, &version, &r.Deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	r.Version = uint64(version)
	return r, nil
}

func (s *Store) Get(key string) (Record, error) {
	if !validKey(key) {
		return Record{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return scan(s.pool.QueryRow(ctx, `SELECT key,value,version,deleted FROM records WHERE key=$1 AND NOT deleted`, key))
}

// Records reads a single MVCC statement snapshot, including tombstones.
func (s *Store) Records() ([]Record, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT key,value,version,deleted FROM records ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Record, 0)
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// beginMutation serializes writers across all pools/processes while allowing
// MVCC readers to see the previous committed record set until commit.
func (s *Store) beginMutation(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE records IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func inputError(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && (pe.Code == "22P02" || pe.Code == "22P05" || pe.Code == "22003" || pe.Code == "22021" || pe.Code == "23514" || pe.Code == "23505") {
		return fmt.Errorf("%w: %s", ErrInvalid, pe.Message)
	}
	return err
}

// Put creates at expected=0 or updates the exact version, including tombstones.
func (s *Store) Put(key string, value json.RawMessage, expected uint64) (Record, error) {
	if !validKey(key) || !json.Valid(value) || expected > math.MaxInt64 {
		return Record{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return Record{}, err
	}
	defer rollback(tx)
	old, err := scan(tx.QueryRow(ctx, `SELECT key,value,version,deleted FROM records WHERE key=$1`, key))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Record{}, err
	}
	if old.Version != expected || expected == math.MaxInt64 {
		return Record{}, ErrConflict
	}
	r, err := scan(tx.QueryRow(ctx, `INSERT INTO records(key,value,version,deleted) VALUES($1,$2,$3,false)
 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,version=EXCLUDED.version,deleted=false
 RETURNING key,value,version,deleted`, key, []byte(value), int64(expected+1)))
	if err != nil {
		return Record{}, inputError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return r, nil
}

func (s *Store) Delete(key string, expected uint64) (Record, error) {
	if !validKey(key) || expected > math.MaxInt64 {
		return Record{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return Record{}, err
	}
	defer rollback(tx)
	old, err := scan(tx.QueryRow(ctx, `SELECT key,value,version,deleted FROM records WHERE key=$1`, key))
	if err != nil {
		return Record{}, err
	}
	if old.Deleted {
		return Record{}, ErrNotFound
	}
	if old.Version != expected || expected == math.MaxInt64 {
		return Record{}, ErrConflict
	}
	r, err := scan(tx.QueryRow(ctx, `UPDATE records SET value=NULL,version=$2,deleted=true WHERE key=$1 RETURNING key,value,version,deleted`, key, int64(expected+1)))
	if err != nil {
		return Record{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return r, nil
}

// ReplaceRecords atomically installs an authoritative set with exact versions.
// Missing keys are removed. Callers must establish replication authority.
func (s *Store) ReplaceRecords(records []Record) error {
	seen := make(map[string]struct{}, len(records))
	for _, r := range records {
		if !validKey(r.Key) || r.Version == 0 || r.Version > math.MaxInt64 || (!r.Deleted && !json.Valid(r.Value)) || (r.Deleted && len(r.Value) != 0) {
			return ErrInvalid
		}
		if _, ok := seen[r.Key]; ok {
			return ErrInvalid
		}
		seen[r.Key] = struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.beginMutation(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `DELETE FROM records`); err != nil {
		return err
	}
	for _, r := range records {
		var value any
		if !r.Deleted {
			value = []byte(r.Value)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO records(key,value,version,deleted) VALUES($1,$2,$3,$4)`, r.Key, value, int64(r.Version), r.Deleted); err != nil {
			return inputError(err)
		}
	}
	return tx.Commit(ctx)
}

// Rollback gets its own deadline because the operation context may be expired.
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// NormalizeJSON validates with PostgreSQL's JSONB parser before a caller sizes
// a replication envelope. SQL NULL signals a value larger than the byte limit.
func (s *Store) NormalizeJSON(value json.RawMessage, maxBytes int) (json.RawMessage, error) {
	if maxBytes <= 0 || !json.Valid(value) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var normalized []byte
	err := s.pool.QueryRow(ctx, `SELECT CASE WHEN octet_length($1::jsonb::text) <= $2 THEN $1::jsonb::text ELSE NULL END`, []byte(value), maxBytes).Scan(&normalized)
	if err != nil {
		return nil, inputError(err)
	}
	if normalized == nil {
		return nil, ErrInvalid
	}
	return json.RawMessage(normalized), nil
}
