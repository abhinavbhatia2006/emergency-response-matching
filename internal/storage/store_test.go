package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/testdb"
	"github.com/jackc/pgx/v5"
	"math"
	"reflect"
	"sync"
	"testing"
)

func openAt(t *testing.T, dsn string) *Store {
	t.Helper()
	s, e := Open(dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dsn := testdb.DSN(t)
	return openAt(t, dsn), dsn
}
func records(t *testing.T, s *Store) []Record {
	t.Helper()
	r, e := s.Records()
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestPersistenceAndTombstones(t *testing.T) {
	s, dsn := openTestStore(t)
	r, e := s.Put("request/1", json.RawMessage(`{"n":9007199254740993}`), 0)
	if e != nil || r.Version != 1 {
		t.Fatalf("put: %+v %v", r, e)
	}
	s.Close()
	s = openAt(t, dsn)
	r, e = s.Get("request/1")
	if e != nil || string(r.Value) != `{"n": 9007199254740993}` {
		t.Fatalf("reopen: %+v %v", r, e)
	}
	r, e = s.Delete("request/1", 1)
	if e != nil || r.Version != 2 || !r.Deleted {
		t.Fatalf("delete: %+v %v", r, e)
	}
	s.Close()
	s = openAt(t, dsn)
	if _, e = s.Get("request/1"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := records(t, s); len(got) != 1 || !got[0].Deleted || got[0].Version != 2 {
		t.Fatal(got)
	}
	if _, e = s.Put("request/1", json.RawMessage(`{}`), 0); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if r, e = s.Put("request/1", json.RawMessage(`{}`), 2); e != nil || r.Version != 3 {
		t.Fatalf("resurrection: %+v %v", r, e)
	}
}

func TestConcurrentCompareAndSwapAcrossPools(t *testing.T) {
	s, dsn := openTestStore(t)
	other := openAt(t, dsn)
	if _, e := s.Put("k", json.RawMessage(`0`), 0); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pool := s
			if i%2 == 0 {
				pool = other
			}
			_, e := pool.Put("k", json.RawMessage(`1`), 1)
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatalf("%d CAS winners", wins)
	}
	r, e := other.Get("k")
	if e != nil || r.Version != 2 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestInvalidInputsAndVersionLimits(t *testing.T) {
	s, _ := openTestStore(t)
	for _, tc := range []struct{ k, v string }{{"", "{}"}, {" ", "{}"}, {"k", "{"}, {"k", `"\u0000"`}, {"k", "1e1000000"}, {"a\x00b", "{}"}} {
		if _, e := s.Put(tc.k, json.RawMessage(tc.v), 0); !errors.Is(e, ErrInvalid) {
			t.Fatalf("%q %q: %v", tc.k, tc.v, e)
		}
	}
	if _, e := s.Put("k", json.RawMessage(`1`), math.MaxUint64); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if e := s.ReplaceRecords([]Record{{Key: "max", Version: math.MaxInt64, Value: json.RawMessage(`null`)}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Put("max", json.RawMessage(`2`), math.MaxInt64); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e := s.Delete("max", math.MaxInt64); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e := s.Delete("missing", 0); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestClosedStorePropagatesDatabaseErrors(t *testing.T) {
	s, _ := openTestStore(t)
	s.Close()
	_, a := s.Get("k")
	_, b := s.Records()
	_, c := s.Put("k", json.RawMessage(`1`), 0)
	_, d := s.Delete("k", 0)
	e := s.ReplaceRecords(nil)
	for _, err := range []error{a, b, c, d, e} {
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
			t.Fatalf("database failure misclassified: %v", err)
		}
	}
}

func TestSchemaConstraints(t *testing.T) {
	_, dsn := openTestStore(t)
	c, e := pgx.Connect(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close(context.Background())
	for _, sql := range []string{`INSERT INTO records VALUES ('zero','{}',0,false)`, `INSERT INTO records VALUES ('negative','{}',-1,false)`, `INSERT INTO records VALUES ('live',NULL,1,false)`, `INSERT INTO records VALUES ('dead','null',1,true)`, `INSERT INTO records VALUES ('','{}',1,false)`} {
		if _, e := c.Exec(context.Background(), sql); e == nil {
			t.Fatalf("constraint accepted %s", sql)
		}
	}
}

func TestReplaceRecordsExactAndRollback(t *testing.T) {
	s, dsn := openTestStore(t)
	if _, e := s.Put("old", json.RawMessage(`1`), 0); e != nil {
		t.Fatal(e)
	}
	input := []Record{{Key: "live", Value: json.RawMessage(`{"n":2}`), Version: 8}, {Key: "gone", Version: 9, Deleted: true}}
	if e := s.ReplaceRecords(input); e != nil {
		t.Fatal(e)
	}
	input[0].Value[0] = 'x'
	want := []Record{{Key: "gone", Version: 9, Deleted: true}, {Key: "live", Version: 8, Value: json.RawMessage(`{"n": 2}`)}}
	if got := records(t, openAt(t, dsn)); !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	for _, bad := range [][]Record{
		{{Key: "x", Version: 1, Value: json.RawMessage(`1`)}, {Key: "x", Version: 2, Deleted: true}},
		{{Key: " ", Version: 1, Deleted: true}}, {{Key: "x", Version: 0, Deleted: true}}, {{Key: "x", Version: math.MaxUint64, Deleted: true}},
		{{Key: "x", Version: 1, Value: json.RawMessage(`{`)}}, {{Key: "x", Version: 1, Deleted: true, Value: json.RawMessage(`null`)}},
		{{Key: "valid", Version: 1, Value: json.RawMessage(`1`)}, {Key: "invalid-jsonb", Version: 1, Value: json.RawMessage(`"\u0000"`)}},
	} {
		if e := s.ReplaceRecords(bad); !errors.Is(e, ErrInvalid) {
			t.Fatalf("bad replacement: %v", e)
		}
		if !reflect.DeepEqual(records(t, s), want) {
			t.Fatal("failed replacement changed database")
		}
	}
	if e := s.ReplaceRecords(nil); e != nil {
		t.Fatal(e)
	}
	if len(records(t, s)) != 0 {
		t.Fatal("empty replacement failed")
	}
}

func TestReplacementReadersSeeOnlyCompleteSets(t *testing.T) {
	s, dsn := openTestStore(t)
	reader := openAt(t, dsn)
	makeSet := func(version uint64) []Record {
		set := make([]Record, 100)
		for i := range set {
			set[i] = Record{Key: fmt.Sprintf("k%03d", i), Version: version, Value: json.RawMessage(`1`)}
		}
		return set
	}
	if err := s.ReplaceRecords(makeSet(1)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.ReplaceRecords(makeSet(2)) }()
	for {
		got := records(t, reader)
		if len(got) != 100 {
			t.Fatalf("partial replacement has %d records", len(got))
		}
		version := got[0].Version
		for _, r := range got {
			if r.Version != version {
				t.Fatal("mixed committed generations")
			}
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
	}
}

func TestDatabaseFailuresAreNotEmptyResults(t *testing.T) {
	s, dsn := openTestStore(t)
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	if _, err = c.Exec(context.Background(), `DROP TABLE records`); err != nil {
		t.Fatal(err)
	}
	_, a := s.Get("missing")
	_, b := s.Records()
	_, cErr := s.Put("k", json.RawMessage(`1`), 0)
	_, d := s.Delete("k", 0)
	e := s.ReplaceRecords(nil)
	for _, err := range []error{a, b, cErr, d, e} {
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) {
			t.Fatalf("database failure hidden: %v", err)
		}
	}
}
