#!/usr/bin/env python3
"""Builds the overnight report: one self-contained HTML file from notes.json and the screenshots it names.

notes.json is {"meta": {...}, "entries": [{"id", "title", "kind", "summary", "details": [...html strings], "images": [{"path", "caption"}],
"commit", "files", "tests"}]}. Run: python build_report.py
"""
import base64
import html
import json
import os
import re
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
NOTES = os.path.join(HERE, 'notes.json')
OUT = os.path.join(HERE, 'overnight-report.html')
REPO = r'C:\Users\kgerp\source\repos\_worktrees\KVGuzzler-overnight'


def git(*args):
    try:
        return subprocess.run(['git', '-C', REPO, *args], capture_output=True, text=True, timeout=60).stdout.strip()
    except Exception:
        return ''


def img_tag(path, caption):
    try:
        raw = open(path, 'rb').read()
    except OSError:
        return f'<p class="missing">(screenshot missing: {html.escape(os.path.basename(path))})</p>'
    mime = 'image/png' if path.lower().endswith('.png') else 'image/jpeg'
    b64 = base64.b64encode(raw).decode()
    cap = f'<figcaption>{caption}</figcaption>' if caption else ''
    return f'<figure><img loading="lazy" alt="{html.escape(re.sub("<[^>]+>", "", caption or "screenshot"))}" src="data:{mime};base64,{b64}">{cap}</figure>'


def main():
    data = json.load(open(NOTES, encoding='utf-8'))
    meta = data.get('meta', {})
    entries = data['entries']
    commits = git('log', '--reverse', '--pretty=%h|%s', 'main..HEAD').splitlines()
    stat = git('diff', '--shortstat', 'main...HEAD')
    branch = git('rev-parse', '--abbrev-ref', 'HEAD')

    nav = ''.join(f'<a href="#{e["id"]}">{html.escape(e["title"])}</a>' for e in entries)
    body = []
    for n, e in enumerate(entries, 1):
        imgs = ''.join(img_tag(i['path'], i.get('caption', '')) for i in e.get('images', []))
        det = ''.join(f'<li>{d}</li>' for d in e.get('details', []))
        meta_bits = []
        if e.get('commit'):
            meta_bits.append(f'commit <code>{html.escape(e["commit"])}</code>')
        if e.get('tests'):
            meta_bits.append(html.escape(e['tests']))
        if e.get('files'):
            meta_bits.append(html.escape(e['files']))
        kind = e.get('kind', 'feature')
        body.append(
            f'<section id="{e["id"]}" class="entry {kind}"><div class="num">{n:02d}</div><div class="main">'
            f'<h2>{html.escape(e["title"])} <span class="tag">{html.escape(kind)}</span></h2>'
            f'<p class="lead">{e["summary"]}</p>'
            f'{"<ul>" + det + "</ul>" if det else ""}'
            f'{"<div class=shots>" + imgs + "</div>" if imgs else ""}'
            f'{"<p class=meta>" + " &middot; ".join(meta_bits) + "</p>" if meta_bits else ""}'
            f'</div></section>'
        )
    commit_rows = ''.join(f'<tr><td><code>{html.escape(c.split("|")[0])}</code></td><td>{html.escape(c.split("|", 1)[1])}</td></tr>' for c in commits if '|' in c)
    css = open(os.path.join(HERE, 'report.css'), encoding='utf-8').read()
    doc = f'''<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>House power: the overnight build</title><style>{css}</style></head><body>
<header class="hero"><div class="wrap">
<p class="eyebrow">Overnight build &middot; {html.escape(time.strftime("%Y-%m-%d %H:%M"))}</p>
<h1>House power, while you slept</h1>
<p class="sub">{meta.get("intro", "")}</p>
<div class="chips"><span>branch <code>{html.escape(branch)}</code></span><span>{len(commits)} commits</span><span>{html.escape(stat)}</span><span>nothing merged, nothing deployed</span></div>
</div></header>
<nav class="toc"><div class="wrap">{nav}</div></nav>
<main class="wrap">
<section class="read-me"><h2>How to read this</h2>{meta.get("how", "")}</section>
{"".join(body)}
<section class="commits"><h2>Commits on the branch</h2><table>{commit_rows}</table>
<p class="meta">To look at it yourself: <code>bash scripts/demo.sh</code> from the branch's worktree (made-up house, its own temporary database, never your real server). To keep any of it, ask for the pieces you like to be made into pull requests.</p></section>
</main><footer><div class="wrap">Everything here ran on a made-up house on this computer. No real device was written to and nothing was deployed.</div></footer></body></html>'''
    open(OUT, 'w', encoding='utf-8').write(doc)
    print(OUT, round(len(doc) / 1e6, 2), 'MB')


if __name__ == '__main__':
    sys.exit(main())
