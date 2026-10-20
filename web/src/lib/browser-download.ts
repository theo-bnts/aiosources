// A same-origin attachment frame starts the browser download without replacing
// the application or opening a tab. Provider responses cannot execute scripts.
export function prepareBrowserDownload() {
  let active = true
  return {
    cancel() {
      active = false
    },
    start(url: string) {
      if (!active) return false
      try {
        const destination = new URL(url, window.location.origin)
        if (
          destination.origin !== window.location.origin ||
          destination.username ||
          destination.password ||
          !['/api/catalog-downloads/', '/api/public/catalog-downloads/'].some((prefix) =>
            destination.pathname.startsWith(prefix),
          ) ||
          destination.search ||
          destination.hash
        )
          return false
        const frame = document.createElement('iframe')
        frame.hidden = true
        frame.title = 'File download'
        frame.dataset.catalogDownload = ''
        frame.sandbox.add('allow-downloads')
        frame.referrerPolicy = 'no-referrer'
        frame.src = destination.href
        document.body.append(frame)
        // Attachment delivery belongs to the browser once its response arrives.
        // Leave enough time for connection setup, independently of page routing.
        window.setTimeout(() => frame.remove(), 120_000)
        active = false
        return true
      } catch {
        return false
      }
    },
  }
}
