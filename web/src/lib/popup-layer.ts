export function popupLayer(trigger: HTMLElement | null) {
  const header = document.querySelector('[data-app-shell] header')
  const inHeader = Boolean(trigger && header?.contains(trigger))
  const dialog = inHeader || Boolean(trigger?.closest('[role="dialog"], [role="alertdialog"]'))
  return {
    dialog,
    top: dialog ? 8 : Math.max(8, (header?.getBoundingClientRect().bottom ?? 0) + 8),
  }
}
