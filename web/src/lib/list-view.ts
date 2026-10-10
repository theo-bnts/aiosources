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
