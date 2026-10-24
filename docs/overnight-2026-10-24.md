# Overnight build, 2026-10-24

Everything here is on one branch, never merged or deployed, and was run only against the simulated demo house on this computer.
The HTML report with screenshots is built by `python scripts/overnight-report/build_report.py` (git-ignored output); the entries
behind it are in `scripts/overnight-report/notes.json`.

To try it: `bash scripts/demo.sh` (made-up house, its own temporary database, port 8097).

## What was built

- **Demo house**: garage sub-panel, desk PC, three weeks of history (`-sim-days`).
- **Runs**: each time a meter was working (an EV charge, a dryer load) on the meter page, `GET /api/v1/meters/{id}/runs`.
