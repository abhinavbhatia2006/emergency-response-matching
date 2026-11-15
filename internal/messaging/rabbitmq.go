package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/abhinavbhatia2006/emergency-response-matching/internal/application"
	amqp "github.com/rabbitmq/amqp091-go"
)

const ioTimeout = 10 * time.Second
const DefaultQueue = "matching.requests"

// AMQP's context publish API does not interrupt socket writes. Deadlines and
// closing the socket on cancellation bound handshake, writes, and shutdown.
type deadlineConn struct{ net.Conn }

func (c deadlineConn) Write(b []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(ioTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

type session struct {
	ch      *amqp.Channel
	conn    *amqp.Connection
	raw     net.Conn
	stop    func() bool
	queue   string
	returns <-chan amqp.Return
}

func connect(ctx context.Context, url, queue string) (*session, error) {
	s := &session{queue: queue}
	conn, err := amqp.DialConfig(url, amqp.Config{Heartbeat: 5 * time.Second, Locale: "en_US", Dial: func(network, address string) (net.Conn, error) {
		raw, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		s.raw = raw
		s.stop = context.AfterFunc(ctx, func() { raw.Close() })
		// The library clears this handshake deadline after connection negotiation,
		// then uses negotiated heartbeat read deadlines for the connection lifetime.
		if err := raw.SetDeadline(time.Now().Add(ioTimeout)); err != nil {
			raw.Close()
			return nil, err
		}
		return deadlineConn{raw}, nil
	}})
	if err != nil {
		s.close()
		return nil, err
	}
	s.conn = conn
	ch, err := conn.Channel()
	if err != nil {
		s.close()
		return nil, err
	}
	s.ch = ch
	if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		s.close()
		return nil, err
	}
	if err = ch.Confirm(false); err != nil {
		s.close()
		return nil, err
	}
	s.returns = ch.NotifyReturn(make(chan amqp.Return, 1))
	return s, nil
}
func (s *session) close() {
	if s.stop != nil {
		s.stop()
	}
	if s.raw != nil {
		s.raw.Close()
	}
}
func (s *session) Publish(ctx context.Context, job application.Job) error {
	ctx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	confirm, err := s.ch.PublishWithDeferredConfirmWithContext(ctx, "", s.queue, true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: job.ID, Body: body, Timestamp: time.Now().UTC()})
	if err != nil {
		return err
	}
	if confirm == nil {
		return errors.New("publisher confirms unavailable")
	}
	ok, err := confirm.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("broker negatively acknowledged publish")
	}
	// RabbitMQ emits basic.return before its confirmation. Both notifications
	// have buffered channels, so this check cannot lose an unroutable message.
	select {
	case returned, open := <-s.returns:
		if !open {
			return errors.New("publish channel closed")
		}
		return fmt.Errorf("unroutable publish: %d %s", returned.ReplyCode, returned.ReplyText)
	default:
		return nil
	}
}
