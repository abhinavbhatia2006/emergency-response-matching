package messaging

import (
	"context"
	"errors"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/application"
	"testing"
)

type outboxStub struct {
	jobs   []application.Job
	acked  []application.Job
	ackErr error
}

func (o *outboxStub) Jobs(context.Context) ([]application.Job, error) { return o.jobs, nil }
func (o *outboxStub) AckJob(_ context.Context, j application.Job) error {
	o.acked = append(o.acked, j)
	return o.ackErr
}

type publishFunc func(context.Context, application.Job) error

func (f publishFunc) Publish(c context.Context, j application.Job) error { return f(c, j) }
func TestOutboxRetainsJobUntilConfirmed(t *testing.T) {
	for _, fail := range []bool{true, false} {
		job := application.Job{ID: "job-1", RequestID: "req-1", Version: 7}
		o := &outboxStub{jobs: []application.Job{job}}
		err := publishPending(context.Background(), o, publishFunc(func(_ context.Context, got application.Job) error {
			if len(o.acked) != 0 {
				t.Fatal("deleted before confirmation")
			}
			if got != job {
				t.Fatalf("job=%+v", got)
			}
			if fail {
				return errors.New("confirmation lost")
			}
			return nil
		}))
		if fail {
			if err == nil || len(o.acked) != 0 {
				t.Fatalf("error=%v acknowledgments=%v", err, o.acked)
			}
		} else {
			if err != nil || len(o.acked) != 1 || o.acked[0] != job {
				t.Fatalf("error=%v acknowledgments=%v", err, o.acked)
			}
		}
	}
}
func TestOutboxStopsOnAcknowledgementFailure(t *testing.T) {
	o := &outboxStub{jobs: []application.Job{{ID: "one"}, {ID: "two"}}, ackErr: errors.New("CAS unavailable")}
	count := 0
	err := publishPending(context.Background(), o, publishFunc(func(context.Context, application.Job) error { count++; return nil }))
	if err == nil || count != 1 {
		t.Fatalf("error=%v publishes=%d", err, count)
	}
}
