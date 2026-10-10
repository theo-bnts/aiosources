import { useEffect, useId, useState } from 'react'
import { dailyCron, dailyTime, intervalChoices, type TimingMode } from '@/lib/schedule-input'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export function DurationSelect({
  label,
  value,
  onChange,
  emptyLabel = 'Not set',
  required = false,
  choices = intervalChoices,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  emptyLabel?: string
  required?: boolean
  choices?: ReadonlyArray<readonly [string, string]>
}) {
  const id = useId()
  const [custom, setCustom] = useState(false)
  const preset = value === '' || choices.some(([duration]) => duration === value)
  const showCustom = custom || !preset
  return (
    <div className="field min-w-0">
      <Label htmlFor={id}>{label}</Label>
      <select
        id={id}
        className="native-select w-full"
        value={showCustom ? 'custom' : value}
        required={required && !showCustom}
        onChange={(event) => {
          setCustom(event.target.value === 'custom')
          if (event.target.value !== 'custom') onChange(event.target.value)
        }}
      >
        <option value="" disabled={required}>
          {emptyLabel}
        </option>
        {choices.map(([duration, text]) => (
          <option key={duration} value={duration}>
            {text}
          </option>
        ))}
        <option value="custom">Custom duration (advanced)</option>
      </select>
      {showCustom && (
        <>
          <Label htmlFor={`${id}-custom`} className="sr-only">
            {label} custom duration
          </Label>
          <Input
            id={`${id}-custom`}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            required={required}
            placeholder="1h30m"
            aria-describedby={`${id}-help`}
          />
          <p id={`${id}-help`} className="help">
            Use a supported duration, such as 90m or 1h30m. Existing custom values are preserved
            exactly.
          </p>
        </>
      )}
    </div>
  )
}

export type TimingPatch = { timing?: TimingMode; every?: string; cron?: string; timezone?: string }

export function ScheduleTimingFields({
  timing,
  every,
  cron,
  timezone,
  onChange,
  label = 'Timing',
  onValidityChange,
}: {
  timing: TimingMode
  every: string
  cron: string
  timezone: string
  onChange: (patch: TimingPatch) => void
  label?: string
  onValidityChange?: (invalid: boolean) => void
}) {
  const id = useId()
  const invalid =
    timing === 'daily'
      ? !dailyTime(cron)
      : timing === 'every'
        ? !every
        : timing === 'cron'
          ? !cron.trim()
          : false
  useEffect(() => {
    onValidityChange?.(invalid)
    return () => onValidityChange?.(false)
  }, [invalid, onValidityChange])
  return (
    <div className="space-y-4">
      <div className="field">
        <Label htmlFor={`${id}-timing`}>{label}</Label>
        <select
          id={`${id}-timing`}
          className="native-select w-full"
          value={timing}
          onChange={(event) => {
            const next = event.target.value as TimingMode
            onChange({
              timing: next,
              every: next === 'every' ? every || '30m' : '',
              cron:
                next === 'daily'
                  ? dailyTime(cron)
                    ? cron
                    : '0 0 * * *'
                  : next === 'cron'
                    ? cron || '0 */6 * * *'
                    : '',
            })
          }}
        >
          <option value="manual">Manual only</option>
          <option value="every">Every…</option>
          <option value="daily">Daily at…</option>
          <option value="cron">Custom schedule (Cron)</option>
        </select>
        {timing === 'manual' && (
          <p className="help">
            Imports start only when requested. Choosing this option removes the automatic frequency
            when saved.
          </p>
        )}
      </div>
      {timing === 'every' && (
        <DurationSelect
          label="Interval"
          value={every}
          required
          onChange={(value) => onChange({ every: value })}
        />
      )}
      {timing === 'daily' && (
        <div className="field">
          <Label htmlFor={`${id}-time`}>Daily time</Label>
          <Input
            id={`${id}-time`}
            type="time"
            required
            value={dailyTime(cron) ?? ''}
            onChange={(event) => onChange({ cron: dailyCron(event.target.value) })}
          />
        </div>
      )}
      {timing === 'cron' && (
        <div className="field">
          <Label htmlFor={`${id}-cron`}>Cron expression</Label>
          <Input
            id={`${id}-cron`}
            value={cron}
            required
            onChange={(event) => onChange({ cron: event.target.value })}
            placeholder="0 */6 * * *"
            aria-describedby={`${id}-cron-help`}
          />
          <p id={`${id}-cron-help`} className="help">
            Advanced: minute, hour, day of month, month, day of week. Existing custom expressions
            stay unchanged until edited.
          </p>
        </div>
      )}
      {invalid && (
        <p role="alert" className="help text-destructive">
          Choose a valid daily time, interval or custom schedule before saving.
        </p>
      )}
      <div className="field">
        <Label htmlFor={`${id}-zone`}>Timezone</Label>
        <Input
          id={`${id}-zone`}
          value={timezone}
          onChange={(event) => onChange({ timezone: event.target.value })}
          placeholder="UTC"
          list={`${id}-zones`}
          aria-describedby={`${id}-zone-help`}
        />
        <datalist id={`${id}-zones`}>
          <option value="UTC" />
          <option value="Europe/Paris" />
          <option value="Europe/London" />
          <option value="America/New_York" />
        </datalist>
        <p id={`${id}-zone-help`} className="help">
          Daily and Cron schedules follow this timezone, including daylight-saving changes. Empty
          uses UTC. Fixed intervals measure elapsed time. The server runs schedules even when this
          page is closed.
        </p>
      </div>
    </div>
  )
}
