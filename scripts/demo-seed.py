#!/usr/bin/env python3
"""Lays out a running `housed -sim -sim-days N` the way a real house would be set up, so a demo shows the whole dashboard: the garage panel with its
circuits inside it, the PC and the monitor on the office circuit, a computer with its collector's CPU, GPU and memory, a group, favorites and a price.

It only talks to the server's HTTP API, like the dashboard does, and only to a server you name (default http://127.0.0.1:8097). Nothing here is a
measurement: the house is made up.

    python scripts/demo-seed.py [http://127.0.0.1:8097]
"""
import json
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = (sys.argv[1] if len(sys.argv) > 1 else 'http://127.0.0.1:8097').rstrip('/')


def call(method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data, method=method, headers={'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            raw = r.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raise SystemExit(f'{method} {path}: {e.code} {e.read().decode()[:300]}')


def wait():
    for _ in range(60):
        try:
            call('GET', '/api/v1/status')
            return
        except (SystemExit, urllib.error.URLError, ConnectionError):
            time.sleep(1)
    raise SystemExit('the server did not answer')


wait()
meters = {m['label']: m for m in call('GET', '/api/v1/meters')['meters']}


def need(label):
    if label not in meters:
        raise SystemExit(f'no meter called {label}: is this a server run with -sim?')
    return meters[label]['id']


# What is inside what: the same layout the simulated house is built with.
inside = {
    'EV charger': 'Garage panel',
    'Workshop': 'Garage panel',
    'Desk PC': 'Office',
    'Monitor': 'Office',
}
for child, parent in inside.items():
    call('PATCH', f'/api/v1/meters/{need(child)}', {'parent_id': need(parent)})

# A price, so every figure has a cost.
call('PUT', '/api/v1/rates', {'mode': 'one', 'currency': 'USD', 'fixed_rate': '0.158', 'bills': []})

# A computer: the desk PC's wall plug and the monitor, with a collector that reports its CPU, GPU and memory.
computers = call('GET', '/api/v1/computers')['computers']
desk = next((c for c in computers if c['name'] == 'Desk'), None) or call('POST', '/api/v1/computers', {'name': 'Desk'})
call('PATCH', f'/api/v1/meters/{need("Desk PC")}', {'computer_id': desk['id']})
call('PATCH', f'/api/v1/meters/{need("Monitor")}', {'computer_id': desk['id']})

# A logical group of two meters that are not next to each other in the wiring.
groups = call('GET', '/api/v1/groups')['groups']
kitchen = next((g for g in groups if g['name'] == 'Kitchen'), None) or call('POST', '/api/v1/groups', {'name': 'Kitchen'})
for label in ('Range', 'Fridge'):
    call('PATCH', f'/api/v1/meters/{need(label)}', {'group_id': kitchen['id']})

# Favorites: what to be one click away on the live page.
for label in ('Desk PC', 'EV charger', 'Garage panel'):
    call('PATCH', f'/api/v1/meters/{need(label)}', {'favorite': True})

# The collector's side of the desk PC: three days of CPU, GPU and memory minutes that add up to less than the wall plug, as real sensors do.
def iso(t):
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(t))


end = int(time.time() // 60 * 60)
start = end - 3 * 24 * 3600
wall = []
step = 6 * 3600
t = start
while t < end:
    q = urllib.parse.urlencode({'meter_id': need('Desk PC'), 'from': iso(t), 'to': iso(min(t + step, end)), 'bucket': 'minute'})
    wall += call('GET', f'/api/v1/series?{q}')['points']
    t += step
minutes = []
for p in wall:
    w = p['avg_watts']
    active = max(0.0, w - 45)  # what the sensors can see: the wall plug also has fans, drives, the motherboard and the power supply's losses
    cpu, gpu, mem = active * 0.22, active * 0.55, 3.0 + active * 0.02
    minutes.append({
        'minute': p['t'],
        'readings': [
            {'name': 'CPU package', 'scope': 'component', 'kind': 'cpu', 'avg_watts': round(cpu, 2), 'max_watts': round(cpu * 1.2, 2), 'samples': 60},
            {'name': 'NVIDIA GeForce RTX 5070 Ti', 'scope': 'component', 'kind': 'gpu', 'avg_watts': round(gpu, 2), 'max_watts': round(gpu * 1.15, 2), 'samples': 60},
            {'name': 'Memory', 'scope': 'component', 'kind': 'other', 'avg_watts': round(mem, 2), 'max_watts': round(mem * 1.1, 2), 'samples': 60},
        ],
        'programs': [],
    })
for i in range(0, len(minutes), 360):
    call('POST', '/api/v1/ingest/pc', {'api_version': 1, 'source_id': 'demo-desk-pc', 'source_name': 'Desk', 'minutes': minutes[i:i + 360]})
print(f'laid out {BASE}: {len(inside)} meters nested, a computer with {len(minutes)} minutes from its collector, a group, favorites and a price')
