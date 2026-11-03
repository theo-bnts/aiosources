export function selectionValues(value: string) {
  return [...new Set(value.split(',').filter(Boolean))]
}

export function selectionMatches(value: string, candidate: string) {
  return !value || selectionValues(value).includes(candidate)
}
