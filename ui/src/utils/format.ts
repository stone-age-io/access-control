import { format, formatDistanceToNow } from 'date-fns'

/**
 * Format a date to a local string.
 * @param date - Date string or Date object
 * @param formatStr - date-fns format string (default: 'PPpp')
 */
export function formatDate(date: string | Date, formatStr = 'PPpp'): string {
  try {
    if (!date) return '-'
    const d = typeof date === 'string' ? new Date(date) : date
    if (isNaN(d.getTime())) return '-'
    return format(d, formatStr)
  } catch {
    return 'Invalid date'
  }
}

/**
 * Format a date to relative time (e.g. "2 hours ago").
 */
export function formatRelativeTime(date: string | Date): string {
  try {
    if (!date) return '-'
    const d = typeof date === 'string' ? new Date(date) : date
    if (isNaN(d.getTime())) return '-'
    return formatDistanceToNow(d, { addSuffix: true })
  } catch {
    return 'Invalid date'
  }
}

/** Truncate a string with an ellipsis. */
export function truncate(str: string, length: number): string {
  if (!str) return ''
  if (str.length <= length) return str
  return str.substring(0, length) + '...'
}

/**
 * Convert a datetime-local input value ("YYYY-MM-DDTHH:MM", local) to a UTC ISO
 * string for PocketBase filters/fields. '' (or invalid) → ''.
 */
export function localInputToISO(local: string): string {
  if (!local) return ''
  const d = new Date(local)
  return isNaN(d.getTime()) ? '' : d.toISOString()
}

/**
 * Convert an ISO/date value to a datetime-local input value ("YYYY-MM-DDTHH:MM",
 * local). '' (or invalid) → ''. Inverse of localInputToISO.
 */
export function isoToLocalInput(iso: string | Date): string {
  if (!iso) return ''
  const d = typeof iso === 'string' ? new Date(iso) : iso
  if (isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/**
 * Title-case a snake_case constant. Example: 'allow_grant' -> 'Allow Grant'.
 */
export function formatConstant(str: string): string {
  if (!str) return ''
  return str
    .split('_')
    .map(word => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
    .join(' ')
}
