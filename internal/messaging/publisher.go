package messaging

import (
	"context"
	"github.com/abhinavbhatia2006/emergency-response-matching/internal/application"
)

type Outbox interface {
	Jobs(context.Context) ([]application.Job, error)
	AckJob(context.Context, application.Job) error
}
type publisher interface {
	Publish(context.Context, application.Job) error
}

func publishPending(ctx context.Context, outbox Outbox, p publisher) error {
	jobs, err := outbox.Jobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := p.Publish(ctx, job); err != nil {
			return err
		}
		if err := outbox.AckJob(ctx, job); err != nil {
			return err
		}
	}
	return nil
}
