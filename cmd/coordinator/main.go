package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/coordinator"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("coordinator stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	addr := flag.String("addr", ":8081", "HTTP listen address")
	data := flag.String("data", "data/coordinator.json", "durable recovery journal path")
	nodesFile := flag.String("nodes", "", "JSON topology configuration path (required)")
	ttl := flag.Duration("heartbeat-timeout", 15*time.Second, "node heartbeat lease")
	flag.Parse()
	if *nodesFile == "" {
		return errors.New("-nodes is required")
	}
	file, err := os.Open(*nodesFile)
	if err != nil {
		return fmt.Errorf("open topology: %w", err)
	}
	defer file.Close()
	var nodes []coordinator.NodeConfig
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&nodes); err != nil {
		return fmt.Errorf("decode topology: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("topology must contain one JSON array")
	}
	journal, err := storage.Open(*data)
	if err != nil {
		return err
	}
	service, err := coordinator.New(journal, nodes, os.Getenv("INTERNAL_TOKEN"), *ttl, nil)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	slog.Info("coordinator listening", "address", listener.Addr().String(), "shards", 3, "nodes", len(nodes))
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
