// Alerts: what needs attention, the rules that raise them, and the summaries of a day or week. The server decides everything; this says it in words.
import type { components } from './api-types';
import { formatDuration, formatKwh, formatMoney } from './lib';

export type Alert = components['schemas']['Alert'];
export type AlertsAnswer = components['schemas']['Alerts'];
export type AlertRule = components['schemas']['AlertRule'];
export type AlertParams = components['schemas']['AlertParams'];
export type AlertWatch = components['schemas']['AlertWatch'];
export type AlertPattern = components['schemas']['AlertPattern'];
export type AlertRuleChange = components['schemas']['AlertRuleChange'];
export type Digest = components['schemas']['Digest'];
export type DigestBody = components['schemas']['DigestBody'];

export type Severity = 'info' | 'warn' | 'urgent';

export const SEVERITIES: { value: Severity; label: string }[] = [
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warning' },
  { value: 'urgent', label: 'Urgent' },
];

export function severityRank(s: string): number {
  return s === 'urgent' ? 3 : s === 'warn' ? 2 : 1;
}

/** How long ago, in the words the dashboard uses: "5 min ago", "3 h 10 min ago". */
export function ago(iso: string, now: Date): string {
  const seconds = (now.getTime() - Date.parse(iso)) / 1000;
  return seconds < 60 ? 'just now' : `${formatDuration(seconds)} ago`;
}

/** The meter kinds of rule the owner can make, in the words they are chosen by. */
export const RULE_KINDS = [
  { value: 'pattern', label: 'Usual pattern', help: 'Learns how this meter normally behaves and says when it does not.' },
  { value: 'level', label: 'Power level', help: 'Draws above or below a level for a while.' },
  { value: 'quiet', label: 'Not working', help: 'Has not worked for a stretch of time.' },
  { value: 'long', label: 'Working too long', help: 'Has been working for longer than it should.' },
] as const;
export type RuleKind = (typeof RULE_KINDS)[number]['value'];

export const WATCHES: { key: 'overdue' | 'too_often' | 'too_long' | 'rest'; label: string }[] = [
  { key: 'overdue', label: 'Has not run for longer than usual' },
  { key: 'too_often', label: 'Runs more often than usual' },
  { key: 'too_long', label: 'Runs for longer than usual' },
  { key: 'rest', label: 'Draws a different amount between runs' },
];

function watts(w: number): string {
  return `${Number.isInteger(w) ? w : w.toFixed(1)} W`;
}

function hours(h: number): string {
  return h < 1 ? `${Math.round(h * 60)} min` : h < 48 ? `${Number.isInteger(h) ? h : h.toFixed(1)} h` : `${(h / 24).toFixed(1)} days`;
}

/** One line saying what one of the owner's rules does. */
export function ruleSummary(r: Pick<AlertRule, 'kind' | 'params'>): string {
  const p = r.params;
  switch (r.kind) {
    case 'level':
      return `${p.op === 'below' ? 'Below' : 'Above'} ${watts(p.watts ?? 0)} for ${formatDuration((p.minutes ?? 0) * 60)}`;
    case 'quiet':
      return `Nothing above ${watts(p.watts ?? 0)} for ${hours(p.hours ?? 0)}`;
    case 'long':
      return `Working above ${watts(p.watts ?? 0)} for more than ${formatDuration((p.minutes ?? 0) * 60)}`;
    case 'pattern': {
      const w = p.watch ?? {};
      const what = WATCHES.filter((x) => w[x.key]).map((x) => x.label.charAt(0).toLowerCase() + x.label.slice(1));
      return what.length > 0 ? `Usual pattern: ${what.join('; ')}` : 'Usual pattern';
    }
  }
  return '';
}

