package messaging

import (
	"context"
	"fmt"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/application"
	"os"
	"testing"
	"time"
)

func brokerTest(t *testing.T) (context.Context, string, string) {
	t.Helper()
	url := os.Getenv("TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("set TEST_RABBITMQ_URL for real RabbitMQ tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx, url, fmt.Sprintf("matching.test.%d", time.Now().UnixNano())
}
func TestBrokerPersistentConfirmedMessageAndRedelivery(t *testing.T) {
	ctx, url, queue := brokerTest(t)
	s, err := connect(ctx, url, queue)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	defer s.ch.QueueDelete(queue, false, false, false)
	job := application.Job{ID: "job-1", RequestID: "req-1", Version: 42}
	if err := s.Publish(ctx, job); err != nil {
		t.Fatal(err)
	}
	d, ok, err := s.ch.Get(queue, false)
	if err != nil || !ok {
		t.Fatalf("delivery ok=%v error=%v", ok, err)
	}
	if d.MessageId != "job-1" || d.DeliveryMode != 2 || string(d.Body) != `{"id":"job-1","request_id":"req-1"}` {
		t.Fatalf("delivery=%+v", d)
	}
	if err := d.Nack(false, true); err != nil {
		t.Fatal(err)
	}
	d, ok, err = s.ch.Get(queue, false)
	if err != nil || !ok || !d.Redelivered {
		t.Fatalf("redelivery ok=%v error=%v delivery=%+v", ok, err, d)
	}
	if err := d.Ack(false); err != nil {
		t.Fatal(err)
	}
}
func TestBrokerUnroutablePublishRetainsOutbox(t *testing.T) {
	ctx, url, queue := brokerTest(t)
	s, err := connect(ctx, url, queue)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if _, err := s.ch.QueueDelete(queue, false, false, false); err != nil {
		t.Fatal(err)
	}
	outbox := &outboxStub{jobs: []application.Job{{ID: "job-1", RequestID: "req-1"}}}
	if err := publishPending(ctx, outbox, s); err == nil {
		t.Fatal("unroutable message was treated as delivered")
	}
	if len(outbox.acked) != 0 {
		t.Fatal("unroutable job removed from outbox")
	}
}
func TestBrokerClosedConnectionRetainsOutbox(t *testing.T) {
	ctx, url, queue := brokerTest(t)
	s, err := connect(ctx, url, queue)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, e := connect(ctx, url, queue)
		if e == nil {
			defer cleanup.close()
			cleanup.ch.QueueDelete(queue, false, false, false)
		}
	}()
	s.close()
	outbox := &outboxStub{jobs: []application.Job{{ID: "job-1", RequestID: "req-1"}}}
	if err := publishPending(ctx, outbox, s); err == nil || len(outbox.acked) != 0 {
		t.Fatalf("err=%v acks=%v", err, outbox.acked)
	}
}
