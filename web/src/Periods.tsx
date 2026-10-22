import { PERIODS, type PeriodKey } from './lib';

export function PeriodPicker({ value, onChange }: { value: PeriodKey; onChange: (k: PeriodKey) => void }) {
  return (
    <div class="seg" role="group" aria-label="Period">
      {PERIODS.map((p) => (
        <button key={p.key} type="button" aria-pressed={p.key === value} onClick={() => onChange(p.key)}>
          {p.label}
        </button>
      ))}
    </div>
  );
}