/** What a meter's learned pattern is, as short lines for the rule editor. Empty lines say why there is none. */
export function patternLines(p: AlertPattern): string[] {
  if (!p.ok) {
    if (p.why === 'no_spikes') return ['This meter does not rise clearly above its resting level, so there is no pattern to watch.'];
    if (p.why === 'too_few_runs') return [`It has worked ${p.runs} time${p.runs === 1 ? '' : 's'} in the days looked at. A pattern needs at least 4.`];
    return ['Not enough recorded history to learn a pattern.'];
  }
  const lines = [
    `Rests at about ${watts(Math.round(p.rest_watts * 10) / 10)}. Counts as working above ${watts(Math.round(p.level_watts))}${p.suggested ? ' (picked from its own readings)' : ''}.`,
    `${p.runs} runs in ${Math.round(p.days)} days: about every ${hours(p.gap_median_hours)}, the longest wait ${hours(p.gap_max_hours)}.`,
    `Each run lasts about ${p.run_median_minutes} min (up to ${p.run_max_minutes}) and reaches about ${watts(Math.round(p.peak_median_watts))}. The most in one day was ${p.most_per_day}.`,
  ];
  lines.push(`A wait longer than ${hours(p.overdue_after_hours)} would raise an alert.`);
  if (p.hours_since_last_run != null) lines.push(`It last ran ${hours(p.hours_since_last_run)} ago.`);
  return lines;
}

/** The sentence for the banner: the worst alert, and how many more there are. */
export function bannerHeadline(banner: Alert[]): { first: Alert | undefined; more: number } {
  const sorted = [...banner].sort((a, b) => severityRank(b.severity) - severityRank(a.severity) || Date.parse(b.opened_at) - Date.parse(a.opened_at));
  return { first: sorted[0], more: Math.max(0, sorted.length - 1) };
}

/** "2026-11-02" and "2026-11-08" as "Nov 2 to Nov 8" (days of the same week) or "Nov 8" (a day). */
export function periodLabel(from: string, to: string): string {
  const fmt = (d: string) => {
    const [y, m, day] = d.split('-').map(Number);
    return new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, day ?? 1)).toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' });
  };
  return from === to ? fmt(from) : `${fmt(from)} to ${fmt(to)}`;
}

export function digestTitle(d: Pick<Digest, 'kind' | 'from' | 'to'>): string {
  return d.kind === 'weekly' ? `Week of ${periodLabel(d.from, d.to)}` : periodLabel(d.from, d.to);
}

/** The energy against the period before: "+3.2% on the week before". */
export function changeText(b: DigestBody): string | undefined {
  if (b.change_share == null) return undefined;
  const pct = b.change_share * 100;
  const before = b.kind === 'weekly' ? 'the week before' : 'the same day last week';
  if (Math.abs(pct) < 0.5) return `About the same as ${before}`;
  return `${pct > 0 ? '+' : ''}${pct.toFixed(Math.abs(pct) < 10 ? 1 : 0)}% on ${before}`;
}

/** Lines about the rest of a summary: what was not recorded, the always-on power, unusual days, alerts. */
export function digestNotes(b: DigestBody): string[] {
  const out: string[] = [];
  if (b.coverage != null && b.coverage < 0.95) out.push(`The house was recorded for ${Math.round(b.coverage * 100)}% of the period.`);
  const o = b.outages;
  const parts = [
    o.power_seconds > 0 ? `power out ${formatDuration(o.power_seconds)}` : '',
    o.server_seconds > 0 ? `server off ${formatDuration(o.server_seconds)}` : '',
    o.unknown_seconds > 0 ? `not recorded ${formatDuration(o.unknown_seconds)}` : '',
  ].filter(Boolean);
  if (parts.length > 0) out.push(`Outages: ${parts.join(', ')}.`);
  if (b.base_watts != null) {
    const was = b.base_prev_watts != null ? ` (${watts(Math.round(b.base_prev_watts))} the period before)` : '';
    out.push(`Always-on power ${watts(Math.round(b.base_watts))}${was}.`);
  }
  if (b.unusual_days > 0) out.push(`${b.unusual_days} unusual day${b.unusual_days === 1 ? '' : 's'}.`);
  const a = b.alerts;
  const total = a.info + a.warn + a.urgent;
  if (total > 0) out.push(`${total} alert${total === 1 ? '' : 's'} raised${a.urgent > 0 ? `, ${a.urgent} urgent` : ''}.`);
  return out;
}

/** How a meter in a summary compares with the period before: "+1.2 kWh". */
export function changeKwh(m: { change_kwh: number }): string {
  return `${m.change_kwh > 0 ? '+' : ''}${formatKwh(m.change_kwh)}`;
}

export function costText(cost: string | null | undefined, estimated = false): string {
  return cost == null ? '' : formatMoney(cost, estimated);
}
