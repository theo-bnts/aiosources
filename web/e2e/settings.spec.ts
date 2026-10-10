import type { Page } from '@playwright/test'
import type { CollectionSettings } from '../src/lib/collection-types'
import { test, expect } from './fixtures'

async function savedSettings(page: Page): Promise<CollectionSettings> {
  const response = await page.request.get('/api/settings/collections')
  expect(response.status()).toBe(200)
  return (await response.json()).settings
}

test('advanced import controls load on demand and folding preserves drafts and navigation protection', async ({
  page,
}) => {
  const original = await savedSettings(page)
  const requests: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/settings/collections')
      requests.push(request.method())
  })
  await page.goto('/providers/settings')
  const section = page.locator('#advanced-import-settings')
  const toggle = section.locator('summary')
  const form = section.getByRole('form', { name: 'Advanced settings', exact: true })
  await expect(section).not.toHaveAttribute('open', '')
  await expect(form).toHaveCount(0)
  expect(requests).toEqual([])
  await toggle.click()
  const workers = form.getByLabel('Simultaneous imports', { exact: true })
  await expect(workers).toHaveValue(String(original.workers))
  const changed = original.workers === 7 ? '6' : '7'
  await workers.fill(changed)
  await toggle.click()
  await expect(form).toBeHidden()
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  const prompt = page.getByRole('alertdialog')
  await expect(prompt).toContainText('advanced settings')
  await prompt.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await toggle.click()
  await expect(workers).toHaveValue(changed)
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  expect(requests.filter((method) => method === 'PUT')).toEqual(['PUT'])
  await toggle.click()
  await page.reload()
  await expect(form).toHaveCount(0)
  await toggle.click()
  await expect(section).toHaveAttribute('open', '')
  await expect.poll(() => new URL(page.url()).searchParams.get('settings')).toBe('advanced')
  await expect(workers).toHaveValue(changed)
  await page.goto('/import-settings')
  await expect(workers).toHaveValue(changed)
  await expect(page).toHaveURL(/\/providers\/settings\?settings=advanced#advanced-import-settings$/)
})

test('moved import settings preserve validation, persisted policies and concurrent-edit drafts', async ({
  page,
  server,
}) => {
  const original = await savedSettings(page)
  await page.goto('/import-settings')
  const form = page.getByRole('form', { name: 'Advanced settings', exact: true })
  const workers = form.getByLabel('Simultaneous imports', { exact: true })
  const progress = form.getByLabel('Responses without new source IDs', { exact: true })
  const save = form.getByRole('button', { name: 'Save', exact: true })
  await expect(form.locator('details')).toHaveCount(0)
  await expect(
    form.getByLabel('Automatically resume technically interrupted runs', { exact: true }),
  ).toHaveCount(0)
  await expect(form.getByLabel('Automatic retries after HTTP 429', { exact: true })).toBeVisible()
  await expect(form.getByLabel('Request timeout (seconds)', { exact: true })).toHaveCount(0)
  await expect(form.getByLabel('Preview page budget', { exact: true })).toHaveCount(0)
  expect(Object.keys(original).sort()).toEqual([
    'max_quota_retries',
    'max_quota_wait_seconds',
    'no_progress_action',
    'no_progress_requests',
    'revision',
    'workers',
  ])
  await expect(workers).toHaveValue(String(original.workers))
  await workers.fill('0')
  await expect(workers).toHaveAttribute('aria-invalid', 'true')
  await expect(save).toBeDisabled()
  expect(await savedSettings(page)).toEqual(original)

  await workers.fill('3')
  await progress.fill('7')
  const saved = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/settings/collections') && response.request().method() === 'PUT',
  )
  await save.click()
  expect((await saved).status()).toBe(200)
  await expect(save).toBeDisabled()
  const updated = await savedSettings(page)
  expect(updated).toEqual({
    ...original,
    workers: 3,
    no_progress_requests: 7,
    revision: updated.revision,
  })
  expect(updated.revision).toBeGreaterThan(original.revision)
  await page.reload()
  await expect(workers).toHaveValue('3')
  await expect(progress).toHaveValue('7')
  await expect(page.getByLabel('Display timezone', { exact: true })).toHaveCount(0)

  await progress.fill('11')
  const session = await (await page.request.get('/api/session')).json()
  let releaseSave!: () => void
  const heldSave = new Promise<void>((resolve) => {
    releaseSave = resolve
  })
  await page.route('**/api/settings/collections', async (route) => {
    if (route.request().method() === 'PUT') await heldSave
    await route.continue()
  })
  try {
    const submitted = page.waitForRequest(
      (request) =>
        request.url().endsWith('/api/settings/collections') && request.method() === 'PUT',
    )
    const rejected = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/settings/collections') &&
        response.request().method() === 'PUT',
    )
    await save.click()
    await submitted
    // Let a real competing write finish after submission but before this request reaches the server.
    // An earlier write may instead be detected by the live refresh, correctly disabling Save.
    const remote = await page.request.put('/api/settings/collections', {
      headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
      data: { ...updated, max_quota_retries: 6 },
    })
    expect(remote.status()).toBe(200)
    releaseSave()
    expect((await rejected).status()).toBe(409)
  } finally {
    releaseSave()
    // A response can arrive before its routing callback finishes. Drain the
    // delayed save and concurrent refresh before changing interception rules.
    await page.unrouteAll({ behavior: 'wait' })
  }
  await expect(form.getByRole('alert')).toContainText('Your unsaved draft is preserved.')
  await expect(progress).toHaveValue('11')
  await expect(save).toBeDisabled()
  await form.getByRole('button', { name: 'Keep my draft', exact: true }).click()
  const retried = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/settings/collections') && response.request().method() === 'PUT',
  )
  await save.click()
  expect((await retried).status()).toBe(200)
  await expect(save).toBeDisabled()
  const resolved = await savedSettings(page)
  expect(resolved).toEqual({ ...updated, no_progress_requests: 11, revision: resolved.revision })
  expect(resolved.revision).toBeGreaterThan(updated.revision)
})

test('browser timestamps and fixed 50-row pages ignore retired display preferences', async ({
  page,
}) => {
  const initial = await savedSettings(page)
  await page.addInitScript(() =>
    localStorage.setItem(
      'torrent-scrapers.display-preferences',
      JSON.stringify({ timezone: 'America/New_York', pageSize: 25 }),
    ),
  )
  await page.goto('/settings')
  await expect(page).toHaveURL(/\/providers\/settings$/)
  await expect(page.getByRole('heading', { name: 'Sources', exact: true, level: 1 })).toBeVisible()
  await expect(page.getByLabel('Display timezone', { exact: true })).toHaveCount(0)
  await page.goto('/torrents?limit=100')
  await expect(page.getByLabel('Rows per page', { exact: true })).toHaveCount(0)
  await page.goto('/health')
  const report = await (await page.request.get('/api/system-health')).json()
  const measured = await page.evaluate(
    (value) =>
      new Intl.DateTimeFormat('en-GB', { dateStyle: 'medium', timeStyle: 'short' }).format(
        new Date(value),
      ),
    report.database.measured_at,
  )
  await expect(page.getByText(`Measured ${measured}.`, { exact: true }).first()).toBeVisible()
  expect(await savedSettings(page)).toEqual(initial)
})
