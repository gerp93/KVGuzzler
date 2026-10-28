// The Hierarchy tab: every meter the server reads, as one tree. A sub-panel's feed holds the circuits inside it; a circuit holds the plugs on
// it; there can be several top-level entries, and none of it needs a main monitor. Each row opens (the arrow at its left) to the wiring of that
// meter, and moves in the tree by the arrows, by dragging onto another meter, or from its "which panel" list. Nothing here is sent to a device:
// it only changes how this dashboard reads and shows them.
import { useState } from 'preact/hooks';
import { Kwh, Watts } from './Watts';
import { PeriodPicker } from './Periods';
import { api } from './api';
import { meterPath } from './Bars';
import { AddPanel } from './AddPanel';
import { breakerLevel, breakerPercent, formatAmps, formatVolts } from './amps';
import { BreakerField } from './Breaker';
import { ComputersCard } from './ComputersCard';
import { DevicesCard } from './DevicesCard';
import { OdometerNow } from './EvCard';
import { flowTree } from './flow';
import { GroupsCard } from './GroupsCard';
import { ComputerView } from './ComputerView';
import { GroupView } from './GroupView';
import { usePoll, useStored } from './hooks';
import { levelsById, meterState, stateTitle, type MeterLevels } from './levels';
import { MeterPage } from './MeterPage';
import { pcParts, usePcDetails, type PcPart } from './pcparts';
import { routePath } from './route';
import { HierarchyDiagram } from './HierarchyDiagram';
import { planTree, type LumpEntry } from './treePlan';
import { restRows } from './treeRest';
import {
  arrangeByPanel, canHold, clampName, formatMoney, indentTarget, outdentTarget, periodRange, periodWords, shownMeters, underHouse, usualPartner,
  type Computer, type Device, type Live, type Meter, type MeterSummary, type PeriodKey, type Summary,
} from './lib';

type Change = Parameters<typeof api.updateMeter>[1];

const TYPE_WORDS: Record<string, string> = { house: 'whole house', sub_panel: 'sub-panel', computer: 'computer' };

function EyeIcon({ off }: { off?: boolean }) {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M1 12s4-7 11-7 11 7 11 7-4 7-11 7S1 12 1 12z" />
      <circle cx="12" cy="12" r="3" />
      {off && <path d="M3 3l18 18" />}
    </svg>
  );
}

/** How much of the house's power a draw is, in percent, for the bar behind a row; nothing without both figures. */
function shareOf(watts: number | null | undefined, house: number | null | undefined): number {
  return watts != null && house != null && house > 0 ? Math.min(100, Math.max(0, (watts / house) * 100)) : 0;
}

interface Shared {
  all: Meter[];
  /** What each meter used and cost over the period picked at the top, by meter id. */
  figures: Map<string, MeterSummary>;
  estimated: boolean;
  deviceName: (id: string) => string;
  /** A device that is one meter (a plug) is named under Devices, and its meter has that name: the tree does not rename it a second time. */
  isOnlyMeterOfDevice: (id: string) => boolean;
  /** The name of a computer, by id: a plug that is its wall plug or one of its peripherals says so on its row. */
  computerName: (id: string) => string;
  watts: Map<string, number>;
  /** The house's power now, for the share of it each row draws as a bar behind it. */
  houseWatts: number | null | undefined;
  /** Each meter's learned resting and working levels, to say whether it is working or resting right now. */
  levels: Map<string, MeterLevels>;
  /** Each meter's live volts and amps, when it reports them. */
  currents: Map<string, { volts: number | null | undefined; amps: number | null | undefined }>;
  dragging: string | null;
  setDragging: (id: string | null) => void;
  onChanged: () => void;
}

