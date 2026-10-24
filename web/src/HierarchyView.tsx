// The Hierarchy tab: every meter the server reads, as one tree. A sub-panel's feed holds the circuits inside it; a circuit holds the plugs on
// it; there can be several top-level entries, and none of it needs a main monitor. Each row opens (the arrow at its left) to the wiring of that
// meter, and moves in the tree by the arrows, by dragging onto another meter, or from its "which panel" list. Nothing here is sent to a device:
// it only changes how this dashboard reads and shows them.
import { useState } from 'preact/hooks';
import { api } from './api';
import { meterPath } from './Bars';
import { FlowChart } from './FlowChart';
import { ComputerView } from './ComputerView';
import { GroupView } from './GroupView';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { MeterPage } from './MeterPage';
import { routePath } from './route';
import {
  arrangeByPanel, canHold, clampName, formatKwh, formatMoney, formatWatts, indentTarget, outdentTarget, periodRange, shownMeters, usualPartner,
  type ApplianceGroup, type Device, type Live, type Meter, type MeterSummary, type PeriodKey, type Summary,
} from './lib';

type Change = Parameters<typeof api.updateMeter>[1];

const TYPE_WORDS: Record<string, string> = { house: 'whole house', sub_panel: 'sub-panel' };

function EyeIcon({ off }: { off?: boolean }) {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M1 12s4-7 11-7 11 7 11 7-4 7-11 7S1 12 1 12z" />
      <circle cx="12" cy="12" r="3" />
      {off && <path d="M3 3l18 18" />}
    </svg>
  );
}

interface Shared {
  all: Meter[];
  /** What each meter used and cost over the period picked at the top, by meter id. */
  figures: Map<string, MeterSummary>;
  estimated: boolean;
  deviceName: (id: string) => string;
  watts: Map<string, number>;
  dragging: string | null;
  setDragging: (id: string | null) => void;
  onChanged: () => void;
}

