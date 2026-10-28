// What a cost was priced at, as labelled figures, only when the price comes from a looked-up utility tariff.
import { api } from './api';
import { Facts } from './Facts';
import { usePoll } from './hooks';
import { tariffFacts } from './lib';

export function RateNote({ kwh, cost }: { kwh: number | null | undefined; cost: string | null | undefined }) {
  const { data } = usePoll(async () => {
    const cfg = await api.rates();
    return cfg.mode === 'lookup' ? api.tariff() : undefined;
  }, 300_000, []);
  if (!data) return null;
  return <Facts items={tariffFacts(data, kwh ?? 0, cost ?? null)} />;
}
