// The EV card's charts as one panel with tabs, so there is one chart to read at a time, big enough to read, instead of a row of small ones. A tab with too few
// points says what it is waiting for in the chart's place, so the panel does not change height under the reader. The two tabs that draw a line for each gas car
// have the gas cars as checkboxes under them, each with the look of its line.
import { useMemo, useState } from 'preact/hooks';
import { Chart, type Line } from './Chart';
import { EvBars } from './EvBars';
import { compKey, DIESEL_PRICE, gasLineStyle, isShown, type GasComparison, type Picks } from './evGas';
import { firstReady, tabReady, type TabId, type TrendTab } from './evTabs';

const HEIGHT = 280;

export function EvTrendPanel({ tabs, comparisons, picks, onTogglePick }: { tabs: TrendTab[]; comparisons: GasComparison[]; picks: Picks; onTogglePick: (key: string) => void }) {
  const [chosen, setChosen] = useState<TabId | null>(null);
  const id = chosen && tabs.some((t) => t.id === chosen) ? chosen : firstReady(tabs);
  const tab = tabs.find((t) => t.id === id);

  // The chart is rebuilt when its inputs are different objects, and the page polls: so what it is given is made again only when the points really changed.
  const key = tab ? `${tab.id}|${tab.endLabels}|${tab.x.join(',')}|${tab.lines.map((l) => `${l.name}:${l.y.join(',')}`).join('|')}` : '';
  const lines = useMemo<Line[]>(() => {
    if (!tab) return [];
    return tab.lines.map((l) => ({ name: l.name, colorVar: l.colorVar, x: tab.x, y: l.y, alpha: l.alpha, width: l.width, fill: l.fill, dashed: l.dashed }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const domain = useMemo(() => tab?.domain ?? [0, 1], [key]) as [number, number];
  if (!tab) return null;

  return (
    <section class="ev-section">
      <div class="ev-section-head">
        <h3 class="ev-section-title">Trends</h3>
        <div class="chips" role="group" aria-label="Chart shown">
          {tabs.map((t) => (
            <button type="button" key={t.id} aria-pressed={t.id === tab.id} onClick={() => setChosen(t.id)}>
              {t.label}
            </button>
          ))}
        </div>
      </div>
      <p class="caption ev-trend-title">{tab.title}</p>
      {tabReady(tab) ? (
        tab.bars ? (
          <EvBars bars={tab.bars} height={HEIGHT} label={tab.title} />
        ) : (
          <Chart lines={lines} domain={domain} height={HEIGHT} format={tab.format} label={tab.title} endLabels={tab.endLabels} yLabel={tab.yLabel} xLabel={tab.xLabel} />
        )
      ) : (
        <div class="ev-waiting" style={{ minHeight: `${HEIGHT}px` }}>
          <p>{tab.waiting}</p>
        </div>
      )}
      {tab.picks === 'cars' && comparisons.length > 0 && (
        <div class="ev-pickrows">
          {(['gas', 'diesel'] as const).map((fuel) => {
            const cars = comparisons.filter((c) => c.fuel === fuel);
            if (cars.length === 0) return null;
            const both = comparisons.some((c) => c.fuel === 'diesel');
            return (
              <div class="ev-picks" role="group" aria-label={`${fuel === 'gas' ? 'Gas' : 'Diesel'} cars to compare with`} key={fuel}>
                <span class="ev-picks-label">{both ? (fuel === 'gas' ? 'Gas' : 'Diesel') : 'Compare with'}</span>
                {cars.map((c) => {
                  const style = gasLineStyle(c, comparisons);
                  const key = compKey(c);
                  return (
                    <label class="ev-pick" key={key}>
                      <input
                        type="checkbox"
                        checked={isShown(c, picks)}
                        onChange={() => {
                          // Ticking is done on this chart: it stays open even when the last car is unticked and it has nothing to draw.
                          setChosen(tab.id);
                          onTogglePick(key);
                        }}
                      />
                      <span class={`ev-swatch${c.custom ? ' custom' : ''}${c.fuel === 'diesel' ? ' diesel' : ''}`} style={{ opacity: style.alpha }} aria-hidden="true" />
                      {c.mpg} mpg{c.custom ? ' (yours)' : ''}
                    </label>
                  );
                })}
              </div>
            );
          })}
        </div>
      )}
      {tab.picks === 'diesel-price' && (
        <div class="ev-picks">
          <label class="ev-pick">
            <input
              type="checkbox"
              checked={picks.on.includes(DIESEL_PRICE)}
              onChange={() => {
                setChosen(tab.id);
                onTogglePick(DIESEL_PRICE);
              }}
            />
            <span class="ev-swatch custom diesel" aria-hidden="true" />
            Diesel
          </label>
        </div>
      )}
    </section>
  );
}
