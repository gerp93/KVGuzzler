// A line under a cost saying how it was priced, only when the price comes from a looked-up utility tariff.
import { api } from './api';
import { usePoll } from './hooks';
import { tariffNote } from './lib';

export function RateNote({ kwh, cost }: { kwh: number | null | undefined; cost: string | null | undefined }) {
  const { data } = usePoll(async () => {
    const cfg = await api.rates();
    return cfg.mode === 'lookup' ? api.tariff() : undefined;
  }, 300_000, []);
  if (!data) return null;
  return <p class="caption">{tariffNote(data, kwh ?? 0, cost ?? null)}</p>;
}
