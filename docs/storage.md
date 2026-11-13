# PostgreSQL storage

Each coordinator and storage node owns a separate PostgreSQL database. `Store` reads and writes rows directly through a pgx connection pool; it has no authoritative in-memory map or JSON persistence files. `Open(dsn)` checks connectivity and initializes the records table. `Close()` releases its connections. Startup has a 15-second deadline, database operations have a five-second deadline, and rollback has its own two-second cleanup deadline. Each store uses at most four pooled connections. Legacy local JSON files are neither imported nor deleted.

Records contain a text primary key, a JSONB value, a positive BIGINT version and a deletion flag. SQL NULL values are reserved for tombstones; live JSON null remains a valid JSONB value. Versions above signed BIGINT capacity are rejected.

`Put` and `Delete` use optimistic concurrency. A new record starts at version 1 with expected version 0. Updates require the current version; stale operations return `ErrConflict`. Deletes retain versioned tombstones. Recreating a deleted record requires its tombstone version. Mutations and authoritative `ReplaceRecords` use transactions and table locks to serialize writes even across connections. Replacement removes rows absent from the supplied set, and an invalid replacement leaves the previous set intact. `Records` returns sorted records and database errors explicitly.

JSONB can normalize whitespace, object keys and numeric representations. The coordinator normalizes candidate values in PostgreSQL before checking the 8 MiB shard transfer limit, so exponent expansion cannot commit an unrecoverable journal entry. Replication sends the journal's accepted record version, compares equivalent JSON numbers precisely, and preserves distinct large integers. PostgreSQL WAL and transaction settings govern durability. Persist its data directory, retain normal synchronous commit settings and back up the coordinator database. The coordinator is still a single writer at the cluster level; database CAS does not supply multi-coordinator consensus.

Routing uses FNV-1a (32-bit) over the full UTF-8 key modulo three. Changing the shard count requires migration.

## Local databases

Install Docker Compose, set `POSTGRES_PASSWORD` to a local secret, then start the seven independent PostgreSQL 17 containers and named volumes:

```sh
export POSTGRES_PASSWORD='choose-a-local-secret'
docker compose -f compose.postgres.yml up -d --wait
```

Set `DATABASE_URL` separately in each service's shell. Replace `<password>` with the URL-encoded password; these are example DSNs, not stored credentials:

| Process | DATABASE_URL |
| --- | --- |
| coordinator | `postgres://matching:<password>@127.0.0.1:15432/coordinator?sslmode=disable` |
| shard-0-primary | `postgres://matching:<password>@127.0.0.1:15433/shard_0_primary?sslmode=disable` |
| shard-0-replica | `postgres://matching:<password>@127.0.0.1:15434/shard_0_replica?sslmode=disable` |
| shard-1-primary | `postgres://matching:<password>@127.0.0.1:15435/shard_1_primary?sslmode=disable` |
| shard-1-replica | `postgres://matching:<password>@127.0.0.1:15436/shard_1_replica?sslmode=disable` |
| shard-2-primary | `postgres://matching:<password>@127.0.0.1:15437/shard_2_primary?sslmode=disable` |
| shard-2-replica | `postgres://matching:<password>@127.0.0.1:15438/shard_2_replica?sslmode=disable` |

Use the shared `INTERNAL_TOKEN`, coordinator topology and node IDs described in [coordinator](coordinator.md) and [node](node.md). `DATABASE_URL` is required; file paths and the former `-data` flag are unsupported. Services close their database pools on shutdown.

These containers separate database processes and volumes but still share one host. Production failure isolation requires independent hosts/infrastructure as appropriate. Do not use the same database for multiple replicas. Do not remove volumes containing data you intend to recover.

## Testing

Point `TEST_DATABASE_URL` at a disposable PostgreSQL 17 database with permission to create schemas. Each database-backed test creates isolated schemas and drops them on cleanup. This deliberately shares one test server and does not simulate independent physical database failures. Without the variable only database integration tests skip; CI sets it so database tests execute.

```sh
export TEST_DATABASE_URL='postgres://test_user:<password>@127.0.0.1:5432/test_database?sslmode=disable'
go test ./...
go test -race ./...
go vet ./...
```

The six-node integration test runs real HTTP nodes and independent schema-backed stores, stops one node, mutates the surviving copy, reopens the old database and verifies repair including tombstones. Storage tests cover database errors, cross-pool CAS, reopen persistence and transactional rollback. A persistent-volume restart should also be checked with a dedicated test PostgreSQL instance before claiming host/container restart durability.
