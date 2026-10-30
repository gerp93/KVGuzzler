// Whether a PC's collector is reporting: it sends a look every few seconds while it runs, so one that has not been heard from for this long is not answering.
export const COLLECTOR_STALE_SECONDS = 30;

export type CollectorState = { state: 'reporting' | 'silent' | 'never'; ageSeconds: number };

export function collectorState(lastSeen: string | null | undefined, now: number): CollectorState {
  const at = lastSeen ? Date.parse(lastSeen) : NaN;
  if (Number.isNaN(at)) return { state: 'never', ageSeconds: 0 };
  const ageSeconds = Math.max(0, Math.round((now - at) / 1000));
  return { state: ageSeconds > COLLECTOR_STALE_SECONDS ? 'silent' : 'reporting', ageSeconds };
}
