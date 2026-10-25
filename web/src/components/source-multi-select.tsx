import { MultiSelect } from '@/components/ui/multi-select'
import { compareListLabels } from '@/lib/list-view'

export function SourceMultiSelect(props: {
  id: string
  value: string[]
  options: { value: string; label: string }[]
  onChange: (value: string[]) => void
}) {
  return (
    <MultiSelect
      {...props}
      options={[...props.options].sort(compareListLabels)}
      label="Sources"
      allLabel="All sources"
      name="provider"
    />
  )
}
