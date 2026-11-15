// Package messaging delivers durable matching jobs with at-least-once semantics.
package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Matcher interface {
	Match(context.Context, string) (domain.Request, error)
}

func handleDelivery(ctx context.Context, matcher Matcher, d amqp.Delivery, retryDelay time.Duration) error {
	var job struct {
		ID        string `json:"id"`
		RequestID string `json:"request_id"`
	}
	if len(d.Body) > 4096 || json.Unmarshal(d.Body, &job) != nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.RequestID) == "" {
		return d.Reject(false)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := matcher.Match(ctx, job.RequestID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil || errors.Is(err, domain.ErrNoMatch) || errors.Is(err, domain.ErrInvalid) || errors.Is(err, storage.ErrNotFound) {
		return d.Ack(false)
	}
	// Retain transient jobs indefinitely, but pace redelivery to avoid a hot loop.
	if err := pause(ctx, retryDelay); err != nil {
		return err
	}
	return d.Nack(false, true)
}
func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
