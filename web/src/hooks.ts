import { useEffect, useState } from 'preact/hooks';
import type { DistanceUnit } from './ev';

export interface Poll<T> {
  data: T | undefined;
  error: Error | undefined;
}

/** Runs `fn` now and every `ms` milliseconds (0 = once), and again whenever `deps` change. Keeps the last good data. */
export function usePoll<T>(fn: () => Promise<T>, ms: number, deps: unknown[]): Poll<T> {
  const [state, setState] = useState<Poll<T>>({ data: undefined, error: undefined });
  useEffect(() => {
    let alive = true;
    const run = () =>
      fn().then(
        (data) => alive && setState({ data, error: undefined }),
        (error: Error) => alive && setState((s) => ({ data: s.data, error })),
      );
    void run();
    const id = ms > 0 ? setInterval(run, ms) : undefined;
    return () => {
      alive = false;
      if (id !== undefined) clearInterval(id);
    };
  }, deps);
  return state;
}

const DISTANCE_KEY = 'kvg.distanceUnit';
const DISTANCE_EVENT = 'kvg-distance-unit';

function storedDistanceUnit(): DistanceUnit {
  try {
    return localStorage.getItem(DISTANCE_KEY) === 'km' ? 'km' : 'miles';
  } catch {
    return 'miles';
  }
}

/** The unit the electric vehicle's distances are shown in: the viewer's own choice, remembered on this device and shared by every such figure on the page. */
export function useDistanceUnit(): [DistanceUnit, (u: DistanceUnit) => void] {
  const [unit, setUnit] = useState<DistanceUnit>(storedDistanceUnit);
  useEffect(() => {
    const changed = () => setUnit(storedDistanceUnit());
    window.addEventListener(DISTANCE_EVENT, changed);
    return () => window.removeEventListener(DISTANCE_EVENT, changed);
  }, []);
  const set = (u: DistanceUnit) => {
    setUnit(u);
    try {
      localStorage.setItem(DISTANCE_KEY, u);
    } catch {
      // storage unavailable: the choice lasts until the page closes
    }
    window.dispatchEvent(new Event(DISTANCE_EVENT));
  };
  return [unit, set];
}

/** Reads and writes one string in localStorage, which may be missing or blocked: then it just remembers in memory. */
export function useStored(key: string, initial: string): [string, (v: string) => void] {
  const [value, setValue] = useState(() => {
    try {
      return localStorage.getItem(key) ?? initial;
    } catch {
      return initial;
    }
  });
  const set = (v: string) => {
    setValue(v);
    try {
      localStorage.setItem(key, v);
    } catch {
      // storage unavailable: the choice lasts until the page closes
    }
  };
  return [value, set];
}
