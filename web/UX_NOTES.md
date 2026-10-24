# Dashboard UX pass: notes for whoever picks this up

Branch `claude/responsive-ui-optimization-qnij7z` (PR #75). Presentation only: `web/src/styles.css`, component markup, small
display helpers. No change to `api.ts`, `api-types.ts`, `lib.ts` logic, `themes/`, the server, the contract or the C# app.

## Working rules used (so two sessions do not collide)
- Presentation in `web/` is this pass's; the server, contract, shell, C# and new API behavior belong to whoever is doing features.
- A feature that needs a new card or tab: add it, then make it use the shared pieces below instead of its own CSS.
- Merge `origin/main` before each push; keep pushes small.

## Who is working on what (updated 2026-10-24)
- The feature session ("Overnight build on a demo branch", branch `claude/overnight-build-0d0d9b`) adds runs and always-on cards
  (`RunsCard.tsx`, `BaseLoadCard.tsx`) and the demo house. It touches `LiveView.tsx`, `MeterPage.tsx`, `api.ts`, `api-types.ts` and appends to
  `styles.css`; a trial merge conflicts only in `styles.css` (both append at the end). Its CSS should live in its own file or above the
  "UX pass" marker; whoever lands second merges main.
- New cards should be `<section class="card">` with `Hint`, `Skeleton`, `.bars`, `var(--muted)` so they fit phone, desktop and ultra-wide.

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

## Ultra-wide (3440 and 5120 px, 1440 or 2160 tall)
Everything is in the last block of `styles.css` and only applies from 1800 px up. The page becomes a grid of 6, 8 (from 2800 px) or 12
(from 3800 px) tracks (`--cols`), the text grows (`html` 112.5% / 125%; `--s` is the same scale for what is drawn in pixels, see
`uiscale.ts`), and each page arranges its cards: Live is a band of figures and chart over a row of tiles (rows stretch to the tallest);
Hierarchy puts the flow chart (sticky) beside the tree, with Devices/Computers/Groups in a third column from 3800 px; History's chart
takes the height that is left; Settings and Help are columns of readable width. Cards pick their width with `--card`, `--hero`, `--chart`,
`--wide`, `--flow`; a card that should not follow them is a `.tile`. Not a media query for phones: the 720 px and container-query rules still decide those.

## How it was checked
`npm run check`, plus screenshots at 390 px, 1366 px, 3440x1440, 5120x1440 and 5120x2160 against `go run ./cmd/housed -sim` (seed history/computers by writing to its
SQLite file or POSTing `contract/examples/ingest-pc.json` with fresh times). All 14 themes were looked at on the Live page.

## Still open
See the bottom of the PR description for the current list.
