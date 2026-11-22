import pg from 'pg'
import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

test('Software uses independent settings blocks, catalog filters and responsive navigation', async ({
  page,
}) => {
  await page.goto('/metadata/software/settings')
  await expect(page.getByRole('heading', { name: 'Software', exact: true, level: 1 })).toBeVisible()
  await expect(
    page.getByRole('tablist', { name: 'Software views', exact: true }).getByRole('tab'),
  ).toHaveText(['Statistics', 'Settings', 'History'])
  await expect(
    page.getByLabel('Automatically update software information', { exact: true }),
  ).not.toBeChecked()

  await openDisclosure(page, 'Catalog filters')
  const catalog = page.getByRole('form', { name: 'Software catalog filters', exact: true })
  await expect(
    catalog.getByRole('switch', {
      name: 'Include commercial & vendor catalog (NVD CPE)',
      exact: true,
    }),
  ).toBeChecked()
  await expect(
    catalog.getByRole('switch', {
      name: 'Exclude web plugins, CMS extensions and developer libraries',
      exact: true,
    }),
  ).toBeChecked()
  await expect(
    catalog.getByRole('switch', { name: 'Include operating systems', exact: true }),
  ).not.toBeChecked()
  const iconsSwitch = catalog.getByRole('switch', {
    name: 'Include software icons index',
    exact: true,
  })
  await expect(iconsSwitch).toBeChecked()
  await iconsSwitch.uncheck()
  await catalog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(catalog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await openDisclosure(page, 'Automatic updates')
  const updates = page.getByRole('form', { name: 'Software automatic updates', exact: true })
  await updates
    .getByLabel('Software information update interval (minutes)', { exact: true })
    .fill('180')
  await updates.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(updates.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await page.reload()
  await openDisclosure(page, 'Catalog filters')
  await expect(
    page.getByRole('switch', { name: 'Include software icons index', exact: true }),
  ).not.toBeChecked()
  await openDisclosure(page, 'Automatic updates')
  await expect(
    page.getByLabel('Software information update interval (minutes)', { exact: true }),
  ).toHaveValue('180')

  await openDisclosure(page, 'Manual actions')
  await expect(
    page.getByRole('button', { name: 'Update software information', exact: true }),
  ).toBeEnabled()
  await openDisclosure(page, 'Maintenance')
  await expect(
    page.getByRole('button', { name: 'Reset software information', exact: true }),
  ).toBeEnabled()

  await page.setViewportSize({ width: 390, height: 844 })
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
  ).toBeLessThanOrEqual(0)
})

test('Optional NVD API key uses Secrets and never appears in configuration responses', async ({
  page,
}) => {
  await page.goto('/metadata/software/settings')
  await page.getByRole('button', { name: 'Add NVD API key', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('NVD API key (optional)', { exact: true }).fill('synthetic-nvd-api-key')
  await editor.getByRole('button', { name: 'Store and select', exact: true }).click()
  await expect(editor).toHaveCount(0)
  const storedNotice = page.getByText(
    'Stored securely and selected. The selection applies when this configuration is saved. The entry remains in Secrets if you discard these changes.',
    { exact: true },
  )
  await expect(storedNotice).toBeVisible()
  const main = page.getByRole('form', { name: 'Software configuration', exact: true })
  await main.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(main.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(storedNotice).toHaveCount(0)

  const response = await page.request.get('/api/aiosource/controls')
  const body = await response.text()
  expect(body).not.toContain('synthetic-nvd-api-key')
  expect(JSON.parse(body).config.software_nvd_secret).toMatch(/^software_nvd_api_key_/)

  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Software', exact: true, level: 1 })).toBeVisible()
  expect(
    (await (await page.request.get('/api/operations?kind=metadata&service=software')).json()).items,
  ).toEqual([])
})

test('Software history renders NVD feed labels, HTTP diagnostics and safe failure messages', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO aiosources.operation_runs(id,kind,service,trigger,resource,lock_key,status,phase,processed,errors,results,created_at,finished_at,error)
      VALUES
      ('software-ok','metadata','software','manual','software',0,'succeeded','completed',1240,0,'{"nvd_checked":1,"sources_synced":4,"full_import":1}',now()-interval '2 seconds',now(),''),
      ('software-legacy','metadata','software','automatic','software',0,'failed','software_nvd_incremental',0,1,'{"snapshot_checked":1}',now()-interval '1 second',now(),'NIST NVD rejected the configured API key. Check the secret.')`)
    await db.query(`INSERT INTO aiosources.operation_events(run_id,phase,processed,errors,duration_ms,http_status,error_code)
      VALUES
      ('software-ok','configuration',0,0,0,0,''),
      ('software-ok','software_nvd_meta',0,0,0,0,''),
      ('software-ok','software_nvd_meta',0,0,145,200,''),
      ('software-ok','software_winget',820,0,0,0,''),
      ('software-ok','software_winget',0,0,310,304,''),
      ('software-ok','succeeded',1240,0,0,0,''),
      ('software-legacy','software_nvd_incremental',0,0,0,0,''),
      ('software-legacy','software_nvd_incremental',0,1,210,403,'authentication'),
      ('software-legacy','failed',0,1,0,0,'')`)
  } finally {
    await db.end()
  }

  await page.goto('/metadata/software/history')
  await expect(page.getByRole('link', { name: 'Information update', exact: true })).toHaveCount(2)
  await expect(
    page.getByText('NIST NVD rejected the configured API key. Check the secret.', { exact: true }),
  ).toBeVisible()

  await page.getByRole('link', { name: 'Open run software-ok', exact: true }).click()
  await expect(page.locator('dl')).toContainText('NVD feed checked')
  await expect(page.locator('dl')).toContainText('Software sources updated')
  await expect(page.locator('dl')).toContainText('Full NVD snapshot imported')
  await expect(page.locator('dl')).not.toContainText('Cleanup records checked')

  const okEvents = page.getByRole('region', { name: 'Operation events', exact: true })
  await expect(okEvents).toContainText('Checking NIST NVD CPE feed')
  await expect(okEvents).toContainText('Request NIST NVD CPE metadata')
  await expect(okEvents).toContainText('145 ms · HTTP 200')
  await expect(okEvents).toContainText('Importing Microsoft WinGet packages')
  await expect(okEvents).toContainText('Download Microsoft WinGet archive')
  await expect(okEvents).toContainText('310 ms · HTTP 304')

  await page.goto('/metadata/software/history/software-legacy')
  await expect(page.getByRole('alert')).toHaveText(
    'NIST NVD rejected the configured API key. Check the secret.',
  )
  await expect(page.locator('dl')).toContainText('NVD feed checked')
  await expect(page.locator('dl')).not.toContainText('Cleanup records checked')
  const failEvents = page.getByRole('region', { name: 'Operation events', exact: true })
  await expect(failEvents).toContainText('Request NIST NVD CPE changes')
  await expect(failEvents).toContainText('210 ms · HTTP 403 · Key or subscription rejected')
})
