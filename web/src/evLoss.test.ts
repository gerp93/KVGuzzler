import { describe, expect, it } from 'vitest';
import { parseKwh, percentText } from './ev';
import { lossTitle } from './evLoss';

describe('parseKwh', () => {
  it('takes a believable battery size', () => {
    expect(parseKwh('88')).toBe(88);
    expect(parseKwh(' 91.0 ')).toBe(91);
  });
  it('refuses the rest', () => {
    for (const bad of ['', 'big', '5', '9.9', '301', '-70']) expect(parseKwh(bad)).toBeNull();
  });
});

describe('percentText', () => {
  it('writes a share as a whole percent', () => {
    expect(percentText(0.8996)).toBe('90 %');
    expect(percentText(1.04)).toBe('104 %');
  });
});

describe('lossTitle', () => {
  it('says what reached the battery, what was lost and what the loss cost', () => {
    expect(lossTitle({ battery_kwh: 22.4, loss_kwh: 2.5, loss_cost: '0.39' })).toBe('22.4 kWh reached the battery, 2.5 kWh was lost ($0.39)');
  });
  it('leaves the price out when there is none, and says nothing without figures', () => {
    expect(lossTitle({ battery_kwh: 22.4, loss_kwh: 2.5, loss_cost: null })).toBe('22.4 kWh reached the battery, 2.5 kWh was lost');
    expect(lossTitle({ battery_kwh: null, loss_kwh: null, loss_cost: null })).toBe('');
  });
});
