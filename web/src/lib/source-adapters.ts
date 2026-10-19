// Keep saved definitions editable without rewriting their original identifier.
// New templates and selections use json; http_json remains readable on upgrade.
export function isJSONAdapter(adapter: unknown): boolean {
  return adapter === 'json' || adapter === 'http_json'
}
