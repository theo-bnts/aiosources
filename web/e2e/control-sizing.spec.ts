import type { Locator, Page } from '@playwright/test'
import { test, expect } from './fixtures'

async function expectConsistentControls(page: Page) {
  await expect
    .poll(async () =>
      page.evaluate(() => {
        const expected = innerWidth < 768 ? 44 : 40
        const issues: string[] = []
        const selectors = [
          '[data-slot="input"]',
          '[data-slot="button"]',
          '[data-slot="switch"]',
          '[data-slot="alert-dialog-action"]',
          '[data-slot="alert-dialog-cancel"]',
          '[data-slot="disclosure-trigger"]',
          '[data-slot="tabs-list"]',
          '.native-select',
          '[role="menuitem"]',
          '[role="menuitemcheckbox"]',
        ]
        for (const element of document.querySelectorAll<HTMLElement>(selectors.join(','))) {
          const box = element.getBoundingClientRect()
          if (!box.width || !box.height || getComputedStyle(element).visibility === 'hidden')
            continue
          const slot = element.dataset.slot
          const fontSize =
            innerWidth < 768 &&
            (slot === 'input' ||
              element.hasAttribute('data-input-like') ||
              element.classList.contains('native-select'))
              ? '16px'
              : '14px'
          const label = element.getAttribute('aria-label') || element.id || slot || element.tagName
          const disclosure = slot === 'disclosure-trigger'
          const expectedHeight =
            disclosure && element.parentElement?.dataset.variant === 'card'
              ? expected - 2
              : expected
          const multiline =
            element.className.includes('h-auto') ||
            (slot === 'button' &&
              !element.dataset.size?.startsWith('icon') &&
              Boolean(element.closest('[data-slot="disclosure-content"]'))) ||
            (disclosure &&
              element.querySelector('span')!.getBoundingClientRect().height >
                parseFloat(getComputedStyle(element).lineHeight) + 1) ||
            element.getAttribute('role')?.startsWith('menuitem')
          if (
            multiline
              ? box.height < expectedHeight - 0.5
              : Math.abs(box.height - expectedHeight) > 0.5
          )
            issues.push(`${label}: height ${box.height}, expected ${expectedHeight}`)
          if (
            slot !== 'switch' &&
            slot !== 'tabs-list' &&
            !element.classList.contains('font-mono') &&
            getComputedStyle(element).fontSize !== fontSize
          )
            issues.push(
              `${label}: font ${getComputedStyle(element).fontSize}, expected ${fontSize}`,
            )
        }
        if (document.documentElement.scrollWidth > innerWidth)
          issues.push('The page overflows the viewport')
        return issues
      }),
    )
    .toEqual([])
}

async function openOptionalSections(scope: Locator) {
  for (let i = 0; i < 50; i++) {
    const closed = scope.locator('details:not([open]) > summary:visible').first()
    if (!(await closed.count())) return
    const parent = await closed.locator('..').elementHandle()
    await closed.click({ position: { x: 8, y: 12 } })
    await expect.poll(() => parent?.evaluate((element) => element.hasAttribute('open'))).toBe(true)
  }
  throw new Error('Optional settings did not finish opening')
}

for (const width of [390, 1440]) {
  test(`control dimensions stay consistent across workspaces and optional settings at ${width}px`, async ({
    page,
  }) => {
    test.setTimeout(120_000)
    await page.setViewportSize({ width, height: 1100 })
    for (const route of [
      '/search',
      '/torrents',
      '/providers/settings',
      '/remotes/settings',
      '/metadata/videos/settings',
      '/metadata/books/settings',
      '/metadata/identification',
      '/metadata/identification/settings',
      '/debrid/settings',
      '/addon/settings',
      '/torznab',
      '/sharing/settings',
      '/security/secrets',
      '/security/verification',
      '/security/sessions',
      '/health/settings',
      '/network/incoming',
      '/network/outgoing',
      '/network/outgoing/routing',
      '/public-access',
      '/public-access/limits',
      '/runs',
      '/logs',
    ]) {
      await page.goto(route)
      await expect(page.locator('main')).toBeVisible()
      await expect(page.locator('main [data-slot], main input').first()).toBeVisible()
      await expect(
        page.locator('main [role="status"]').filter({ hasText: /^(Loading|Searching)/ }),
      ).toHaveCount(0)
      await openOptionalSections(page.locator('main'))
      await expectConsistentControls(page)
    }
  })
}

test('connection forms, source templates and header dialogs use the same responsive controls', async ({
  page,
}) => {
  test.setTimeout(120_000)
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    for (const [route, action] of [
      ['/addon/settings', 'Create access'],
      ['/torznab', 'Create access'],
      ['/sharing/settings', 'Create share'],
      ['/remotes/settings', 'Add remote catalog'],
      ['/network/outgoing', 'Add proxy'],
    ]) {
      await page.goto(route)
      await page.getByRole('button', { name: action, exact: true }).click()
      const dialog = page.getByRole('dialog')
      await expect(dialog).toBeVisible()
      await openOptionalSections(dialog)
      await expectConsistentControls(page)
      const dropdown = dialog.locator('button[aria-haspopup="menu"]').first()
      if (await dropdown.count()) {
        await dropdown.click()
        await expect(page.getByRole('menu')).toBeVisible()
        await expectConsistentControls(page)
        await page.keyboard.press('Escape')
      }
      await dialog.getByRole('button', { name: 'Close', exact: true }).click()
    }
    await page.goto('/providers/new')
    await page
      .getByRole('row')
      .filter({ has: page.getByText('HTML', { exact: true }) })
      .getByRole('button', { name: 'Use this template', exact: true })
      .click()
    const stages = page.getByRole('tablist', { name: 'Source configuration stages', exact: true })
    for (const stage of await stages.getByRole('tab').all()) {
      await stage.click()
      await openOptionalSections(page.locator('main'))
      await expectConsistentControls(page)
    }
    for (const name of ['Search settings', 'Interface preferences', 'About AIOSources']) {
      await page.getByRole('button', { name, exact: true }).click()
      await expect(page.getByRole('dialog')).toBeVisible()
      await expectConsistentControls(page)
      await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).click()
    }
  }
})
