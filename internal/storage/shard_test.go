package storage

import (
	"fmt"
	"testing"
)

func TestShardRouting(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 300; i++ {
		key := fmt.Sprintf("request/%d", i)
		shard, err := Shard(key, 3)
		if err != nil || shard < 0 || shard >= 3 {
			t.Fatalf("invalid shard %d: %v", shard, err)
		}
		again, _ := Shard(key, 3)
		if again != shard {
			t.Fatal("unstable routing")
		}
		seen[shard] = true
	}
	if len(seen) != 3 {
		t.Fatalf("not distributed across three shards: %v", seen)
	}
	// Independently calculated FNV-1a 32-bit test vectors, modulo three.
	for _, tc := range []struct {
		key  string
		want int
	}{{"a", 1}, {"b", 1}, {"hello", 0}} {
		got, err := Shard(tc.key, 3)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %d want %d, %v", tc.key, got, tc.want, err)
		}
	}
	for _, n := range []int{0, -1} {
		if _, err := Shard("key", n); err == nil {
			t.Fatal("invalid shard count accepted")
		}
	}
	if _, err := Shard("", 3); err == nil {
		t.Fatal("empty key accepted")
	}
}
