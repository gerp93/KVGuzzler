// The EV card's charts as one panel with tabs, so there is one chart to read at a time, big enough to read, instead of a row of small ones. A tab with too few
// points says what it is waiting for in the chart's place, so the panel does not change height under the reader. The two tabs that draw a line for each gas car
// have the gas cars as checkboxes under them, each with the look of its line.
import { useMemo, useState } from 'preact/hooks';
import { Chart, type Line } from './Chart';
import { EvBars } from './EvBars';
import { gasLineStyle, type GasComparison } from './evGas';
import { firstReady, tabReady, type TabId, type TrendTab } from './evTabs';

const HEIGHT = 280;

export function EvTrendPanel({ tabs, comparisons, hidden, onTogglePick }: { tabs: TrendTab[]; comparisons: GasComparison[]; hidden: number[]; onTogglePick: (mpg: number) => void }) {
  const [chosen, setChosen] = useState<TabId | null>(null);
  const id = chosen && tabs.some((t) => t.id === chosen) ? chosen : firstReady(tabs);
  const tab = tabs.find((t) => t.id === id);

  // The chart is rebuilt when its inputs are different objects, and the page polls: so what it is given is made again only when the points really changed.
  const key = tab ? `${tab.id}|${tab.endLabels}|${tab.x.join(',')}|${tab.lines.map((l) => `${l.name}:${l.y.join(',')}`).join('|')}` : '';
  const lines = useMemo<Line[]>(() => {
    if (!tab) return [];
    return tab.lines.map((l) => ({ name: l.name, colorVar: l.colorVar, x: tab.x, y: l.y, alpha: l.alpha, width: l.width, fill: l.fill }));
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
      {tab.picks && comparisons.length > 0 && (
        <div class="ev-picks" role="group" aria-label="Gas cars to compare with">
          <span class="ev-picks-label">Compare with</span>
          {comparisons.map((c) => {
            const style = gasLineStyle(c, comparisons);
            return (
              <label class="ev-pick" key={c.mpg}>
                <input
                  type="checkbox"
                  checked={!hidden.includes(c.mpg)}
                  onChange={() => {
                    // Ticking is done on this chart: it stays open even when the last car is unticked and it has nothing to draw.
                    setChosen(tab.id);
                    onTogglePick(c.mpg);
                  }}
                />
                <span class={`ev-swatch${c.custom ? ' custom' : ''}`} style={{ opacity: style.alpha }} aria-hidden="true" />
                {c.mpg} mpg{c.custom ? ' (yours)' : ''}
              </label>
            );
          })}
        </div>
      )}
    </section>
  );
}
