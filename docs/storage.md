# Storage foundation

Each storage process owns one JSON snapshot file in a private data directory. `Store` serializes writes with a mutex and exposes detached copies to readers. It is safe for goroutines in one process; sharing a file between processes is unsupported.

`Put` and `Delete` use optimistic concurrency. A new record starts at version 1 with expected version 0. Updating requires the current version; stale operations return `ErrConflict`. Deletes keep versioned tombstones so future synchronization can distinguish deletion from absence. Recreating a deleted record requires the tombstone version.

The store writes a full candidate snapshot to a temporary file in the same directory, syncs and closes it, then renames it over the previous snapshot. Only after the rename succeeds does it publish the new in-memory state. Failed writes leave visible memory unchanged. Startup rejects malformed snapshots rather than silently replacing data.

The file is synced but the directory entry is not explicitly synced. This supports process restart and container restart demonstrations; it does not promise survival of sudden host power loss. Rename atomicity also depends on the operating system/filesystem. Linux containers are the deployment target. Snapshot rewriting has O(dataset size) write cost and is intended for small demonstrations.

Routing uses FNV-1a (32-bit) over the full UTF-8 key, modulo the configured shard count. Runtime deployment will use three shards. Keep the count fixed for an existing dataset: changing it requires migration.

Replication, coordinator routing, heartbeats and reconciliation are the next increment. This package alone does not provide distributed consistency or failover.
