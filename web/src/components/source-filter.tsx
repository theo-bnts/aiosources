import { SourceMultiSelect } from '@/components/source-multi-select'
import { Label } from '@/components/ui/label'

export function SourceFilter({
  id,
  value,
  sources,
  onChange,
}: {
  id: string
  value: string[]
  sources: { id: string; name: string }[]
  onChange: (value: string[]) => void
}) {
  return (
    <div className="field sm:col-span-2">
      <Label htmlFor={id}>Sources</Label>
      <SourceMultiSelect
        id={id}
        value={value}
        options={sources.map((source) => ({ value: source.id, label: source.name }))}
        onChange={onChange}
      />
    </div>
  )
}
