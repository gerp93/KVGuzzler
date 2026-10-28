// A list of meters as bars in a table: name, live watts, and (over a period) energy, cost and share, each in a column of its own under a heading. A meter that holds others is followed by the ones
// inside it and by an "Other (not metered)" line (its figure less theirs). Each real meter opens its own page.
import { Kwh, Watts } from './Watts';
import { levelsById, meterState, stateTitle, type Levels } from './levels';
import { formatMoney, formatShare, moneyText, periodWords, type MeterSummary, type PeriodKey, type Summary } from './lib';
import type { BarRow } from './notMetered';
import { routePath } from './route';

export function Bars({ rows, of, summary, baseKwh, period, levels }: { rows: BarRow[]; of: number; summary?: Summary; baseKwh: number | null; period?: PeriodKey; levels?: Levels }) {
  const byId = levelsById(levels);
  // The key under the table is only worth its line when at least one cell is tinted.
  const tinted = rows.some((r) => !r.lump && !r.remainder && meterState(byId.get(r.meter.id), r.watts) !== undefined);
  const find = (id: string): MeterSummary | undefined => summary?.meters.find((c) => c.meter_id === id);
  return (
    <>
      <ul class="bars cols">
        {/* Two different things sit side by side: what it draws right now, and what it used over the period picked. */}
        <li class="head group" aria-hidden="true">
          <span />
          <span class="num">Right now</span>
          <span class="num">Over {period ? periodWords(period) : 'the period'}</span>
        </li>
        <li class="head" aria-hidden="true">
          <span />
          <span />
          <span class="num">Power</span>
          <span class="num">Energy</span>
          <span class="num">Cost</span>
          <span class="num">Share</span>
        </li>
        {rows.map((r) => {
          // A parent already holds what is inside it: what it carries beyond them is its figure less theirs.
          let mine: { kwh: number; cost?: string | null } | undefined = r.lump ? (r.lump.kwh != null ? { kwh: r.lump.kwh, cost: r.lump.cost } : undefined) : find(r.meter.id);
          if (mine && r.remainder) {
            const inside = r.remainder.of.map(find);
            const costs = [mine.cost, ...inside.map((c) => c?.cost)];
            mine = {
              ...mine,
              kwh: Math.max(0, mine.kwh - inside.reduce((t, c) => t + (c?.kwh ?? 0), 0)),
              cost: costs.every((c) => c != null) ? moneyText(Math.max(0, Number(mine.cost) - inside.reduce((t, c) => t + Number(c?.cost), 0))) : null,
            };
          }
          const share = mine ? formatShare(mine.kwh, baseKwh) : '';
          const title = r.lump ? r.lump.label : r.remainder ? `Other (not metered) on ${r.meter.label}` : r.meter.label;
          const derived = !!(r.lump || r.remainder);
          // Working or resting right now, by the meter's own learned levels; a derived row (a lump, "Other (not metered)") is no meter and has none.
          const level = derived ? undefined : byId.get(r.meter.id);
          const state = meterState(level, r.watts);
          return (
            <li key={r.lump ? `lump-${r.lump.label}` : r.remainder ? `rest-${r.meter.id}` : r.meter.id} class={derived ? 'rest' : undefined} style={r.depth ? { paddingLeft: `${r.depth * 1.25}rem` } : undefined}>
              <span class="name" title={title}>
                {r.lump ? r.lump.label : r.remainder ? 'Other (not metered)' : <a href={meterPath(r.meter)}>{r.meter.label}</a>}
              </span>
              <span class="bar" aria-hidden="true">
                <i style={{ width: `${of > 0 ? Math.min(100, (r.watts / of) * 100) : 0}%` }} />
              </span>
              <span class={state ? `num now ${state}` : 'num now'} title={state && level ? stateTitle(state, level) : undefined}>
                <Watts w={r.watts} />
              </span>
              <span class="figs">
                <span class="num energy">{mine && <Kwh k={mine.kwh} />}</span>
                <span class="num cost">{mine && mine.cost != null ? formatMoney(mine.cost, summary?.cost_estimated) : ''}</span>
                <span class="num share">{share}</span>
              </span>
            </li>
          );
        })}
      </ul>
      {tinted && (
        <p class="state-key caption">
          <span class="swatch working" aria-hidden="true" /> Working: drawing above its usual resting level
          <span class="swatch resting" aria-hidden="true" /> Resting: at or near what it always draws (often 0 W)
          <span class="hintnote">Learned from each meter's last 30 days; hover a figure for its usual level.</span>
        </p>
      )}
    </>
  );
}

/** Where a meter's own page is: a computer's wall plug opens the computer, anything else the meter. */
export function meterPath(m: { id: string; scope: string; computer_id?: string | null }): string {
  return m.scope === 'pc' && m.computer_id ? routePath('hierarchy', 'computer', m.computer_id) : routePath('hierarchy', 'meter', m.id);
}
