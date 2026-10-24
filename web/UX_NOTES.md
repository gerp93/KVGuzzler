# Dashboard UX pass: notes for whoever picks this up

Branch `claude/responsive-ui-optimization-qnij7z` (PR #75). Presentation only: `web/src/styles.css`, component markup, small
display helpers. No change to `api.ts`, `api-types.ts`, `lib.ts` logic, `themes/`, the server, the contract or the C# app.

## Working rules used (so two sessions do not collide)
- Presentation in `web/` is this pass's; the server, contract, shell, C# and new API behavior belong to whoever is doing features.
- A feature that needs a new card or tab: add it, then make it use the shared pieces below instead of its own CSS.
- Merge `origin/main` before each push; keep pushes small.

## Shared pieces added
- `Hint` (`Hint.tsx`): long explanations go under an "About this" link, not in the card body.
- `Skeleton` (`Skeleton.tsx`): the first-load placeholder.
- `Spark`/`useTrail` (`Spark.tsx`): a trend line from live readings.
- `TabIcon` (`Icons.tsx`): the tab bar's icons.
- `PeriodPicker` shows short labels on a phone.
- Lists of meters (`.bars`) restack by **container width** (`@container`, cards are containers), not by screen width.
- `--on-primary` / `--on-top` (styles.css): black or white text chosen from a theme's fill, since the vendored themes give no text colour for them.
- `.page-narrow` on a page's first card makes the page one column of 44rem (Settings, Help).
- `.wide` on a card spans both columns on a desktop.

## How it was checked
`npm run check`, plus screenshots at 390 px and 1366 px against `go run ./cmd/housed -sim` (seed history/computers by writing to its
SQLite file or POSTing `contract/examples/ingest-pc.json` with fresh times). All 14 themes were looked at on the Live page.

## Still open
See the bottom of the PR description for the current list.
