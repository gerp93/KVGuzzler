import { describe, expect, it } from 'vitest';
import { figureText, flowTree, labelRoom, layoutFlow, type FlowNode } from './flow';
import type { Meter, Summary } from './lib';

const m = (id: string, scope: Meter['scope'], extra: Partial<Meter> = {}): Meter => ({
  id, device_id: 'd', scope, type: scope === 'mains' ? 'house' : scope === 'circuit' ? 'circuit' : 'device', label: id, enabled: true, index: null, max_watts: null, ...extra,
});
const f = (meter_id: string, scope: Meter['scope'], kwh: number, cost?: string) => ({ meter_id, scope, kwh, cost, monitored_hours: 1 });

const meters = [m('mains', 'mains'), m('garage', 'circuit'), m('ev', 'circuit', { parent_id: 'garage' }), m('lights', 'circuit', { parent_id: 'garage' }), m('dryer', 'circuit'), m('fridge', 'plug', { parent_id: 'dryer' })];
const summary: Summary = {
  from: 'a', to: 'b', house_kwh: 20, house_cost: '3.00', unmetered_kwh: 3,
  meters: [f('mains', 'mains', 20, '3.00'), f('garage', 'circuit', 10, '1.50'), f('ev', 'circuit', 8, '1.20'), f('lights', 'circuit', 1, '0.15'), f('dryer', 'circuit', 7, '1.05'), f('fridge', 'plug', 2, '0.30')],
};

const flat = (n: FlowNode): FlowNode[] => [n, ...n.children.flatMap(flat)];

describe('the flow chart tree', () => {
  it('is the house, its top-level circuits and "Everything else" (what no circuit accounts for), with each parent holding its own children', () => {
    const tree = flowTree(meters, summary)!;
    expect(tree).toMatchObject({ id: 'house', kwh: 20, cost: '3.00' });
    expect(tree.children.map((c) => [c.id, c.kwh])).toEqual([['garage', 10], ['dryer', 7], ['everything-else', 3]]);
    expect(tree.children[0]!.children.map((c) => [c.id, c.kwh])).toEqual([['ev', 8], ['lights', 1], ['rest-garage', 1]]);
    expect(tree.children[2]).toMatchObject({ meterId: null, label: 'Everything else', cost: '0.45', group: true, children: [] });
    expect(tree.children[1]!.children.map((c) => c.id)).toEqual(['fridge', 'rest-dryer']); // a plug on a circuit
  });

  it('without a whole-house clamp the top-level circuits added up stand in for it', () => {
    const noHouse = { ...summary, house_kwh: null, house_cost: null, meters: summary.meters.filter((x) => x.meter_id !== 'mains') };
    const tree = flowTree(meters.filter((x) => x.id !== 'mains'), noHouse)!;
    expect(tree).toMatchObject({ id: 'all', label: 'Everything measured', kwh: 17, cost: '2.55' });
    expect(tree.children.map((c) => c.id)).toEqual(['garage', 'dryer']);
  });

  it('can start from any meter, and is nothing when there is nothing to draw', () => {
    expect(flowTree(meters, summary, 'garage')!.children.map((c) => c.id)).toEqual(['ev', 'lights', 'rest-garage']);
    expect(flowTree(meters, summary, 'nope')).toBeUndefined();
    expect(flowTree(meters, { ...summary, meters: [] })).toBeUndefined();
  });

  it('never lets the parts exceed the whole: no remainder when the children add up to more', () => {
    const over = { ...summary, meters: summary.meters.map((x) => (x.meter_id === 'garage' ? { ...x, kwh: 8.5 } : x)) };
    const garage = flowTree(meters, over, 'garage')!;
    expect(garage.children.some((c) => c.id === 'rest-garage')).toBe(false);
  });
});

describe('a computer in the flow chart', () => {
  it("has its collector's parts as bands and what they do not account for as Everything else", () => {
    const withPc = [...meters, m('wall', 'pc', { type: 'computer', computer_id: 'c1', parent_id: 'dryer' })];
    const sum: Summary = { ...summary, meters: [...summary.meters, f('wall', 'pc', 2, '0.30')] };
    const virtual = new Map([['wall', [{ label: 'GPU', kwh: 1, cost: '0.15' }, { label: 'CPU package', kwh: 0.6, cost: '0.09' }]]]);
    const wall = flowTree(withPc, sum, 'wall', virtual)!;
    expect(wall.children.map((c) => [c.label, Math.round(c.kwh * 10) / 10, !!c.rest])).toEqual([['GPU', 1, false], ['CPU package', 0.6, false], ['Everything else', 0.4, true]]);
    // Without parts it is as before: nothing under it.
    expect(flowTree(withPc, sum, 'wall')!.children).toEqual([]);
    // Inside the house, the computer sits in the circuit it is on.
    const house = flowTree(withPc, sum, undefined, virtual)!;
    const dryer = house.children.find((c) => c.id === 'dryer')!;
    expect(dryer.children.find((c) => c.id === 'wall')!.children).toHaveLength(3);
  });
});

