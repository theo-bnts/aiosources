export type MultiSelectOption = { value: string; label: string; group?: string }

export function selectionGroupState(value: string[], options: MultiSelectOption[]) {
  const selected = new Set(value)
  const count = options.filter((option) => selected.has(option.value)).length
  return count === 0 ? false : count === options.length ? true : ('indeterminate' as const)
}

export function toggleSelectionGroup(
  value: string[],
  options: MultiSelectOption[],
  checked: boolean,
) {
  const group = new Set(options.map((option) => option.value))
  return checked ? [...new Set([...value, ...group])] : value.filter((item) => !group.has(item))
}

export function selectionLabels(
  value: string[],
  options: MultiSelectOption[],
  summarizeGroups: boolean,
) {
  const complete = new Set(
    summarizeGroups
      ? options
          .map((option) => option.group)
          .filter(
            (group) =>
              group &&
              selectionGroupState(
                value,
                options.filter((option) => option.group === group),
              ) === true,
          )
      : [],
  )
  const labels: string[] = []
  const emitted = new Set<string>()
  for (const item of value) {
    const option = options.find((candidate) => candidate.value === item)
    if (option?.group && complete.has(option.group)) {
      if (!emitted.has(option.group)) labels.push(option.group)
      emitted.add(option.group)
    } else {
      labels.push(option?.label ?? item)
    }
  }
  return labels.join(', ')
}