/** `rows` and `at` are the rows the move arrows read; `depth` is how far in the row is drawn (the plan may put it deeper, as under "Everything else"). */
function Row({ meter, rows, at, depth, hot, shared }: { meter: Meter; rows: { meter: Meter; depth: number }[]; at: number; depth: number; hot: boolean; shared: Shared }) {
  const { all, figures, estimated, deviceName, isOnlyMeterOfDevice, computerName, watts, currents, dragging, setDragging, onChanged } = shared;
  const [label, setLabel] = useState(meter.label);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState('');
  const [chosen, setChosen] = useState<120 | 240 | null>(null);
  const [over, setOver] = useState(false);

  const refoss = meter.device_id.startsWith('refoss-');
  const second = meter.pair_with ? all.find((c) => c.id === meter.pair_with) : undefined;
  const volts = chosen ?? (meter.pair_with ? 240 : 120);
  const legs = [meter, ...(second ? [second] : [])].map(clampName).join(' + ');
  const parent = meter.parent_id ? all.find((c) => c.id === meter.parent_id) : undefined;

  const run = (id: string, body: Change) =>
    api.updateMeter(id, body).then(
      () => {
        setError('');
        onChanged();
      },
      (e: Error) => setError(e.message),
    );

  const into = indentTarget(rows, at, all);
  const out = outdentTarget(meter, all);

  // The second clamp of a breaker could be any other input of this monitor that is not part of a pair already.
  const taken = new Set(all.flatMap((c) => (c.pair_with ? [c.pair_with] : [])));
  const free = all.filter((c) => c.device_id === meter.device_id && c.id !== meter.id && !c.pair_with && (!taken.has(c.id) || c.id === meter.pair_with));
  const usual = usualPartner(meter, free);
  const options = [...(usual ? [usual] : []), ...free.filter((c) => c !== usual)];
  const holders = all.filter((c) => canHold(c, meter, all));
  const inPanel = meter.scope === 'circuit';
  const placeable = meter.scope !== 'mains';

  const watt = watts.get(meter.id);
  const livePct = breakerPercent(currents.get(meter.id)?.amps, meter.breaker_amps);
  const nearBreaker = livePct !== null && breakerLevel(livePct) !== 'ok' ? livePct : null;
  const used = figures.get(meter.id);
  const share = meter.scope === 'mains' ? 0 : shareOf(watt, shared.houseWatts);
  // Working or resting right now, by the meter's own learned levels (the Live page's); none when too little is known.
  const level = shared.levels.get(meter.id);
  const state = meterState(level, watt);
  // A computer's wall plug and its peripherals (a monitor) are separate plugs, each on the circuit it is plugged into; the row says whose they are.
  const ofComputer = meter.computer_id ? computerName(meter.computer_id) : '';
  const owner = meter.type === 'computer' ? ofComputer : meter.scope === 'plug' && ofComputer ? `peripheral of ${ofComputer}` : '';
  const tag = [legs !== meter.label ? legs : '', TYPE_WORDS[meter.type] ?? '', meter.ev ? 'EV charger' : '', owner, refoss ? deviceName(meter.device_id) : ''].filter(Boolean).join(' · ');
  const dragged = dragging ? all.find((c) => c.id === dragging) : undefined;
  const dropOk = !!dragged && dragged.id !== meter.id && canHold(meter, dragged, all);

  return (
    <li
      class={`hier-row s-${state ?? 'none'}${over && dropOk ? ' drop' : ''}${hot ? ' hot' : ''}${open ? ' open' : ''}`}
      data-entry={`row:${meter.id}`}
      style={depth > 0 ? { marginLeft: `${depth * 1.25}rem` } : undefined}
      onDragOver={(e) => {
        if (!dropOk) return;
        e.preventDefault();
        if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        setOver(false);
        if (!dropOk || !dragged) return;
        e.preventDefault();
        setDragging(null);
        void api.updateMeter(dragged.id, { parent_id: meter.id }).then(onChanged, (err: Error) => setError(err.message));
      }}
    >
      <div class="hier-main" style={`--share: ${share}%`}>
        <button type="button" class="twisty" aria-expanded={open} aria-label={`Settings for ${legs}`} onClick={() => setOpen(!open)}>
          <span aria-hidden="true">▸</span>
        </button>
        <span
          class="drag"
          draggable={true}
          title="Drag onto another meter to put this one inside it"
          onDragStart={(e) => {
            const dt = e.dataTransfer;
            if (dt) {
              dt.setData('text/plain', meter.id);
              dt.effectAllowed = 'move';
              const row = (e.currentTarget as HTMLElement).closest('li');
              if (row) dt.setDragImage(row, 16, 16);
            }
            // Changing the page in the same tick as the drag starts makes the browser cancel the drag, so wait a beat.
            setTimeout(() => setDragging(meter.id), 0);
          }}
          onDragEnd={() => setDragging(null)}
        >
          ⠿
        </span>
        <span class="kind-dot" title={state && level ? stateTitle(state, level) : undefined} />
        {isOnlyMeterOfDevice(meter.device_id) ? (
          <span class="grow meter-name" title="Rename it under Devices: a plug has one name">
            {meter.label}
          </span>
        ) : (
          <label class="grow">
            <span class="visually-hidden">Name for {legs}</span>
            <input
              value={label}
              maxLength={60}
              onInput={(e) => setLabel((e.target as HTMLInputElement).value)}
              onBlur={() => label !== meter.label && void run(meter.id, { label })}
              onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
            />
          </label>
        )}
        <span class="caption hier-tag" title={tag}>
          {nearBreaker !== null && (
            <span class={`badge ${nearBreaker > 100 ? 'over' : 'high'}`} title={`Drawing ${nearBreaker}% of its ${meter.breaker_amps} A breaker right now`}>
              {nearBreaker}% of breaker
            </span>
          )}{' '}
          {meter.ev && (
            <>
              <OdometerNow meterId={meter.id} brief />
              {' · '}
            </>
          )}
          {tag}
        </span>
        <span class="num hier-watts">{watt !== undefined ? <Watts w={watt} /> : ''}</span>
        <span class="num hier-amps">{watt !== undefined ? formatAmps(currents.get(meter.id)?.amps) : ''}</span>
        <span class="num hier-volts">{watt !== undefined ? formatVolts(currents.get(meter.id)?.volts) : ''}</span>
        <span class="num hier-energy">{used ? <Kwh k={used.kwh} /> : ''}</span>
        <span class="num hier-cost">{used?.cost != null ? formatMoney(used.cost, estimated) : used ? '—' : ''}</span>
        <div class="hier-actions">
          {out && (
            <button type="button" class="icon" title={parent ? `Move out of ${parent.label}` : 'Move out'} aria-label={`Move ${legs} out of its panel`} onClick={() => void run(meter.id, { parent_id: out.parent })}>
              ←
            </button>
          )}
          {into && (
            <button type="button" class="icon" title={`Move into ${into.label}`} aria-label={`Move ${legs} into the meter above`} onClick={() => void run(meter.id, { parent_id: into.id })}>
              →
            </button>
          )}
          <button type="button" class="icon" title="Hide" aria-label={`Hide ${legs}`} onClick={() => void run(meter.id, { enabled: false })}>
            <EyeIcon />
          </button>
          <a class="button icon" href={meterPath(meter)} title="Open this meter's page" aria-label={`Open ${legs}`}>
            ›
          </a>
        </div>
      </div>

      {open && (
        <div class="hier-panel">
          {(placeable || refoss) && (
            <div class="pg">
              <span class="pg-title">Placement</span>
              {placeable && (
                <label class="field">
                  <span>{inPanel ? 'Which panel it is in' : 'Which circuit it is plugged into'}</span>
                  <select value={meter.parent_id ?? ''} onChange={(e) => void run(meter.id, { parent_id: (e.target as HTMLSelectElement).value || null })}>
                    <option value="">{inPanel ? 'The main panel' : 'Parent not metered'}</option>
                    {holders.map((c) => (
                      <option key={c.id} value={c.id}>
                        {inPanel ? `The sub-panel fed by ${c.label}` : c.label}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {refoss && (
                <label class="field">
                  <span>Type</span>
                  <select value={meter.type} onChange={(e) => void run(meter.id, { type: (e.target as HTMLSelectElement).value as 'house' | 'sub_panel' | 'circuit' })}>
                    <option value="house">Whole house (a monitor on the main panel)</option>
                    <option value="sub_panel">Sub-panel (feeds other circuits)</option>
                    <option value="circuit">Circuit (one breaker)</option>
                  </select>
                </label>
              )}
            </div>
          )}
          {refoss && (
            <div class="pg">
              <span class="pg-title">Wiring</span>
              <div class="field">
                <span>Breaker voltage</span>
                <div class="segmented" role="radiogroup" aria-label="Breaker voltage">
                  <label class={volts === 120 ? 'on' : ''}>
                    <input
                      type="radio"
                      name={`volts-${meter.id}`}
                      checked={volts === 120}
                      onChange={() => {
                        setChosen(120);
                        if (meter.pair_with) void run(meter.id, { pair_with: null });
                      }}
                    />
                    120 V · one wire
                  </label>
                  <label class={volts === 240 ? 'on' : ''}>
                    <input type="radio" name={`volts-${meter.id}`} checked={volts === 240} onChange={() => setChosen(240)} />
                    240 V · two wires
                  </label>
                </div>
              </div>
              {volts === 240 && (
                <label class="field">
                  <span>The other clamp of this breaker</span>
                  <select value={meter.pair_with ?? ''} onChange={(e) => void run(meter.id, { pair_with: (e.target as HTMLSelectElement).value || null })}>
                    <option value="" disabled={!!meter.pair_with}>
                      Choose the other clamp…
                    </option>
                    {options.map((c) => (
                      <option key={c.id} value={c.id}>
                        {clampName(c)}
                        {c === usual ? ' (the usual match)' : ''}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              <div class="field">
                <span>Clamp direction (a load should read positive)</span>
                <div class="toggles">
                  {[meter, ...(second ? [second] : [])].map((c) => (
                    <label key={c.id} class="check">
                      <input type="checkbox" checked={c.invert === true} onChange={(e) => void run(c.id, { invert: (e.target as HTMLInputElement).checked })} />
                      Flip {clampName(c)}
                    </label>
                  ))}
                </div>
              </div>
            </div>
          )}
          {inPanel && (
            <div class="pg">
              <span class="pg-title">Breaker</span>
              <BreakerField meter={meter} now={currents.get(meter.id) ?? { volts: null, amps: null }} onChange={(amps) => void run(meter.id, { breaker_amps: amps })} />
            </div>
          )}
          {(meter.scope === 'circuit' || meter.scope === 'plug') && (
            <div class="pg">
              <span class="pg-title">Electric vehicle</span>
              <label class="check">
                <input type="checkbox" checked={meter.ev === true} onChange={(e) => void run(meter.id, { ev: (e.target as HTMLInputElement).checked })} />
                This meter charges my electric vehicle
              </label>
              {meter.ev && (
                <p class="block">
                  Odometer: <OdometerNow meterId={meter.id} />
                </p>
              )}
            </div>
          )}
        </div>
      )}
      {error && <p class="note error">{error}</p>}
    </li>
  );
}

/** A part of a computer (its CPU, GPU, memory, and what they leave out): read-only, from its collector, and it opens the computer's page. It is not
 * a meter, so it has no wiring and cannot be moved. */
function PartRow({ id, part, depth, href, hot, share }: { id: string; part: PcPart; depth: number; href: string; hot: boolean; share: number }) {
  return (
    <li class={`hier-row virtual${hot ? ' hot' : ''}`} data-entry={id} style={{ marginLeft: `${depth * 1.25}rem` }}>
      <div class="hier-main" style={`--share: ${share}%`}>
        <span />
        <span />
        <span />
        <span class="grow part-name" title={part.remainder ? 'Wall plug less the parts above: fans, drives, motherboard, power-supply loss' : 'As its collector reports it'}>
          {part.label}
        </span>
        <span class="caption hier-tag">{part.remainder ? 'remainder' : 'collector'}</span>
        <span class="num hier-watts">{part.watts != null ? <Watts w={part.watts} /> : ''}</span>
        <span class="hier-amps" />
        <span class="hier-volts" />
        <span class="num hier-energy"><Kwh k={part.kwh} /></span>
        <span class="num hier-cost">{part.cost != null ? formatMoney(part.cost, true) : '—'}</span>
        <div class="hier-actions">
          <a class="button icon" href={href} title="Open the computer's page" aria-label={`Open the computer's page for ${part.label}`}>
            ›
          </a>
        </div>
      </div>
    </li>
  );
}

/** A line worked out from others, not a meter: "Everything else" (the house less its circuits), "Not individually metered" inside it, and "Other (not metered)"
 * under a meter that holds others (what its own reading carries beyond what is listed inside it). Read-only, and never added to anything. */
function LumpLine({ line, hot, share }: { line: LumpEntry; hot: boolean; share: number }) {
  // "Everything else" holds the rows under it, so its name starts at the row's edge (where a meter's arrow is), not in the name column: what is inside it
  // is then drawn further in than it is.
  const parent = line.id === 'else';
  return (
    <li class={`hier-row virtual lump${parent ? ' lump-head' : ''}${hot ? ' hot' : ''}`} data-entry={line.id} style={{ marginLeft: `${line.depth * 1.25}rem` }}>
      <div class="hier-main" style={`--share: ${share}%`}>
        {!parent && <span />}
        {!parent && <span />}
        {!parent && <span />}
        <span class={`grow part-name${parent ? ' lump-parent' : ''}`} title={line.title}>
          {line.label}
        </span>
        <span class="caption hier-tag">{line.tag}</span>
        <span class="num hier-watts">{line.watts != null ? <Watts w={line.watts} /> : ''}</span>
        <span class="hier-amps" />
        <span class="hier-volts" />
        <span class="num hier-energy">{line.kwh != null ? <Kwh k={line.kwh} /> : '—'}</span>
        <span class="num hier-cost">{line.cost != null ? formatMoney(line.cost) : '—'}</span>
        <div class="hier-actions" />
      </div>
    </li>
  );
}

function Hidden({ meter, deviceName, onChanged }: { meter: Meter; deviceName: string; onChanged: () => void }) {
  const [error, setError] = useState('');
  return (
    <li class="hier-main">
      <span class="grow">
        {meter.label} <span class="caption">{deviceName}</span>
      </span>
      <button type="button" class="show-btn" onClick={() => void api.updateMeter(meter.id, { enabled: true }).then(onChanged, (e: Error) => setError(e.message))}>
        <EyeIcon off /> Show
      </button>
      {error && <span class="note error">{error}</span>}
    </li>
  );
}

/** The Hierarchy tab: the tree, or (the address says which) one meter, one computer or one appliance group open on its own page. */
export function HierarchyView({ meters, open, onChanged }: { meters: Meter[]; open: string[]; onChanged: () => void }) {
  const [kind, id] = open;
  const back = { href: routePath('hierarchy'), label: 'Hierarchy' };
  if (kind === 'meter' && id) return <MeterPage id={id} meters={meters} back={back} onChanged={onChanged} />;
  if (kind === 'computer' && id) return <ComputerView id={id} meters={meters} />;
  if (kind === 'group' && id) return <GroupView id={id} meters={meters} />;
  return <Tree meters={meters} onChanged={onChanged} />;
}

function Tree({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const { data: devices } = usePoll<Device[]>(api.devices, 30_000, [meters.length]);
  const { data: computers } = usePoll<Computer[]>(api.computers, 30_000, [meters.length]);
  const { data: live } = usePoll<Live>(api.live, 2000, []);
  // Each meter's resting and working levels change over weeks: read them every few minutes.
  const { data: levelData } = usePoll(api.levels, 5 * 60_000, []);
  const levels = levelsById(levelData);
  const [dragging, setDragging] = useState<string | null>(null);
  // The line under the pointer, in the tree or in the diagram (an entry id of treePlan): both draw it highlighted.
  const [hover, setHover] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState('');
  // The same period as the Live page (they share the saved choice, made there): this is a setup page, so it has no picker of its own; only the energy and cost columns follow it.
  const [period, setPeriod] = useStored('period', '24h');
  const { data: summary } = usePoll<Summary>(
    () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      return api.summary(from, to);
    },
    60_000,
    [period, meters.length],
  );
  const figures = new Map((summary?.meters ?? []).map((m) => [m.meter_id, m]));
  // A computer's own collector reports its CPU, GPU and memory: shown under its wall plug, not stored as meters.
  const pcDetails = usePcDetails(meters, summary?.from, summary?.to);

  const names = new Map((devices ?? []).map((d) => [d.id, d.name || d.host]));
  const deviceName = (id: string) => names.get(id) ?? '';
  const perDevice = new Map<string, number>();
  for (const m of meters) perDevice.set(m.device_id, (perDevice.get(m.device_id) ?? 0) + 1);
  const isOnlyMeterOfDevice = (id: string) => perDevice.get(id) === 1;
  const computerNames = new Map((computers ?? []).map((c) => [c.id, c.name]));
  const computerName = (id: string) => computerNames.get(id) ?? '';
  const watts = new Map((live?.readings ?? []).map((r) => [r.meter_id, r.watts]));
  const currents = new Map((live?.readings ?? []).map((r) => [r.meter_id, { volts: r.volts, amps: r.amps }]));
  const rows = underHouse(arrangeByPanel(shownMeters(meters)));
  // What each meter that holds others carries beyond them, drawn after the last row inside it, from the flow chart's own figures for the period.
  const rests = summary ? restRows(rows, flowTree(meters, summary), watts, live?.house_watts) : [];
  // The lines in the order they are drawn, read by the tree below and by the diagram: the same list.
  const entries = planTree({
    rows,
    watts,
    houseWatts: live?.house_watts,
    summary,
    rests,
    partsOf: (m) => {
      const detail = pcDetails.get(m.id);
      return detail ? { parts: pcParts(detail, watts.get(m.id) ?? null), href: routePath('hierarchy', 'computer', detail.computer.id) } : undefined;
    },
  });
  const secondLegs = new Set(meters.flatMap((m) => (m.pair_with ? [m.pair_with] : [])));
  const hidden = meters.filter((m) => !m.enabled && !secondLegs.has(m.id));
  const shared: Shared = { all: meters, figures, estimated: summary?.cost_estimated ?? false, deviceName, isOnlyMeterOfDevice, computerName, watts, houseWatts: live?.house_watts, levels, currents, dragging, setDragging, onChanged };

  return (
    <>
      <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
      <div class="tree-layout">
        <div class="tree-stack">
      <section class="card wide">
        <div class="row hier-title">
          <div>
            <h2>Hierarchy</h2>
            <p class="hier-sub">
              {rows.length} {rows.length === 1 ? 'meter' : 'meters'}
            </p>
          </div>
          <div class="inline-actions">
            {summary && (
              <a class="button" href={api.treeExportUrl(summary.from, summary.to)} download title="Every meter over the period picked on Live">
                Download as CSV
              </a>
            )}
            <button type="button" aria-expanded={adding} onClick={() => setAdding(!adding)}>
              ＋ Add
            </button>
          </div>
        </div>
        {adding && (
          <AddPanel
            onAdded={() => {
              setAdding(false);
              onChanged();
            }}
            onCancel={() => setAdding(false)}
          />
        )}
        {error && <p class="note error">{error}</p>}
        {dragging && (
          <div
            class="hier-top-drop"
            onDragOver={(e) => {
              e.preventDefault();
              if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
            }}
            onDrop={(e) => {
              e.preventDefault();
              const id = dragging;
              setDragging(null);
              void api.updateMeter(id, { parent_id: null }).then(onChanged, (err: Error) => setError(err.message));
            }}
          >
            Drop here to take it out of its panel
          </div>
        )}
        {rows.length === 0 ? (
          <p class="note">No meters shown. Use ＋ Add.</p>
        ) : (
          <>
            <div class="hier-groups" aria-hidden="true">
              <span />
              <span class="hier-grp">Right now</span>
              <span class="hier-grp" title={`Energy and cost for ${periodWords(period as PeriodKey)}`}>
                {periodWords(period as PeriodKey).replace(/^the /, '')}
              </span>
            </div>
            <div class="hier-head" aria-hidden="true">
              <span class="hier-tag" />
              <span class="num">Now</span>
              <span class="num">Amps</span>
              <span class="num">Volts</span>
              <span class="num">Energy</span>
              <span class="num">Cost</span>
            </div>
            <ul
              class="hier"
              onMouseOver={(e) => setHover((e.target as HTMLElement).closest('[data-entry]')?.getAttribute('data-entry') ?? null)}
              onMouseLeave={() => setHover(null)}
            >
              {entries.map((e) =>
                e.kind === 'row' ? (
                  <Row key={e.id} meter={e.meter} rows={rows} at={e.at} depth={e.depth} hot={hover === e.id} shared={shared} />
                ) : e.kind === 'part' ? (
                  <PartRow key={e.id} id={e.id} part={e.part} depth={e.depth} href={e.href} hot={hover === e.id} share={shareOf(e.part.watts, live?.house_watts)} />
                ) : (
                  <LumpLine key={e.id} line={e} hot={hover === e.id} share={e.id === 'else' ? 0 : shareOf(e.watts, live?.house_watts)} />
                ),
              )}
            </ul>
            {levels.size > 0 && (
              <p class="hier-key caption">
                <span class="key-dot working" aria-hidden="true" /> Working: drawing above its usual resting level
                <span class="key-dot resting" aria-hidden="true" /> Resting: at or near what it always draws (often 0 W)
              </p>
            )}
          </>
        )}
        {hidden.length > 0 && (
          <details>
            <summary>
              {hidden.length} hidden {hidden.length === 1 ? 'meter' : 'meters'}
            </summary>
            <ul class="hier">
              {hidden.map((m) => (
                <Hidden key={m.id} meter={m} deviceName={deviceName(m.device_id)} onChanged={onChanged} />
              ))}
            </ul>
          </details>
        )}
      </section>
      <div class="tree-extra">
        <section class="card wide diagram-card">
          <h2>Diagram</h2>
          <HierarchyDiagram entries={entries} watts={watts} figures={figures} stateOf={(id) => meterState(levels.get(id), watts.get(id))} hover={hover} onHover={setHover} />
        </section>
        <DevicesCard meters={meters} onChanged={onChanged} />
        <ComputersCard meters={meters} onChanged={onChanged} watts={watts} from={summary?.from} to={summary?.to} />
        <GroupsCard meters={meters} onChanged={onChanged} watts={watts} deviceName={deviceName} from={summary?.from} to={summary?.to} />
      </div>
        </div>
      </div>
    </>
  );
}
