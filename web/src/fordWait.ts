// The wait Ford asks for between two looks: it answers one request per 30 seconds, the server refuses one inside that, and the buttons that make a look
// count the time down. The server says how long is left (`read_wait_seconds`); the page only counts.
import { useEffect, useState } from 'preact/hooks';
import type { FordLink } from './lib';

/** Ford answers one look every 30 seconds. */
export const MIN_CALL_GAP_S = 30;

export function useFordWait(cfg: FordLink | undefined): { waitLeft: number; wait: (seconds: number) => void } {
  // When Ford may be asked again (a millisecond time) and the clock that counts down to it.
  const [until, setUntil] = useState(0);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const left = cfg?.read_wait_seconds ?? 0;
    if (left > 0) setUntil(Date.now() + left * 1000);
  }, [cfg]);
  useEffect(() => {
    if (until <= Date.now()) return;
    const id = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(id);
  }, [until]);
  const wait = (seconds: number) => {
    setNow(Date.now());
    setUntil(Date.now() + seconds * 1000);
  };
  return { waitLeft: Math.max(0, Math.ceil((until - now) / 1000)), wait };
}
