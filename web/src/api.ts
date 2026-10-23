// The house API's client. Paths are relative, so the page works wherever the server serves it from.
import type { Channel, Computer, ComputerDetail, Device, Live, NewDevice, RateConfig, Series, Status, Summary } from './lib';

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
  if (res.status === 204) return undefined as T;
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
  devices: () => request<{ devices: Device[] }>('api/v1/devices').then((r) => r.devices),
  addDevice: (nd: NewDevice) =>
    request<Device>('api/v1/devices', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(nd),
    }),
  removeDevice: (id: string) => request<void>(`api/v1/devices/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  computers: () => request<{ computers: Computer[] }>('api/v1/computers').then((r) => r.computers),
  computerDetail: (id: string, from: string, to: string) =>
    request<ComputerDetail>(`api/v1/computers/${encodeURIComponent(id)}/detail?${q({ from, to })}`),
  addComputer: (name: string) =>
    request<Computer>('api/v1/computers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  updateComputer: (id: string, body: { name?: string; source_id?: string | null }) =>
    request<Computer>(`api/v1/computers/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  removeComputer: (id: string) => request<void>(`api/v1/computers/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  rates: () => request<RateConfig>('api/v1/rates'),
  setRates: (cfg: RateConfig) =>
    request<RateConfig>('api/v1/rates', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg),
    }),
  updateChannel: (id: string, body: { label?: string; enabled?: boolean; computer_id?: string | null }) =>
    request<Channel>(`api/v1/channels/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
};
