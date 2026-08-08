export function toDate(iso: string | null | undefined): Date | null {
  if (!iso) return null;
  const d = new Date(iso.replace(' ', 'T'));
  return isNaN(d.getTime()) ? null : d;
}

export function formatTime(iso: string | null | undefined): string {
  const d = toDate(iso);
  if (!d) return '—';
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function formatTimeShort(iso: string | null | undefined): string {
  const d = toDate(iso);
  if (!d) return '—';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

export function timeAgo(iso: string | null | undefined): string {
  const d = toDate(iso);
  if (!d) return '—';
  const diff = Math.floor((Date.now() - d.getTime()) / 1000);
  if (diff < 60) return `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  return `${Math.floor(diff / 86400)}d`;
}

export function getHour(iso: string | null | undefined): string {
  const d = toDate(iso);
  return d ? String(d.getHours()).padStart(2, '0') : '00';
}

export function todayStr(): string {
  const d = new Date();
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  return `${d.getFullYear()}-${mm}-${dd}`;
}

export function intervalLabel(seconds: number): string {
  return seconds >= 60 ? `${Math.round(seconds / 60)}m` : `${seconds}s`;
}

export function dayOf(iso: string | null | undefined): string {
  const d = toDate(iso);
  return d ? `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` : '';
}
