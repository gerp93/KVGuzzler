// The house API's client. Paths are relative, so the page works wherever the server serves it from.
import type { Channel, Live, Series, Status, Summary } from './lib';

/** The contract's major version this dashboard speaks (see contract/openapi.yaml). */
export const SUPPORTED_API_VERSION = 1;

export class ApiError extends Error {}

/** The server speaks a different major version of the contract: one side needs updating. */
export class VersionMismatch extends ApiError {}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  const version = res.headers.get('X-KVG-Api-Version');
  if (version !== null && Number(version) !== SUPPORTED_API_VERSION) {
    throw new VersionMismatch(
      `The server speaks API version ${version} but this dashboard speaks ${SUPPORTED_API_VERSION}. Update the older one.`,
    );
  }
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = (await res.json()) as { message?: string };
      if (body.message) message = body.message;
    } catch {
      // not JSON: keep the status line
    }
    throw new ApiError(message);
  }
  return (await res.json()) as T;
}

const q = (params: Record<string, string | undefined>) => {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined) u.set(k, v);
  return u.toString();
};

export const api = {
  status: () => request<Status>('api/v1/status'),
  channels: () => request<{ channels: Channel[] }>('api/v1/channels').then((r) => r.channels),
  live: () => request<Live>('api/v1/live'),
  series: (channelId: string, from: string, to: string, bucket?: string) =>
    request<Series>(`api/v1/series?${q({ channel_id: channelId, from, to, bucket })}`),
  summary: (from: string, to: string) => request<Summary>(`api/v1/summary?${q({ from, to })}`),
  updateChannel: (id: string, body: { label?: string; enabled?: boolean }) =>
    request<Channel>(`api/v1/channels/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
};
