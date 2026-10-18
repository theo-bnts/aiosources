import { useMemo } from 'react'
import type { Run, RunMode, RunStatus } from './types'

const integers = new Intl.NumberFormat('en-GB')
const decimals = new Intl.NumberFormat('en-GB', { maximumFractionDigits: 2 })
const percentages = new Intl.NumberFormat('en-GB', { style: 'percent', maximumFractionDigits: 1 })

// Historical diagnostics can predate the current wording. Format only their
// first letter; preserve identifiers, acronyms, punctuation and raw data.
export function messageText(value: string) {
  return value.replace(
    /^(\s*)(\p{Ll})/u,
    (_, space: string, letter: string) => space + letter.toUpperCase(),
  )
}

export function percentage(value: number, total: number) {
  if (value > 0 && total > 0 && value / total < 0.001) return '<0.1%'
  return percentages.format(total > 0 ? value / total : 0)
}
export const modeLabels: Record<RunMode, string> = {
  preview: 'Preview',
  incremental: 'Update',
  full: 'Rebuild source catalog',
  metadata: 'Fetch source details',
}
export function runModeLabel(run: Pick<Run, 'mode' | 'remote_catalog'>) {
  if (run.remote_catalog && run.mode === 'full') return 'Full sync'
  if (run.remote_catalog && run.mode === 'incremental') return 'Sync changes'
  return modeLabels[run.mode]
}
export const statusLabels: Record<RunStatus, string> = {
  queued: 'Queued',
  running: 'Running',
  paused: 'Paused',
  succeeded: 'Completed',
  failed: 'Failed',
  cancelled: 'Cancelled',
}
export function number(value: number) {
  return integers.format(value)
}
export function useDate() {
  return useMemo(() => {
    const dates = new Intl.DateTimeFormat('en-GB', {
      dateStyle: 'medium',
      timeStyle: 'short',
    })
    return (value?: string) => {
      if (!value) return 'Not provided'
      const parsed = new Date(value)
      return Number.isNaN(parsed.getTime()) ? value : dates.format(parsed)
    }
  }, [])
}
export function bytes(value: unknown) {
  if (value === null || value === undefined || value === '') return 'Not provided'
  const n = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(n) || n < 0) return String(value)
  if (n < 1024) return `${number(n)} B`
  const power = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4)
  return `${decimals.format(n / 1024 ** power)} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][power]}`
}

// datetime-local fields use the same browser timezone as displayed timestamps.
export function localDateTime(value?: string | null, precision: 'minutes' | 'seconds' = 'minutes') {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, precision === 'seconds' ? 19 : 16)
}

export function dateTimeToISO(
  value: string,
  previous?: string | null,
  precision: 'minutes' | 'seconds' = 'minutes',
) {
  if (!value) return null
  // Untouched fields retain fractional seconds and the offset of a repeated DST hour.
  if (previous && localDateTime(previous, precision) === value) return previous
  return new Date(value).toISOString()
}
