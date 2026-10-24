import { useId } from 'react'
import { ArrowDownIcon, ArrowUpIcon } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { sortFields, type ResultPreferences, type SortRule } from '@/lib/result-preferences'

export function ResultSorting({
  value = [],
  onChange,
  language = false,
  sources = true,
  defaultOrder = 'largest first',
}: {
  value?: SortRule[]
  onChange: (value: SortRule[]) => void
  language?: boolean
  sources?: boolean
  defaultOrder?: string
}) {
  const id = useId()
  const options = sortFields.filter(
    (field) => (language || field.value !== 'language') && (sources || field.value !== 'source'),
  )
  return (
    <Disclosure title="Sorting">
      <div className="space-y-4">
        {[0, 1, 2].map((index) => {
          const rule = value[index]
          return (
            <div key={index} className="field min-w-0">
              <Label htmlFor={`${id}-${index}`}>
                {index === 0 ? 'Sort by' : `Then by (${index + 1})`}
              </Label>
              <div className="flex min-w-0 items-center gap-2">
                <select
                  id={`${id}-${index}`}
                  className="native-select min-w-0 flex-1"
                  value={rule?.field ?? ''}
                  disabled={index > value.length}
                  onChange={(event) => {
                    const field = options.find((option) => option.value === event.target.value)
                    const next = [...value]
                    if (field) next[index] = { field: field.value, direction: field.direction }
                    else next.splice(index, 1)
                    onChange(next)
                  }}
                >
                  <option value="">None</option>
                  {options.map((field) => (
                    <option
                      key={field.value}
                      value={field.value}
                      disabled={value.some(
                        (other, at) =>
                          at !== index &&
                          (other.field === field.value ||
                            (['age', 'published'].includes(other.field) &&
                              ['age', 'published'].includes(field.value))),
                      )}
                    >
                      {field.label}
                    </option>
                  ))}
                </select>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  disabled={!rule}
                  aria-label={`${index === 0 ? 'Sort' : `Sort ${index + 1}`} direction: ${rule?.direction === 'asc' ? 'ascending' : 'descending'}`}
                  title={
                    rule?.direction === 'asc'
                      ? 'Ascending · click to reverse'
                      : 'Descending · click to reverse'
                  }
                  onClick={() =>
                    onChange(
                      value.map((item, at) =>
                        at === index
                          ? { ...item, direction: item.direction === 'asc' ? 'desc' : 'asc' }
                          : item,
                      ),
                    )
                  }
                >
                  {rule?.direction === 'asc' ? <ArrowUpIcon /> : <ArrowDownIcon />}
                </Button>
              </div>
            </div>
          )
        })}
      </div>
      <p className="help">
        Rules apply from top to bottom. Descending puts cached results, preferred languages and
        larger sizes first. Without custom rules: {defaultOrder}.
      </p>
    </Disclosure>
  )
}

export function ResponseTimeout({
  value,
  onChange,
  stream = true,
}: {
  value?: number
  onChange: (value?: number) => void
  stream?: boolean
}) {
  const id = useId()
  return (
    <div className="field min-w-0">
      <Label htmlFor={id}>
        {stream ? 'Stream response timeout (seconds)' : 'Response timeout (seconds)'}
      </Label>
      <Input
        id={id}
        type="number"
        min={0}
        max={stream ? 60 : 600}
        step={1}
        placeholder="Instance default"
        value={value || ''}
        onChange={(event) => onChange(Number(event.target.value) || undefined)}
      />
    </div>
  )
}

// Native query forms keep a scalar sort in their URL. Present the same neutral
// field names and direction control without introducing a second query format.
export function QuerySorting({
  value,
  onChange,
  options,
}: {
  value: string
  onChange: (value: string) => void
  options: { label: string; asc: string; desc: string }[]
}) {
  const id = useId()
  const selected =
    options.find((option) => option.asc === value || option.desc === value) ?? options[0]
  const ascending = selected.asc === value
  return (
    <Disclosure title="Sorting" open>
      <div className="field min-w-0">
        <Label htmlFor={id}>Sort by</Label>
        <div className="flex min-w-0 items-center gap-2">
          <select
            id={id}
            className="native-select min-w-0 flex-1"
            value={selected.desc}
            onChange={(event) => onChange(event.target.value)}
          >
            {options.map((option) => (
              <option key={option.desc} value={option.desc}>
                {option.label}
              </option>
            ))}
          </select>
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label={`Sort direction: ${ascending ? 'ascending' : 'descending'}`}
            title={ascending ? 'Ascending · click to reverse' : 'Descending · click to reverse'}
            onClick={() => onChange(ascending ? selected.desc : selected.asc)}
          >
            {ascending ? <ArrowUpIcon /> : <ArrowDownIcon />}
          </Button>
        </div>
      </div>
    </Disclosure>
  )
}

export function ResultLimits<T extends ResultPreferences & { cached_only?: boolean }>({
  value,
  onChange,
  cache = false,
}: {
  value: T
  onChange: (value: T) => void
  cache?: boolean
}) {
  const id = useId()
  return (
    <div className="space-y-4">
      <div className="grid items-start gap-4 sm:grid-cols-2">
        <div className="field min-w-0">
          <Label htmlFor={`${id}-results`}>Maximum results</Label>
          <Input
            id={`${id}-results`}
            type="number"
            min={0}
            max={10000}
            step={1}
            placeholder="No limit"
            disabled={value.cached_only}
            value={value.max_results || ''}
            onChange={(event) =>
              onChange({ ...value, max_results: Number(event.target.value) || undefined })
            }
          />
        </div>
        <div className="field min-w-0">
          <Label htmlFor={`${id}-cached`}>Maximum cached results</Label>
          <Input
            id={`${id}-cached`}
            type="number"
            min={0}
            max={10000}
            step={1}
            placeholder="No limit"
            disabled={!cache && !value.cached_only}
            value={value.max_cached || ''}
            onChange={(event) =>
              onChange({ ...value, max_cached: Number(event.target.value) || undefined })
            }
          />
        </div>
        <ResponseTimeout
          value={value.response_seconds}
          onChange={(response_seconds) => onChange({ ...value, response_seconds })}
          stream={cache}
        />
      </div>
      <p className="help">
        {value.cached_only
          ? 'Only the cached limit applies. The general limit is kept for later.'
          : cache
            ? 'The cached limit stops new checks; local observations are reused and unchecked results stay Unknown. Maximum results also limits candidates for new checks.'
            : 'Limits apply across all pages. The cached limit applies only with Cached results only.'}{' '}
        {cache
          ? 'The timeout limits availability checks, within the instance request deadline.'
          : 'The timeout can shorten the instance request deadline for this connection.'}
      </p>
    </div>
  )
}
