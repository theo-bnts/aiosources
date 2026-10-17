import { MultiSelect } from '@/components/ui/multi-select'

export function SourceMultiSelect(props: {
  id: string
  value: string[]
  options: { value: string; label: string }[]
  onChange: (value: string[]) => void
}) {
  return <MultiSelect {...props} label="Sources" allLabel="All sources" name="provider" />
}
