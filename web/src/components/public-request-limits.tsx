import { useState } from 'react'
import { FloppyDiskIcon, XIcon } from '@phosphor-icons/react'
import { isRevisionConflict } from '@/lib/api'
import { isWholeNumberString } from '@/lib/format'
import { useNetworkSettings, useSaveNetwork } from '@/lib/network-settings'
import type { NetworkSettings, PublicLimits } from '@/lib/network-types'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

const fields = [
  ['setup_per_minute', 'Link creations per minute', 100000, 12],
  ['requests_per_minute', 'Public requests per minute', 100000, 120],
  ['concurrent', 'Simultaneous public requests', 1000, 50],
] as const

type Values = Record<keyof PublicLimits, { enabled: boolean; value: string }>
function draftValues(limits: PublicLimits): Values {
  return Object.fromEntries(
    fields.map(([field, , , fallback]) => [
      field,
      { enabled: limits[field] > 0, value: String(limits[field] || fallback) },
    ]),
  ) as Values
}

export function PublicRequestLimitsPanel() {
  const query = useNetworkSettings()
  const [draft, setDraft] = useState<{ base: NetworkSettings; values: Values } | null>(null)
  const save = useSaveNetwork(() => setDraft(null))
  const values = draft?.values ?? (query.data && draftValues(query.data.public_limits))
  const dirty = Boolean(
    draft &&
    fields.some(([key]) => {
      const field = draft.values[key],
        saved = query.data?.public_limits[key]
      return (
        field.enabled !== (saved !== undefined && saved > 0) ||
        (field.enabled && field.value !== String(saved))
      )
    }),
  )
  const invalid = Boolean(
    values &&
    fields.some(([key, , max]) => {
      const field = values[key]
      return field.enabled && !isWholeNumberString(field.value, 1, max)
    }),
  )
  const conflict = Boolean(
    draft && isRevisionConflict(draft.base.revision, query.data?.revision, save.error),
  )
  useUnsavedChanges(dirty, 'public request limits', save.isPending)
  function change(values: Values) {
    if (!query.data) return
    const base = draft?.base ?? query.data
    if (
      base.revision === query.data.revision &&
      fields.every(([key]) => {
        const field = values[key]
        const saved = query.data.public_limits[key]
        return (
          field.enabled === (saved !== undefined && saved > 0) &&
          (!field.enabled || field.value === String(saved))
        )
      })
    ) {
      setDraft(null)
    } else {
      setDraft({ base, values })
    }
    save.reset()
  }
  return (
    <Disclosure
      id="public-request-limits"
      title="Public request limits"
      help="Switch off any limit to remove that restriction. Per-minute limits apply to each visitor IP and are shared by public connections. Simultaneous requests are limited across this application process. Configure visitor IP recognition in Network → Incoming when behind a reverse proxy. Changes apply without restarting or replacing installation links. Lowering concurrency lets active requests finish."
    >
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading request limits" />
      ) : (
        values && (
          <form
            className="space-y-5"
            aria-label="Request limits"
            onSubmit={(event) => {
              event.preventDefault()
              if (draft && dirty && !invalid && !conflict && !save.isPending)
                save.mutate({
                  ...draft.base,
                  public_limits: Object.fromEntries(
                    fields.map(([key]) => [
                      key,
                      values[key].enabled ? Number(values[key].value) : 0,
                    ]),
                  ) as PublicLimits,
                })
            }}
          >
            <div className="grid gap-5 md:grid-cols-3">
              {fields.map(([key, label, max]) => (
                <div className="field min-w-0" key={key}>
                  <div className="flex items-center justify-between gap-3">
                    <Label
                      htmlFor={`public-limit-${key}`}
                      help={`Range: 1–${max}. Switch off for no limit.`}
                    >
                      {label}
                    </Label>
                    <Switch
                      aria-label={`${label} limit`}
                      checked={values[key].enabled}
                      disabled={save.isPending}
                      onCheckedChange={(enabled) =>
                        change({ ...values, [key]: { ...values[key], enabled } })
                      }
                    />
                  </div>
                  <Input
                    id={`public-limit-${key}`}
                    type="number"
                    min={1}
                    max={max}
                    step={1}
                    required
                    disabled={save.isPending || !values[key].enabled}
                    value={values[key].enabled ? values[key].value : ''}
                    placeholder={values[key].enabled ? undefined : 'Unlimited'}
                    onChange={(event) =>
                      change({ ...values, [key]: { ...values[key], value: event.target.value } })
                    }
                  />
                </div>
              ))}
            </div>
            {conflict && (
              <p className="notice notice-warning" role="status">
                Settings changed elsewhere. Your draft is preserved; reload the saved settings
                before saving.
              </p>
            )}
            <ErrorNotice error={save.error} />
            <div className="toolbar">
              <Button type="submit" disabled={!dirty || invalid || conflict || save.isPending}>
                <FloppyDiskIcon aria-hidden="true" />
                {save.isPending ? 'Saving…' : 'Save'}
              </Button>
              <FormStatus
                dirty={dirty}
                saving={save.isPending}
                saved={save.isSuccess}
                message={
                  dirty && invalid ? 'Enter whole numbers within the allowed ranges.' : undefined
                }
              />
              {conflict && (
                <Button
                  type="button"
                  variant="warning"
                  disabled={save.isPending}
                  onClick={() => {
                    setDraft(null)
                    save.reset()
                  }}
                >
                  <XIcon aria-hidden="true" />
                  Discard changes and reload
                </Button>
              )}
            </div>
          </form>
        )
      )}
    </Disclosure>
  )
}
