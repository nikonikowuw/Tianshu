export function formatBytes(bytes: number, locale: string): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B'
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

export function formatMimeBadge(mimeType: string): string {
  const lower = mimeType.toLowerCase()
  switch (lower) {
    case 'image/jpeg':
      return 'JPEG'
    case 'image/png':
      return 'PNG'
    case 'image/webp':
      return 'WEBP'
    case 'image/gif':
      return 'GIF'
    case 'application/pdf':
      return 'PDF'
    case 'text/plain':
      return 'TXT'
    case 'text/csv':
      return 'CSV'
    case 'application/vnd.openxmlformats-officedocument.wordprocessingml.document':
      return 'DOCX'
    case 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet':
      return 'XLSX'
    case 'application/vnd.openxmlformats-officedocument.presentationml.presentation':
      return 'PPTX'
    default: {
      const parts = lower.split('/')
      return (parts[1] || lower).toUpperCase().slice(0, 10)
    }
  }
}

export function formatFileTimestamp(iso: string, locale: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) {
    return iso
  }
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(parsed)
}