describe('the flow chart layout', () => {
  const tree = flowTree(meters, summary)!;
  const layout = layoutFlow(tree, 720);
  const at = (id: string) => layout.nodes.find((n) => n.node.id === id)!;

  it('puts each level in its own column, and sizes every band by the same scale', () => {
    expect([at('house').depth, at('garage').depth, at('ev').depth]).toEqual([0, 1, 2]);
    expect(at('house').x).toBeLessThan(at('garage').x);
    expect(at('garage').x).toBeLessThan(at('ev').x);
    const k = at('house').h / 20;
    expect(at('garage').h / 10).toBeCloseTo(k, 6);
    expect(at('ev').h / 8).toBeCloseTo(k, 6);
  });

  it('never lets bands in a column overlap or leave the drawing', () => {
    for (const depth of [0, 1, 2]) {
      const col = layout.nodes.filter((n) => n.depth === depth).sort((a, b) => a.y - b.y);
      col.forEach((n, i) => {
        expect(n.y + n.h).toBeLessThanOrEqual(layout.height);
        if (i > 0) expect(n.y).toBeGreaterThanOrEqual(col[i - 1]!.y + col[i - 1]!.h);
      });
    }
  });

  it('draws a link from every parent to each child, as wide as the child, leaving the parent in order', () => {
    expect(layout.links).toHaveLength(flat(tree).length - 1);
    const link = (to: string) => layout.links.find((l) => l.to.node.id === to)!;
    expect(link('ev').w).toBeCloseTo(at('ev').h, 6);
    expect(link('ev').y0).toBeLessThan(link('lights').y0);
  });

  it('keeps labels from sitting on top of each other', () => {
    for (const depth of [1, 2]) {
      const col = layout.nodes.filter((n) => n.depth === depth).sort((a, b) => a.labelY - b.labelY);
      col.forEach((n, i) => {
        if (i > 0) expect(n.labelY - col[i - 1]!.labelY).toBeGreaterThanOrEqual(7);
      });
    }
  });
});

describe('a plug or a computer on a circuit nothing clamps', () => {
  // The PC and its monitor say no circuit they are on: they belong to the house beside the circuits, not inside the garage panel.
  const loose = [...meters, m('wall', 'pc', { type: 'computer', computer_id: 'c1' }), m('monitor', 'plug')];
  const sum: Summary = { ...summary, meters: [...summary.meters, f('wall', 'pc', 2, '0.30'), f('monitor', 'plug', 0.5, '0.08')] };

  it('is listed inside "Everything else" as the circuits table has it, with what none of them explains as its last line', () => {
    const tree = flowTree(loose, sum)!;
    expect(tree.children.map((c) => [c.id, c.kwh])).toEqual([['garage', 10], ['dryer', 7], ['everything-else', 3]]);
    const group = tree.children[2]!;
    expect(group).toMatchObject({ label: 'Everything else', cost: '0.45' });
    expect(group.children.map((c) => [c.label, c.kwh])).toEqual([['wall', 2], ['monitor', 0.5], ['Not individually metered', 0.5]]);
    // 20 kWh house less 10 + 7 is the group's 3; the plugs and the last line are inside it, not added to it: nothing is counted twice.
    expect(tree.children.reduce((t, c) => t + c.kwh, 0)).toBeCloseTo(20, 9);
    expect(group.children.reduce((t, c) => t + c.kwh, 0)).toBeCloseTo(group.kwh, 9);
    expect(group.children[2]).toMatchObject({ rest: true, cost: '0.07' });
  });

  it('still groups them when the circuits used more than the house did, saying only what they add up to', () => {
    const over = { ...sum, house_kwh: 15, meters: sum.meters.map((x) => (x.meter_id === 'mains' ? { ...x, kwh: 15 } : x)) };
    const tree = flowTree(loose, over)!;
    expect(tree.children.map((c) => c.id)).toEqual(['garage', 'dryer', 'everything-else']);
    const group = tree.children[2]!;
    expect(group).toMatchObject({ kwh: 2.5, cost: '0.38' });
    expect(group.children.map((c) => c.id)).toEqual(['wall', 'monitor']); // no "Not individually metered": what is left of the house is not known
    // With no plug either, there is nothing to say about what is left, and no group.
    expect(flowTree(meters, { ...over, house_kwh: 15 })!.children.map((c) => c.id)).toEqual(['garage', 'dryer']);
  });

  it('stands in the all-circuits whole when there is no whole-house clamp, which is the sum of everything at the top', () => {
    const noHouse = { ...sum, house_kwh: null, house_cost: null, meters: sum.meters.filter((x) => x.meter_id !== 'mains') };
    const tree = flowTree(loose.filter((x) => x.id !== 'mains'), noHouse)!;
    expect(tree).toMatchObject({ id: 'all', kwh: 19.5 });
    expect(tree.children.map((c) => c.id)).toEqual(['garage', 'dryer', 'wall', 'monitor']);
  });

  it('is not drawn at the top once it says which circuit it is on, and a plug with no figures is left out', () => {
    const placed = loose.map((x) => (x.id === 'wall' ? { ...x, parent_id: 'dryer' } : x));
    const tree = flowTree(placed, sum)!;
    expect(tree.children.map((c) => c.id)).not.toContain('wall');
    expect(tree.children.find((c) => c.id === 'dryer')!.children.map((c) => c.id)).toContain('wall');
    const quiet = { ...sum, meters: sum.meters.filter((x) => x.meter_id !== 'monitor') };
    expect(flowTree(loose, quiet)!.children.map((c) => c.id)).not.toContain('monitor');
  });
});

