"""Observe actual PostgreSQL pod members; CNPG alone owns promotion and fencing."""
import json, os, ssl, time, urllib.request
from pathlib import Path
import psycopg
from concurrent.futures import ThreadPoolExecutor

TOKEN_PATH=Path('/var/run/secrets/kubernetes.io/serviceaccount/token')
CA='/var/run/secrets/kubernetes.io/serviceaccount/ca.crt'
KUBE='https://kubernetes.default.svc'
COORDINATOR=os.environ.get('COORDINATOR_URL','http://storage-coordinator:8080')
def pods():
    request=urllib.request.Request(KUBE+'/api/v1/namespaces/emergency/pods?labelSelector=cnpg.io%2Fcluster',headers={'Authorization':'Bearer '+TOKEN_PATH.read_text().strip()})
    with urllib.request.urlopen(request,context=ssl.create_default_context(cafile=CA),timeout=5) as response:
        return json.load(response)['items']
def report(pod):
    name=pod['metadata']['name'];cluster=pod['metadata']['labels']['cnpg.io/cluster']
    if cluster not in ['shard-0','shard-1','shard-2']:return
    body=dict(nodeId=name,shard=int(cluster[-1]),role='UNKNOWN',healthy=False,lagBytes=-1)
    try:
        ip=pod['status'].get('podIP')
        if not ip:raise ValueError('No pod IP')
        with psycopg.connect(host=ip,dbname='emergency',user=os.environ['DB_USER'],password=os.environ['DB_PASSWORD'],connect_timeout=2,options='-c statement_timeout=2000') as db:
            standby,lag=db.execute("SELECT pg_is_in_recovery(), CASE WHEN pg_is_in_recovery() THEN COALESCE(pg_wal_lsn_diff(pg_last_wal_receive_lsn(),pg_last_wal_replay_lsn()),0)::bigint ELSE 0 END").fetchone()
        body.update(role='STANDBY' if standby else 'PRIMARY',healthy=True,lagBytes=lag)
    except Exception as error:print(f'member_unavailable node={name} error={type(error).__name__}',flush=True)
    request=urllib.request.Request(COORDINATOR+'/internal/nodes/heartbeat',data=json.dumps(body).encode(),headers={'Content-Type':'application/json','X-Internal-Token':os.environ['INTERNAL_TOKEN']},method='POST')
    with urllib.request.urlopen(request,timeout=5) as response:response.read()
def safe_report(pod):
    try:report(pod)
    except Exception as error:print(f'report_retry error={type(error).__name__}',flush=True)
while True:
    try:
        with ThreadPoolExecutor(max_workers=6) as workers:
            list(workers.map(safe_report,pods()))
    except Exception as error:print(f'discovery_retry error={type(error).__name__}',flush=True)
    time.sleep(5)
