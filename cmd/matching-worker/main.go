// matching-worker publishes pending outbox jobs and performs queued matches.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/application"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/messaging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("matching worker stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	url := os.Getenv("COORDINATOR_URL")
	if url == "" {
		url = "http://127.0.0.1:8081"
	}
	token := os.Getenv("INTERNAL_TOKEN")
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		return errors.New("RABBITMQ_URL is required")
	}
	service, err := application.New(url, token)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- messaging.RunPublisher(ctx, rabbitURL, messaging.DefaultQueue, service) }()
	go func() { done <- messaging.RunConsumer(ctx, rabbitURL, messaging.DefaultQueue, service) }()
	slog.Info("matching worker started", "queue", messaging.DefaultQueue)
	first := <-done
	cancel()
	second := <-done
	if first != nil && !errors.Is(first, context.Canceled) {
		return first
	}
	if second != nil && !errors.Is(second, context.Canceled) {
		return second
	}
	return nil
}
