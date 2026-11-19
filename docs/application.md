# Request API and dashboard

The request API serves the seven public endpoints and a React dashboard. Start the PostgreSQL-backed storage cluster first, then build the frontend and API from the repository root:

```sh
cd web
npm ci
npm test
npm run build
cd ..
go build -o bin/request-api ./cmd/request-api
export COORDINATOR_URL=http://127.0.0.1:8081
export INTERNAL_TOKEN='<same token as the coordinator>'
./bin/request-api -addr 127.0.0.1:8080 -web web/dist
```

Use Node.js 22.12 or newer for frontend builds. Open http://127.0.0.1:8080. React uses same-origin JSON endpoints; no broker credentials or internal token reach the browser. For frontend development run `npm run dev` in `web`; Vite proxies API paths to port 8080. The compiled frontend directory must exist when the Go API starts. The dashboard shows actual counts, storage readiness, request/resource forms, lookup, matching and lifecycle actions. It refreshes every 15 seconds and supports narrow screens and keyboard-operated dialogs.

## Public routes

| Method | Path | Input | Success |
|---|---|---|---|
| POST | `/requests` | `{"type":"water","coordinates":{"latitude":28.633,"longitude":77.2167},"details":"drinking water"}` |201 request|
| GET | `/requests/{id}` | none |200 request|
| PUT | `/requests/{id}/status` | `{"status":"cancelled"}` |200 request|
| POST | `/resources` | `{"type":"water","coordinates":{"latitude":28.633,"longitude":77.2167},"availability":true}` |201 resource|
| GET | `/resources` | none |200 resource array|
| PUT | `/resources/{id}/status` | `{"status":"available"}` |200 resource|
| POST | `/match/{id}` | none |200 request|

GET `/requests` returns the request collection. GET `/healthz` checks process liveness and GET `/readyz` checks storage availability. Readiness checks storage only. Error objects use `{"error":"message"}` with status 400 for invalid input or transitions, 404 for missing records, 409 for conflicts or no suitable match, 413 for oversized input, and 503 for unavailable storage or uncertain write outcomes. Bodies are limited to 64 KiB and reject unknown fields and extra JSON values. IDs are 32 lowercase hexadecimal characters. The API trims and lowercases assistance types and location names. The five supported types are food, water, medicine, transportation, and shelter.

Creation atomically saves a pending request and an outbox job before returning 201. The [matching worker](messaging.md) publishes/consumes jobs through RabbitMQ. Creating a resource or making it available atomically queues one rescan job, so large waiting lists do not require one transaction per pending request. Manual match is immediate and uses the same idempotent business operation as the worker. No available resource leaves the request pending.

Only matching assigns `matched`. Pending requests can be cancelled; matched requests can be completed or cancelled. Cancellation makes the assigned resource available and schedules a rescan; completion marks it unavailable. A reserved resource cannot be manually reactivated. Request/resource changes use atomic compare-and-swap batches, so concurrent workers cannot reserve one resource for two requests. Duplicate message delivery returns the existing assignment; terminal requests are not matched again.

503 after a write can mean the journal committed but replication did not finish. Read the current entity before retrying; creation retries can create another ID because the public create API does not implement an idempotency key. The UI does not automatically retry writes.

## Demonstration limits

The API is an unauthenticated local demonstration; bind locally or put it behind appropriate access control before wider exposure. Storage uses one coordinator and full-shard transfers bounded to 8 MiB. Batches support 256 mutations. Deleted outbox jobs retain tombstones for recovery; historical jobs count toward shard capacity even when no jobs are pending. There is no online compaction. Capacity exhaustion rejects the change before commit, rather than discarding recovery information. This design is intended for bounded demonstration datasets, not continuous production dispatch.

## Coordinate matching

In either creation form, tap the OpenStreetMap map or drag its pin to choose the location. **Use my location** asks the browser for a one-time position, which you can adjust before submitting. Device location requires permission and HTTPS (or localhost for development). Both coordinate fields also accept manual input, including when map tiles cannot load.

**Name** is an optional, user-supplied label for the pin. The dashboard displays it alongside the coordinates, with **Pinned location** as the fallback. The system does not verify that the name corresponds to the coordinates. Geographic matching uses the pin's coordinates.

Both creation endpoints accept a `coordinates` object with numeric `latitude` and `longitude`. An incomplete, invalid, or explicitly null coordinate object returns HTTP 400. Matching chooses the nearest available same-type resource within `MAX_MATCH_DISTANCE_METERS` (default 50000), and geographic matches include `distance_meters`. Existing clients can omit `coordinates` and supply a nonempty district in `location`. District-only requests still match by exact district and report no geographic distance. `GET /config` returns the configured maximum and `distance_method: "haversine"`. See [geographic matching](geographic-matching.md).

For a pinned record, omitting the optional name returns an empty `location` and displays **Pinned location** in the dashboard. Both creation endpoints accept an optional `directions` string of up to 200 characters for records with coordinates. The name and directions are for display.

## Response map

Open **Map** in the main navigation to see requests as orange clipboard pins and resources as teal package pins. Select a pin to inspect the record. A numbered pin groups records that share the same coordinates. **Show on map** in either list opens the corresponding pin. Records without usable coordinates remain accessible through the lists.

Selecting a currently matched request also highlights its assigned resource if both records have coordinates. The dashed line shows the stored straight-line distance. **Show all pins** returns to the map overview.
