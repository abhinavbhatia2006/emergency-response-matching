package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestRunRejectsMissingToken(t *testing.T) {
	if err := run(context.Background(), []string{}, ""); err == nil {
		t.Fatal("missing token accepted")
	}
}

func TestRunRegistersAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	registered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { registered <- struct{}{}; cancel() }))
	defer server.Close()
	err := run(ctx, []string{"-addr", "127.0.0.1:0", "-data", filepath.Join(t.TempDir(), "data.json"), "-shard", "0", "-coordinator", server.URL, "-id", "shard-0-primary"}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-registered:
	default:
		t.Fatal("never registered")
	}
}
