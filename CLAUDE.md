# CLAUDE.md - KVGuzzler

A PC power-consumption monitor and electricity-cost estimator (Avalonia / .NET, Windows + Linux).

**This repo follows [gerp93/KVG_Standards](https://github.com/gerp93/KVG_Standards)** - read its
[`dotnet-avalonia.md`](https://github.com/gerp93/KVG_Standards/blob/main/dotnet-avalonia.md) before
changing release, update, theming or database-location code, and use its `app-standards` skill for
audits. Gaps in the standard are fixed there, not worked around here.

## Commands

```bash
dotnet build KVGuzzler.sln -warnaserror     # warnings are errors, as in CI
dotnet test KVGuzzler.sln                    # unit tests + headless Avalonia UI tests
dotnet run --project src/KVGuzzler           # a development run (separate -dev data folder)
```

## Repo layout and releases

The repo also holds a whole-house energy monitor beside the PC app: `contract/` (OpenAPI between the parts, `npm test`),
`server/` (Go house server, see its README; `go vet ./... && go test ./...`), `web/` (TypeScript dashboard, embedded in the server
binary; build it first: `cd web && npm run check`) and `shell/` (the Electron viewer: a window that loads the dashboard from the server and keeps only the server's address; `cd shell && npm run check`). The PC app stays C#: sensors and per-program shares only
exist on the PC. The Avalonia window is **frozen** (no new features; new UI goes in the dashboard) and is retired only once the
dashboard does everything it does; the collector service stays. **Desktop releases and the .NET CI are path-filtered** (`auto-release.yml`, `ci.yml`): only
changes under `src/`, `tests/`, `KVGuzzler.sln`, `Directory.Build.props` (and `VERSION_BUMP.md` for releases) cut a desktop
release, so server/web/viewer/docs-only pushes do not. To force a desktop release with no code change, edit `VERSION_BUMP.md`.

## Rules for the house server (`server/`, `contract/`)

- Devices are only **read**. The Refoss client has a method allowlist (no `*.Set`); a device host is a plain host/IP, never a URL,
  path or port (`internal/hostcheck`). Refoss local auth is turned off on the devices (a 401 is reported, not worked around).
- Scopes (`mains | circuit | plug | pc`) are never summed across each other: the house is the `mains` meters alone, circuits
  and plugs are parts of it, and the PC's wall plug is a slice of a circuit and is added to nothing.
- A reading above its meter's cap is dropped, not averaged, and said so in the notes. Time a device did not answer is not credited.
- Pricing in the server (`server/internal/rates`) is a **port** of `KVGuzzler.Core.Billing` (bills, flat fees, `RateSchedule`); its tests are
  `BillingTests.cs` ported case for case. A change to a pricing rule goes in both places, with the test in both. Money is exact (`big.Rat`),
  never a float; bill dates are local calendar days. "One rate", "my bills" and the **OpenEI lookup** exist there (the carbon footprint is ported too, below). The lookup is a port of `OpenEiClient`/`Tariff`/`UsageProfile`
  (`server/internal/openei`, `rates/tariff.go`; the parsing cases are ported one for one): the free API key is saved **write-only** on the server (`PUT /openei`, never
  returned, sent to `api.openei.org` only, never in an error text), the chosen rate is read again by its label (a browser never sends a grid), a tariff over a week old is
  read again (the old one stays if that fails), and a day's price is the grid weighted by the hours the house really uses power (the mains clamps' last 60 days, else the
  computers' wall plugs; learned at most hourly) when the tariff varies by time. `RefreshRates` runs hourly from `housed`. **Not checked against the real service.**
- **Carbon footprint in the house server** (`server/internal/carbon`, a port of `KVGuzzler.Core.Carbon` with its cases): the grid's intensity per hour from the US EIA
  (generation by fuel for a balancing authority, IPCC life-cycle factors, storage and imports left out, partly reported hours skipped; **EIA only: Electricity Maps was left out of
  the server because a free key did not give the data wanted**, and a setup saved for it is ignored), saved per hour in `carbon_intensity` by a feed run from `housed` (every 30 min, retry after 5 min or an hour when told to slow down, follows the saved setup).
  `PUT /carbon` takes a reading first and **only keeps a setup that got one**; the key is write-only, never returned, never in an error text, and a key left out keeps the saved one.
  **Secrets (the EIA and OpenEI keys) live in `secrets.json` beside the database, mode 0600, not in the database** (`Store.Secret`/`SetSecret`): the database is copied by the nightly
  snapshots and shared read-only, and a key should not travel with the history. A key an earlier version left in the meta table is moved there the first time it is read. Each meter and the PC's sensors carry `carbon_g` (each hour's kWh at that hour's
  intensity, the average of the hours that have one for hours that do not, and `carbon_estimated_kwh` says how much); it is the grid's average, never "what one more kWh would
  cause", always shown as ≈. The daily CSV gets `pc_co2_kg`/`peripherals_co2_kg` once any intensity was saved. **Not checked against the real services.**
- **A device's address and name can be changed** (`PATCH /api/v1/devices/{id}`, the Edit button in the Hierarchy tab's Devices section; `devices.Manager.Update`). Ids are made from the address a device was first added at (`shelly-<host>`, `refoss-<host>:em3`) and **never change afterwards** (a device that is one meter, a plug, has one name: renaming the device renames its meter, and the tree shows it read-only; a monitor's inputs are named in the tree): the device is rebuilt at the new address with its first id (`SetIdentity` on the Shelly and Refoss sources, also used by `Restore`), so its meters, their settings and their history stay what they were. A new address is read once first; if nothing answers like the device, nothing changes and the old one keeps being polled (502).
- Plugs: `server/internal/shelly` is a port of `ShellyPlug` (one read-only GET; a plug often has the PC or a fridge on it, so nothing may
  switch it). A device is read once before it is saved, so a wrong address is refused with 502; a saved device is restored after a restart
  even if it is offline. The PC's own wall plug is added with scope `pc`: a slice, added to nothing.
- **Deployment settings are never committed.** The box's address, the port and the server settings (`HOUSED_TZ`...) live in
  `~/.config/kvguzzler/deploy.env` on the PC and `/etc/housed/housed.env` on the box; the repo has only `scripts/deploy.env.example` with
  placeholders. Deploy with `scripts/deploy-server.sh` (see `server/README.md`); it restores the old binary if the new one does not answer.
- **Runs** (`server/internal/runs`, `GET /api/v1/meters/{id}/runs`, `web/src/RunsCard.tsx`): the times a meter was working, found from the stored minutes only. The working level is a quarter of the way from the meter's
  10th to its 99th percentile (never below 10 W; the 99th so a meter that works only a few percent of the time, a water heater, still has a level), learned from at least the last 30 days so an hour is judged like a month, or `min_watts`; a meter whose spread is under 15 W has no level and no runs (`threshold_watts` null,
  the card hides). A dip of up to 5 minutes does not end a run, under 3 minutes is not a run. A run is listed in the period it **starts** in and is followed 12 h past the period's end; its cost is its energy at the price of the
  day it started (null without a price, never zero), the totals count all runs even past the 500 listed. Runs are a view of one meter's own reading: never added to anything.
- **Always on** (`server/internal/baseload`, `GET /api/v1/baseload` for the house, `GET /api/v1/meters/{id}/baseload`, `web/src/BaseLoadCard.tsx`): the 5th percentile of a meter's minute averages over the period (not the lowest minute),
  with the average of the same minutes for its share, and a year of it priced at today's price (null without a price); under 60 minutes of data answers 204. The house is the mains legs added minute by minute, leaving out
  a minute where a leg is missing. `parts` (house only) are the floors of the meters that hold no other meter: **each is its own floor, never added to each other or to the house's**.
- **Lowest draw** (`minutes.avg_w`, `MeterSummary.min_watts`, `Summary.house_min_watts`, `Lowest` in `Watts.tsx`): the lowest average of a **whole minute** (at least 30 s recorded) in the period, shown as "lowest ..." in the Highest draw tile of the Live page and of a meter's page.
  It is a minute's average, not a single reading (only the highest second is kept), and nullable (no whole minute: nothing, never 0). **The house's is the mains legs added minute by minute** (`Store.LowestMinuteWatts`, a minute where a leg is missing is left out), never the legs' lowest minutes added;
  with no whole-house clamp the Live tile has no lowest (the top-level meters' lowest minutes do not coincide). A computer's page has none yet.
- **Amps and breakers** (`minutes.max_a`, `meters.breaker_amps`, `web/src/amps.ts`, `Breaker.tsx`): the highest current of each minute is kept next to the highest power (nullable: a meter that reports none has none, never 0 A) and shown
  as `max_amps` / `peak_amps`. **A 240 V pair carries the same current in both legs: its amps are the larger leg's, never the two added** (live in the Refoss source, in the stored minutes and when pairing merges old history),
  and its volts are the two legs added. A breaker size (1 to 1000 A, circuits only in the UI) is only used to say what share of it a current is. The 80% rule is for a load that runs 3 hours or more, so
  (`amps.ts`, `breakerVerdict`) a run of 3 h+ whose average current (`minutes.avg_a`) was 80% of the breaker or more is the warning; a single high minute is only a note when the meter's runs are known; above 100% always warns.
- **CSV exports of the runs and the tree** (`export_tables.go`): `GET /meters/{id}/runs.csv` lists every run (the JSON lists the newest 500) through the same `computeRuns` as the card, and `GET /tree.csv` lists every shown
  meter in Hierarchy order with `inside_count`; a parent's row contains its children's, so those rows are never meant to be added. Labels go through `csvCell` (nothing runs as a formula).
- **Unusual days** (`server/internal/anomaly`, `GET /api/v1/anomalies`, `web/src/AnomalyCard.tsx`): a complete local day is unusual when it is far above the median of the same kind of day (weekday or weekend) over the 4 weeks before it,
  by three measures at once (1.5x the usual, 3.5 typical swings, and at least 1 kWh for the house / 0.3 kWh for a meter more); under 4 earlier days of the kind says nothing, today is never judged. A meter whose day is explained by
  something that holds it being unusual the same day is **named as a cause, not listed again**, and causes are already in the figure, never added to it.
- **Page look** (`design.css`, `Kpi.tsx`): a page starts with a head (`.page-head`: its name, a line of context, the period) and **figure tiles** (`Kpi`: label with icon, the figure large with its unit quiet, a line saying the period) - one lead tile
  (`now`, with the trend) and the period's totals. Related things go in a `.panel` (one surface; the `.card`s inside it are its sections, divided by lines, not boxes in boxes). Colour is carried by small marks (a tile's edge and icon), text stays in the theme's text
  colours, and everything comes from the theme's custom properties. **With no whole-house clamp the Live page is "Everything measured"** (`measured.ts`: the top-level circuits, plugs and computers added, never anything inside another meter) so the tiles, chart and flow
  chart have real figures; it says it is not the whole house. **With one, the Circuits table lists only clamped circuits and ends with "Everything else"** (the house less those circuits: `unmetered_kwh`), with the plugs and computers that name no circuit they are on listed inside it (`notMetered.ts`, indented, never added to it) and, last, "Not individually metered" (what none of them explains). Without a house figure they are plain rows of the table.
- **Ultra-wide layouts** (`styles.css`, from 1800 px; each page has its own, measured in the browser, not left to one grid): Live is the head and tiles, then bands (`.band-main`: chart | circuits, `.band-flow`: where it goes | insights) of panels as tall as the
  tallest in the band, then the tools panel; a meter, computer or group page is the head and tiles over 2/3/4 CSS columns (`column-span: all` on the head, tiles and notes, `break-inside: avoid` on cards) so there is never a card in
  a box of empty space; Hierarchy is the flow chart and `.tree-extra` (devices, computers, groups) beside the tree; History is the chart and a full-height compare column. Below 1800 px the bands are `display: contents`, so the DOM order
  (chart first on a meter page) is the small-screen order. A new card goes in the band or column it belongs to, never loose in `main`.
- **Computers** are their own kind of appliance (`/api/v1/computers`): a name, optionally a PC collector that reports its insides
  (`source_id`), one wall plug (a `pc`-scope plug: the whole PC at the wall) and any number of peripherals (`plug`-scope plugs, each a separate
  device). A collector's first push adds its computer; a plug added as "my PC's plug" joins the one computer without a wall plug, else gets a
  computer of its own. Only plugs can belong to a computer. A computer's plugs are listed under it, never alone, and none of it is added to the
  house or circuit figures; "setup" (wall plus peripherals) is a separate labelled figure. Open its wall plug in the Hierarchy tab for the Guzzler-style breakdown. In the Hierarchy tab each computer is a row in the tree's columns (`ComputersCard.tsx`) showing that setup total (Now, Energy, Cost over the picked period), with an arrow that opens its name, wall plug and peripherals.
- **Groups** (`/api/v1/groups`) are **logical groupings, not places in the wiring**: any meters except the mains (a plug, a circuit, a computer's plug) shown as one line
  with their total over the period, for things like an entertainment center on two power strips or a desk and its computer. A meter is in one group at a time, and a
  group never moves a meter in the tree or changes any figure of the meter itself. The group's total adds its members' energy and cost, **except a member that another
  member already contains** (a plug on a circuit that is in the same group, through `parent_id`, however deep): it is listed (`not_added` in the detail) but not
  added a second time. The cost is shown only when every counted member has one. A hidden meter is neither shown nor counted. Groups are managed in the Hierarchy
  tab (`GroupsCard.tsx`: name, members chosen from a list, totals in the same columns as the tree) and each has a page (`#/hierarchy/group/<id>`).
- **Electric vehicles** are a mark on a meter, not a second thing to wire (`ev` on a meter, `store/ev.go`, `api/ev.go`, `EvCard.tsx`): the Hierarchy tab's "This meter charges an
  electric vehicle" box, only for a circuit or a plug (the mains is refused), and the meter is measured and summed exactly as before. Its page then takes **odometer readings** (`odometer_readings`:
  a local calendar day and the miles the user typed; nothing is read from the car). A reading counts as taken at the start of its day, so a stretch runs from the start of one reading's day to the
  start of the next's, and `GET /meters/{id}/ev` divides that stretch's miles into the meter's energy and its price (`meterSummary`, so the same `RateSchedule` as everywhere): cost per mile, miles per kWh
  at the wall (charging losses included), and `since_last` for the days after the latest reading, which has no miles yet. An odometer only goes up (a reading out of order is refused); a stretch with no energy
  recorded has no cost per mile, never a free one; `coverage` says how much of a stretch the meter was measured (an unanswering device is not credited, so a low figure means the cost is too low) and the
  card says so. Turning the mark off keeps the readings. There is no gas comparison yet: it is the cost per mile the user sets beside a gas car's.
- **Panels nest** (`parent_id` on a meter, `store/parent.go`): a `circuit` that feeds a sub-panel holds the circuits clamped inside it, and a plug, a computer's
  wall plug (`pc`) or a peripheral says which circuit it is on. A circuit with no parent is on the main panel, under the house; there can be several top-level
  entries and nothing needs a main monitor to exist. The parent's own reading already contains its children's, so they
  are **never added to it**: what is left is shown as "Other (not metered)" (parent minus children, in the dashboard, per parent), and the house's `unmetered_kwh`
  takes off only the top-level circuits. Any meter but the mains can have a parent, the parent is a circuit, a loop is refused (409), and a meter that becomes the
  mains or the second leg of a pair leaves the tree (its children go to the top). The parent may be on another device (a garage monitor's feed is a circuit of the
  main panel's monitor). The **Hierarchy tab** (`web/src/HierarchyView.tsx`) is where meters are laid out and wired: one tree across all monitors, an arrow on each row opens its wiring (clamp direction, 120/240 V with the other clamp, type, which panel), meters move by the arrows, by dragging onto the meter they are inside, or from the "which panel" list, and the eye hides one. Settings no longer lists meters, devices, computers or groups: they are set up in the Hierarchy tab (the **+ Add** button, `AddPanel.tsx`, adds a Shelly plug, a Refoss monitor, a computer or an appliance group; the Devices, Computers and Appliance groups sections sit under the tree). Settings is only for what gets the app working (price, carbon, database, theme). **A computer's parts** (`pcparts.ts`): a PC's wall plug that belongs to a computer has type `computer`, and the CPU, GPU and memory its collector reports are shown as read-only rows under it in the Hierarchy tab and as bands under it in the flow chart. They are **not stored meters** (they come from the computer's detail for the period, and only count while the PC is sampling); the wall plug's reading already contains them, so what they leave out is "Other (not metered)" (the wall plug less the parts, never the parts added to it). Their cost is the wall plug's average price over the period, so it is shown as approximate. Programs are estimates with no figure of their own in the tree, so they are only on the computer's page. A circuit with only plugs on it is a circuit, not a sub-panel (only circuits inside it make it one). **The flow chart** (`flow.ts`: the tree and its layout, pure and tested; `FlowChart.tsx`: the SVG; a plug or a computer's wall plug that names no circuit it is on is plugged into a circuit nothing clamps, so it is a part of the house beside the metered circuits at the top level, its energy comes off "Other (not metered)", and with no whole-house clamp the root is "Everything measured", not "all circuits"; every band says its energy and its cost, and a meter that used nothing over the period has no band) draws the same tree as bands as wide as the energy through them, on the Hierarchy tab, the Live page and a meter's page; a parent's band is its own reading, its children are drawn from it and what is left is "Other (not metered)", so no band is ever the sum of its parts. **A meter's own page** (`MeterPage.tsx`, `#/hierarchy/meter/<id>`) shows its figures, what is inside it nested with an "Other (not metered)" line, and a chart; a computer's wall plug opens the computer. **Favorites** (`favorite` on a meter, a star on each Hierarchy row, saved on the server so every device agrees) appear as chips on the Live page next to "House"; picking one shows that meter's page (or the computer) in place of the house. Which chip is picked is a per-viewer convenience in localStorage.
  **A meter has a `type`** (`house | sub_panel | circuit | device`, `model.MeterType`): what it is, in the user's words. It describes; the math still follows `scope` and the tree.
  A Refoss meter's type is the user's to choose (house = the mains scope; sub_panel and circuit are both the circuit scope, told apart by the saved `type` column), a plug or the
  PC's wall plug is a device, and a circuit with circuits inside it reads as a sub-panel until the user says otherwise (`Store.settleTypes`).
- The PC collector pushes to `POST /api/v1/ingest/pc` (idempotent per source, minute and name, so the backlog can be resent). A reading above its
  cap is dropped and said so; a different major `api_version` is refused with 426. The C# client is `RelayFeed` in `KVGuzzler.Core/Relay`, run by the collector service beside the carbon feed: **`relay.json`** beside
  `wall-meter.json` (`{"host": "192.168.68.110"}`, or `host:port`; default port 8080; a plain host only, never a URL) says where the house server
  is, and without it nothing is sent. It **replays `history.db`** (no separate outbox): the watermark of the last minute sent is in `meta`
  (`relay_watermark`), only whole minutes older than two minutes are sent, the first time only the last 7 days, batches of up to 360 minutes. A server
  that is away is retried every minute, a different `api_version` every 10, and a batch the server refuses three times in a row is skipped so it cannot
  hold up the rest (it stays in this PC's own history). The push's field names are checked against `contract/examples/ingest-pc.json` in the tests.
  The source id (`relay_source_id`) is made once and kept. The first push from a new source adopts the only computer when it has no collector.
  **The live look** rides beside the minutes: every 5 s, when the collector saved a newer look than the last one sent, `RelayFeed.LiveStepAsync` pushes a
  minutes-free batch with `live` (the readings with their kinds from the last saved minutes, the per-program watts only while fresh, notes, the learned
  idle watts) and the 50 newest `runs` (a run still going grows, sending it again replaces it). It is not history: it never touches the watermark, has its
  own retry timer and `LiveStatus` (a missed look never hides what happened to the history), and `live` is left out of a minutes push (the contract has
  no null for it). The server keeps only the newest look per source (`pc_live`, by its `at`; a late older one does not replace it), drops a look whose
  time makes no sense without refusing the minutes with it, and gives the dashboard its **age by the server's own clock** (`age_seconds`, never the PC's):
  past 30 s the page calls it "not answering". `GET /api/v1/computers/{id}/live` is the one-row version the page asks for every 5 s.
  `GET /api/v1/computers/{id}/series` gives one line per sensor, the PC's own wall reading and each meter it reads (never the "inside" parts of a
  sensor, never added together) plus the gaps, which the chart shades instead of drawing a line across them.
