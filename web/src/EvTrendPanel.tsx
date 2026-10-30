// The EV card's charts as one panel with tabs, so there is one chart to read at a time, big enough to read, instead of a row of small ones. A tab with too few
// points says what it is waiting for in the chart's place, so the panel does not change height under the reader.
import { useMemo, useState } from 'preact/hooks';
import { Chart, type Line } from './Chart';
import { firstReady, tabReady, type TabId, type TrendTab } from './evTabs';

const HEIGHT = 260;

export function EvTrendPanel({ tabs }: { tabs: TrendTab[] }) {
  const [chosen, setChosen] = useState<TabId | null>(null);
  const id = chosen && tabs.some((t) => t.id === chosen) ? chosen : firstReady(tabs);
  const tab = tabs.find((t) => t.id === id);

  // The chart is rebuilt when its inputs are different objects, and the page polls: so what it is given is made again only when the points really changed.
  const key = tab ? `${tab.id}|${tab.x.join(',')}|${tab.y.join(',')}` : '';
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const lines = useMemo<Line[]>(() => (tab ? [{ name: tab.label, colorVar: tab.colorVar, x: tab.x, y: tab.y }] : []), [key]);
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
        <Chart lines={lines} domain={domain} height={HEIGHT} format={tab.format} label={tab.title} />
      ) : (
        <div class="ev-waiting" style={{ minHeight: `${HEIGHT}px` }}>
          <p>{tab.waiting}</p>
        </div>
      )}
    </section>
  );
}
