import { useRef, useState, type ReactNode } from 'react'
import { Switch } from '@/components/ui/switch'
import { MultiSelect } from '@/components/ui/multi-select'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'
import { InfoTip } from '@/components/ui/info-tip'
import { browserLanguage, languageChoices } from '@/lib/language-preferences'
import type { Config } from '@/lib/aiosource-types'

type Selection = Pick<
  Config,
  | 'musicbrainz_official_only'
  | 'musicbrainz_exclude_spoken'
  | 'musicbrainz_include_covers'
  | 'musicbrainz_types'
  | 'musicbrainz_languages'
>

const musicReleaseTypeOptions = [
  { value: 'album', label: 'Albums' },
  { value: 'ep', label: 'EPs' },
  { value: 'single', label: 'Singles' },
  { value: 'broadcast', label: 'Broadcasts' },
  { value: 'other', label: 'Other release types' },
]

export function MusicImportFilters({
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
  const previousLanguages = useRef(
    value.musicbrainz_languages.length ? value.musicbrainz_languages : [browserLanguage()],
  )
  if (value.musicbrainz_languages.length) previousLanguages.current = value.musicbrainz_languages
  const languages = value.musicbrainz_languages
  const types = value.musicbrainz_types.length ? value.musicbrainz_types : ['album', 'ep', 'single']
  return (
    <Disclosure
      title="Catalog filters"
      open={open}
      onOpenChange={setOpen}
      help="Only selected release groups are kept in the local catalog. Changing filters triggers a fresh projection or export import; entries outside the selection are removed after it finishes. Source records and other metadata catalogs are retained."
    >
      <div className="min-w-0 space-y-5">
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Official releases only"
              checked={value.musicbrainz_official_only}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ musicbrainz_official_only: checked })}
            />
            Official releases only
          </label>
          <InfoTip label="Official releases only">
            Keep release groups that have at least one official release. Excludes bootleg-only,
            promotional-only and pseudo-release entries.
          </InfoTip>
        </div>
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Exclude audiobooks and spoken word"
              checked={value.musicbrainz_exclude_spoken}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ musicbrainz_exclude_spoken: checked })}
            />
            Exclude audiobooks and spoken word
          </label>
          <InfoTip label="Exclude audiobooks and spoken word">
            Skip MusicBrainz Audiobook, Audio drama and Spokenword entries so audiobooks remain
            handled by the Books catalog (Open Library).
          </InfoTip>
        </div>
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Import Cover Art Archive index"
              checked={value.musicbrainz_include_covers}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ musicbrainz_include_covers: checked })}
            />
            Import Cover Art Archive index
          </label>
          <InfoTip label="Import Cover Art Archive index">
            Imports the lightweight Cover Art Archive metadata dump (only front-cover presence flags
            are stored; cover images themselves are never downloaded to disk and stay remote).
          </InfoTip>
        </div>
        <div className="field min-w-0">
          <Label
            htmlFor="musicbrainz_types"
            help="Select which MusicBrainz primary release group types to include. Defaults to Albums, EPs and Singles."
          >
            Release types
          </Label>
          <MultiSelect
            id="musicbrainz_types"
            label="Release types"
            allLabel="Albums, EPs and Singles"
            value={types}
            disabled={busy}
            options={musicReleaseTypeOptions}
            onChange={(selected) =>
              onChange({
                musicbrainz_types: selected.length ? selected : ['album', 'ep', 'single'],
              })
            }
          />
        </div>
        <div className="space-y-3">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Filter music languages"
              checked={languages.length > 0}
              disabled={busy}
              onCheckedChange={(checked) =>
                onChange({ musicbrainz_languages: checked ? previousLanguages.current : [] })
              }
            />
            Filter by release language
          </label>
          <div className="field min-w-0">
            <Label
              htmlFor="musicbrainz_languages"
              help="A release group needs at least one release in the selected languages (or marked instrumental/no linguistic content when matching). Leave disabled to keep all music regardless of language."
            >
              Release languages
            </Label>
            <MultiSelect
              id="musicbrainz_languages"
              label="Release languages"
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
              onChange={(selected) => onChange({ musicbrainz_languages: selected })}
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
