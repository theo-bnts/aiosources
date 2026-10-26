// Source identifiers remain data when they match a directory or action name.
// Ordinary URLs stay stable; only conflicting identifiers need another path.
export function sourceSettingsPath(id: string) {
  const prefix = ['new', 'settings'].includes(id.toLowerCase()) ? '/providers/id/' : '/providers/'
  return prefix + encodeURIComponent(id)
}

export function isSourceSettingsPath(pathname: string) {
  return (
    !/^\/providers\/(?:new|settings)$/i.test(pathname) &&
    /^\/providers\/(?:id\/)?[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/i.test(pathname)
  )
}
