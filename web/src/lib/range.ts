export type Range = {
  from: number
  to: number
  bucket: 'day' | 'week' | 'month'
  tzOffsetMinutes: number
  label: string
}

export type PresetKey =
  | '7d'
  | '30d'
  | '90d'
  | 'year'
  | 'all'
  | 'thisWeek'
  | 'thisMonth'
  | 'thisYear'

/** Auto-select bucket based on span in seconds */
function autoBucket(spanSec: number): 'day' | 'week' | 'month' {
  const spanDays = spanSec / 86400
  if (spanDays <= 45) return 'day'
  if (spanDays <= 550) return 'week'
  return 'month'
}

/** Local midnight (00:00:00) for the given Date */
function localMidnight(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

/**
 * presetRange computes a Range for the given preset key.
 * All from/to are unix seconds computed using the browser's LOCAL timezone.
 * tzOffsetMinutes = -now.getTimezoneOffset() (conventional sign).
 */
export function presetRange(key: PresetKey, now = new Date()): Range {
  const nowSec = Math.floor(now.getTime() / 1000)
  const tz = -now.getTimezoneOffset()

  switch (key) {
    case '7d': {
      const from = nowSec - 7 * 86400
      return { from, to: nowSec, bucket: autoBucket(7 * 86400), tzOffsetMinutes: tz, label: 'Last 7 days' }
    }
    case '30d': {
      const from = nowSec - 30 * 86400
      return { from, to: nowSec, bucket: autoBucket(30 * 86400), tzOffsetMinutes: tz, label: 'Last 30 days' }
    }
    case '90d': {
      const from = nowSec - 90 * 86400
      return { from, to: nowSec, bucket: autoBucket(90 * 86400), tzOffsetMinutes: tz, label: 'Last 90 days' }
    }
    case 'year': {
      const from = nowSec - 365 * 86400
      return { from, to: nowSec, bucket: autoBucket(365 * 86400), tzOffsetMinutes: tz, label: 'Last year' }
    }
    case 'all': {
      // Very large span → month
      return { from: 0, to: nowSec, bucket: 'month', tzOffsetMinutes: tz, label: 'All time' }
    }
    case 'thisWeek': {
      // Monday of the current local week
      const day = now.getDay() // 0=Sun…6=Sat
      const daysSinceMonday = (day + 6) % 7
      const monday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - daysSinceMonday)
      const from = Math.floor(monday.getTime() / 1000)
      const span = nowSec - from
      return { from, to: nowSec, bucket: autoBucket(span), tzOffsetMinutes: tz, label: 'This week' }
    }
    case 'thisMonth': {
      const first = new Date(now.getFullYear(), now.getMonth(), 1)
      const from = Math.floor(first.getTime() / 1000)
      const span = nowSec - from
      return { from, to: nowSec, bucket: autoBucket(span), tzOffsetMinutes: tz, label: 'This month' }
    }
    case 'thisYear': {
      const jan1 = new Date(now.getFullYear(), 0, 1)
      const from = Math.floor(jan1.getTime() / 1000)
      const span = nowSec - from
      return { from, to: nowSec, bucket: autoBucket(span), tzOffsetMinutes: tz, label: 'This year' }
    }
  }
}

/**
 * customRange builds a Range for an explicit [startDate, endDate] (inclusive end day).
 * from = startOfDay(startDate) in local time
 * to   = startOfDay(endDate) + 86400 (exclusive, so end day is fully included)
 */
export function customRange(startDate: Date, endDate: Date): Range {
  const from = Math.floor(localMidnight(startDate).getTime() / 1000)
  const to = Math.floor(localMidnight(endDate).getTime() / 1000) + 86400
  const span = to - from
  const tz = -startDate.getTimezoneOffset()
  const label = `${formatDate(startDate)} – ${formatDate(endDate)}`
  return { from, to, bucket: autoBucket(span), tzOffsetMinutes: tz, label }
}

function formatDate(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/**
 * msToHuman converts milliseconds into a human-readable "Xh Ym" string.
 * Under an hour it returns "Ym"; zero returns "0m".
 */
export function msToHuman(ms: number): string {
  const totalMin = Math.floor(ms / 60_000)
  if (totalMin === 0) return '0m'
  const h = Math.floor(totalMin / 60)
  const m = totalMin % 60
  if (h === 0) return `${m}m`
  if (m === 0) return `${h}h`
  return `${h}h ${m}m`
}
