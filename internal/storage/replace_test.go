package storage

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestReplaceSnapshotExactDurableAndDetached(t *testing.T) {
	s, path := openTestStore(t)
	if _, err := s.Put("old", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}
	records := []Record{{Key: "live", Value: json.RawMessage(`{"n":2}`), Version: 8}, {Key: "gone", Version: 9, Deleted: true}}
	if err := s.ReplaceSnapshot(records); err != nil {
		t.Fatal(err)
	}
	records[0].Value[0] = 'x'
	records[1].Version = 10
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{{Key: "gone", Version: 9, Deleted: true}, {Key: "live", Value: json.RawMessage(`{"n":2}`), Version: 8}}
	if !reflect.DeepEqual(s.Snapshot(), want) || !reflect.DeepEqual(reopened.Snapshot(), want) {
		t.Fatalf("replacement differs: %+v / %+v", s.Snapshot(), reopened.Snapshot())
	}
	if err := s.ReplaceSnapshot(nil); err != nil {
		t.Fatal(err)
	}
	reopened, err = Open(path)
	if err != nil || len(reopened.Snapshot()) != 0 {
		t.Fatalf("empty snapshot: %v", err)
	}
}

func TestReplaceSnapshotRejectsInvalidWithoutChanges(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.Put("old", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	for _, records := range [][]Record{
		{{Key: "x", Version: 1, Value: json.RawMessage(`1`)}, {Key: "x", Version: 2, Deleted: true}},
		{{Key: " ", Version: 1, Deleted: true}}, {{Key: "x", Version: 0, Deleted: true}},
		{{Key: "x", Version: 1, Value: json.RawMessage(`{`)}}, {{Key: "x", Version: 1, Deleted: true, Value: json.RawMessage(`null`)}},
	} {
		if err := s.ReplaceSnapshot(records); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v: %v", records, err)
		}
		if !reflect.DeepEqual(before, s.Snapshot()) {
			t.Fatal("rejected replacement changed memory")
		}
	}
}

func TestReplaceSnapshotDiskFailureKeepsMemory(t *testing.T) {
	s, path := openTestStore(t)
	if _, err := s.Put("old", json.RawMessage(`1`), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceSnapshot([]Record{{Key: "new", Version: 4, Deleted: true}}); err == nil {
		t.Fatal("disk failure accepted")
	}
	got, err := s.Get("old")
	if err != nil || got.Version != 1 {
		t.Fatalf("old data lost: %+v %v", got, err)
	}
	if len(s.Snapshot()) != 1 {
		t.Fatal("partial replacement")
	}
}
