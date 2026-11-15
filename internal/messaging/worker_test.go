package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/storage"
	amqp "github.com/rabbitmq/amqp091-go"
)

type matcherFunc func(context.Context, string) (domain.Request, error)

func (f matcherFunc) Match(c context.Context, id string) (domain.Request, error) { return f(c, id) }

type ackRecorder struct{ ack, reject, requeue bool }

func (a *ackRecorder) Ack(uint64, bool) error { a.ack = true; return nil }
func (a *ackRecorder) Nack(_ uint64, _ bool, requeue bool) error {
	a.reject = true
	a.requeue = requeue
	return nil
}
func (a *ackRecorder) Reject(_ uint64, requeue bool) error {
	a.reject = true
	a.requeue = requeue
	return nil
}
func TestDeliveryAcknowledgesTerminalOutcomes(t *testing.T) {
	for _, matchErr := range []error{nil, domain.ErrNoMatch, domain.ErrInvalid, storage.ErrNotFound} {
		a := &ackRecorder{}
		err := handleDelivery(context.Background(), matcherFunc(func(_ context.Context, id string) (domain.Request, error) {
			if id != "req-1" {
				t.Fatalf("id=%q", id)
			}
			return domain.Request{}, matchErr
		}), amqp.Delivery{Acknowledger: a, Body: []byte(`{"id":"job-1","request_id":"req-1"}`)}, time.Millisecond)
		if err != nil || !a.ack || a.reject {
			t.Fatalf("error=%v ack=%+v", err, a)
		}
	}
}
func TestDeliveryRejectsPoisonWithoutMatching(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"request_id":" "}`, `{"request_id":"req"} garbage`} {
		a := &ackRecorder{}
		err := handleDelivery(context.Background(), matcherFunc(func(context.Context, string) (domain.Request, error) {
			t.Fatal("matched poison")
			return domain.Request{}, nil
		}), amqp.Delivery{Acknowledger: a, Body: []byte(body)}, time.Millisecond)
		if err != nil || !a.reject || a.requeue || a.ack {
			t.Fatalf("body=%s err=%v ack=%+v", body, err, a)
		}
	}
}
func TestDeliveryRequeuesTransientFailureAfterDelay(t *testing.T) {
	a := &ackRecorder{}
	start := time.Now()
	err := handleDelivery(context.Background(), matcherFunc(func(context.Context, string) (domain.Request, error) {
		return domain.Request{}, errors.New("coordinator unavailable")
	}), amqp.Delivery{Acknowledger: a, Body: []byte(`{"id":"job","request_id":"req"}`)}, 30*time.Millisecond)
	if err != nil || !a.reject || !a.requeue || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("err=%v ack=%+v elapsed=%v", err, a, time.Since(start))
	}
}
func TestCancellationLeavesDeliveryUnacknowledged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &ackRecorder{}
	err := handleDelivery(ctx, matcherFunc(func(context.Context, string) (domain.Request, error) { return domain.Request{}, context.Canceled }), amqp.Delivery{Acknowledger: a, Body: []byte(`{"id":"job","request_id":"req"}`)}, time.Hour)
	if !errors.Is(err, context.Canceled) || a.ack || a.reject {
		t.Fatalf("err=%v ack=%+v", err, a)
	}
}