function Row({ meter, rows, at, shared }: { meter: Meter; rows: { meter: Meter; depth: number }[]; at: number; shared: Shared }) {
  const { all, figures, estimated, deviceName, watts, dragging, setDragging, onChanged } = shared;
  const depth = rows[at]!.depth;
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
  const used = figures.get(meter.id);
  const tag = [legs !== meter.label ? legs : '', TYPE_WORDS[meter.type] ?? '', refoss ? deviceName(meter.device_id) : ''].filter(Boolean).join(' · ');
  const dragged = dragging ? all.find((c) => c.id === dragging) : undefined;
  const dropOk = !!dragged && dragged.id !== meter.id && canHold(meter, dragged, all);

  return (
    <li
      class={`hier-row${over && dropOk ? ' drop' : ''}`}
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
      <div class="hier-main">
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
        <span class="caption hier-tag" title={tag}>
          {tag}
        </span>
        <span class="num hier-watts">{watt !== undefined ? formatWatts(watt) : ''}</span>
        <span class="num hier-energy">{used ? formatKwh(used.kwh) : ''}</span>
        <span class="num hier-cost">{used?.cost != null ? formatMoney(used.cost, estimated) : used ? '—' : ''}</span>
        <div class="hier-actions">
          <button
            type="button"
            class="icon star"
            aria-pressed={meter.favorite === true}
            title={meter.favorite ? 'Remove from the favorites on Live' : 'Add to the favorites on Live'}
            aria-label={`${meter.favorite ? 'Unfavorite' : 'Favorite'} ${legs}`}
            onClick={() => void run(meter.id, { favorite: !meter.favorite })}
          >
            {meter.favorite ? '★' : '☆'}
          </button>
          <a class="button icon" href={meterPath(meter)} title="Open this meter's page" aria-label={`Open ${legs}`}>
            ›
          </a>
          <button type="button" class="icon" disabled={!out} title={parent ? `Move out of ${parent.label}` : 'Already at the top'} aria-label={`Move ${legs} out of its panel`} onClick={() => out && void run(meter.id, { parent_id: out.parent })}>
            ←
          </button>
          <button type="button" class="icon" disabled={!into} title={into ? `Move into ${into.label}` : 'Nothing above it can hold it'} aria-label={`Move ${legs} into the meter above`} onClick={() => into && void run(meter.id, { parent_id: into.id })}>
            →
          </button>
          <button type="button" class="icon" title="Hide" aria-label={`Hide ${legs}`} onClick={() => void run(meter.id, { enabled: false })}>
            <EyeIcon />
          </button>
        </div>
      </div>

      {open && (
        <div class="hier-panel">
          {refoss && (
            <>
              <fieldset>
                <legend>Clamp direction</legend>
                {[meter, ...(second ? [second] : [])].map((c) => (
                  <label key={c.id} class="check">
                    <input type="checkbox" checked={c.invert === true} onChange={(e) => void run(c.id, { invert: (e.target as HTMLInputElement).checked })} />
                    Flip {clampName(c)}
                  </label>
                ))}
                <p class="caption block">A load reads positive. Flip a clamp that reads backwards here, or turn the clamp round in the panel, not both.</p>
              </fieldset>
              <fieldset>
                <legend>Breaker</legend>
                <label class="check">
                  <input
                    type="radio"
                    name={`volts-${meter.id}`}
                    checked={volts === 120}
                    onChange={() => {
                      setChosen(120);
                      if (meter.pair_with) void run(meter.id, { pair_with: null });
                    }}
                  />
                  120 V, one wire
                </label>
                <label class="check">
                  <input type="radio" name={`volts-${meter.id}`} checked={volts === 240} onChange={() => setChosen(240)} />
                  240 V, two wires
                </label>
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
              </fieldset>
              <label class="field">
                <span>Type</span>
                <select value={meter.type} onChange={(e) => void run(meter.id, { type: (e.target as HTMLSelectElement).value as 'house' | 'sub_panel' | 'circuit' })}>
                  <option value="house">Whole house (a monitor on the main panel)</option>
                  <option value="sub_panel">Sub-panel (feeds other circuits)</option>
                  <option value="circuit">Circuit (one breaker)</option>
                </select>
              </label>
            </>
          )}
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
        </div>
      )}
      {error && <p class="note error">{error}</p>}
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
  if (kind === 'meter' && id) return <MeterPage id={id} meters={meters} back={back} />;
  if (kind === 'computer' && id) return <ComputerView id={id} meters={meters} />;
  if (kind === 'group' && id) return <GroupView id={id} meters={meters} />;
  return <Tree meters={meters} onChanged={onChanged} />;
}

function Tree({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const { data: devices } = usePoll<Device[]>(api.devices, 30_000, [meters.length]);
  const { data: live } = usePoll<Live>(api.live, 2000, []);
  const { data: groups } = usePoll<ApplianceGroup[]>(api.groups, 30_000, [meters]);
  const [dragging, setDragging] = useState<string | null>(null);
  const [error, setError] = useState('');
  // The same period as the Live page (they share the saved choice); only the energy and cost columns follow it.
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

  const names = new Map((devices ?? []).map((d) => [d.id, d.name || d.host]));
  const deviceName = (id: string) => names.get(id) ?? '';
  const watts = new Map((live?.readings ?? []).map((r) => [r.meter_id, r.watts]));
  const rows = arrangeByPanel(shownMeters(meters));
  const secondLegs = new Set(meters.flatMap((m) => (m.pair_with ? [m.pair_with] : [])));
  const hidden = meters.filter((m) => !m.enabled && !secondLegs.has(m.id));
  const shared: Shared = { all: meters, figures, estimated: summary?.cost_estimated ?? false, deviceName, watts, dragging, setDragging, onChanged };

  return (
    <>
      <section class="card wide">
        <div class="row">
          <h2>Where the energy goes</h2>
          <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
        </div>
        {summary ? <FlowChart meters={meters} summary={summary} /> : <p class="note">Loading…</p>}
        <p class="caption">
          Each band is as wide as the energy that went through it over the period. A meter's band already counts what is inside it, so the parts of a band
          never add to more than the band itself: what is left over is "Everything else". The Energy and Cost columns below follow the same period.
        </p>
      </section>
      <section class="card wide">
        <h2>Hierarchy</h2>
        <p class="caption">
          Every meter, laid out as the house is wired. A sub-panel holds the circuits inside it, and a circuit holds the plugs on it. A parent's own reading already
          counts what is inside it, so nothing is added twice. Open a row (the arrow) for its wiring; move it with the arrows, or drag it onto the meter it is inside.
          Nothing here is sent to a device.
        </p>
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
          <p class="note">No meters are shown. Add a device in Settings, or show one below.</p>
        ) : (
          <>
            <div class="hier-head" aria-hidden="true">
              <span class="hier-tag" />
              <span class="num">Now</span>
              <span class="num">Energy</span>
              <span class="num">Cost</span>
            </div>
            <ul class="hier">
              {rows.map(({ meter }, at) => (
                <Row key={meter.id} meter={meter} rows={rows} at={at} shared={shared} />
              ))}
            </ul>
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
      {groups && groups.length > 0 && (
        <section class="card wide">
          <h2>Appliance groups</h2>
          <ul class="bars">
            {groups.map((g) => (
              <li key={g.id}>
                <span class="name">
                  <a href={routePath('hierarchy', 'group', g.id)}>{g.name}</a>
                </span>
                <span class="caption">{g.meter_ids.length} plugs shown as one</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
