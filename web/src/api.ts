// The house API's client. Paths are relative, so the page works wherever the server serves it from.
import type { ApplianceGroup, CarbonConfig, Meter, Computer, ComputerDetail, ComputerEstimate, ComputerHistory, HouseHistory, Device, GroupDetail, Live, NewDevice, OpenEiStatus, PcLive, PcSeries, RateConfig, Series, Status, Summary, TariffCandidate, TariffInfo } from './lib';
import type { BaseLoad } from './baseload';
import type { MeterRuns } from './runs';

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
  meters: () => request<{ meters: Meter[] }>('api/v1/meters').then((r) => r.meters),
  live: () => request<Live>('api/v1/live'),
  series: (meterId: string, from: string, to: string, bucket?: string) =>
    request<Series>(`api/v1/series?${q({ meter_id: meterId, from, to, bucket })}`),
  summary: (from: string, to: string) => request<Summary>(`api/v1/summary?${q({ from, to })}`),
  /** The power a meter (or the house) hardly ever goes below; undefined when under an hour was recorded. */
  meterBaseLoad: (meterId: string, from: string, to: string) => request<BaseLoad | undefined>(`api/v1/meters/${encodeURIComponent(meterId)}/baseload?${q({ from, to })}`),
  houseBaseLoad: (from: string, to: string) => request<BaseLoad | undefined>(`api/v1/baseload?${q({ from, to })}`),
  /** The times a meter was working over a period (a car charging, a dryer drying). */
  meterRuns: (meterId: string, from: string, to: string) => request<MeterRuns>(`api/v1/meters/${encodeURIComponent(meterId)}/runs?${q({ from, to })}`),
  devices: () => request<{ devices: Device[] }>('api/v1/devices').then((r) => r.devices),
  addDevice: (nd: NewDevice) =>
    request<Device>('api/v1/devices', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(nd),
    }),
  updateDevice: (id: string, body: { name?: string; host?: string }) =>
    request<Device>(`api/v1/devices/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  removeDevice: (id: string) => request<void>(`api/v1/devices/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  groups: () => request<{ groups: ApplianceGroup[] }>('api/v1/groups').then((r) => r.groups),
  groupDetail: (id: string, from: string, to: string) =>
    request<GroupDetail>(`api/v1/groups/${encodeURIComponent(id)}/detail?${q({ from, to })}`),
  addGroup: (name: string) =>
    request<ApplianceGroup>('api/v1/groups', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  renameGroup: (id: string, name: string) =>
    request<ApplianceGroup>(`api/v1/groups/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  removeGroup: (id: string) => request<void>(`api/v1/groups/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  computers: () => request<{ computers: Computer[] }>('api/v1/computers').then((r) => r.computers),
  computerDetail: (id: string, from: string, to: string) =>
    request<ComputerDetail>(`api/v1/computers/${encodeURIComponent(id)}/detail?${q({ from, to })}`),
  /** The PC's latest look, small enough to ask for every few seconds; undefined when there is none yet. */
  computerLive: (id: string) => request<PcLive | undefined>(`api/v1/computers/${encodeURIComponent(id)}/live`),
  /** Today's price and what the computer really used per day over a window; `plugs` are the meter ids of the plugs to add. */
  computerEstimate: (id: string, window: string, plugs: string[], includePc: boolean) =>
    request<ComputerEstimate>(
      `api/v1/computers/${encodeURIComponent(id)}/estimate?${q({ window, peripherals: plugs.join(',') || undefined, include_pc: includePc ? undefined : 'false' })}`,
    ),
  /** The computer's energy over the rolling periods, hour to all time. */
  computerHistory: (id: string) => request<ComputerHistory>(`api/v1/computers/${encodeURIComponent(id)}/history`),
  /** Where the daily CSV for a period can be downloaded (a plain link: the browser saves it). */
  computerExportUrl: (id: string, from: string, to: string) => `api/v1/computers/${encodeURIComponent(id)}/export.csv?${q({ from, to })}`,
  /** What each part of the PC drew over a period, with the gaps to shade; undefined without a collector. */
  computerSeries: (id: string, from: string, to: string) =>
    request<PcSeries | undefined>(`api/v1/computers/${encodeURIComponent(id)}/series?${q({ from, to })}`),
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
  carbon: () => request<CarbonConfig>('api/v1/carbon'),
  /** Saves the source, place and (when given) a new key; the server takes a reading first and only keeps a setup that got one. */
  setCarbon: (body: { place: string; key?: string }) =>
    request<CarbonConfig>('api/v1/carbon', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  removeCarbon: () => request<void>('api/v1/carbon', { method: 'DELETE' }),
  houseHistory: () => request<HouseHistory>('api/v1/history'),
  houseEstimate: (window: string) => request<ComputerEstimate>(`api/v1/estimate?${q({ window })}`),
  houseExportUrl: (from: string, to: string) => `api/v1/export.csv?${q({ from, to })}`,
  openEi: () => request<OpenEiStatus>('api/v1/openei'),
  setOpenEiKey: (apiKey: string) =>
    request<OpenEiStatus>('api/v1/openei', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ api_key: apiKey }),
    }),
  lookupRates: (address: string) => request<{ candidates: TariffCandidate[] }>(`api/v1/rates/lookup?${q({ address })}`).then((r) => r.candidates),
  tariff: () => request<TariffInfo | undefined>('api/v1/rates/tariff'),
  setRates: (cfg: RateConfig) =>
    request<RateConfig>('api/v1/rates', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg),
    }),
  updateMeter: (id: string, body: { label?: string; enabled?: boolean; computer_id?: string | null; group_id?: string | null; invert?: boolean; pair_with?: string | null; favorite?: boolean; type?: 'house' | 'sub_panel' | 'circuit'; parent_id?: string | null }) =>
    request<Meter>(`api/v1/meters/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
};
