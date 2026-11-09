export function buildVersion(value: string | undefined) {
  const display = value && value !== '__AIOSOURCES_VERSION__' ? value.slice(0, 200) : 'dev'
  const published = /^(\d+\.\d+\.\d+)\.([0-9a-f]{12})$/.exec(display)
  return {
    display,
    release: published?.[1],
    build: published?.[2],
    url: published
      ? `https://github.com/theo-bnts/aiosources/releases/tag/build-${published[2]}`
      : undefined,
  }
}
