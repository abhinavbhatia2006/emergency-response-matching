#!/usr/bin/env python3
"""Demonstrate storage failover and recovery on the local Compose demonstration stack.

Stops one storage node, writes during its outage, restarts it, and checks both
copies. Requires Docker and a running stack. Never removes persistent volumes.
"""
import argparse
import json
import subprocess
import time
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--env-file', required=True)
    parser.add_argument('--url', default='http://127.0.0.1:18081')
    args = parser.parse_args()
    compose = ['docker', 'compose', '--env-file', args.env_file]
    def command(*parts):
        return subprocess.check_output(compose + list(parts), text=True, timeout=90)
    def call(method, path, body=None):
        req = urllib.request.Request(args.url.rstrip('/')+path, method=method,
            data=None if body is None else json.dumps(body).encode(), headers={'Content-Type':'application/json'})
        with urllib.request.urlopen(req, timeout=40) as response:
            return json.load(response)
    def wait(check, label):
        deadline=time.monotonic()+120
        while time.monotonic()<deadline:
            try:
                if check(): return
            except (OSError, subprocess.SubprocessError): pass
            time.sleep(1)
        raise AssertionError(label+' timed out')
    def records(service):
        # Expansion happens inside the container; credentials never leave it.
        raw=command('exec','-T',service,'sh','-c',
            'wget -qO- --header="Authorization: Bearer $INTERNAL_TOKEN" http://127.0.0.1:9100/internal/records')
        return {r['key']:r for r in json.loads(raw)}
    def shard(key):
        value=2166136261
        for byte in key.encode(): value=((value^byte)*16777619)&0xffffffff
        return value%3
    marker='failure-'+uuid.uuid4().hex[:12]
    coordinates={'latitude':1.3521,'longitude':103.8198}
    created=[]
    covered=set()
    for _ in range(60):
        request=call('POST','/requests',{'type':'water','location':marker,
            'coordinates':coordinates,'details':'Storage failover demonstration'})
        created.append(request);covered.add(shard('request/'+request['id']))
        if len(covered)==3:break
    assert covered=={0,1,2},'all shards must receive demonstration records'
    for index in range(3):
        for role in ['primary','replica']:
            stored=records(f'shard-{index}-{role}')
            for request in created:
                key='request/'+request['id']
                assert (key in stored)==(shard(key)==index), 'incorrect shard placement'
    target=next(r for r in created if shard('request/'+r['id'])==0)
    key='request/'+target['id']
    before=records('shard-0-primary')[key]
    stopped=False
    try:
        stopped=True
        command('stop','shard-0-primary')
        wait(lambda:not call('GET','/development/shards')['shards'][0]['nodes'][0]['ready'],'failed node detection')
        assert call('GET','/requests/'+target['id'])['id']==target['id']
        changed=call('PUT','/requests/'+target['id']+'/status',{'status':'cancelled'})
        assert changed['status']=='cancelled'
        replica=records('shard-0-replica')[key]
        assert replica['version']>before['version']
        print('PASS: three-shard distribution, failed-node detection, replica read and write during outage',flush=True)
    finally:
        if stopped: command('start','shard-0-primary')
    wait(lambda:call('GET','/development/shards')['shards'][0]['nodes'][0]['ready'],'node recovery')
    wait(lambda:records('shard-0-primary').get(key)==records('shard-0-replica').get(key),'replica synchronization')
    assert call('GET','/requests/'+target['id'])['status']=='cancelled'
    print(json.dumps({'result':'PASS','location':marker,'request_id':target['id'],
        'shards':sorted(covered),'version_before':before['version'],
        'version_after':records('shard-0-primary')[key]['version'],
        'checks':['before/during/after storage failure','replica availability','recovery synchronization']},indent=2))


if __name__=='__main__': main()