- **The viewer (`shell/`) shows only the server.** The address is a plain host and port, never a URL with a path/user/query (checked on the way in
  and again when read); the window only navigates to the server's origin or its own two pages, anything else opens in the browser (http/https
  only); sandboxed, context-isolated, and only its own `file:` pages may use the IPC (the remote dashboard gets no bridge). It bundles no copy of the
  dashboard, so a server update updates every UI. **Releases:** `release-viewer.yml` (manual) publishes the unsigned installer as a **pre-release** tagged `viewer-vX.Y.Z`, never a plain `vX.Y.Z`: those tags are the desktop app's, and its Velopack updater reads this repo's releases (non-pre-releases only). The viewer's Help > Check for updates only offers the download page; there is no silent updater.
- **The main dashboard (Live) has the same kinds of figure as a computer's page**, for the house (the mains alone): period picker, energy, cost, right now, max draw
  (`house_peak_watts`, an upper bound: the legs' peaks added), the carbon line, the rate note, each circuit's energy/cost/share of the house, `GET /estimate`,
  `GET /history` and `GET /export.csv` (the house and each circuit per day) and a house chart. None of it adds a circuit or plug to the house.
- Meter ids never contain a slash (they are URL path segments). Money is a decimal string. The contract's integer `api_version`
  changes only for a breaking change, and the server and its clients move together. The one rename that did not bump it: "channel" became "meter"
  everywhere (API paths, `meter_id`, the `meters` table; `store.renameChannelsToMeters` moves an older database on open), because only the embedded
  dashboard reads those endpoints and the PC collector's ingest body never had the word (see `contract/README.md`).
- **"Meter" is the one word** for anything measured, in the tree and in the UI: a circuit, a sub-panel's feed, the whole house, a plug, the PC's wall plug.
  A Refoss clamp socket is an "input" in prose, a Refoss monitor is a device. Do not bring "channel" back.
- **Refoss monitors** (`internal/refoss`, only the **EM16P** has been seen: `testdata/status-em16p.json` is a real answer, with the network details taken out; a repository
  fixture must never hold the Wi-Fi name, MAC or address). `Refoss.Status.Get` answers one object per input, `em:1` to `em:18`, with signed `power`: A1-A6 (`em:1-6`), B1-B6 (`em:7-12`)
  and C1-C6 (`em:13-18`), named for the phase whose voltage they use. A US split-phase panel reads about 118 V on A and B and nothing on C. A **240 V breaker has a clamp on each of
  its two legs, one on an A input and one on a B input, and is one circuit: the two added** (`pair_with` on the meter; the other leg is switched off and not reported on its own).
  A clamp on backwards reads negative on a load: `invert` (Flip sign) fixes it in software; a reading still negative after that is dropped and **said so by name** in the notes (the
  `source.Noter` interface), never counted as zero. Every input is a meter when the monitor is added, and the ones with nothing on them at that moment are switched off (they come
  back in Settings). All are circuits until set to `mains` per meter (a monitor on the main panel). A clamp's sign and pairing are the user's: **never change them for the user**. **Pairing two clamps merges what each recorded before into the circuit's history** (`Store.mergeLegHistory`: energy added exactly minute by minute, the highest readings added, the leg's own rows removed so pairing again cannot add them twice); without that, a circuit paired after it had been recording reads only one leg for as far back as they were apart.
  **A device registers its meters again every time the server starts** (`Store.UpsertMeter`), and a Refoss input always says "circuit": a meter the user made the house (scope `mains`) must keep it, or each restart
  (a deploy, a power cut) silently turns the whole-house figure off and the Live page adds the top-level meters instead (a garage panel counted twice). `TestAClampsSignAndScopeAreSetAndKept` checks it.
  The monitor is only ever read (`Refoss.DeviceInfo.Get`, `Refoss.Status.Get`); the energy is integrated by the collector from the instantaneous power, as for a plug.
  **Not checked on an EM06P or on the main panel** (only a garage sub-panel's three circuits).

## Rules that are easy to break

- **Never hand-edit vendored files.** `src/KVGuzzler/KvgDbLocation/` and (when wired)
  `src/KVGuzzler/Themes/VisualAssault/` start with a `// Vendored-from:` line. Fix bugs upstream
  (KVG_Standards / VisualAssault), then re-run `scripts/update-kvg-dblocation.sh <ref>` /
  `scripts/update-visual-assault-avalonia.sh <tag>`. KVG_Standards has no tags yet, so `main` is the
  documented interim pin; VisualAssault must be a release tag, never `main`.
- **Background collector (approved exception to the per-user install, see KVG_Standards'
  `dotnet-avalonia.md`, "Exception: per-machine install").** `KVGuzzler.exe --collector` is the same
  program running headless, as a Windows service (`KVGuzzlerCollector`; `-dev` for a development run).
  It writes `history.db` in `%ProgramData%\KVGuzzler[-dev]`; the window only reads it. The service
  logic is `PowerCollector` in `KVGuzzler.Core` (clock-driven, tested); `Collector/` is the thin host.
  The window never creates `history.db`, only the collector does. A gap is saved with its reason
  (asleep / off / lost power) and shown apart from measured energy; sleep is an estimate shown on its own
  line and never added to the "active" figures (CPU/GPU/memory sensors = active power; the whole-PC
  total is the wall meter's job, not built yet). A running service locks `KVGuzzler.exe`, so turn background recording off
  before rebuilding a development copy.
  **Updates:** the service runs `current\KVGuzzler.exe` from the per-user install, so it must be stopped before Velopack can
  replace the files (otherwise: "one or more running processes prevented it", the old version comes back). The service is created
  with an ACL letting the logged-in user start/stop it (`ServiceInstaller.ServiceSecurity`); `ApplyUpdateAsync` stops it first
  (`EnsureStoppedForUpdateAsync`, UAC only if the ACL is missing, as on a service made by an older build) and the after-update hook
  starts it again.
- **Impossible readings are dropped, not averaged.** NVML answers with nonsense (50-300 kW) on the first read after the PC wakes.
  `PowerSampler` drops any reading above `MaxComponentWatts` (sensors) / `MaxMeterWatts` (plugs) like a missing one and says so in the
  notes; `HistoryStore.RepairImplausibleReadings` (run by the collector at start) fixes rows saved before that. Keep both.
- **Wall meter.** A smart plug between the wall and the PC is the whole-PC figure (`ReadingScope.Wall`).
  It already contains every sensor reading, so it is shown next to them and **never added to them**
  (`PowerSampler` sums `Component` only). `ShellyPlug` only ever sends `GET`; nothing in the app may
  switch the plug, because the PC is plugged into it. The meter's address lives in
  `wall-meter.json` beside the history (not in the per-user settings: the collector service reads it too).
  Only a plain host or IP is accepted, never a URL or path.
  **Gaps are filled from the plug's own counter.** The status answer's `aenergy.total` (Wh since the plug last restarted) is kept with
  every sample (`WallMeterSource.LatestCounter`, saved in `history.db` meta by `SetLive`). After a gap (sleep, or a collector restart
  whose gap is "off"/"lost power") `PowerCollector` waits for the plug's first answer, and when the counter went up (not down: a plug
  restart; not implying more than `MaxPlugWatts`; same plug address; read within `CounterNearGap` of the gap's start) saves the
  difference as `plug_kwh` on the **gap row**, never as a sample row. It counts in `ReadingScope.Wall` totals only
  (`HistoryStore.Query`/`EnergyKwh`, spread evenly over the gap so any period gets its share), never in sensor energy, `Monitored`
  time or the chart lines. A counter that is missing or went down leaves the gap as it was. Up to a second or two at each end is
  counted both sampled and in the counter's difference.
- **Other meters (monitor, speakers).** More smart plugs, each named by the user, in `peripheral-meters.json` beside `wall-meter.json`
  (`PeripheralMetersConfig`, polled by `PeripheralMetersSource`, followed by the collector like the wall meter). Their readings are
  `ReadingScope.Peripheral` (scope 3 in `samples`) and the reading **name is the label**, which is also the history key. Renaming
  (Settings) moves the history with `HistoryStore.RenamePeripheral` (run again ~25 s later, because the collector may still save a few
  seconds under the old name); a label may not be the wall meter's reading name (`samples` is keyed by minute and name). They are
  **never** in the sensor total, the wall figure, the per-program split or the PC's own total peak: every existing query filters by
  scope, so keep it that way. The Dashboard's "Count" row ticks which of PC and meters make up energy, cost, max draw, "right now",
  the chart (one dashed line per meter) and "My usage": `SettingKeys.PeripheralsOff` holds the meters left out (a new meter counts until
  switched off) and `SettingKeys.PcOff` the PC being unticked ("peripherals only", only while meters exist; `includePc` in
  `UsageSummary`/`PowerChart`). The combined max draw is only an upper bound (shown "≤"). **Gaps** are filled per meter from each
  plug's own `aenergy.total` like the wall meter (`gap_peripherals` table, `IPeripheralEnergyCounters`, counters saved in `meta` so a
  restart can fill too); that energy is in `PeripheralKwh`/`QueryPeripherals` (as `GapKwh`), never sampled and never in the chart. A
  meter that stops answering is named in the live notes (`IPowerSourceNotes`). Export (Dashboard > Energy history) is
  `HistoryExport.DailyCsv`, one row per local day; each plug row has an "Open in browser" button (`PlugPageUrl`: only `http://` + a host that
  passed `IsValidHost`, never a path; `MainWindow.UriLauncher` is the test seam); labels that could run as a spreadsheet formula are written as text. Removing a meter
  leaves its history but stops counting it.
- **Rate source is a choice of three** (Settings > Electricity rate > "Price from"; `SettingKeys.RateMode`: `one`, `bills`, `lookup`;
  `RateMode`): one fixed rate, per-bill prices, or a **looked-up tariff** from the OpenEI Utility Rate Database (`OpenEiClient`, window only,
  free key typed once, saved in the settings database and never shown again; the key is a query parameter because that is how OpenEI
  documents it, so it only goes to `api.openei.org`). A tariff is reduced to a 12 months x weekday/weekend x 24 hours grid of $/kWh
  (`Tariff`: the first tier's rate + adj + the month's fuel adjustment; fixed and demand charges are left out, tiers beyond the first are
  ignored and the Settings text says so; rates that cannot be priced per kWh are left out, never guessed). Everything else prices **per
  day** (`RateSchedule.RateAt`), so in lookup mode a day's price is the grid weighted by `UsageProfile` (the share of usage in each hour,
  weekday/weekend, learned from the last 60 days of history on the background read, at most hourly, only for a tariff that varies by
  time). Today's tariff is applied to the past as well; bills are the exact figure. A saved tariff over a week old is fetched again by its
  label when the window opens (the old one is kept if that fails). With a looked-up tariff the Cost card carries a footnote
  (`StatRateNote`) naming the utility, the rate, what the period averaged to per kWh, and that it is the energy charge only. **Not checked against the real service**: written from OpenEI's
  documented field list (the docs give no full sample answer), parsing leaves out what it cannot make sense of.
