/**
 * Duration display, mirroring internal/service/timetracking.go.
 *
 * A day is 8 hours and a week is 5 days. This has to match the server exactly: a
 * "2d" estimate the UI renders as "16h" while the server stored 960 minutes is the
 * kind of disagreement nobody notices until a milestone rollup is argued over.
 */
const MIN_PER_HOUR = 60;
const MIN_PER_DAY = 60 * 8;
const MIN_PER_WEEK = 60 * 8 * 5;

/** formatMinutes picks the largest whole unit that loses nothing: 960 → "2d". */
export function formatMinutes(minutes: number): string {
  if (!minutes || minutes <= 0) return "0m";
  if (minutes % MIN_PER_WEEK === 0) return `${minutes / MIN_PER_WEEK}w`;
  if (minutes % MIN_PER_DAY === 0) return `${minutes / MIN_PER_DAY}d`;
  if (minutes % MIN_PER_HOUR === 0) return `${minutes / MIN_PER_HOUR}h`;
  return `${minutes}m`;
}

/**
 * formatMinutesLong is the same value spelled out, for tooltips and rollups where
 * "2d" is ambiguous unless you already know a day is eight hours.
 */
export function formatMinutesLong(minutes: number): string {
  if (!minutes || minutes <= 0) return "none";
  const hours = minutes / MIN_PER_HOUR;
  if (hours >= 8) return `${round(hours / 8)} day${hours / 8 === 1 ? "" : "s"} (${round(hours)}h)`;
  if (hours >= 1) return `${round(hours)} hour${round(hours) === 1 ? "" : "s"}`;
  return `${minutes} minutes`;
}

const round = (n: number) => Math.round(n * 10) / 10;

/**
 * formatPair renders spent-vs-estimate in one shared unit.
 *
 * Per-value formatting produced "270m of 2d", which makes the reader do the
 * conversion before they can tell whether that is a problem. Hours are the common
 * denominator everyone can compare at a glance; only a sub-hour budget drops to
 * minutes, where hours would round everything to "0.5h of 0.5h".
 */
export function formatPair(spentMinutes: number, estimateMinutes: number): string {
  const to = sameUnit(spentMinutes, estimateMinutes);
  return `${to(spentMinutes)} of ${to(estimateMinutes)}`;
}

/**
 * sameUnit returns a formatter locked to one unit across every value passed in, so
 * two numbers meant to be compared are never rendered in different scales.
 */
export function sameUnit(...values: number[]): (minutes: number) => string {
  const minutesOnly = Math.max(0, ...values) < MIN_PER_HOUR;
  return (m: number) => (minutesOnly ? `${m}m` : `${round(m / MIN_PER_HOUR)}h`);
}

/** Accepts the same shapes the server does, so the form can validate before sending. */
const DURATION_RE = /^(\d+(?:\.\d+)?)\s*([mhdw]?)$/i;

export function parseDuration(input: string): number | null {
  const m = DURATION_RE.exec(input.trim().toLowerCase());
  if (!m) return null;
  const n = Number(m[1]);
  if (!Number.isFinite(n) || n <= 0) return null;
  const per =
    m[2] === "h" ? MIN_PER_HOUR : m[2] === "d" ? MIN_PER_DAY : m[2] === "w" ? MIN_PER_WEEK : 1;
  return Math.round(n * per);
}
