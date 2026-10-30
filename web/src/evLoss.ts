// Words for the charging loss in the charges list. Kept apart from `ev.ts` because it formats money, which `lib.ts` does and `lib.ts` already imports `ev.ts`.
import type { EvCharge } from './ev';
import { formatMoney } from './lib';

/** What a charge's efficiency cell says behind it: the energy that reached the battery and what was lost, with what the loss cost. */
export function lossTitle(c: Pick<EvCharge, 'battery_kwh' | 'loss_kwh' | 'loss_cost'>): string {
  if (c.battery_kwh == null || c.loss_kwh == null) return '';
  const cost = c.loss_cost != null ? ` (${formatMoney(c.loss_cost)})` : '';
  return `${c.battery_kwh.toFixed(1)} kWh reached the battery, ${c.loss_kwh.toFixed(1)} kWh was lost${cost}`;
}
