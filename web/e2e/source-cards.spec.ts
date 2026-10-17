import pg from 'pg'
import { test, expect } from './fixtures'

test.describe('source cards', () => {
  test.use({ collectionSource: true })

  test('schedule precedes Last run and Next disappears while an import is active or blocked', async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const source = {
      version: 1,
      id: 'example',
      name: 'Example collection',
      adapter: 'http_json',
      enabled: true,
      url: server.sourceURL,
      schedule: { enabled: true, every: '1h' },
    }
    const document = await page.request.put('/api/providers/example', {
      headers: { 'X-CSRF-Token': session.csrf_token },
      data: { json: JSON.stringify(source), revision: '' },
    })
    expect(document.status()).toBe(200)
    const revision = (await document.json()).revision
    const db = new pg.Client({ connectionString: server.databaseURL })
    await db.connect()
    try {
      await db.query(
        `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
        VALUES('card-run','example','Example collection','full','succeeded',$1,$2,now())`,
        [revision, source],
      )
      await page.reload()
      const card = page.getByRole('region', { name: 'Source Example collection', exact: true })
      await expect(card).toBeVisible()
      await expect(card.locator('dt')).toHaveText(['Schedule', 'Last run'])
      await expect(card.getByRole('link', { name: 'Every 1 hour', exact: true })).toHaveAttribute(
        'href',
        '/providers/example#schedules',
      )
      await expect(card.getByRole('link', { name: 'Completed', exact: true })).toHaveAttribute(
        'href',
        '/runs/card-run',
      )
      await expect(card.getByText(/^Next:/)).toBeVisible()
      await expect(card.getByText('Adapter', { exact: true })).toHaveCount(0)
      await expect(card.getByText('Configuration', { exact: true })).toHaveCount(0)
      await expect(
        card.getByRole('button', { name: 'Run manual import', exact: true }),
      ).toBeEnabled()
      await expect(card.getByRole('link', { name: 'Edit', exact: true })).toBeVisible()
      await expect(card.getByRole('button', { name: 'Delete', exact: true })).toBeVisible()

      await db.query(`UPDATE ingest.runs SET status='running',finished_at=NULL WHERE id='card-run';
        UPDATE ingest.schedule_clocks SET last_error='An active collection prevented this scheduled attempt; the next scheduled tick will retry' WHERE provider_id='example'`)
      await page.reload()
      await expect(card.getByText('Running', { exact: true })).toBeVisible()
      await expect(card.getByText(/^Next:/)).toHaveCount(0)
      await expect(card).not.toContainText('Import in progress')
      await expect(card).not.toContainText('An active collection prevented')
      await expect(
        card.getByRole('button', { name: 'Run manual import', exact: true }),
      ).toBeDisabled()

      await db.query(
        `UPDATE ingest.runs SET status='succeeded',finished_at=now() WHERE id='card-run'`,
      )
      await page.reload()
      await expect(card.getByText(/^Next:/)).toBeVisible()
      await expect(card).not.toContainText('An active collection prevented')

      await db.query(
        `UPDATE ingest.runs SET status='paused',pause_reason='manual' WHERE id='card-run'`,
      )
      await page.reload()
      await expect(card.getByText('Paused', { exact: true })).toBeVisible()
      await expect(card.getByText(/^Next:/)).toHaveCount(0)
      await expect(card).toContainText('Existing run needs review in History')
      await page.setViewportSize({ width: 390, height: 844 })
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      expect(server.sourceRequests()).toBe(0)
    } finally {
      await db.end()
    }
  })
})

test('public classification disables announce editing without deleting the saved secret reference', async ({
  page,
  server,
}) => {
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token }
  expect(
    (
      await page.request.put('/api/providers/example', {
        headers,
        data: {
          json: JSON.stringify({
            version: 1,
            id: 'example',
            name: 'Example source',
            adapter: 'http_json',
            enabled: false,
            url: server.sourceURL,
          }),
          revision: '',
        },
      })
    ).status(),
  ).toBe(200)
  expect(
    (
      await page.request.put('/api/secrets/example-announce', {
        headers,
        data: { revision: 0, value: 'https://announce.example.invalid/synthetic-secret' },
      })
    ).status(),
  ).toBe(204)
  const { config } = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers,
        data: {
          ...config,
          trackers: { example: { private: true, announce_secret: 'example-announce' } },
        },
      })
    ).status(),
  ).toBe(204)
  await page.goto('/debrid/settings')
  const form = page.getByRole('form', { name: 'Private source playback', exact: true })
  const privateSource = form.getByRole('switch', { name: 'Private source', exact: true })
  const announce = form.getByLabel('Announce URL secret', { exact: true })
  await expect(announce).toBeEnabled()
  await expect(announce).toHaveValue('example-announce')
  await privateSource.uncheck()
  await expect(announce).toBeDisabled()
  await expect(form.getByRole('button', { name: 'Add announce URL', exact: true })).toBeDisabled()
  await privateSource.check()
  await expect(announce).toBeEnabled()
  await expect(announce).toHaveValue('example-announce')
  await privateSource.uncheck()
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(form.getByText('Changes saved.', { exact: true })).toBeVisible()
  await page.reload()
  await expect(privateSource).not.toBeChecked()
  await expect(announce).toBeDisabled()
  await expect(announce).toHaveValue('example-announce')
})
