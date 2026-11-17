package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/application"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/config"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("request-api", flag.ContinueOnError)
	addr := flags.String("addr", "127.0.0.1:8080", "HTTP listen address")
	web := flags.String("web", "web/dist", "compiled frontend directory")
	if e := flags.Parse(args); e != nil {
		return e
	}
	base := os.Getenv("COORDINATOR_URL")
	if base == "" {
		base = "http://127.0.0.1:8081"
	}
	maxDistance, e := config.MaxMatchDistanceMeters()
	if e != nil {
		return e
	}
	service, e := application.New(base, os.Getenv("INTERNAL_TOKEN"), application.WithMaxDistanceMeters(maxDistance))
	if e != nil {
		return fmt.Errorf("invalid coordinator URL or internal token")
	}
	api := service.Handler()
	static := http.FileServer(http.Dir(*web))
	hasUI := false
	if info, e := os.Stat(filepath.Join(*web, "index.html")); e == nil && !info.IsDir() {
		hasUI = true
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/requests") || strings.HasPrefix(r.URL.Path, "/resources") || strings.HasPrefix(r.URL.Path, "/match/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/config" {
			api.ServeHTTP(w, r)
			return
		}
		if !hasUI {
			api.ServeHTTP(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		static.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	listener, e := net.Listen("tcp", *addr)
	if e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case e = <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx, os.Args[1:]); e != nil {
		log.Fatal(e)
	}
}
