import { useState, type ComponentProps } from 'react'
import { useBrowserPreferences } from '@/lib/browser-preferences'
import { dateTimeToISO, localDateTime } from '@/lib/format'
import { Input } from '@/components/ui/input'

type Precision = 'minutes' | 'seconds' | 'milliseconds'
type Props = Omit<
  ComponentProps<typeof Input>,
  'type' | 'value' | 'defaultValue' | 'onChange' | 'step'
> & {
  value?: string | null
  defaultValue?: string | null
  onValueChange?: (value: string) => void
  precision?: Precision
}

// Keep an instant, not an unzoned wall-clock string. A browser preference change
// can then reformat an open field without changing what its form will save.
export function DateTimeInput({
  value,
  defaultValue,
  onValueChange,
  precision = 'minutes',
  ...props
}: Props) {
  useBrowserPreferences()
  const [local, setLocal] = useState(defaultValue ?? '')
  const instant = value === undefined ? local : value
  return (
    <Input
      {...props}
      type="datetime-local"
      min="0001-01-01T00:00"
      max="9999-12-31T23:59:59.999"
      step={precision === 'milliseconds' ? '0.001' : precision === 'seconds' ? '1' : '60'}
      value={localDateTime(instant, precision)}
      onChange={(event) => {
        const next = dateTimeToISO(event.target.value, instant, precision) ?? ''
        if (value === undefined) setLocal(next)
        onValueChange?.(next)
      }}
    />
  )
}

export function useDateTimeZoneLabel() {
  return useBrowserPreferences().timeZone === 'utc' ? 'UTC' : 'browser local time'
}
