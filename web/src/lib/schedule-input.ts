export type TimingMode = 'manual' | 'every' | 'daily' | 'cron'

export const intervalChoices = [
  ['30m', '30 minutes'],
  ['1h', '1 hour'],
  ['6h', '6 hours'],
  ['12h', '12 hours'],
  ['24h', '24 hours'],
  ['168h', '1 week'],
] as const

// Recognize only an ordinary daily expression. Never normalize the stored text
// merely because its controls were opened; custom cron/duration values stay exact.
export function dailyTime(cron: string): string | undefined {
  const match = /^\s*(\d{1,2})\s+(\d{1,2})\s+\*\s+\*\s+\*\s*$/.exec(cron)
  if (!match || Number(match[1]) > 59 || Number(match[2]) > 23) return undefined
  return `${match[2].padStart(2, '0')}:${match[1].padStart(2, '0')}`
}

export function dailyCron(time: string): string {
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(time)) return ''
  const [hour, minute] = time.split(':').map(Number)
  return `${minute} ${hour} * * *`
}

export function timingMode(every?: string, cron?: string): TimingMode {
  return cron ? (dailyTime(cron) ? 'daily' : 'cron') : every ? 'every' : 'manual'
}

export function intervalLabel(value: string): string {
  return intervalChoices.find(([duration]) => duration === value)?.[1] ?? value
}

export function frequencyLabel(every?: string, cron?: string): string {
  if (cron) {
    const time = dailyTime(cron)
    return time ? `Daily at ${time}` : `Custom schedule · ${cron}`
  }
  return every ? `Every ${intervalLabel(every)}` : 'Manual only'
}
