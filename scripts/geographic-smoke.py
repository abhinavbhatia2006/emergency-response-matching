#!/usr/bin/env python3
"""Verify geographic matching using public demonstration coordinates.

Requires a deployed API and worker. Creates records
in Delhi and Mumbai, then completes/cancels its requests. No external distance service is used. Run against a demonstration instance, not real dispatch.
"""
import argparse
import json
import time
import urllib.error
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', default='http://127.0.0.1:8080')
    parser.add_argument('--timeout', type=float, default=180)
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error('timeout must be positive')
    base = args.url.rstrip('/')
    marker = 'geo-smoke-' + uuid.uuid4().hex[:10]

    def call(method, path, body=None, expected=200):
        request = urllib.request.Request(base + path, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Content-Type': 'application/json'})
        try:
            response = urllib.request.urlopen(request, timeout=45)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            data = json.load(response)
            assert response.status == expected, (method, path, response.status, data)
            return data

    config = call('GET', '/config')
    limit = config['max_distance_meters']
    assert config['distance_method'] == 'haversine'
    assert 5000 <= limit < 1000000, 'This demonstration expects a local matching radius of 5-999 km'

    # Public city-centre coordinates, not personal addresses.
    # The two labels deliberately differ.
    origin = {'latitude': 28.6139, 'longitude': 77.2090}
    destination = {'latitude': 28.6330, 'longitude': 77.2167}
    remote = {'latitude': 19.0760, 'longitude': 72.8777}
    far = call('POST', '/resources', {'type': 'transportation',
        'location': marker + '-mumbai', 'coordinates': remote, 'availability': True}, 201)
    near = call('POST', '/resources', {'type': 'transportation',
        'location': marker + '-delhi-origin', 'coordinates': origin, 'availability': True}, 201)
    created = call('POST', '/requests', {'type': 'transportation',
        'location': marker + '-delhi-destination', 'coordinates': destination,
        'details': 'Public-coordinate geographic-distance verification'}, 201)
    assert created['coordinates'] == destination
    deadline = time.monotonic() + args.timeout
    while True:
        matched = call('GET', '/requests/' + created['id'])
        if matched['status'] == 'matched':
            break
        if time.monotonic() >= deadline:
            raise AssertionError('Geographic match timed out; inspect worker logs')
        time.sleep(1)
    assert matched['resource_id'] == near['id'], 'did not select the reachable nearby resource'
    distance = matched.get('distance_meters')
    assert isinstance(distance, (int, float)) and 1000 < distance <= limit, distance
    repeated = call('POST', '/match/' + created['id'])
    assert repeated['distance_meters'] == distance
    assert call('PUT', '/requests/' + created['id'] + '/status', {'status': 'completed'})['status'] == 'completed'

    outside = call('POST', '/requests', {'type': 'transportation',
        'location': marker + '-outside-limit', 'coordinates': destination,
        'details': 'The only remaining resource is farther than the configured geographic limit'}, 201)
    call('POST', '/match/' + outside['id'], expected=409)
    assert call('GET', '/requests/' + outside['id'])['status'] == 'pending'
    call('PUT', '/requests/' + outside['id'] + '/status', {'status': 'cancelled'})
    call('PUT', '/resources/' + far['id'] + '/status', {'status': 'unavailable'})
    print(json.dumps({'result': 'PASS', 'url': base, 'request_id': created['id'],
        'distance_meters': distance, 'limit_meters': limit,
        'checks': ['coordinate persistence', 'cross-district geographic match',
                   'asynchronous worker', 'persisted distance', 'out-of-range rejection']}, indent=2))


if __name__ == '__main__':
    main()
