// Which meter's odometer the Ford connection reads. Ford is linked once for the house but reads one charging meter, so the Connectors tab has to say which: the one
// picked there, else the one already being read, else the first charging meter.
import type { Meter } from './lib';

export function pickFordMeter(meters: Meter[], linkedId: string | null | undefined, chosenId: string): Meter | undefined {
  const cars = meters.filter((m) => m.ev);
  return cars.find((m) => m.id === chosenId) ?? meters.find((m) => m.id === linkedId) ?? cars[0];
}
