# Storage nodes

Each storage node owns one dedicated PostgreSQL database and serves one of three shard
groups. Run two nodes per group with distinct IDs, addresses and database DSNs.
Give each node a separate database; use independent PostgreSQL instances for independent failure domains. The coordinator is the only writer
and sends complete authoritative shard record sets, including tombstones and exact
versions. Replacement removes records absent from the incoming record set.

Build with `go build ./cmd/storage-node`. Set `INTERNAL_TOKEN` to the same nonempty
secret used by the coordinator and `DATABASE_URL` to this node's PostgreSQL DSN (see [database setup](storage.md)), then run, for example:

```text
storage-node -addr :9100 -shard 0 -coordinator http://localhost:8081 -id shard-0-primary
```

Configure this ID, URL and shard in the coordinator topology before starting the
node. The listener starts before registration so the coordinator can reconcile the
node immediately. Registration retries on failure, then heartbeats run every two
seconds. A failed heartbeat is retried; readiness is owned by the coordinator and
requires successful record reconciliation. `/healthz` reports process liveness,
not readiness or ability to accept application writes.

All `/internal/` requests require `Authorization: Bearer <token>`:

| Method | Path | Result |
| --- | --- | --- |
| GET | `/internal/data/{key...}` | Live record, or 404 for absent/deleted data |
| GET | `/internal/records` | Sorted records, including tombstones |
| PUT | `/internal/records` | Replace the shard after durable persistence |

Record replacement requests contain a JSON array, have an 8 MiB body limit, and reject unknown
fields, extra JSON values, duplicate keys, invalid records and keys belonging to
another shard. Empty arrays clear the shard; JSON null is rejected. Errors are JSON.
Record replacement is one database transaction: invalid input or persistence failure leaves the prior rows unchanged. Database read and write failures return 503; absent or deleted records return 404. JSON is the HTTP transfer format, not a persistent file format.

The service uses HTTP deadlines and graceful signal shutdown. Deploy internal
endpoints only on a trusted network; use HTTPS or a secure service network when
traffic crosses untrusted infrastructure. Bearer authentication does not encrypt
traffic. Do not expose node ports to application clients. This full-record-set design
is intended for small datasets; a shard must fit within the request bound.
