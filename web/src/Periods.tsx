import { PERIODS, type PeriodKey } from './lib';

// A phone has no room for "24 hours": the short form is shown there (the full word stays in the accessible name).
const SHORT: Record<PeriodKey, string> = { '1h': '1h', '24h': '24h', '7d': '7d', '30d': '30d', '1y': '1y', all: 'All' };

export function PeriodPicker({ value, onChange }: { value: PeriodKey; onChange: (k: PeriodKey) => void }) {
  return (
    <div class="seg period" role="group" aria-label="Period">
      {PERIODS.map((p) => (
        <button key={p.key} type="button" aria-pressed={p.key === value} aria-label={p.label} onClick={() => onChange(p.key)}>
          <span class="long" aria-hidden="true">{p.label}</span>
          <span class="short" aria-hidden="true">{SHORT[p.key]}</span>
        </button>
      ))}
    </div>
  );
}
