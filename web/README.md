# web

The house dashboard: Vite + TypeScript + Preact, with uPlot for the chart. Mobile first, themed with the vendored
VisualAssault CSS (`src/themes/themes.css`, pinned to a tag by `scripts/update-visual-assault-web.sh`, never hand-edited).

`npm run build` writes the finished site into `../server/internal/webui/dist`, and the Go server embeds it (`go:embed`), so one
binary carries the server and its page. That folder is not committed; build `web/` before building the server.

```bash
cd web
npm install
npm run gen:types     # src/api-types.ts from ../contract/openapi.yaml (committed; CI checks it is current)
npm run check         # typecheck, tests, build
npm run dev           # vite on :5173, proxying /api to a server on :8080 (run the server with -sim)
```

## What it shows

- **Live**: the house (the two mains clamps added), circuits ranked by watts, "everything else" (house minus circuits, derived),
  and the PC as an "also measured" slice that is never added to anything. When some meters are starred as favorites in the Hierarchy tab (saved on the server), a row of chips (House, then each favorite) sits on top; a chip shows that meter's page, or the computer's, in place of the house.
- **History**: the house over a period, with up to three circuits, plugs or the PC drawn on their own lines.
- **Hierarchy**: a flow chart on top (the whole house on the left, each meter's band as wide as the energy through it over the period, a parent's parts to its right and what is left as "Everything else"; the same chart is on the Live page and on a meter's page), then every meter as one tree across all monitors (a sub-panel holds its circuits, a circuit holds its plugs; several top-level entries are fine), with its live reading, and the energy and cost it had over the period picked at the top (the same picker as Live: hour to all time; only those two columns follow it). An arrow on a row opens its wiring (clamp direction, 120/240 V and the other clamp, type, which panel); meters move with the arrows, by dragging onto the meter they are inside, or from a list; the eye hides one. The open link on a row (›) opens that meter's page: energy, cost, right now, highest draw, carbon, what is inside it (with "Everything else") and a chart. A computer's wall plug opens the computer with its full breakdown (the whole PC at the wall, its peripherals, a chart, and what its collector reports: sensors, programs, times it was not recording). Appliance groups are listed under the tree.
- **Settings**: what gets the app working: the price, and a theme. Meters are laid out and renamed in the Hierarchy tab.

The address says what is open (`#/hierarchy/computer/<id>`), so back, reload and a copied link all land in the same place.

The dashboard checks the server's `X-KVG-Api-Version` and refuses to show data from a different major version of the contract.
