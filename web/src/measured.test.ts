import { describe, expect, it } from 'vitest';
import { measuredTotals, topLevelMeters } from './measured';
import { periodWords, type Meter, type Summary } from './lib';

const m = (id: string, scope: Meter['scope'], extra: Partial<Meter> = {}): Meter => ({
  id, device_id: 'd', scope, type: scope === 'circuit' ? 'circuit' : 'device', label: id, enabled: true, index: null, max_watts: null, ...extra,
});
const f = (meter_id: string, scope: Meter['scope'], kwh: number, cost: string | null, peak: number | null = null) => ({ meter_id, scope, kwh, cost, peak_watts: peak, monitored_hours: 1 });

// A garage panel holding a charger, a PC and a monitor that name no circuit, and a hidden spare.
const meters = [
  m('garage', 'circuit'),
  m('ev', 'circuit', { parent_id: 'garage' }),
  m('pc', 'pc', { type: 'computer' }),
  m('monitor', 'plug'),
  m('spare', 'circuit', { enabled: false }),
  m('mains', 'mains', { type: 'house' }),
];
const summary: Summary = {
  from: 'a', to: 'b', house_kwh: null,
  meters: [f('garage', 'circuit', 3.4, '0.53', 7000), f('ev', 'circuit', 3.3, '0.52', 7000), f('pc', 'pc', 0.15, '0.02', 300), f('monitor', 'plug', 0.05, '0.01', 40), f('spare', 'circuit', 9, '1.00', 100)],
};

describe('what is at the top of the tree', () => {
  it('is the circuits on no panel and the plugs and computers that name no circuit, never what is inside another meter or the mains', () => {
    expect(topLevelMeters(meters).map((x) => x.id)).toEqual(['garage', 'pc', 'monitor']);
  });
  it('treats a meter whose parent is hidden as at the top', () => {
    const orphan = [m('p', 'circuit', { enabled: false }), m('c', 'circuit', { parent_id: 'p' })];
    expect(topLevelMeters(orphan).map((x) => x.id)).toEqual(['c']);
  });
});

describe('everything measured', () => {
  it('adds the top-level meters only: the charger is inside the garage panel and is not added again', () => {
    const t = measuredTotals(meters, summary, [{ meter_id: 'garage', watts: 54 }, { meter_id: 'ev', watts: 0 }, { meter_id: 'pc', watts: 147 }, { meter_id: 'monitor', watts: 40 }]);
    expect(t.kwh).toBeCloseTo(3.6, 9); // 3.4 + 0.15 + 0.05, not the charger's 3.3 as well
    expect(t.watts).toBe(54 + 147 + 40);
    expect(t.cost).toBe('0.56');
    expect(t.peakWatts).toBe(7340);
    expect(t.count).toBe(3);
  });
  it('has no cost unless every meter has one, and nothing at all when nothing is measured', () => {
    const noPrice = { ...summary, meters: summary.meters.map((x) => (x.meter_id === 'pc' ? { ...x, cost: null } : x)) };
    expect(measuredTotals(meters, noPrice, []).cost).toBeNull();
    expect(measuredTotals(meters, noPrice, []).watts).toBeNull();
    const none = measuredTotals([], undefined, []);
    expect(none).toEqual({ watts: null, kwh: null, cost: null, peakWatts: null, count: 0 });
  });
});

describe('the period in words', () => {
  it('names each period, and all of them for all time', () => {
    expect(['1h', '3h', '8h', '24h', '3d', '7d', '14d', '30d', 'ytd', '1y', 'all'].map((k) => periodWords(k as never))).toEqual([
      'the last hour', 'the last 3 hours', 'the last 8 hours', 'the last 24 hours', 'the last 3 days', 'the last 7 days', 'the last 14 days',
      'the last 30 days', 'this year so far', 'the last year', 'all recorded time',
    ]);
  });
});
