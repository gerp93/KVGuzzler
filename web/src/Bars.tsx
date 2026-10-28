// A list of meters as bars in a table: name, live watts, and (over a period) energy, cost and share, each in a column of its own under a heading. A meter that holds others is followed by the ones
// inside it and by an "Other (not metered)" line (its figure less theirs). Each real meter opens its own page.
import { Kwh, Watts } from './Watts';
import { formatMoney, houseShare, periodWords, type MeterSummary, type PeriodKey, type Row, type Summary } from './lib';
import { routePath } from './route';

export function Bars({ rows, of, summary, baseKwh, period }: { rows: Row[]; of: number; summary?: Summary; baseKwh: number | null; period?: PeriodKey }) {
  const find = (id: string): MeterSummary | undefined => summary?.meters.find((c) => c.meter_id === id);
  return (
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
        let mine = find(r.meter.id);
        if (mine && r.remainder) {
          const inside = r.remainder.of.map(find);
          const costs = [mine.cost, ...inside.map((c) => c?.cost)];
          mine = {
            ...mine,
            kwh: Math.max(0, mine.kwh - inside.reduce((t, c) => t + (c?.kwh ?? 0), 0)),
            cost: costs.every((c) => c != null) ? Math.max(0, Number(mine.cost) - inside.reduce((t, c) => t + Number(c?.cost), 0)).toFixed(2) : null,
          };
        }
        const share = mine ? houseShare(mine.kwh, baseKwh) : null;
        const title = r.remainder ? `Other (not metered) on ${r.meter.label}` : r.meter.label;
        return (
          <li key={r.remainder ? `rest-${r.meter.id}` : r.meter.id} class={r.remainder ? 'rest' : undefined} style={r.depth ? { paddingLeft: `${r.depth * 1.25}rem` } : undefined}>
            <span class="name" title={title}>
              {r.remainder ? 'Other (not metered)' : <a href={meterPath(r.meter)}>{r.meter.label}</a>}
            </span>
            <span class="bar" aria-hidden="true">
              <i style={{ width: `${of > 0 ? Math.min(100, (r.watts / of) * 100) : 0}%` }} />
            </span>
            <span class="num"><Watts w={r.watts} /></span>
            <span class="figs">
              <span class="num energy">{mine && <Kwh k={mine.kwh} />}</span>
              <span class="num cost">{mine && mine.cost != null ? formatMoney(mine.cost, summary?.cost_estimated) : ''}</span>
              <span class="num share">{share != null ? `${share}%` : ''}</span>
            </span>
          </li>
        );
      })}
    </ul>
  );
}

/** Where a meter's own page is: a computer's wall plug opens the computer, anything else the meter. */
export function meterPath(m: { id: string; scope: string; computer_id?: string | null }): string {
  return m.scope === 'pc' && m.computer_id ? routePath('hierarchy', 'computer', m.computer_id) : routePath('hierarchy', 'meter', m.id);
}
