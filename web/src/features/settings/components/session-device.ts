export type ParsedSessionDevice = {
  deviceType: 'desktop' | 'mobile' | 'tablet' | 'unknown'
  browser: string
  os: string
}

export function parseSessionUserAgent(ua: string): ParsedSessionDevice {
  const raw = ua.trim()
  if (!raw) {
    return { deviceType: 'unknown', browser: '', os: '' }
  }

  let browser = ''
  if (/Edg\//i.test(raw)) {
    browser = 'Edge'
  } else if (/OPR\/|Opera/i.test(raw)) {
    browser = 'Opera'
  } else if (/Chrome\/|CriOS\//i.test(raw) && !/Chromium/i.test(raw)) {
    browser = 'Chrome'
  } else if (/Chromium/i.test(raw)) {
    browser = 'Chromium'
  } else if (/Firefox\/|FxiOS\//i.test(raw)) {
    browser = 'Firefox'
  } else if (/Safari\//i.test(raw) && !/Chrome|Chromium/i.test(raw)) {
    browser = 'Safari'
  } else if (/curl\//i.test(raw)) {
    browser = 'curl'
  }

  let os = ''
  let deviceType: ParsedSessionDevice['deviceType'] = 'desktop'
  if (/iPad|Tablet/i.test(raw)) {
    os = /iPad/i.test(raw) ? 'iPadOS' : 'Android'
    deviceType = 'tablet'
  } else if (/iPhone|iPod/i.test(raw)) {
    os = 'iOS'
    deviceType = 'mobile'
  } else if (/Android/i.test(raw)) {
    os = 'Android'
    deviceType = /Mobile/i.test(raw) ? 'mobile' : 'tablet'
  } else if (/Windows NT/i.test(raw)) {
    os = 'Windows'
  } else if (/Mac OS X|Macintosh/i.test(raw)) {
    os = 'macOS'
  } else if (/Linux/i.test(raw)) {
    os = 'Linux'
  } else if (!browser) {
    deviceType = 'unknown'
  }

  return { deviceType, browser, os }
}

export function formatSessionDeviceLabel(
  parsed: ParsedSessionDevice,
  fallback: string
): string {
  if (parsed.browser && parsed.os) {
    return `${parsed.browser} · ${parsed.os}`
  }
  return parsed.browser || parsed.os || fallback
}

export function formatSettingsTimestamp(iso: string, locale: string): string {
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

export function formatAvatarMaxSize(bytes: number, locale: string): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '2 MB'
  const mb = bytes / (1024 * 1024)
  const formatted = new Intl.NumberFormat(locale, {
    maximumFractionDigits: mb >= 10 || Number.isInteger(mb) ? 0 : 1,
  }).format(mb)
  return `${formatted} MB`
}
