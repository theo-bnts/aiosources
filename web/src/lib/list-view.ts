// Preserve the surrounding view (including page-size overrides and detail tabs)
// while clearing only the filters owned by this list.
export function resetListView(
  search: URLSearchParams,
  filters: readonly string[] = [],
  offsetKey = 'offset',
) {
  const next = new URLSearchParams(search)
  for (const key of [...filters, offsetKey]) next.delete(key)
  return next
}

const names = new Intl.Collator('en', { numeric: true, sensitivity: 'base' })

export function compareListNames(
  a: { name?: string; id?: string },
  b: { name?: string; id?: string },
) {
  return (
    names.compare(a.name || a.id || '', b.name || b.id || '') ||
    names.compare(a.id || '', b.id || '')
  )
}

export function compareListLabels(
  a: { label: string; value: string },
  b: { label: string; value: string },
) {
  return names.compare(a.label, b.label) || names.compare(a.value, b.value)
}

export function matchesListQuery(query: string, ...values: (string | undefined)[]) {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  const text = values.filter(Boolean).join(' ').toLocaleLowerCase()
  return words.every((word) => text.includes(word))
}
