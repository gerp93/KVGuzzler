import { describe, expect, it } from 'vitest';
import { parseRoute, routePath } from './route';

describe('the hash router', () => {
  it('reads a tab, and falls back when there is none', () => {
    expect(parseRoute('#/live', 'history')).toEqual({ tab: 'live', rest: [] });
    expect(parseRoute('', 'history')).toEqual({ tab: 'history', rest: [] });
    expect(parseRoute('#', 'history')).toEqual({ tab: 'history', rest: [] });
    expect(parseRoute('#/', 'history')).toEqual({ tab: 'history', rest: [] });
  });

  it('reads what is open inside a tab', () => {
    expect(parseRoute('#/appliances/computer/comp-1a2b', 'live')).toEqual({ tab: 'appliances', rest: ['computer', 'comp-1a2b'] });
  });

  it('round-trips ids that need escaping, such as a plug meter with a colon', () => {
    const path = routePath('appliances', 'meter', 'shelly-192.168.1.67:plug');
    expect(path).toBe('#/appliances/meter/shelly-192.168.1.67%3Aplug');
    expect(parseRoute(path, 'live').rest).toEqual(['meter', 'shelly-192.168.1.67:plug']);
  });

  it('does not fall over on a broken escape', () => {
    expect(parseRoute('#/appliances/meter/%E0%A4%A', 'live').rest).toEqual(['meter', '%E0%A4%A']);
  });
});
