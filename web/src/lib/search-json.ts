class ExactNumber {
  constructor(readonly source: string) {}
  toString() {
    return this.source
  }
}

// Keep mapped values lossless while leaving pagination and other API metadata numeric.
// Source-aware JSON.parse is standard; older engines must not silently round values.
export function decodeSearchJSON<T>(text: string): T {
  const parsed: unknown = JSON.parse(
    text,
    (_key, value: unknown, context?: { source?: string }) => {
      if (typeof value !== 'number') return value
      if (context?.source) return new ExactNumber(context.source)
      throw new Error(
        'This browser cannot display these numbers without losing precision. Update your browser to view this data safely.',
      )
    },
  )
  function metadata(value: unknown): unknown {
    if (value instanceof ExactNumber) return Number(value.source)
    if (Array.isArray(value)) return value.map(metadata)
    if (value && typeof value === 'object') {
      return Object.fromEntries(
        Object.entries(value).map(([key, entry]) => [
          key,
          ['fields', 'before', 'after'].includes(key) ? entry : metadata(entry),
        ]),
      )
    }
    return value
  }
  return metadata(parsed) as T
}

export function exactJSON(value: unknown, indent = 2): string {
  function encode(entry: unknown, depth: number): string {
    if (entry instanceof ExactNumber) return entry.source
    if (entry === null || typeof entry !== 'object') return JSON.stringify(entry) ?? 'null'
    const array = Array.isArray(entry)
    const values = array
      ? entry.map((item) => encode(item, depth + 1))
      : Object.entries(entry).map(
          ([key, item]) => `${JSON.stringify(key)}:${indent ? ' ' : ''}${encode(item, depth + 1)}`,
        )
    if (!values.length) return array ? '[]' : '{}'
    const padding = ' '.repeat(indent * (depth + 1))
    const endPadding = ' '.repeat(indent * depth)
    const body = indent
      ? `\n${padding}${values.join(`,\n${padding}`)}\n${endPadding}`
      : values.join(',')
    return `${array ? '[' : '{'}${body}${array ? ']' : '}'}`
  }
  return encode(value, 0)
}

export function exactField(value: unknown): string {
  if (value === null || value === undefined) return 'Not provided'
  if (typeof value === 'string') return value
  return exactJSON(value, 0)
}

function equalValue(left: unknown, right: unknown): boolean {
  if (left instanceof ExactNumber || right instanceof ExactNumber) {
    return (
      left instanceof ExactNumber && right instanceof ExactNumber && left.source === right.source
    )
  }
  if (left === right) return true
  if (!left || !right || typeof left !== 'object' || typeof right !== 'object') return false
  if (Array.isArray(left) !== Array.isArray(right)) return false
  const a = Object.keys(left)
  const b = Object.keys(right)
  if (a.length !== b.length) return false
  return a.every(
    (key) =>
      Object.hasOwn(right, key) &&
      equalValue((left as Record<string, unknown>)[key], (right as Record<string, unknown>)[key]),
  )
}

export function fieldChanges(
  before: Record<string, unknown> | null,
  after: Record<string, unknown> | null,
) {
  const previous = before ?? {}
  const next = after ?? {}
  return [...new Set([...Object.keys(previous), ...Object.keys(next)])].sort().flatMap((key) => {
    const had = Object.hasOwn(previous, key)
    const has = Object.hasOwn(next, key)
    if (had === has && equalValue(previous[key], next[key])) return []
    return [
      {
        key,
        had,
        has,
        before: previous[key],
        after: next[key],
        kind: !had ? 'Added' : !has ? 'Removed' : 'Changed',
      },
    ]
  })
}
