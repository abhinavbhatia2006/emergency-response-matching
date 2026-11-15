package messaging

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// RunPublisher polls the transactional outbox; deleting a job requires a
// positive broker confirmation. Reconnects may publish duplicates safely.
func RunPublisher(ctx context.Context, url, queue string, outbox Outbox) error {
	return reconnect(ctx, time.Second, func() error {
		s, err := connect(ctx, url, queue)
		if err != nil {
			return err
		}
		defer s.close()
		for {
			if err := publishPending(ctx, outbox, s); err != nil {
				return err
			}
			if err := pause(ctx, 500*time.Millisecond); err != nil {
				return err
			}
		}
	})
}

// RunConsumer owns its connection, uses manual acknowledgments and limits
// outstanding work to one delivery. Cancellation closes the socket, causing
// RabbitMQ to requeue any delivery whose outcome was not acknowledged.
func RunConsumer(ctx context.Context, url, queue string, matcher Matcher) error {
	return reconnect(ctx, time.Second, func() error {
		s, err := connect(ctx, url, queue)
		if err != nil {
			return err
		}
		defer s.close()
		if err := s.ch.Qos(1, 0, false); err != nil {
			return err
		}
		deliveries, err := s.ch.Consume(queue, "", false, false, false, false, nil)
		if err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case d, ok := <-deliveries:
				if !ok {
					return errors.New("broker delivery stream closed")
				}
				if err := handleDelivery(ctx, matcher, d, 2*time.Second); err != nil {
					return err
				}
			}
		}
	})
}

func reconnect(ctx context.Context, initial time.Duration, run func() error) error {
	delay := initial
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		err := run()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("matching worker session interrupted; retrying", "error", err, "delay", delay)
		if time.Since(started) > 30*time.Second {
			delay = initial
		}
		if err := pause(ctx, delay); err != nil {
			return err
		}
		delay *= 2
		if delay > 15*time.Second {
			delay = 15 * time.Second
		}
	}
}
