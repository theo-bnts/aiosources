import { useEffect, useRef, useState } from 'react'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { selectionValues } from '@/lib/filter-values'

// Filters use the same multi-value control as connections. Keep the existing
// single-value URL/form contract, extending it with comma-separated choices.
export function FilterSelect({
  id,
  label,
  allLabel,
  options,
  value,
  defaultValue = '',
  onChange,
  name,
  selectGroups = false,
}: {
  id: string
  label: string
  allLabel: string
  options: MultiSelectOption[]
  value?: string
  defaultValue?: string
  onChange?: (value: string) => void
  name?: string
  selectGroups?: boolean
}) {
  const [draft, setDraft] = useState(defaultValue)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    const form = input.current?.form
    const reset = (event: Event) => {
      queueMicrotask(() => {
        if (!event.defaultPrevented) setDraft(defaultValue)
      })
    }
    form?.addEventListener('reset', reset)
    return () => form?.removeEventListener('reset', reset)
  }, [defaultValue])
  const current = value ?? draft
  const selected = selectionValues(current)
  const choices = [
    ...options,
    ...selected
      .filter((item) => !options.some((option) => option.value === item))
      .map((item) => ({ value: item, label: item })),
  ]
  return (
    <>
      {name && <input ref={input} type="hidden" name={name} value={current} />}
      <MultiSelect
        id={id}
        label={label}
        allLabel={allLabel}
        options={choices}
        selectGroups={selectGroups}
        value={selected}
        onChange={(values) => {
          const next = values.join(',')
          setDraft(next)
          onChange?.(next)
        }}
      />
    </>
  )
}
