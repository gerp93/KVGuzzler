// The charges of an electric vehicle as a list, newest first: each is when it began, what it used and cost, how far the car's battery rose and, when the car's
// reports allow it, the energy per percent and the efficiency. A list rather than a table because it lives in a narrow column beside the charts. The sums
// are the server's; the tracker that finds the charges is described in the server's notes.
import { useState } from 'preact/hooks';
import { batteryText, lowCoverage, pctNote, percentText, perPct, whenText, type EvChargesAnswer } from './ev';
import { lossTitle } from './evLoss';
import { formatDuration, formatMoney } from './lib';
import { routePath } from './route';
import { Kwh } from './Watts';

/** How many charges are listed before "Show all". */
const SHOWN = 5;

export function EvChargeList({ data, onRemove, problem, onOpenSettings }: { data: EvChargesAnswer; onRemove: (id: number) => void; problem: string; onOpenSettings: () => void }) {
  const [showAll, setShowAll] = useState(false);
  // The charge whose removal is being confirmed.
  const [removing, setRemoving] = useState<number | null>(null);
  const { charges, consistency } = data;

  return (
    <section class="ev-section">
      <div class="ev-section-head">
        <h3 class="ev-section-title">Charges</h3>
        {charges.length > 0 && <span class="ev-section-sub">{charges.length === 1 ? '1 charge' : `${charges.length} charges`}</span>}
      </div>

      {consistency && (
        <p class="charge-summary">
          <strong>{perPct(consistency.kwh_per_pct)} kWh per %</strong> typical
          {consistency.charges > 1 ? ` (${perPct(consistency.lowest)} to ${perPct(consistency.highest)})` : ''}. A full battery takes about{' '}
          <strong>{(consistency.kwh_per_pct * 100).toFixed(0)} kWh</strong> at the wall.
        </p>
      )}
      {consistency && data.battery_kwh == null && (
        <p class="caption">
          Set the battery's usable kWh to see how much of that reaches it.{' '}
          <button type="button" class="small" onClick={onOpenSettings}>
            Set the battery size
          </button>
        </p>
      )}

      {charges.length === 0 ? (
        <p class="caption">A charge is listed here as soon as the meter starts drawing power.</p>
      ) : (
        <ul class="charge-list">
          {(showAll ? charges : charges.slice(0, SHOWN)).map((c) => {
            const note = pctNote(c.pct_status);
            return (
              <li class="charge" key={c.id}>
                <div class="charge-line">
                  <span class="charge-when">
                    {whenText(c.started_at)}
                    {c.active && <span class="badge live">charging</span>}
                    {lowCoverage(c.coverage) && (
                      <span class="badge" title={`Measured ${Math.round(c.coverage * 100)}% of this charge: the energy and cost are understated`}>
                        {Math.round(c.coverage * 100)}% measured
                      </span>
                    )}
                  </span>
                  <span class="charge-used">
                    <Kwh k={c.kwh} />
                    <span class="charge-cost">{c.cost != null ? formatMoney(c.cost, c.cost_estimated) : '—'}</span>
                  </span>
                </div>
                <div class="charge-line charge-detail">
                  <span>
                    {batteryText(c)} · {formatDuration(c.duration_seconds)}
                  </span>
                  <span class="charge-result" title={lossTitle(c)}>
                    {note ? (
                      <span class="charge-note" title={note.long}>
                        {note.short}
                      </span>
                    ) : (
                      <>
                        <strong>{perPct(c.kwh_per_pct ?? 0)}</strong> kWh/%{c.efficiency != null ? ` · ${percentText(c.efficiency)} efficient` : ''}
                      </>
                    )}
                    {!c.active && (
                      <button type="button" class="icon charge-x" title="Remove this charge" aria-label={`Remove the charge of ${whenText(c.started_at)}`} onClick={() => setRemoving(c.id)}>
                        ✕
                      </button>
                    )}
                  </span>
                </div>
                {removing === c.id && (
                  <div class="charge-confirm">
                    <span>Remove this charge?</span>
                    <button type="button" class="small" onClick={() => onRemove(c.id)}>
                      Yes, remove
                    </button>
                    <button type="button" class="small" onClick={() => setRemoving(null)}>
                      Cancel
                    </button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {charges.length > SHOWN && (
        <button type="button" class="ev-more" onClick={() => setShowAll(!showAll)}>
          {showAll ? 'Show fewer' : `Show all ${charges.length}`}
        </button>
      )}
      {problem && <p class="note error">{problem}</p>}
      {!data.car_linked && <p class="caption">The battery figures need the car linked to Ford, in the <a href={routePath('connectors')}>Connectors tab</a>.</p>}
    </section>
  );
}
