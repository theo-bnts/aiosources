import { useState, type ReactNode } from 'react'
import { Switch } from '@/components/ui/switch'
import { MultiSelect } from '@/components/ui/multi-select'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'
import { InfoTip } from '@/components/ui/info-tip'
import type { Config } from '@/lib/aiosource-types'

type Selection = Pick<
  Config,
  | 'software_include_nvd'
  | 'software_exclude_plugins'
  | 'software_include_os'
  | 'software_include_icons'
  | 'software_platforms'
>

const defaultSoftwarePlatforms = ['windows', 'macos', 'linux', 'android', 'ios']

const softwarePlatformOptions = [
  { value: 'windows', label: 'Windows' },
  { value: 'macos', label: 'macOS' },
  { value: 'linux', label: 'Linux' },
  { value: 'android', label: 'Android' },
  { value: 'ios', label: 'iOS' },
]

export function SoftwareImportFilters({
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
  const platforms = value.software_platforms.length
    ? value.software_platforms
    : defaultSoftwarePlatforms
  return (
    <Disclosure
      title="Catalog filters"
      open={open}
      onOpenChange={setOpen}
      help="Only selected software sources, categories and operating systems are projected into the local catalog. Changing filters triggers an automatic re-projection or refresh; entries outside the selection are removed only after the new catalog finishes building. Source records and other metadata catalogs are retained."
    >
      <div className="min-w-0 space-y-5">
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Include commercial & vendor catalog (NVD CPE)"
              checked={value.software_include_nvd}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ software_include_nvd: checked })}
            />
            Include commercial &amp; vendor catalog (NVD CPE)
          </label>
          <InfoTip label="Include commercial & vendor catalog (NVD CPE)">
            Include NIST NVD Official CPE Dictionary 2.0 entries (US Gov Public Domain) alongside
            Microsoft WinGet and Homebrew Cask for broad commercial and cross-platform software
            coverage.
          </InfoTip>
        </div>
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Exclude web plugins, CMS extensions and developer libraries"
              checked={value.software_exclude_plugins}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ software_exclude_plugins: checked })}
            />
            Exclude web plugins, CMS extensions and developer libraries
          </label>
          <InfoTip label="Exclude web plugins, CMS extensions and developer libraries">
            Filter out WordPress, Drupal, Joomla and Jenkins plugins, npm/PyPI/RubyGems/Maven
            libraries, browser add-ons and hardware firmwares from NVD CPE so the catalog stays
            focused on real software applications.
          </InfoTip>
        </div>
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Include operating systems"
              checked={value.software_include_os}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ software_include_os: checked })}
            />
            Include operating systems
          </label>
          <InfoTip label="Include operating systems">
            Include NVD CPE operating system entries (cpe:2.3:o:) to identify Windows, Linux and
            macOS installation ISOs.
          </InfoTip>
        </div>
        <div className="flex items-center gap-1.5">
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label="Include software icons index"
              checked={value.software_include_icons}
              disabled={busy}
              onCheckedChange={(checked) => onChange({ software_include_icons: checked })}
            />
            Include software icons index
          </label>
          <InfoTip label="Include software icons index">
            Imports lightweight metadata from selfh.st/icons (CC BY 4.0). Remote PNG icons stay
            external and are never downloaded or stored on disk.
          </InfoTip>
        </div>
        <div className="field min-w-0">
          <Label
            htmlFor="software_platforms"
            help="Select which operating system platforms to include in the local Software catalog. Defaults to all five platforms."
          >
            Platforms
          </Label>
          <MultiSelect
            id="software_platforms"
            label="Platforms"
            allLabel="All platforms"
            value={platforms}
            disabled={busy}
            options={softwarePlatformOptions}
            onChange={(selected) =>
              onChange({
                software_platforms: selected.length ? selected : defaultSoftwarePlatforms,
              })
            }
          />
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
