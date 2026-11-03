// The outage log as the History tab's card shows it: what each kind is called, how its times are shown and typed.
import type { components } from './api-types';

export type Outage = components['schemas']['Outage'];
export type Outages = components['schemas']['Outages'];
export type OutageKind = Outage['kind'];

export const OUTAGE_KINDS: OutageKind[] = ['power', 'server', 'unknown'];

/** What a kind is called. */
export function kindLabel(kind: OutageKind): string {
  switch (kind) {
    case 'power':
      return 'Power outage';
    case 'server':
      return 'Server off';
    default:
      return 'Not recorded';
  }
}

/** What a kind means for the figures, for a tooltip: only a stretch the server was off, or could not explain, is missing something. */
export function kindMeaning(kind: OutageKind): string {
  switch (kind) {
    case 'power':
      return 'The power was out, so nothing was used and the figures are right.';
    case 'server':
      return 'The power was on but nothing was recording, so what the house used then is missing from the figures.';
    default:
      return 'Nothing was recorded and the cause is not known, so the figures may be missing what the house used.';
  }
}

/** Where an outage came from. */
export function sourceLabel(source: Outage['source']): string {
  switch (source) {
    case 'plugs':
      return 'Found from the plugs';
    case 'gap':
      return 'Found from a gap';
    default:
      return 'Typed in';
  }
}

const two = (n: number) => String(n).padStart(2, '0');

/** An instant as the value of a datetime-local input: the viewer's own date and time, to the minute. */
export function toLocalInput(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}T${two(d.getHours())}:${two(d.getMinutes())}`;
}

/** The instant a datetime-local value means in the viewer's zone, as an ISO string; null for an empty or unreadable value. */
export function fromLocalInput(value: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return null;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

/** When an outage ran, in the viewer's zone: "Nov 2, 1:14 PM – 2:47 PM", with the end's day too when it is another day. */
export function whenText(o: Pick<Outage, 'started_at' | 'ended_at'>): string {
  const from = new Date(o.started_at);
  const to = new Date(o.ended_at);
  const day = (d: Date) => d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  const time = (d: Date) => d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  const sameDay = from.toDateString() === to.toDateString();
  return `${day(from)}, ${time(from)} – ${sameDay ? time(to) : `${day(to)}, ${time(to)}`}`;
}
