package messaging

import (
	"context"
	"errors"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/application"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/domain"
	"sync"
	"testing"
	"time"
)

func TestReconnectRetriesAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	start := time.Now()
	err := reconnect(ctx, time.Millisecond, func() error {
		calls++
		if calls == 3 {
			cancel()
		}
		return errors.New("offline")
	})
	if !errors.Is(err, context.Canceled) || calls != 3 || time.Since(start) < 3*time.Millisecond {
		t.Fatalf("err=%v calls=%d elapsed=%v", err, calls, time.Since(start))
	}
}

type liveOutbox struct {
	mu      sync.Mutex
	pending bool
	ack     chan struct{}
}

func (o *liveOutbox) Jobs(context.Context) ([]application.Job, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.pending {
		return nil, nil
	}
	return []application.Job{{ID: "live-job", RequestID: "live-request", Version: 1}}, nil
}
func (o *liveOutbox) AckJob(context.Context, application.Job) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pending = false
	close(o.ack)
	return nil
}
func TestBrokerPublisherAndConsumerLifecycle(t *testing.T) {
	brokerCtx, url, queue := brokerTest(t)
	ctx, cancel := context.WithCancel(brokerCtx)
	defer cancel()
	cleanup, err := connect(brokerCtx, url, queue)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.close()
	defer cleanup.ch.QueueDelete(queue, false, false, false)
	o := &liveOutbox{pending: true, ack: make(chan struct{})}
	matched := make(chan string, 2)
	done := make(chan error, 2)
	go func() {
		done <- RunConsumer(ctx, url, queue, matcherFunc(func(_ context.Context, id string) (domain.Request, error) {
			matched <- id
			return domain.Request{}, nil
		}))
	}()
	go func() { done <- RunPublisher(ctx, url, queue, o) }()
	select {
	case id := <-matched:
		if id != "live-request" {
			t.Fatal(id)
		}
	case <-ctx.Done():
		t.Fatal("consumer did not receive job")
	}
	select {
	case <-o.ack:
	case <-ctx.Done():
		t.Fatal("outbox not acknowledged")
	}
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not stop")
		}
	}
}
