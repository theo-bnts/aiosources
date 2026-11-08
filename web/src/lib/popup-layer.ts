export function popupLayer(trigger: HTMLElement | null) {
  const dialog = Boolean(trigger?.closest('[role="dialog"], [role="alertdialog"]'))
  const header = document.querySelector('[data-app-shell] header')
  return {
    dialog,
    top: dialog ? 8 : Math.max(8, (header?.getBoundingClientRect().bottom ?? 0) + 8),
  }
}
