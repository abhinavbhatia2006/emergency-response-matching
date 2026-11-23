#!/usr/bin/env python3
"""Verify distribution, failover, and recovery in the local Compose stack.

Run all checks with no --scenario, as CI does, or select one demonstration.
Stopped storage services are restarted even when an assertion fails.
"""
import argparse
import json
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def shard_for(key):
    """The same three-way FNV-1a partition used by the coordinator."""
    value = 2166136261
    for byte in key.encode():
        value = ((value ^ byte) * 16777619) & 0xFFFFFFFF
    return value % 3


def wait(check, label):
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        try:
            if check():
                return
        except (OSError, subprocess.SubprocessError):
            pass
        time.sleep(1)
    raise AssertionError(f'{label} timed out')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--env-file', required=True)
    parser.add_argument('--url', default='http://127.0.0.1:18081')
    parser.add_argument('--scenario', choices=['all', 'distribution', 'failover', 'recovery', 'outage'],
                        default='all')
    parser.add_argument('--pause', action='store_true', help='pause to inspect an outage before restarting nodes')
    args = parser.parse_args()
    compose = ['docker', 'compose', '--env-file', args.env_file]
    primary, replica = 'shard-0-primary', 'shard-0-replica'

    def command(*parts):
        return subprocess.check_output([*compose, *parts], text=True, timeout=90)

    def call(method, path, body=None, expected=200):
        req = urllib.request.Request(
            args.url.rstrip('/') + path,
            method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Content-Type': 'application/json'},
        )
        try:
            response = urllib.request.urlopen(req, timeout=40)
        except urllib.error.HTTPError as error:
            if error.code != expected:
                raise
            response = error
        with response:
            assert response.status == expected, f'{method} {path}: HTTP {response.status}, expected {expected}'
            return json.load(response)

    def records(service):
        # Expand the credentials inside the container.
        raw = command('exec', '-T', service, 'sh', '-c',
                      'wget -qO- --header="Authorization: Bearer $INTERNAL_TOKEN" http://127.0.0.1:9100/internal/records')
        return {record['key']: record for record in json.loads(raw)}

    def shard_zero_nodes():
        return call('GET', '/development/shards')['shards'][0]['nodes']

    def result(**evidence):
        print(json.dumps({'result': 'PASS', 'scenario': args.scenario, 'location': marker, **evidence},
                         indent=2), flush=True)

    marker = 'failure-' + uuid.uuid4().hex[:12]
    # Use a remote demo pin to keep these requests outside the existing demo resources' matching areas.
    coordinates = {'latitude': 0, 'longitude': -140}
    created = {}
    for _ in range(60):
        request = call('POST', '/requests', {
            'type': 'water', 'location': marker, 'coordinates': coordinates, 'details': 'Storage demonstration',
        }, expected=201)
        key = 'request/' + request['id']
        created[key] = shard_for(key)
        if set(created.values()) == {0, 1, 2}:
            break
    assert set(created.values()) == {0, 1, 2}, 'all shards must receive demonstration records'

    if args.scenario in ('all', 'distribution'):
        for index in range(3):
            for role in ('primary', 'replica'):
                stored = records(f'shard-{index}-{role}')
                for key, assigned in created.items():
                    assert (key in stored) == (assigned == index), f'incorrect placement for {key}'
        print('PASS: each request is present on both copies of its assigned shard only', flush=True)
        for key, index in created.items():
            print(f'{key} -> shard {index} primary and replica', flush=True)
        if args.scenario == 'distribution':
            result(shards=sorted(set(created.values())), placements=created)
            return

    key = next(key for key, index in created.items() if index == 0)
    request_id = key.split('/', 1)[1]
    target_url = '/requests/' + request_id
    before = records(primary)[key]
    synchronized = before

    if args.scenario != 'outage':
        try:
            command('stop', primary)
            wait(lambda: not shard_zero_nodes()[0]['ready'], 'failed node detection')
            assert call('GET', target_url)['id'] == request_id
            changed = call('PUT', target_url + '/status', {'status': 'cancelled'})
            assert changed['status'] == 'cancelled'
            written_on_replica = records(replica)[key]
            assert written_on_replica['version'] > before['version']
            print(f"PASS: replica served a read and advanced {request_id} from version "
                  f"{before['version']} to {written_on_replica['version']} with the primary down", flush=True)
            if args.pause:
                input('Refresh Shard details in the dashboard. Press Enter to restore the primary. ')
        finally:
            print(f'Restarting {primary}', flush=True)
            command('start', primary)
        wait(lambda: all(node['ready'] for node in shard_zero_nodes()), 'primary recovery')

        if args.scenario == 'failover':
            result(request_id=request_id, version_before=before['version'],
                   version_on_replica=written_on_replica['version'])
            return

        wait(lambda: records(primary).get(key) == records(replica).get(key) == written_on_replica,
             'replica synchronization')
        assert call('GET', target_url)['status'] == 'cancelled'
        synchronized = records(primary)[key]
        print(f"PASS: the recovered primary and replica both contain version {synchronized['version']}",
              flush=True)
        if args.scenario == 'recovery':
            result(request_id=request_id, version_before=before['version'], version_after=synchronized['version'])
            return

    healthy_keys = [key for key, index in created.items() if index != 0]
    try:
        command('stop', primary, replica)
        wait(lambda: not any(node['ready'] for node in shard_zero_nodes()), 'both copies unavailable')
        unavailable = call('GET', target_url, expected=503)
        assert unavailable.get('error'), 'an unavailable record must return a JSON error'
        print(f'Shard 0 request {request_id} returned HTTP 503.', flush=True)
        for healthy_key in healthy_keys:
            healthy_id = healthy_key.split('/', 1)[1]
            persisted = call('GET', '/requests/' + healthy_id)
            assert persisted['id'] == healthy_id, 'healthy shard read failed'
            print(f'Shard {created[healthy_key]} request {healthy_id} returned HTTP 200.', flush=True)
        if args.pause:
            input('Inspect the unavailable shard. Press Enter to restore both copies. ')
    finally:
        print(f'Restarting {primary} and {replica}', flush=True)
        command('start', primary, replica)
    wait(lambda: all(node['ready'] for node in shard_zero_nodes()), 'both copies recovered')
    for service in (primary, replica):
        assert records(service)[key] == synchronized, f'{service} lost the last committed record'
    assert call('GET', target_url)['id'] == request_id
    if args.scenario == 'all':
        assert call('GET', target_url)['status'] == 'cancelled'
    result(request_id=request_id, shards=sorted(set(created.values())), version_before=before['version'],
           version_after=synchronized['version'], affected_shard_http=503, healthy_shards_http=200)


if __name__ == '__main__':
    main()
