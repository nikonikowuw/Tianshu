/**
 * Date-only helpers for the audit range filter.
 *
 * The URL keeps calendar dates (`YYYY-MM-DD`) so a shared link stays readable.
 * Requests convert those dates to RFC3339 boundaries in the viewer's local
 * timezone: the selected day is the day the operator sees in the table.
 */
const DATE_ONLY_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/

export function parseDateOnly(value: string | undefined): Date | null {
  if (!value) return null
  const match = DATE_ONLY_PATTERN.exec(value)
  if (!match) return null

  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(year, month - 1, day)
  // `new Date(0..99, ...)` maps to 1900..1999; keep four-digit years exact.
  if (year < 100) date.setFullYear(year)

  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return null
  }
  return date
}

export function isValidDateOnly(value: string): boolean {
  return parseDateOnly(value) !== null
}

export function formatDateOnly(date: Date): string {
  const year = String(date.getFullYear()).padStart(4, '0')
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function toBoundaryRFC3339(
  value: string,
  endOfDay: boolean
): string | null {
  const date = parseDateOnly(value)
  if (!date) return null
  if (endOfDay) {
    date.setHours(23, 59, 59, 999)
  } else {
    date.setHours(0, 0, 0, 0)
  }
  return date.toISOString()
}

export type NormalizedDateRange = {
  from?: string
  to?: string
  reversed: boolean
}

/**
 * Repair a reversed URL range instead of silently swapping only the request:
 * callers can write the normalized values back to the URL so the badge, URL and
 * API request all describe the same interval.
 */
export function normalizeDateRange(
  from?: string,
  to?: string
): NormalizedDateRange {
  if (from && to && from > to) {
    return { from: to, to: from, reversed: true }
  }
  return { from, to, reversed: false }
}
