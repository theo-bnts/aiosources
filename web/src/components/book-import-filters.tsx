import { useRef, useState, type ReactNode } from 'react'
import { Switch } from '@/components/ui/switch'
import { MultiSelect } from '@/components/ui/multi-select'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'
import { InfoTip } from '@/components/ui/info-tip'
import { browserLanguage, languageChoices } from '@/lib/language-preferences'
import type { Config } from '@/lib/aiosource-types'

type Selection = Pick<Config, 'openlibrary_relevant_only' | 'openlibrary_languages'>

export function BookImportFilters({
  value,
  onChange,
  busy,
  footer,
}: {
  value: Selection
  onChange: (patch: Partial<Selection>) => void
  busy: boolean
  footer?: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const previous = useRef(
    value.openlibrary_languages.length ? value.openlibrary_languages : [browserLanguage()],
  )
  if (value.openlibrary_languages.length) previous.current = value.openlibrary_languages
  const languages = value.openlibrary_languages
  return (
    <Disclosure
      title="Catalog filters"
      open={open}
      onOpenChange={setOpen}
      help="Only selected information is saved. Changing filters starts a new resumable import; entries outside the selection are removed after it finishes. Source records and video information are retained."
    >
      <div className="min-w-0 space-y-5">
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Relevant editions only"
              checked={value.openlibrary_relevant_only}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ openlibrary_relevant_only: checked })}
            />
            Relevant editions only
          </label>
          <InfoTip label="Relevant editions only">
            Keep digital, audio and scanned editions, plus editions matching imported books.
            Availability is not guaranteed.
          </InfoTip>
        </div>
        <div className="space-y-3">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Filter book languages"
              checked={languages.length > 0}
              disabled={busy}
              onCheckedChange={(checked) =>
                onChange({ openlibrary_languages: checked ? previous.current : [] })
              }
            />
            Filter by language
          </label>
          <div className="field min-w-0">
            <Label
              htmlFor="openlibrary_languages"
              help="A book needs one edition matching the enabled filters. Translations count. Unknown languages are excluded when language filtering is enabled."
            >
              Edition languages
            </Label>
            <MultiSelect
              id="openlibrary_languages"
              label="Edition languages"
              allLabel="All languages"
              value={languages}
              disabled={busy || !languages.length}
              options={languageChoices(languages)
                .sort(
                  (a, b) =>
                    Number(b.value === browserLanguage()) - Number(a.value === browserLanguage()),
                )
                .map((option) => ({
                  ...option,
                  label:
                    option.value === browserLanguage()
                      ? `Browser language (${option.label})`
                      : option.label,
                }))}
              onChange={(selected) => onChange({ openlibrary_languages: selected })}
            />
          </div>
        </div>
      </div>
      {busy && (
        <p role="status" className="help">
          Wait for the current update or reset to finish before changing catalog filters.
        </p>
      )}
      {footer}
    </Disclosure>
  )
}
