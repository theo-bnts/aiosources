import { expect, type Page } from '@playwright/test'

// Follow the visible hierarchy by clicking each parent before its child. This
// also works for controlled disclosures and lazily loaded settings forms.
export async function openDisclosure(page: Page, title: string) {
  const escaped = title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const summary = page.locator('summary').filter({
    hasText: new RegExp(`^${escaped}\\s*(?:Configured)?$`),
  })
  await expect(summary).toHaveCount(1)
  const parents = summary.locator('xpath=ancestor::details')
  for (let index = 0; index < (await parents.count()); index++) {
    const parent = parents.nth(index)
    if ((await parent.getAttribute('open')) === null)
      await parent.locator(':scope > summary').click()
    await expect(parent).toHaveAttribute('open', '')
  }
}