- **Carbon footprint (two sources, chosen in Settings: Electricity Maps or EIA).** `CarbonConfig.Source` (`CarbonSources`; files without one are
  Electricity Maps). **EIA** (`EiaClient`, US only, free registration key as a query parameter to `api.eia.gov` only) reads hourly generation by
  fuel for a balancing authority (`MISO`, `PJM`, `ERCO`...) and works the intensity out itself: generation-weighted IPCC life-cycle factors
  (`EiaClient.GramsPerKwh`: coal 820, gas 490, oil 650, nuclear 12, solar 45, wind 11, hydro 24, geothermal 38, biomass/other 230), leaving out
  storage (BAT, PS), unknown fuels and imports, and skipping partly reported hours. A request returns about 2.5 days, and the feed saves every
  hour, so hours missed while the collector was off are backfilled (Electricity Maps' free key only gives the latest). A saved setup belongs to
  its source: the other source's key is never sent to the wrong service. The original Electricity Maps notes follow. `carbon.json` in the machine-wide data folder holds a free personal Electricity Maps key and a
  place (zone code or "lat, lon"), set in Settings > Carbon footprint; the key is plain text there and is shown nowhere after saving.
  The **collector service** fetches (`CarbonFeed`, every 30 minutes, own timer, follows the file) `GET /v3/carbon-intensity/latest` with the
  key in the `auth-token` header and saves it per hour in `carbon_intensity`, plus its last outcome in `meta` (`CarbonStatus`, shown in
  Settings). The key goes only to `CarbonConfig.ApiBase` (a constant, `https://api.electricitymaps.com/v4`, never read from the file) and the place is validated and encoded,
  so nothing typed can change the request. The free key has no history, so hours before it was set up or while the collector was off
  have no figure: `CarbonCalculator` values them at the average of the hours that do (and says how much, `EstimatedShare`), and values
  the plugs' counter energy across gaps (no hour of its own) at the period average. Carbon follows the same Count choice as energy
  (`UsageStat.Carbon`), is a grid *average* (location based), never "what one more kWh would cause", and is always shown as "≈".
  Errors carry Electricity Maps' own `message` (never the key) so a refused key can be diagnosed from Settings. The host, `/v4/...` path
  and `auth-token` header were checked to answer (401 without a key) but **a real key has not been tried here**; parsing is tolerant. If
  the API moves again, change `CarbonConfig.ApiBase` and `ElectricityMapsClient.ParseLatest`. The free key is one zone chosen at signup:
  Minnesota (Xcel/NSP) is `US-MIDW-MISO` there and `MISO` at EIA. A free key can be valid yet refused with "You do not have access to this
  specific endpoint for this specific zone"; `scripts/test-carbon-key.ps1` (hidden key prompt) shows which endpoints a key can read.
- **Per-program power is an estimate and must read as one.** `ProgramAttributor` shares only the power above
  the learned idle baseline, by each program's share of CPU/GPU busy time; the baseline and anything no
  program claimed are their own rows, and the rows must add up to exactly the energy that went in (tested).
  Never present these numbers as measurements, and never add them to the sensor or wall figures: they
  split the CPU and GPU energy that is already counted.
  **Program names:** an executable is the program (`chrome.exe`), except interpreters (`ProgramLabeler.IsGeneric`:
  python, node, java, dotnet, pwsh...), which are labelled with what they run, `python.exe (ComfyUI)`, from the
  command line, else the parent program, else the venv folder. Looked up once per process id (`ProgramNamer`, via
  `WindowsProcessInspector`, limited-access handle, no admin); only the short label is kept, never the command
  line (arguments can hold secrets). Labels are history keys, so changing the label rules splits old and new rows.
- **Prices come from `RateSchedule`, never from reading the single rate setting directly.** In "My bills" mode each
  bill's price (total minus its flat fees, over its kWh) applies to the dates it covers; cost of a period is its
  energy priced stretch by stretch (`RateSchedule.Cost`), so a period spanning two bills is right. Bill dates are
  local calendar days. Money is `decimal`, kept as text in the database. Two bills may not share a day.
- **Velopack:** `VelopackApp.Build().Run()` is the first statement in `Main`; the running version comes
  only from Velopack (`UpdateService.VersionText`); `CheckForUpdatesAsync` throws outside an installed
  build, so `UpdateService` guards on `IsInstalled`. The `Velopack` NuGet package must stay a direct
  reference of `src/KVGuzzler` (the release workflow installs the matching `vpk` from it). Never change
  the package id (`KVGuzzler`) after releasing: installed copies find their updates by it. The repo
  must stay public so the updater can read releases without a token.
- **Database:** WAL mode through `Microsoft.Data.Sqlite`. Before moving the file, close every connection
  and call `ClearAllPools()` (`AppServices.CloseDatabase`) or recent writes sit in the `-wal` file and the
  copy is unreadable. Dev runs use the `KVGuzzler-dev` folder; one running copy per data folder
  (`InstanceLock`); a configured-but-missing database path asks the user, it never creates an empty file.
- **Window layout.** Default 1240x860, minimum 960x640; page content is capped at 1200 wide (820 for Settings/Help/Estimator).
  Shared look (cards, text classes `h1/h2/caption/note/hero/mono`, the `seg` period picker, `bar`) is `Views/UiStyles.axaml`;
  list rows share the `BarRow` template. One period picker (`StatsPeriodPanel`) drives the Dashboard stats, chart and program lists.
  A component's power is shown as a **share of the whole PC at the wall** when a wall meter exists (else of the sensor total, and
  the header says so); the chart's whole-PC line uses the theme's `Text` colour because `PrimaryAction` is invisible on dark themes.
- **Four cards across the top of the Dashboard**: energy, cost, right now, and **Max draw** (`HistoryStore.QueryPeak`): the highest
  once-a-second reading in the period, of the whole PC at the wall when there is a meter, else of the sensors' *total*. The total's peak is
  its own per-minute row (`total_peaks`, written by `PowerCollector`) because the sensors' own peaks never all land in the same second;
  minutes from before that table existed only give an upper bound (the sum of their peaks), shown as "≤". The big numbers sit in a
  `Viewbox` that only shrinks, so a long figure still fits the narrower cards at the minimum window width.
- **Cost estimator** (Dashboard, under Energy history) has three bases: a typed constant load, the live draw (wall figure if there is a
  wall meter, else the sensors), and "My usage" - real kWh per day over a window, counting off/asleep time, via
  `UsageSummary.AverageDay`. **Sleep power** is only used for the history detail line when there is no wall meter, so Settings hides it once a
  plug is configured (the plug's own counter fills the gaps, see the wall meter rule above).
- **No menu bar.** Help/About lives in the window (Help tab). No DevTools in release builds.
- **Logo:** none yet and none is to be fabricated. See TODO.md.
- Commit `packages.lock.json` files; package versions float within a major, the lock files pin them.
