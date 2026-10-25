# Security and operational limits

This is a local academic demonstration. Public request/resource APIs have no user authentication or ownership model and must not be exposed to the Internet as a real emergency service.

- Public browser traffic goes through the frontend proxy. The coordinator requires a random internal token. Credentials stay in ignored `.env` locally and Kubernetes Secrets in the isolated demo cluster; the frontend contains no secrets.
- Default Compose publishes the UI, database and management ports only on loopback. The optional testing override publishes the coordinator only on loopback and retains token validation. Remove that override when integration testing is finished.
- SQL uses parameters for application values. Types, coordinates, lengths and transitions are validated. A resource cannot be made available while assigned. Stale matching tokens are rejected.
- Kubernetes app containers run as non-root, drop capabilities and reject privilege escalation. Pod resource requests/limits and health probes are configured.
- The supplied coordinator NetworkPolicy requires a network-policy-capable CNI to enforce network isolation. Default kindnet does not provide that enforcement; the internal token remains enforced by the application. Use a suitable CNI before relying on this policy as a security boundary.
- Flyway and the coordinator currently share the database owner account in this demo. Separate migration and runtime roles, transport TLS, externally managed secrets, authenticated public APIs, rate limits and dependency/image scanning are needed for deployment beyond localhost.
- One RabbitMQ instance and one coordinator are single points of failure. Durable outbox records survive broker outages, but requests cannot be served while all database members for their shard are unavailable.
- PostgreSQL replication is asynchronous. A primary failure may lose recently committed data not yet replicated. Tests on this laptop do not establish zero data loss or production availability guarantees.
- Assignment recovery is resume-forward. Durable candidate selection survives an interrupted remote write; the next owner retries that same allocation. Cancellation is restricted to PENDING, so no worker releases resources solely because a lease expired.
- Replica fallback is limited to single-record public reads and labels potentially stale results. Listing, candidate search, reservations and writes use primaries; incomplete search never becomes a false NO_RESOURCE outcome.
- A single-node kind cluster shares the laptop's failure domain. It demonstrates pod/database-member recovery, not tolerance of the laptop or storage disk failing.
- The member reporter's `lagBytes` is received-but-not-replayed WAL on a standby, not the full primary-to-standby network lag. A healthy probe does not authorize promotion; CNPG owns election and fencing.
- Per-shard pg_dump/restore proves individual backups can be restored. It does not produce a coordinated cross-shard point-in-time snapshot.
