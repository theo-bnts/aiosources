import { useId, useRef } from 'react'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { browserLanguage, type LanguagePreferences } from '@/lib/language-preferences'

export function LanguageFilter({
  value,
  onChange,
  layout = 'inline',
}: {
  value: LanguagePreferences
  onChange: (value: LanguagePreferences) => void
  layout?: 'inline' | 'stacked'
}) {
  const id = useId()
  const last = useRef<Exclude<LanguagePreferences['mode'], 'all'>>(
    value.mode === 'all' ? 'unknown' : value.mode,
  )
  if (value.mode !== 'all') last.current = value.mode
  const names = new Intl.DisplayNames(['en'], { type: 'language' })
  const languages = [
    ...new Set(
      [
        'ar',
        'bg',
        'cs',
        'da',
        'de',
        'el',
        'en',
        'es',
        'fi',
        'fr',
        'he',
        'hi',
        'hr',
        'hu',
        'id',
        'it',
        'ja',
        'ko',
        'nl',
        'no',
        'pl',
        'pt',
        'ro',
        'ru',
        'sk',
        'sl',
        'sr',
        'sv',
        'th',
        'tr',
        'uk',
        'vi',
        'zh',
        browserLanguage(),
        value.language,
      ].filter(Boolean),
    ),
  ].sort((a, b) => (names.of(a) || a).localeCompare(names.of(b) || b))
  return (
    <div
      className={
        layout === 'stacked' ? 'grid gap-4 sm:grid-cols-2' : 'flex flex-wrap items-end gap-3'
      }
    >
      <label
        className={
          layout === 'stacked'
            ? 'flex flex-row-reverse items-center justify-between gap-3 text-sm sm:col-span-2'
            : 'flex items-center gap-3 self-center text-sm'
        }
      >
        <Switch
          aria-label="Filter languages"
          checked={value.mode !== 'all'}
          onCheckedChange={(checked) =>
            onChange({ ...value, mode: checked ? last.current : 'all' })
          }
        />
        Filter languages
      </label>
      <div className="field min-w-0 max-w-full">
        <Label htmlFor={`${id}-language`}>Preferred language</Label>
        <select
          id={`${id}-language`}
          className={layout === 'stacked' ? 'native-select w-full' : 'native-select max-w-full'}
          value={value.language}
          disabled={value.mode === 'all'}
          onChange={(e) => onChange({ ...value, language: e.target.value })}
        >
          <option value="">Browser language ({names.of(browserLanguage())})</option>
          {languages.map((code) => (
            <option key={code} value={code}>
              {names.of(code) || code}
            </option>
          ))}
        </select>
      </div>
      <div className="field min-w-0 max-w-full">
        <Label htmlFor={`${id}-include`}>Include</Label>
        <select
          id={`${id}-include`}
          className={layout === 'stacked' ? 'native-select w-full' : 'native-select max-w-full'}
          disabled={value.mode === 'all'}
          value={value.mode === 'all' ? last.current : value.mode}
          onChange={(e) =>
            onChange({ ...value, mode: e.target.value as LanguagePreferences['mode'] })
          }
        >
          <option value="preferred">Preferred only</option>
          <option value="multi">Preferred + MULTI</option>
          <option value="unknown">Preferred + MULTI + unknown</option>
        </select>
      </div>
    </div>
  )
}
