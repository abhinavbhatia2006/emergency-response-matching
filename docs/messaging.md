# Matching worker

Run the coordinator and PostgreSQL storage nodes, provision a RabbitMQ account
with access to the selected virtual host, then start the worker:

```sh
export COORDINATOR_URL=http://127.0.0.1:8081
export INTERNAL_TOKEN='<same internal token as coordinator>'
export RABBITMQ_URL='amqp://username:password@127.0.0.1:5672/'
go run ./cmd/matching-worker
```

`COORDINATOR_URL` defaults to `http://127.0.0.1:8081`. `RABBITMQ_URL` is
required. Keep broker credentials and the internal token on the server; the
browser communicates only with the public API. Use `amqps://` when broker
transport needs TLS. The RabbitMQ user must be able to declare, write, and read
the durable `matching.requests` queue.

One process runs two independent connections: a publisher polls the coordinator
outbox every 500 ms, and a manual-acknowledgment consumer matches requests with a
prefetch of one. Messages are persistent, carry the unique outbox job ID as the
AMQP message ID, and use mandatory routing and publisher confirms. The publisher
removes an outbox record only after a positive confirmation and no return.
Unroutable messages, broker failures, and uncertain confirmations retain the
outbox job. A crash between publish and outbox deletion can deliver duplicates;
application matching is idempotent and commits request/resource changes together.

The consumer acknowledges successful and already-processed work, no available
resource, invalid domain transitions, and deleted requests. Resource availability
changes schedule another attempt for pending requests. Malformed JSON, oversized
payloads, and empty identifiers are rejected without requeue (discarded; no dead
letter queue is configured). Transient coordinator failures requeue after two
seconds and retry indefinitely, avoiding a tight retry loop without losing work.
A `rescan` request ID is accepted for application-managed pending-request scans.

Both connections reconnect with exponential delays from one to fifteen seconds.
Socket writes and confirmation waits have ten-second limits, TCP dialing has a
five-second limit, and negotiated heartbeats detect lost connections. SIGINT or
SIGTERM cancels both loops and closes sockets; unacknowledged deliveries return
to the queue. Use a persistent RabbitMQ data volume to preserve durable messages
across broker replacement; a durable queue alone cannot preserve deleted broker
data. This is at-least-once delivery, not exactly-once delivery.

## Tests

```sh
TEST_RABBITMQ_URL='amqp://username:password@127.0.0.1:5672/' go test ./internal/messaging -count=1
```

Without `TEST_RABBITMQ_URL`, broker tests skip and unit tests still run. Broker
tests use temporary queues and cover confirmed persistent publishing, mandatory
returns, unacknowledged redelivery, closed-connection outbox retention, and the
combined publisher/consumer shutdown lifecycle. Unit tests cover acknowledgment
ordering, poison rejection, transient delays, cancellation, and reconnect backoff.
