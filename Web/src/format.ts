// Display formatting shared by the pages. The rules follow Status.duration and Cost.dollars in BeltlineCore;
// the browser never recomputes spend, estimates or partial-cost flags, it only formats what the daemon sent.

export const emDash = "—";
export const costNotTracked = "cost not tracked";

export interface FormatLocale {
  /** Defaults to the browser's locale. Tests pass a fixed one. */
  locale?: string | string[];
  /** Defaults to the browser's time zone. */
  timeZone?: string;
}

type DateInput = string | number | Date | null | undefined;

function toDate(value: DateInput): Date | null {
  if (value === null || value === undefined || value === "") return null;
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatDateTime(value: DateInput, options: FormatLocale & Intl.DateTimeFormatOptions = {}): string {
  const date = toDate(value);
  if (!date) return emDash;
  const { locale, timeZone, ...format } = options;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone, ...format }).format(date);
}

export function formatDate(value: DateInput, options: FormatLocale & Intl.DateTimeFormatOptions = {}): string {
  const date = toDate(value);
  if (!date) return emDash;
  const { locale, timeZone, ...format } = options;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone, ...format }).format(date);
}

export function formatTime(value: DateInput, options: FormatLocale & Intl.DateTimeFormatOptions = {}): string {
  const date = toDate(value);
  if (!date) return emDash;
  const { locale, timeZone, ...format } = options;
  return new Intl.DateTimeFormat(locale, { timeStyle: "short", timeZone, ...format }).format(date);
}

/**
 * Seconds as Status.duration shows them: rounded to whole seconds, negative values clamped to zero,
 * `42s` below a minute, `3m 5s` below an hour, then `2h 10m`.
 */
export function formatDuration(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds)) return emDash;
  const total = Math.max(0, Math.round(seconds));
  const minutes = Math.floor(total / 60);
  if (minutes >= 60) return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
  return minutes > 0 ? `${minutes}m ${total % 60}s` : `${total}s`;
}

/**
 * USD nanodollars as Cost.dollars shows them: two decimal places, or four for a positive amount under
 * one cent. A missing amount is "cost not tracked", not zero. A nonfinite amount is an em dash.
 */
export function formatMoney(nanos: number | null | undefined): string {
  if (nanos === null || nanos === undefined) return costNotTracked;
  if (!Number.isFinite(nanos)) return emDash;
  const places = nanos > 0 && nanos < 10_000_000 ? 4 : 2;
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: places,
    maximumFractionDigits: places
  }).format(nanos / 1_000_000_000);
}

/** A context size as ContextSize.label writes it: `250k`, `1M`, `1.048576M`. */
export function formatContextTokens(tokens: number): string {
  const scaled = (divisor: number, suffix: string, width: number) => {
    const whole = Math.floor(tokens / divisor);
    const remainder = tokens % divisor;
    if (remainder === 0) return `${whole}${suffix}`;
    const digits = String(remainder).padStart(width, "0").replace(/0+$/, "");
    return `${whole}.${digits}${suffix}`;
  };
  if (tokens >= 1_000_000) return scaled(1_000_000, "M", 6);
  if (tokens >= 1_000) return scaled(1_000, "k", 3);
  return String(tokens);
}

/** How long ago a moment was, as Format.ago writes it: `just now`, `5 min ago`, `3 h ago`, `2 d ago`. */
export function formatAgo(value: DateInput, now: Date = new Date()): string {
  const date = toDate(value);
  if (!date) return emDash;
  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours} h ago` : `${Math.floor(hours / 24)} d ago`;
}
