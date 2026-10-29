// Pure display helpers shared by the billing pages.

/** Data sizes are decimal (1 GB = 10^9 bytes), matching how the plans are sold. */
export function formatBytes(bytes: number): string {
  if (bytes >= 1e12) return `${trim(bytes / 1e12)} TB`;
  if (bytes >= 1e9) return `${trim(bytes / 1e9)} GB`;
  if (bytes >= 1e6) return `${trim(bytes / 1e6)} MB`;
  return `${bytes} B`;
}

function trim(n: number): string {
  return n >= 100 ? n.toFixed(0) : n.toFixed(1).replace(/\.0$/, '');
}

export function formatCents(cents: number, currency = 'usd'): string {
  return (cents / 100).toLocaleString(undefined, {
    style: 'currency',
    currency: currency.toUpperCase(),
    minimumFractionDigits: cents % 100 === 0 ? 0 : 2,
  });
}

export function formatDate(iso: string | null): string {
  // Periods are UTC-aligned (a month ends at 00:00 UTC on the 1st), so render
  // in UTC or customers west of Greenwich see the day before.
  return iso ? new Date(iso).toLocaleDateString(undefined, { timeZone: 'UTC' }) : '';
}
