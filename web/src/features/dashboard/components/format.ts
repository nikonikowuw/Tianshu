export function formatCount(value: number, locale: string): string {
  if (!Number.isFinite(value)) return '0'
  return new Intl.NumberFormat(locale).format(value)
}

export function formatStorageBytes(bytes: number, locale: string): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let idx = 0
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024
    idx += 1
  }
  const formatter = new Intl.NumberFormat(locale, {
    maximumFractionDigits:
      idx === 0 ? 0 : value >= 100 ? 0 : value >= 10 ? 1 : 2,
  })
  return `${formatter.format(value)} ${units[idx]}`
}

export function formatShortUtcDate(dateStr: string, locale: string): string {
  const parsed = new Date(`${dateStr}T00:00:00Z`)
  if (Number.isNaN(parsed.getTime())) {
    return dateStr
  }
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  }).format(parsed)
}

export function formatActivityTimestamp(iso: string, locale: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) {
    return iso
  }
  return new Intl.DateTimeFormat(locale, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(parsed)
}
