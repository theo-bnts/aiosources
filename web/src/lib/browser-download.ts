// Reserve the tab during the click: opening one after an asynchronous provider
// response is blocked by browsers that require a current user gesture.
export function prepareBrowserDownload() {
  let target: Window | null = null
  const placeholder = () => {
    try {
      return target && !target.closed && target.location.href === 'about:blank' ? target : null
    } catch {
      // If the visitor navigated elsewhere, this is no longer our placeholder.
      return null
    }
  }
  const cancel = () => {
    placeholder()?.close()
    target = null
  }
  try {
    target = window.open('about:blank', '_blank')
    if (target) {
      target.opener = null
      const doc = target.document
      doc.documentElement.lang = 'en'
      doc.title = 'Preparing download · AIOSources'
      const referrer = doc.createElement('meta')
      referrer.name = 'referrer'
      referrer.content = 'no-referrer'
      doc.head.append(referrer)
      const colors = getComputedStyle(document.body)
      Object.assign(doc.body.style, {
        margin: '0',
        padding: '3rem 1.5rem',
        background: colors.backgroundColor,
        color: colors.color,
        fontFamily: colors.fontFamily,
        fontSize: '14px',
        lineHeight: '1.6',
      })
      const main = doc.createElement('main')
      main.style.maxWidth = '32rem'
      main.style.margin = '0 auto'
      const heading = doc.createElement('h1')
      heading.textContent = 'Preparing your download…'
      heading.style.fontSize = '1.25rem'
      heading.style.fontWeight = '600'
      const message = doc.createElement('p')
      message.textContent =
        'You can return to AIOSources while the debrid service prepares the file. Keep the search page open; this tab will open the file when it is ready.'
      main.append(heading, message)
      doc.body.replaceChildren(main)
    }
  } catch {
    cancel()
  }
  return {
    cancel,
    start(url: string) {
      try {
        const destination = new URL(url)
        if (destination.protocol !== 'https:' || destination.username || destination.password) {
          cancel()
          return false
        }
        const tab = placeholder()
        if (!tab) return false
        // An attachment keeps its opener document visible instead of navigating.
        // Leave a finished state in that tab rather than a preparing message.
        tab.document.title = 'Download ready · AIOSources'
        tab.document.querySelector('h1')?.replaceChildren('Your download is ready')
        tab.document
          .querySelector('p')
          ?.replaceChildren('Check your browser’s downloads. You can close this tab.')
        tab.location.replace(destination.href)
        // The provider owns the tab from this point. Leaving the search view
        // must never close a download that has already started.
        target = null
        return true
      } catch {
        cancel()
        return false
      }
    },
  }
}
