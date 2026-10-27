import { useEffect } from 'preact/hooks';
import { createPortal } from 'preact/compat';
import { api } from './api';
import { usePoll } from './hooks';
import { visiblePeriods, type PeriodKey } from './lib';

// A phone has no room for "24 hours": the short form is shown there (the full word stays in the accessible name).
const SHORT: Record<PeriodKey, string> = {
  '1h': '1h', '3h': '3h', '8h': '8h', '24h': '24h', '3d': '3d', '7d': '7d', '14d': '14d', '30d': '30d', ytd: 'YTD', '1y': '1y', all: 'All',
};

/** The period choice (only the periods there is that much history for) is a floating island (a page that is one screen long and only changes its chart keeps it `inline`, in the page) at the bottom of the window, so it can be changed wherever the page is scrolled to. It is drawn into the page's
 *  body, not where it is written: a card or panel that contains its layout would otherwise pin it inside itself. While it is shown the page leaves room under its last line. */
export function PeriodPicker({ value, onChange, inline = false }: { value: PeriodKey; onChange: (k: PeriodKey) => void; inline?: boolean }) {
  // Only the periods the recorded history reaches back to are offered (the server says when recording began).
  const { data: status } = usePoll(api.status, 60_000, []);
  const periods = visiblePeriods(status?.data_since, value, new Date());
  useEffect(() => {
    if (inline) return;
    document.body.classList.add('has-period');
    return () => document.body.classList.remove('has-period');
  }, [inline]);
  const picker = (
    <div class="seg period" role="group" aria-label="Period">
      {periods.map((p) => (
        <button key={p.key} type="button" aria-pressed={p.key === value} aria-label={p.label} onClick={() => onChange(p.key)}>
          <span class="long" aria-hidden="true">{p.label}</span>
          <span class="short" aria-hidden="true">{SHORT[p.key]}</span>
        </button>
      ))}
    </div>
  );
  return inline ? picker : createPortal(<div class="period-island">{picker}</div>, document.body);
}
