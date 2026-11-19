# Geographic matching

New requests and resources require latitude and longitude. The matcher selects the nearest available resource of the same assistance type within the configured geographic radius. Equal distances are resolved by resource ID. The assignment and its `distance_meters` are saved atomically with resource reservation in PostgreSQL.

Distances are calculated entirely in Go using the haversine formula with mean Earth radius 6,371,008.8 metres. This is a spherical great-circle approximation of direct geographic separation. It does not account for transport routes, barriers, terrain, traffic or travel time. No external distance service, network request or API key is involved.

## Matching radius

The default maximum is **50,000 metres (50 km), inclusive**. Set `MAX_MATCH_DISTANCE_METERS=25000` to use a 25 km search radius. The value must be finite, greater than zero and at most 20,015,000 metres. This upper validation bound is a technical limit, not a recommended service area.

API and worker must use the same setting. Compose passes the same environment value to both. In Kubernetes, edit `max-distance-meters` in the `matching-settings` ConfigMap generator in `deploy/kubernetes/kustomization.yaml` and reapply the bundle; the generated hash rolls both workloads. `GET /config` exposes the API's configured `max_distance_meters` and `distance_method: "haversine"`, allowing the dashboard to show the actual limit.

No single radius is optimal for every resource type or region. A smaller radius favors local support but can leave requests unmatched; a larger radius increases candidate coverage. Select a radius using observed resource density and the intended service area. The configurable default is a demonstration policy, not an operational emergency response guarantee.

## Coordinate representation

```json
{
  "type": "transportation",
  "location": "central delhi",
  "details": "Transport assistance",
  "coordinates": {"latitude": 28.6330, "longitude": 77.2167}
}
```

Resources use the same coordinate object plus `availability`. Both coordinate members must be provided together. Latitude must be finite in [-90,90], longitude in [-180,180]. Zero is valid and must not be mistaken for a missing value. A matched request records `distance_meters`, including a legitimate zero distance.

The dashboard and API both enforce the coordinate requirement. Matching requires coordinates on both records. The optional `location` field is a display name supplied by the user.

No eligible resource within the radius leaves the request pending; manual matching returns 409. The worker can reconsider pending requests when resources are added or released. Distance is captured at assignment time and retained with the request's history after completion or cancellation.

## Verification

```sh
python scripts/geographic-smoke.py --url http://127.0.0.1:8080
```

Run this on an isolated demonstration instance. It uses public city-centre coordinates in Delhi and Mumbai, verifies cross-district asynchronous assignment and persisted distance, and rejects the remaining resource outside the radius. It creates demonstration records and closes its requests. Existing nearby transportation resources can affect candidate selection. The demonstration expects a configured radius between 5 km and 999 km.

Unit tests cover known distances, zero, symmetry, poles, antipodes, the antimeridian, invalid coordinates, exact radius boundaries, deterministic ties and concurrent reservation. The ordinary deployment smoke also verifies HTTP 400 for submissions that provide a name without coordinates.
