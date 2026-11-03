import { test, expect, selectMultiple } from './fixtures'
import pg from 'pg'

test('logs preserve alert context, include debug on demand and filter services independently', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    for (const service of ['tmdb', 'alldebrid', 'torbox']) {
      await db.query(
        `INSERT INTO ingest.activity_logs(event_id,level,kind,message,service)
         VALUES($1,'error','aiosource.incident',$2,$3)`,
        [`fixture-${service}`, `Failure fixture: ${service}`, service],
      )
    }
    await db.query(
      `INSERT INTO ingest.activity_logs(event_id,level,kind,message,provider_id)
       VALUES('fixture-source','info','service.started','Source fixture: tmdb','tmdb')`,
    )
    await db.query(
      `INSERT INTO ingest.activity_logs(event_id,level,kind,title,message,data)
       VALUES('fixture-alert','warn','aiosource.incident','Retained notification title','Information fixture: preserved context','{"retry":true}'),
             ('fixture-debug','debug','run.page','','Detailed request fixture','{}')`,
    )
    await page.goto('/logs')
    await expect(page.getByText('Retained notification title', { exact: true })).toBeVisible()
    await expect(
      page.getByText('Information fixture: preserved context', { exact: true }),
    ).toBeVisible()
    await expect(page.getByText('Detailed request fixture', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('checkbox', { name: 'debug', exact: true })).not.toBeChecked()
    const alert = page.getByRole('row').filter({ hasText: 'Retained notification title' })
    await alert.getByRole('button', { name: /^Metadata/ }).click()
    await expect(page.getByRole('dialog', { name: 'Event metadata', exact: true })).toContainText(
      '"retry": true',
    )
    await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).click()
    await page.getByRole('checkbox', { name: 'debug', exact: true }).check()
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click()
    await expect(page.getByText('Detailed request fixture', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect(page.getByText('Detailed request fixture', { exact: true })).toHaveCount(0)
    const scope = page.getByLabel('Source or service', { exact: true })
    await scope.click()
    for (const service of ['tmdb', 'alldebrid', 'torbox'])
      await expect(
        page.locator(`[role="menuitemcheckbox"][data-value="service:${service}"]`),
      ).toHaveCount(1)
    await page.keyboard.press('Escape')
    for (const service of ['tmdb', 'alldebrid', 'torbox']) {
      await selectMultiple(page, 'Source or service', [`service:${service}`])
      await page.getByRole('button', { name: 'Apply filters', exact: true }).click()
      await expect(page).toHaveURL(new RegExp(`/logs\\?service=${service}$`))
      await expect(page.getByText(`Failure fixture: ${service}`, { exact: true })).toBeVisible()
      await expect(page.getByText('Source fixture: tmdb', { exact: true })).toHaveCount(0)
      const link = page.locator('.table-frame').getByRole('link')
      await expect(link).toHaveCount(1)
      await expect(link).toHaveAttribute(
        'href',
        service === 'tmdb' ? '/metadata/videos' : '/debrid',
      )
      await page.reload()
      await expect(scope).toHaveText(
        { tmdb: 'TMDB', alldebrid: 'AllDebrid', torbox: 'TorBox' }[service]!,
      )
    }
    await page.goto('/logs?provider=tmdb')
    await expect(scope).toHaveText('tmdb')
    await expect(page.getByText('Source fixture: tmdb', { exact: true })).toBeVisible()
    await expect(page.getByText(/^Failure fixture:/)).toHaveCount(0)
    await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect(scope).toHaveText('All sources and service activity')
    await expect(page.getByText(/^Failure fixture:/)).toHaveCount(3)
    await expect(page.getByText('Source fixture: tmdb', { exact: true })).toBeVisible()
  } finally {
    await db.end()
  }
})
