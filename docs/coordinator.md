# Storage coordinator

The coordinator owns routing and write order for three shard groups, each configured with two nodes. It hashes complete record keys with FNV-1a modulo three. Node IDs, shard numbers and URLs are configured by the operator; registration never accepts an arbitrary destination URL.

The coordinator keeps an authoritative recovery journal on its own persistent volume. This is a full additional copy of the data, not just routing metadata. Reads of existing records still require a healthy shard replica with the expected version and content. The journal lets the coordinator reject stale replica contents and restore a recovering node. Run exactly one coordinator against a journal: this system is not a consensus database and does not support multiple coordinator writers.

## Writes and failure semantics

A write requires a live node in the target shard and a matching expected record version. The coordinator persists the new journal record, then sends the shard snapshot to the live copies. It returns success once at least one replica has durably acknowledged the new snapshot. A failed node is immediately excluded from routing.

If every replication attempt fails after the journal commit, the caller receives HTTP 503. **The outcome is uncertain, not rolled back.** Re-registration will complete journal recovery. Clients should read the current record before retrying a versioned update. A retry with an obsolete expected version returns 409. This avoids silently overwriting a concurrent update. Journal durability inherits the storage package's documented process-restart guarantees and power-loss limitations.

This favors availability with one surviving copy. It does not claim quorum consensus, survival of loss of the journal volume, or coordinator high availability. Keep the journal volume safe and back it up. No automatic election occurs.

## Heartbeats and recovery

Nodes register by ID and send periodic heartbeats. Before accepting a heartbeat, the coordinator synchronizes that node's entire shard, including tombstones. Only successful synchronization renews its readiness lease. Expired leases and transport failures make nodes unavailable. On restart, the coordinator trusts no node until it has synchronized from the persisted journal.

Writes and reads are serialized through one coordinator mutex, including replication. This keeps the prototype's ordering understandable and prevents concurrent compare-and-swap winners. Full-shard snapshot replication is intentionally simple and has O(shard size) network/disk cost per write or heartbeat. Internal payloads are bounded to 8 MiB; this is for small demonstration datasets.

## Internal HTTP

All internal routes require `Authorization: Bearer <INTERNAL_TOKEN>`. Use private networking or TLS in deployment; bearer authentication alone does not encrypt traffic. `/healthz` checks process liveness, not full cluster readiness. `/internal/nodes` reports readiness for each replica.

| Method | Route | Body / result |
| --- | --- | --- |
| POST | `/internal/nodes/register` | `{"id":"shard-0-primary"}` |
| POST | `/internal/nodes/heartbeat` | Same as registration; synchronizes and renews lease |
| GET | `/internal/nodes` | Node ID, URL, shard, readiness and heartbeat time |
| PUT | `/internal/data/{key}` | `{"value":{...},"expected_version":0}`; returns record |
| GET | `/internal/data/{key}` | Record, or 404 |
| DELETE | `/internal/data/{key}` | `{"expected_version":1}`; returns tombstone |
| GET | `/internal/data` | Non-deleted records; fails rather than returning partial data |

Invalid JSON/input returns 400, invalid authentication 401, missing records/nodes 404, stale versions 409, oversized bodies 413, and unavailable storage 503. Request/resource business validation belongs to the application API, not this generic JSON storage interface.

## Run locally

Build with `go build -o bin/coordinator ./cmd/coordinator`. Set `INTERNAL_TOKEN`, then run:

```sh
./bin/coordinator -addr 127.0.0.1:8081 -nodes config/nodes.local.json -data data/coordinator.json
```

The heartbeat timeout defaults to 15 seconds and is configurable with `-heartbeat-timeout`. The six storage nodes must use the exact configured IDs and listen addresses. Local config uses ports 9100 through 9105. Persist each node's data separately from the coordinator journal.

Query `/internal/nodes` with the bearer token to see whether all six copies are ready. Stop one node to observe continued operation through the surviving copy, then restart it with the same data file. Its next registration/heartbeat restores the current snapshot before it becomes ready. Stop both copies of a shard to observe 503 responses. Never erase the coordinator journal while retaining data you intend to recover.
