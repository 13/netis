function pad(n: number): string {
  return String(n).padStart(2, "0");
}

// The backend serializes naive UTC datetimes without a 'Z' suffix.
// Append 'Z' so JavaScript treats them as UTC instead of local time.
function toUtc(iso: string): Date {
  return new Date(iso.endsWith("Z") ? iso : iso + "Z");
}

// A host counts as "online" if it was observed within this window.
export const ONLINE_WINDOW_MS = 10 * 60 * 1000;

export const MIN_PASSWORD_LENGTH = 5;

export function isOnline(iso: string | null | undefined): boolean {
  if (!iso) return false;
  return Date.now() - toUtc(iso).getTime() < ONLINE_WINDOW_MS;
}

export function relativeTime(iso: string | null | undefined): string {
  if (!iso) return "never";
  const diff = Date.now() - toUtc(iso).getTime();
  const sec = Math.floor(diff / 1000);
  if (sec < 45) return "just now";
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const days = Math.floor(hr / 24);
  if (days < 30) return `${days}d ago`;
  return fmtDate(iso);
}

export function fmtDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = toUtc(iso);
  return `${pad(d.getUTCDate())}.${pad(d.getUTCMonth() + 1)}.${d.getUTCFullYear()}`;
}

export function fmtDateTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = toUtc(iso);
  return `${pad(d.getUTCDate())}.${pad(d.getUTCMonth() + 1)}.${d.getUTCFullYear()} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}
