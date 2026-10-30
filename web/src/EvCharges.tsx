// The charges of an electric vehicle, in the EV card: each time the charging meter drew power, with what it used, what it cost and how far the car's
// battery rose, and from those the energy it takes per percent and how steady that is. A charge is found by the server from the meter alone; the
// battery's charge comes from the car (Ford) at each end. The sums are the server's.
import { useState } from 'preact/hooks';
import { api } from './api';
import { batteryText, lowCoverage, pctNote, percentText, perPct, whenText, type EvChargesAnswer } from './ev';
import { lossTitle } from './evLoss';
import { usePoll } from './hooks';
import { formatDuration, formatMoney } from './lib';
import { Kwh } from './Watts';

/** How many charges are listed before "Show all". */
const SHOWN = 6;

export function EvCharges({ meterId, refresh, onOpenSettings }: { meterId: string; refresh: number; onOpenSettings: () => void }) {
  const [version, setVersion] = useState(0);
  const { data } = usePoll<EvChargesAnswer>(() => api.evCharges(meterId), 30_000, [meterId, version, refresh]);
  const [showAll, setShowAll] = useState(false);
  // The charge whose removal is being confirmed.
  const [removing, setRemoving] = useState<number | null>(null);
  const [problem, setProblem] = useState('');
  if (!data) return null;

  const remove = (id: number) => {
    setProblem('');
    void api.deleteEvCharge(meterId, id).then(
      () => {
        setRemoving(null);
        setVersion((v) => v + 1);
      },
      (e: Error) => setProblem(e.message),
    );
  };

  const { charges, consistency } = data;
  return (
    <div class="ev-charges">
      <h3 class="ev-sub">Charges</h3>
      {charges.length === 0 ? (
        <p class="caption">A charge is listed here as soon as the meter starts drawing power.</p>
      ) : (
        <>
          {consistency && (
            <div class="ev-figures">
              <div class="stat">
                <span class="caption">Energy per percent</span>
                <p class="big small ev-figure">{perPct(consistency.kwh_per_pct)}</p>
                <span class="caption">
                  kWh per % · {consistency.charges} {consistency.charges === 1 ? 'charge' : 'charges'}
                </span>
                {consistency.charges > 1 && (
                  <span class="caption">
                    from {perPct(consistency.lowest)} to {perPct(consistency.highest)}
                  </span>
                )}
              </div>
              <div class="stat">
                <span class="caption">Full battery</span>
                <p class="big small ev-figure">{(consistency.kwh_per_pct * 100).toFixed(1)}</p>
                <span class="caption">kWh at the wall, 0 to 100 %</span>
              </div>
              {data.losses && (
                <>
                  <div class="stat">
                    <span class="caption">Charging efficiency</span>
                    <p class="big small ev-figure">{percentText(data.losses.efficiency)}</p>
                    <span class="caption">of the wall's energy reached the battery</span>
                  </div>
                  <div class="stat">
                    <span class="caption">Lost charging</span>
                    <p class="big small ev-figure">{data.losses.loss_kwh.toFixed(1)}</p>
                    <span class="caption">kWh{data.losses.loss_cost != null ? ` · ${formatMoney(data.losses.loss_cost)}` : ''}</span>
                  </div>
                </>
              )}
            </div>
          )}
          {consistency && data.battery_kwh == null && (
            <p class="caption">
              Set the battery's usable kWh to see how much of the wall's energy reaches it.{' '}
              <button type="button" class="small" onClick={onOpenSettings}>
                Open settings
              </button>
            </p>
          )}
          {data.losses && data.losses.efficiency > 1 && (
            <p class="caption bad">More energy reached the battery than the wall gave: the battery's usable kWh is set too high.</p>
          )}
          <div class="ev-trips-wrap">
            <table class="ev-trips">
              <caption class="visually-hidden">Charges, newest first</caption>
              <thead>
                <tr>
                  <th scope="col">Started</th>
                  <th scope="col">Duration</th>
                  <th scope="col">Battery</th>
                  <th scope="col">Energy</th>
                  <th scope="col">Cost</th>
                  <th scope="col">kWh per %</th>
                  {data.battery_kwh != null && <th scope="col">Efficiency</th>}
                  <th scope="col">
                    <span class="visually-hidden">Remove</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {(showAll ? charges : charges.slice(0, SHOWN)).map((c) => {
                  const note = pctNote(c.pct_status);
                  return (
                    <tr key={c.id}>
                      <td>
                        {whenText(c.started_at)}
                        {c.active && <span class="badge live">charging</span>}
                        {lowCoverage(c.coverage) && (
                          <span class="badge" title={`Measured ${Math.round(c.coverage * 100)}% of this charge: the energy and cost are understated`}>
                            {Math.round(c.coverage * 100)}% measured
                          </span>
                        )}
                      </td>
                      <td>{formatDuration(c.duration_seconds)}</td>
                      <td>{batteryText(c)}</td>
                      <td>
                        <Kwh k={c.kwh} />
                      </td>
                      <td>{c.cost != null ? formatMoney(c.cost, c.cost_estimated) : '—'}</td>
                      <td>
                        {note ? (
                          <span class="charge-note" title={note.long}>
                            {note.short}
                          </span>
                        ) : (
                          <strong>{perPct(c.kwh_per_pct ?? 0)}</strong>
                        )}
                      </td>
                      {data.battery_kwh != null && (
                        <td title={lossTitle(c)}>{c.efficiency != null ? percentText(c.efficiency) : '—'}</td>
                      )}
                      <td>
                        {c.active ? null : removing === c.id ? (
                          <span class="chips">
                            <button type="button" class="small" onClick={() => remove(c.id)}>
                              Yes, remove
                            </button>
                            <button type="button" class="small" onClick={() => setRemoving(null)}>
                              Cancel
                            </button>
                          </span>
                        ) : (
                          <button type="button" class="small" aria-label={`Remove the charge of ${whenText(c.started_at)}`} onClick={() => setRemoving(c.id)}>
                            Remove
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {charges.length > SHOWN && (
            <button type="button" class="ev-more" onClick={() => setShowAll(!showAll)}>
              {showAll ? 'Show fewer' : `Show all ${charges.length}`}
            </button>
          )}
        </>
      )}
      {problem && <p class="note error">{problem}</p>}
      {!data.car_linked && <p class="caption">The battery figures need the car linked to Ford, from Settings.</p>}
    </div>
  );
}
