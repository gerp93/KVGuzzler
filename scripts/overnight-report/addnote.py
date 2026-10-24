#!/usr/bin/env python3
"""Adds (or replaces, by id) one entry in notes.json from a JSON file, and copies its screenshots next to the report so they outlive the tool's folder.

    python addnote.py entry.json
"""
import json
import os
import shutil
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
NOTES = os.path.join(HERE, 'notes.json')
IMG = os.path.join(HERE, 'img')
os.makedirs(IMG, exist_ok=True)

entry = json.load(open(sys.argv[1], encoding='utf-8'))
data = json.load(open(NOTES, encoding='utf-8')) if os.path.exists(NOTES) else {'meta': {}, 'entries': []}
for im in entry.get('images', []):
    src = im['path'] if os.path.isabs(im['path']) else os.path.join(HERE, im['path'])
    if os.path.exists(src) and os.path.dirname(os.path.abspath(src)) != IMG:
        dst = os.path.join(IMG, f"{entry['id']}-{os.path.basename(src)}")
        shutil.copyfile(src, dst)
        im['path'] = 'img/' + os.path.basename(dst)
data['entries'] = [e for e in data['entries'] if e['id'] != entry['id']] + [entry] if entry['id'] not in [e['id'] for e in data['entries']] else [entry if e['id'] == entry['id'] else e for e in data['entries']]
json.dump(data, open(NOTES, 'w', encoding='utf-8'), indent=1)
print('notes:', len(data['entries']), 'entries')
