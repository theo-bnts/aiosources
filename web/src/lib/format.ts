import { useMemo } from 'react'
import type { RunMode, RunStatus } from './types'

const integers = new Intl.NumberFormat('en-GB')
export const modeLabels: Record<RunMode, string> = {
  preview: 'Preview',
  incremental: 'Update',
  full: 'Rebuild source catalog',
  metadata: 'Fetch source details',
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
  return `${new Intl.NumberFormat('en-GB', { maximumFractionDigits: 2 }).format(n / 1024 ** power)} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][power]}`
}
