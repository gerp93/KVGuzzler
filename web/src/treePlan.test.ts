import { describe, expect, it } from 'vitest';
import { flowTree } from './flow';
import { arrangeByPanel, shownMeters, underHouse, type Meter, type Summary } from './lib';
import type { PcPart } from './pcparts';
import { planTree } from './treePlan';
import { restRows } from './treeRest';
import { layoutDiagram } from './treeDiagram';

const m = (id: string, scope: Meter['scope'], parent: string | null = null): Meter =>
  ({ id, device_id: 'd', scope, label: id, enabled: true, type: scope === 'mains' ? 'house' : 'circuit', parent_id: parent, invert: false, pair_with: null, favorite: false, ev: false }) as unknown as Meter;

const summary = {
  from: 'a',
  to: 'b',
  house_kwh: 10,
  house_cost: '2.00',
  unmetered_kwh: 4,
  meters: [
    { meter_id: 'house', scope: 'mains', kwh: 10, cost: '2.00', monitored_hours: 1 },
    { meter_id: 'range', scope: 'circuit', kwh: 5, cost: '1.00', monitored_hours: 1 },
    { meter_id: 'garage', scope: 'circuit', kwh: 1, cost: '0.20', monitored_hours: 1 },
    { meter_id: 'ev', scope: 'circuit', kwh: 0.5, cost: '0.10', monitored_hours: 1 },
    { meter_id: 'pc', scope: 'pc', kwh: 1.5, cost: '0.30', monitored_hours: 1 },
    { meter_id: 'monitor', scope: 'plug', kwh: 0.5, cost: '0.10', monitored_hours: 1 },
  ],
} as unknown as Summary;

const cpu: PcPart = { key: 'cpu', label: 'CPU', kwh: 0.5, cost: '0.10', watts: 50 };

describe('the plan of the Hierarchy tab', () => {
  // The plugs and the computer name no circuit they are on, as on the Live page.
  const meters = [m('house', 'mains'), m('range', 'circuit'), m('garage', 'circuit'), m('ev', 'circuit', 'garage'), m('pc', 'pc'), m('monitor', 'plug')];
  const rows = underHouse(arrangeByPanel(shownMeters(meters)));
  const watts = new Map([['house', 1000], ['range', 500], ['garage', 100], ['ev', 50], ['pc', 150], ['monitor', 40]]);
  const plan = (rests = restRows(rows, flowTree(meters, summary), watts, 1000)) =>
    planTree({ rows, watts, houseWatts: 1000, summary, rests, partsOf: (x) => (x.id === 'pc' ? { parts: [cpu], href: '#pc' } : undefined) });

  it('puts the plugs and the computer under "Everything else", after the circuits, with "Not individually metered" last inside it', () => {
    expect(plan().map((e) => [e.kind === 'row' ? e.meter.id : e.kind === 'part' ? e.part.label : e.label, e.depth])).toEqual([
      ['house', 0],
      ['range', 1],
      ['garage', 1],
      ['ev', 2],
      ['Other (not metered)', 2], // what the garage's feed carries beyond the ev
      ['Everything else', 1],
      ['pc', 2],
      ['CPU', 3],
      ['monitor', 2],
      ['Not individually metered', 2],
    ]);
  });

  it('has no "Other (not metered)" under the house: what is left of it is the group', () => {
    expect(plan().filter((e) => e.kind === 'lump' && e.label === 'Other (not metered)' && e.depth === 1)).toEqual([]);
  });

  it('says the figures the Live page says: the house less its circuits, and what the plugs leave of that', () => {
    const lumps = plan().filter((e) => e.kind === 'lump');
    const group = lumps.find((e) => e.label === 'Everything else')!;
    expect(group).toMatchObject({ watts: 400, kwh: 4, cost: '0.80' });
    const left = lumps.find((e) => e.label === 'Not individually metered')!;
    expect(left).toMatchObject({ watts: 210, kwh: 2 });
  });

  it('keeps the rows the move arrows read, whatever depth they are drawn at', () => {
    const monitor = plan().find((e) => e.kind === 'row' && e.meter.id === 'monitor')!;
    expect(monitor.kind === 'row' && rows[monitor.at]!.meter.id).toBe('monitor');
  });

  it('leaves the rows as they are with no whole-house meter', () => {
    const flat = underHouse(arrangeByPanel(shownMeters([m('range', 'circuit'), m('monitor', 'plug')])));
    const out = planTree({ rows: flat, watts: new Map(), houseWatts: null, summary: undefined, rests: [], partsOf: () => undefined });
    expect(out.map((e) => [e.kind === 'row' ? e.meter.id : '?', e.depth])).toEqual([['range', 0], ['monitor', 0]]);
  });

  it('does not claim a power now when the house has no reading', () => {
    const out = planTree({ rows, watts: new Map(), houseWatts: null, summary, rests: [], partsOf: () => undefined });
    expect(out.filter((e) => e.kind === 'lump').every((e) => e.kind === 'lump' && e.watts === null)).toBe(true);
  });
});

describe('the diagram layout', () => {
  const sizes = { boxW: 100, boxH: 30, gapX: 20, slot: 40, pad: 5 };

  it('gives every leaf its own row, centres a parent on its children and runs a column per level', () => {
    const l = layoutDiagram(
      [
        { id: 'house', depth: 0 },
        { id: 'a', depth: 1 },
        { id: 'a1', depth: 2 },
        { id: 'a2', depth: 2 },
        { id: 'b', depth: 1 },
      ],
      sizes,
    );
    const at = (id: string) => l.nodes.find((n) => n.id === id)!;
    expect([at('a1').y, at('a2').y, at('b').y]).toEqual([5, 45, 85]);
    expect(at('a').y).toBe(25);
    expect(at('house').y).toBe(55); // between a (25) and b (85)
    expect([at('house').x, at('a').x, at('a1').x]).toEqual([5, 125, 245]);
    expect(l.links.map((k) => `${k.from}>${k.to}`)).toEqual(['house>a', 'a>a1', 'a>a2', 'house>b']);
    expect(l.width).toBe(10 + 3 * 100 + 2 * 20);
    expect(l.height).toBe(10 + 2 * 40 + 30);
  });

  it('stacks several top-level lines with no link between them', () => {
    const l = layoutDiagram([{ id: 'x', depth: 0 }, { id: 'y', depth: 0 }], sizes);
    expect(l.links).toEqual([]);
    expect(l.nodes.map((n) => n.y)).toEqual([5, 45]);
  });
});
