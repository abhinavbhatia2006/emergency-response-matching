#!/usr/bin/env python3
"""Verify a deployed API, built dashboard, and asynchronous matching workflow.

Creates a small set of uniquely named demonstration records and leaves them
available for inspection. Requires only Python 3's standard library.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import json
import re
import time
import urllib.error
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', default='http://127.0.0.1:8080')
    parser.add_argument('--timeout', type=float, default=120)
    parser.add_argument('--count', type=int, default=6)
    args = parser.parse_args()
    if args.count < 1 or args.count > 20 or args.timeout <= 0:
        parser.error('count must be 1-20 and timeout must be positive')
    base = args.url.rstrip('/')
    area = 'smoke-' + uuid.uuid4().hex[:12]

    def call(method, path, body=None, expected=200):
        payload = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(base + path, data=payload, method=method,
                                         headers={'Content-Type': 'application/json'})
        try:
            response = urllib.request.urlopen(request, timeout=35)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            if response.status != expected:
                raise AssertionError(f'{method} {path}: {response.status}, expected {expected}: {raw[:300]!r}')
            if 'application/json' not in response.headers.get('Content-Type', ''):
                raise AssertionError(f'{path}: response is not JSON')
            return json.loads(raw)

    def wait(check, description):
        deadline = time.monotonic() + args.timeout
        last = None
        while time.monotonic() < deadline:
            try:
                if check():
                    return
            except (OSError, AssertionError) as error:
                last = error
            time.sleep(.5)
        raise AssertionError(f'{description} timed out; last error: {last}')

    wait(lambda: call('GET', '/readyz').get('status') == 'ready', 'API readiness')
    with urllib.request.urlopen(base + '/', timeout=15) as page:
        html = page.read().decode()
        assert 'text/html' in page.headers.get('Content-Type', '')
        assert 'id="root"' in html, 'dashboard root missing'
        policy = page.headers.get('Content-Security-Policy', '')
        image_sources = next((parts[1:] for parts in map(str.split, policy.split(';'))
                              if parts and parts[0] == 'img-src'), [])
        assert 'https://tile.openstreetmap.org' in image_sources, 'dashboard blocks OpenStreetMap tiles'
    assets = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
    assert assets, 'compiled dashboard assets missing'
    for asset in assets:
        with urllib.request.urlopen(base + asset, timeout=15) as response:
            assert response.status == 200 and response.read(), f'empty asset: {asset}'
            assert 'text/html' not in response.headers.get('Content-Type', ''), f'asset returned HTML: {asset}'
    print('PASS: storage readiness and compiled React assets', flush=True)

    resources = [call('POST', '/resources', {'type': 'food', 'location': area, 'availability': True}, 201)
                 for _ in range(args.count)]
    with ThreadPoolExecutor(max_workers=min(args.count, 6)) as pool:
        requests = list(pool.map(lambda i: call('POST', '/requests', {
            'type': 'food', 'location': area, 'details': f'Deployment verification {i}'}, 201), range(args.count)))
    wait(lambda: all(call('GET', '/requests/' + r['id'])['status'] == 'matched' for r in requests),
         'RabbitMQ asynchronous assignment')
    matched = [call('GET', '/requests/' + r['id']) for r in requests]
    ids = [r['resource_id'] for r in matched]
    assert len(set(ids)) == args.count, 'resource assigned more than once'
    assert set(ids) == {r['id'] for r in resources}
    print(f'PASS: {args.count} concurrent requests matched asynchronously without double booking', flush=True)

    first = matched[0]
    duplicate = call('POST', '/match/' + first['id'])
    assert duplicate['resource_id'] == first['resource_id']
    call('PUT', '/resources/' + first['resource_id'] + '/status', {'status': 'available'}, 400)
    completed = call('PUT', '/requests/' + first['id'] + '/status', {'status': 'completed'})
    assert completed['status'] == 'completed'
    listed = call('GET', '/resources')
    used = next(r for r in listed if r['id'] == first['resource_id'])
    assert used['status'] == 'unavailable' and not used['availability']
    pending = call('POST', '/requests', {'type': 'medicine', 'location': area, 'details': 'No matching resource'}, 201)
    call('POST', '/match/' + pending['id'], expected=409)
    assert call('PUT', '/requests/' + pending['id'] + '/status', {'status': 'cancelled'})['status'] == 'cancelled'
    call('POST', '/resources', {'type': 'food', 'location': area}, 400)
    call('GET', '/requests/' + '0' * 32, expected=404)
    print('PASS: idempotent match, lifecycle, resource reservation, and error contracts', flush=True)
    print(json.dumps({'url': base, 'location': area, 'request_ids': [r['id'] for r in requests],
                      'resource_ids': ids}, indent=2), flush=True)


if __name__ == '__main__':
    main()
