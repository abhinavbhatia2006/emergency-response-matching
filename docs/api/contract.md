# Public API baseline

Implemented public API; browser access uses the `/api` prefix. Resource types: WATER, FOOD, MEDICINE, TRANSPORT, SHELTER. IDs are UUIDs; timestamps are UTC ISO-8601.

| Method | Path | Success | Error cases |
| --- | --- | --- | --- |
| POST | /requests | 201 + Location + persisted request in PENDING | 400 invalid input; 503 storage unavailable |
| GET | /requests/{id} | 200 request | 400 malformed ID; 404 absent; 503 unavailable |
| PUT | /requests/{id}/status | 200 updated request | 400 missing/blank status; 404 absent; 409 forbidden/stale transition |
| POST | /resources | 201 + Location + resource | 400 invalid input; 503 unavailable |
| GET | /resources | 200 aggregated collection | 400 invalid filters; 503 incomplete shard search |
| PUT | /resources/{id}/status | 200 updated resource | 400 missing/blank status; 404 absent; 409 ownership/transition conflict |
| POST | /match/{requestId} | 200 MATCH_QUEUED or current state | 404 absent; 409 ineligible state; 503 unavailable |

Manual match returns 200 as specified by the assignment baseline, with an explicit queued indication. It does not promise an immediate assignment. Duplicate triggers must not create competing assignments. Initial events are produced through the request transaction's outbox.

## Creation payloads

Request:

```json
{
  "type": "WATER",
  "location": {"label": "Relief camp A", "latitude": 12.9716, "longitude": 77.5946},
  "details": "Drinking water for five people",
  "priority": "HIGH"
}
```

Resource:

```json
{
  "type": "WATER",
  "location": {"label": "Volunteer hub B", "latitude": 12.973, "longitude": 77.596},
  "description": "Sealed drinking water",
  "availability": true
}
```

Validate finite latitude in [-90, 90], longitude in [-180, 180], enum values, nonblank required labels/details, and bounded text sizes. Location labels are limited to 200 characters; details and descriptions to 2000. Lists accept limit 1–200 and offset 0–10000. Availability false creates an UNAVAILABLE resource.

Status payload uses `{"status":"IN_PROGRESS"}` or an appropriate resource status. The server owns matching states and reservation fields. Clients cannot bypass guarded transitions.

## Errors

```json
{
  "timestamp": "2026-10-23T00:00:00Z",
  "status": 409,
  "code": "INVALID_STATUS_TRANSITION",
  "message": "Request cannot enter the requested state",
  "path": "/requests/example/status",
  "requestId": "correlation-id"
}
```

`requestId` in the error envelope denotes correlation, not a domain entity ID. Use a consistent correlation header across HTTP, events and logs. Do not include SQL or credentials.

## Internal operations

Coordinator contracts must cover node registration/heartbeat; typed request/resource reads, writes and safe administrative deletion; transactional request/outbox insertion; guarded request transitions; complete candidate search; resource reserve/allocate/release; durable saga progress; outbox lease/confirm; and topology reads. Generic arbitrary table or SQL execution is prohibited. Administrative deletion must reject records participating in active workflows.
