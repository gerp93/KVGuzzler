import { describe, expect, it } from 'vitest';
import type { Meter } from './lib';
import { pickFordMeter } from './fordMeter';

const meter = (id: string, ev: boolean) => ({ id, label: id, ev }) as unknown as Meter;
const meters = [meter('house', false), meter('car-a', true), meter('car-b', true)];

describe('which meter Ford reads', () => {
  it('is the charging meter picked in the tab', () => {
    expect(pickFordMeter(meters, 'car-a', 'car-b')?.id).toBe('car-b');
  });

  it('is the meter already being read when nothing is picked', () => {
    expect(pickFordMeter(meters, 'car-b', '')?.id).toBe('car-b');
  });

  it('keeps reading a meter whose charging mark was turned off', () => {
    expect(pickFordMeter([meter('car-a', false), meter('car-b', true)], 'car-a', '')?.id).toBe('car-a');
  });

  it('is the first charging meter when none is linked yet', () => {
    expect(pickFordMeter(meters, undefined, '')?.id).toBe('car-a');
  });

  it('is nothing when no meter charges a vehicle', () => {
    expect(pickFordMeter([meter('house', false)], null, '')).toBeUndefined();
  });

  it('ignores a pick that is not a charging meter', () => {
    expect(pickFordMeter(meters, 'car-a', 'house')?.id).toBe('car-a');
  });
});