describe('what each band says', () => {
  const node = (label: string, kwh: number, cost: string | null, children: FlowNode[] = []): FlowNode => ({ id: label, label, kwh, cost, meterId: label, children });

  it('gives every band its cost beside its energy, however short the band is, and the cost alone on a narrow screen', () => {
    expect(figureText(node('EV charger', 42.2, '6.58'), false)).toBe('42.2 kWh · $6.58');
    expect(figureText(node('Dryer', 35.3, '5.57'), true)).toBe('35.3 kWh · ≈ $5.57');
    expect(figureText(node('Dryer', 35.3, '5.57'), false, false)).toBe('$5.57');
  });

  it('says only the energy when no price is known, never a made-up zero', () => {
    expect(figureText(node('Dryer', 35.3, null), false)).toBe('35.3 kWh');
  });

  it('leaves room for the longest label of the last column, within limits', () => {
    const tree = node('House', 20, '3.00', [node('Garage', 10, '1.50', [node('South Garage Subpanel', 2.75, '0.43'), node('EV', 8, '1.20')])]);
    const room = labelRoom(tree, false, 1, 230, 380);
    expect(room).toBeGreaterThan(230);
    expect(room).toBeLessThanOrEqual(380);
    expect(labelRoom(node('A', 1, null), false, 1, 230, 380)).toBe(230); // short labels keep the least
    expect(labelRoom(node('A very long name '.repeat(8), 1, '1.00'), false, 1, 230, 380)).toBe(380);
  });
});

describe('a meter that used nothing', () => {
  const zero = [...meters, m('welder', 'circuit', { parent_id: 'garage' }), m('spare', 'circuit')];
  const sum: Summary = { ...summary, meters: [...summary.meters, f('welder', 'circuit', 0, '0.00'), f('spare', 'circuit', 0, '0.00')] };

  it('has no band, inside a panel or at the top, though it is measured', () => {
    const tree = flowTree(zero, sum)!;
    expect(tree.children.map((c) => c.id)).toEqual(['garage', 'dryer', 'everything-else']);
    expect(tree.children[0]!.children.map((c) => c.id)).toEqual(['ev', 'lights', 'rest-garage']);
  });

  it('still has one on its own page only if it used something, and its parts of nothing are left out', () => {
    expect(flowTree(zero, sum, 'welder')!.kwh).toBe(0);
    const virtual = new Map([['dryer', [{ label: 'Idle part', kwh: 0, cost: '0.00' }, { label: 'Real part', kwh: 1, cost: '0.15' }]]]);
    const dryer = flowTree(zero, sum, 'dryer', virtual)!;
    expect(dryer.children.map((c) => c.label)).toEqual(['fridge', 'Real part', 'Everything else']);
  });
});
