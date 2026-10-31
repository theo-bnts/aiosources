import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { test, expect } from './fixtures'

test('book catalog filters keep their defaults, save one coherent scope and fit mobile', async ({
  page,
}) => {
  await page.goto('/metadata/books/settings')
  const form = page.getByRole('form', { name: 'Books configuration', exact: true })
  const relevance = page.getByRole('switch', { name: 'Relevant editions only', exact: true })
  const languages = page.getByRole('switch', { name: 'Filter book languages', exact: true })
  const selection = page.getByRole('button', { name: 'Edition languages', exact: true })
  await expect(relevance).toBeChecked()
  await expect(languages).not.toBeChecked()
  await expect(selection).toBeDisabled()
  await languages.check()
  await expect(selection).toContainText('Browser language (English)')
  await selection.click()
  await page.getByRole('menuitemcheckbox', { name: 'French', exact: true }).click()
  await page.keyboard.press('Escape')
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await (await page.request.get('/api/aiosource/controls')).json()).config).toMatchObject({
    openlibrary_relevant_only: true,
    openlibrary_languages: ['en', 'fr'],
  })
  await page.reload()
  await expect(selection).toContainText('French')
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    await expect(relevance).toBeVisible()
    await expect(selection).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    if (process.env.QA_DIRECTORY) {
      await mkdir(process.env.QA_DIRECTORY, { recursive: true })
      await page.screenshot({
        path: join(process.env.QA_DIRECTORY, `book-filters-${width}.png`),
        fullPage: true,
      })
    }
  }
  await languages.uncheck()
  await relevance.uncheck()
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect((await (await page.request.get('/api/aiosource/controls')).json()).config).toMatchObject({
    openlibrary_relevant_only: false,
    openlibrary_languages: [],
  })
})
