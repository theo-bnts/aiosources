import { useRef, useState } from 'react'
import { Switch } from '@/components/ui/switch'
import { MultiSelect } from '@/components/ui/multi-select'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'
import { browserLanguage, languageChoices } from '@/lib/language-preferences'
import type { Config } from '@/lib/aiosource-types'

type Selection = Pick<Config, 'openlibrary_relevant_only' | 'openlibrary_languages'>

export function BookImportFilters({
  value,
  onChange,
  busy,
}: {
  value: Selection
  onChange: (patch: Partial<Selection>) => void
  busy: boolean
}) {
  const [open, setOpen] = useState(true)
  const previous = useRef(
    value.openlibrary_languages.length ? value.openlibrary_languages : [browserLanguage()],
  )
  if (value.openlibrary_languages.length) previous.current = value.openlibrary_languages
  const languages = value.openlibrary_languages
  return (
    <Disclosure title="Catalog filters" open={open} onOpenChange={setOpen}>
      <fieldset disabled={busy} className="min-w-0 space-y-5">
        <div className="space-y-2">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Relevant editions only"
              checked={value.openlibrary_relevant_only}
              onCheckedChange={(checked) => onChange({ openlibrary_relevant_only: checked })}
            />
            Relevant editions only
          </label>
          <p className="help">
            Keep digital, audio and scanned editions, plus editions matching imported books.
            Availability is not guaranteed.
          </p>
        </div>
        <div className="space-y-3">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Filter book languages"
              checked={languages.length > 0}
              onCheckedChange={(checked) =>
                onChange({ openlibrary_languages: checked ? previous.current : [] })
              }
            />
            Filter by language
          </label>
          <fieldset disabled={!languages.length} className="field min-w-0">
            <Label htmlFor="openlibrary_languages">Edition languages</Label>
            <MultiSelect
              id="openlibrary_languages"
              label="Edition languages"
              allLabel="All languages"
              value={languages}
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
          </fieldset>
          <p className="help">
            A book needs one edition matching the enabled filters. Translations count. Unknown
            languages are excluded when language filtering is enabled.
          </p>
        </div>
      </fieldset>
      <p className="help">
        Only selected information is saved. Changing filters starts a new resumable import; entries
        outside the selection are removed after it finishes. Source records and video information
        are retained.
      </p>
      {busy && (
        <p role="status" className="help">
          Wait for the current update or reset to finish before changing catalog filters.
        </p>
      )}
    </Disclosure>
  )
}
