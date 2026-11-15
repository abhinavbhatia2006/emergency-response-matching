package storage

import (
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
)

func TestApplyAtomicRollbackAndValidation(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.Put("existing", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		mutations []Mutation
		want      error
	}{
		{"conflict", []Mutation{{Key: "new", Value: json.RawMessage(`1`)}, {Key: "existing", Value: json.RawMessage(`2`)}}, ErrConflict},
		{"missing delete", []Mutation{{Key: "new", Value: json.RawMessage(`1`)}, {Key: "missing", Delete: true, Expected: 1}}, ErrNotFound},
		{"SQL invalid", []Mutation{{Key: "new", Value: json.RawMessage(`1`)}, {Key: "invalid", Value: json.RawMessage(`"\u0000"`)}}, ErrInvalid},
		{"duplicate", []Mutation{{Key: "new", Value: json.RawMessage(`1`)}, {Key: "new", Value: json.RawMessage(`2`)}}, ErrInvalid},
		{"overflow", []Mutation{{Key: "new", Value: json.RawMessage(`1`), Expected: math.MaxUint64}}, ErrInvalid},
		{"empty", nil, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Apply(tc.mutations); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			all := records(t, s)
			if len(all) != 1 || all[0].Version != 1 {
				t.Fatalf("partial batch: %+v", all)
			}
		})
	}
	got, err := s.Apply([]Mutation{{Key: "new", Value: json.RawMessage(`{"n":1e2}`)}, {Key: "existing", Expected: 1, Delete: true}})
	if err != nil || len(got) != 2 || got[0].Version != 1 || got[1].Version != 2 || !got[1].Deleted {
		t.Fatalf("batch: %+v %v", got, err)
	}
}

func TestApplyConcurrentPoolsSingleWinner(t *testing.T) {
	s, dsn := openTestStore(t)
	second := openAt(t, dsn)
	if _, err := s.Put("resource", json.RawMessage(`0`), 0); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pool := s
			if i%2 == 0 {
				pool = second
			}
			_, err := pool.Apply([]Mutation{{Key: "resource", Value: json.RawMessage(`1`), Expected: 1}, {Key: "assignment", Value: json.RawMessage(`1`)}})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if wins != 1 || len(records(t, s)) != 2 {
		t.Fatalf("wins=%d records=%+v", wins, records(t, s))
	}
}
