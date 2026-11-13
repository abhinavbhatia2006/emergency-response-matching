// Command storage-node runs one durable shard replica.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/node"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv("INTERNAL_TOKEN")); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("INTERNAL_TOKEN is required")
	}
	flags := flag.NewFlagSet("storage-node", flag.ContinueOnError)
	addr := flags.String("addr", ":9100", "HTTP listen address")
	shard := flags.Int("shard", -1, "shard index (0, 1 or 2)")
	coordinator := flags.String("coordinator", "http://localhost:8081", "coordinator base URL")
	id := flags.String("id", "", "configured node ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*id) == "" {
		return errors.New("a node ID and named flags are required")
	}
	store, err := storage.Open(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()
	replica, err := node.New(store, *shard, 3, token)
	if err != nil {
		return fmt.Errorf("node configuration: %w", err)
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: replica.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatResult := make(chan error, 1)
	go func() {
		heartbeatResult <- node.RunHeartbeat(heartbeatCtx, &http.Client{Timeout: 5 * time.Second}, *coordinator, *id, token, 2*time.Second)
	}()
	var result error
	heartbeatDone := false
	select {
	case <-ctx.Done():
	case result = <-serveResult:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	case result = <-heartbeatResult:
		heartbeatDone = true
		if errors.Is(result, context.Canceled) {
			result = nil
		}
	}
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		server.Close()
		if result == nil {
			result = err
		}
	}
	if !heartbeatDone {
		<-heartbeatResult
	}
	return result
}
