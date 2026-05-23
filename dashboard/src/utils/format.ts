/**
 * Shared formatting utilities.
 * Extracted from LiveView, CompletedList, and CompletedRun to eliminate duplication.
 */

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

function abbreviateRelativeUnits(value: string): string {
  return value
    .replace(/\bseconds?\b/g, 'sec')
    .replace(/\bminutes?\b/g, 'min');
}

/** Format a UTC timestamp as a human-readable relative time string, e.g. "3 minutes ago". */
export function relativeTime(ts: string): string {
  if (!ts) return '–';
  const diffMs = Date.now() - new Date(ts).getTime();
  const s = Math.round(diffMs / 1000);
  if (Math.abs(s) < 60)   return abbreviateRelativeUnits(rtf.format(-s, 'seconds'));
  const m = Math.round(s / 60);
  if (Math.abs(m) < 60)   return abbreviateRelativeUnits(rtf.format(-m, 'minutes'));
  const h = Math.round(m / 60);
  if (Math.abs(h) < 24)   return rtf.format(-h, 'hours');
  return rtf.format(-Math.round(h / 24), 'days');
}

/** Format a task duration between a created timestamp and an optional finished timestamp. */
export function duration(
  created: string,
  finished: { String: string; Valid: boolean } | null | undefined
): string {
  if (!finished?.Valid || !finished.String) return '–';
  const ms = new Date(finished.String).getTime() - new Date(created).getTime();
  if (ms < 0) return '–';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

/** Unwrap a nullable SQL string field, returning '–' when absent. */
export function nullStr(f: { String: string; Valid: boolean } | null | undefined): string {
  if (!f) return '–';
  return f.Valid && f.String ? f.String : '–';
}

/** Map a task status string to a neon CSS color variable. */
export function taskOutcomeColor(status: string): string {
  if (status === 'completed') return 'var(--color-neon-green)';
  if (status === 'failed')    return 'var(--color-neon-magenta)';
  return 'var(--color-text-muted)';
}

/** Truncate a string to 64 characters, appending '…' if cut. */
export function truncate64(str: string): string {
  return str.length > 64 ? str.slice(0, 64) + '…' : str;
}
