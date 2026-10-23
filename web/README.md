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
  and the PC as an "also measured" slice that is never added to anything.
- **History**: the house over a period, with up to three circuits, plugs or the PC drawn on their own lines.
- **Appliances**: a list of what used the energy, with the derived remainder and cost. Several plugs can be grouped under one name (an entertainment center) and show as one row. Click an appliance or a group for its own page; click a computer for
  the full breakdown (the whole PC at the wall, its peripherals, a chart, and what its collector reports: sensors, programs, times it was not recording).
- **Settings**: devices, computers (tie a wall plug and peripherals to each), appliance groups, the price, rename or hide a channel (changes only this server's label) and a theme.

The address says what is open (`#/appliances/computer/<id>`), so back, reload and a copied link all land in the same place.

The dashboard checks the server's `X-KVG-Api-Version` and refuses to show data from a different major version of the contract.
