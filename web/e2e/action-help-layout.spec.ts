import { test, expect } from './fixtures'

for (const width of [390, 1440]) {
  test(`help and action areas keep compact spacing at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1100 })
    let checked = 0
    for (const path of [
      '/metadata/videos/settings',
      '/metadata/books/settings',
      '/debrid/settings',
      '/health/settings',
    ]) {
      await page.goto(path)
      await expect(page.locator('main')).toBeVisible()
      await expect(page.locator('main [data-slot="disclosure-trigger"]').first()).toBeVisible()
      const result = await page.evaluate(() => {
        const issues: string[] = []
        let checked = 0
        for (const summary of document.querySelectorAll<HTMLElement>(
          'main details:not([open]) > summary',
        )) {
          const help = summary.querySelector<HTMLElement>('[data-slot="info-trigger"]')
          if (!help || !summary.getBoundingClientRect().height) continue
          const before = summary.getBoundingClientRect().height
          const display = help.style.display
          help.style.display = 'none'
          const without = summary.getBoundingClientRect().height
          help.style.display = display
          checked++
          if (before !== without) issues.push(`${summary.textContent}: ${before} / ${without}`)
        }
        for (const actions of document.querySelectorAll<HTMLElement>('main .card-actions')) {
          if (getComputedStyle(actions).borderTopWidth !== '0px')
            issues.push('Actions have a separator')
        }
        return { checked, issues }
      })
      checked += result.checked
      expect(result.issues).toEqual([])
    }
    expect(checked).toBeGreaterThan(0)
  })
}
