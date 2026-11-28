import pg from 'pg'
import { test, expect } from './fixtures'
import { openDisclosure } from './disclosures'

async function headers(page: import('@playwright/test').Page) {
  const session = await (await page.request.get('/api/session')).json()
  return { 'X-CSRF-Token': session.csrf_token }
}

test('access limits have independent saves, supported defaults and compatible bookmarks', async ({
  page,
}) => {
  await page.goto('/security/limits')
  for (const name of [
    'Administrator limits',
    'Trusted user limits',
    'Public request limits',
    'Configuration limits',
  ]) {
    await expect(
      page
        .locator('details')
        .filter({ has: page.locator(':scope > summary', { hasText: new RegExp(`^${name}$`) }) }),
    ).not.toHaveAttribute('open', '')
  }
  await openDisclosure(page, 'Administrator limits')
  const admin = page.getByRole('form', { name: 'Administrator limits', exact: true })
  await expect(
    admin.getByLabel('New administrator session duration (hours)', { exact: true }),
  ).toHaveValue('24')
  await expect(
    admin.getByLabel('Maximum active administrator sessions', { exact: true }),
  ).toHaveValue('10')
  await expect(
    admin.getByLabel('Maximum active administrator sessions', { exact: true }),
  ).toHaveAttribute('max', '100')
  await expect(admin.getByLabel('Total sign-in attempts per minute', { exact: true })).toHaveValue(
    '50',
  )
  await admin.getByLabel('Maximum active administrator sessions', { exact: true }).fill('101')
  await expect(admin.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await admin.getByLabel('Maximum active administrator sessions', { exact: true }).fill('9')
  await admin.getByLabel('New administrator session duration (hours)', { exact: true }).fill('48')
  await openDisclosure(page, 'Trusted user limits')
  const trusted = page.getByRole('form', { name: 'Trusted user limits', exact: true })
  await trusted.getByLabel('Maximum active sessions per trusted user', { exact: true }).fill('4')
  await admin.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(admin.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(trusted.getByRole('button', { name: 'Save', exact: true })).toBeEnabled()
  expect(
    (await (await page.request.get('/api/settings/runtime/trusted_security')).json()).values
      .session_limit,
  ).toBe(10)
  await trusted.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(trusted.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.goto('/public-access/limits')
  await expect(page).toHaveURL(/\/administration\/security\/limits#public-request-limits$/)
  await expect(page.getByLabel('Public requests per minute', { exact: true })).toBeVisible()
  await page.goto('/security/settings#sessions')
  await expect(page).toHaveURL(/\/administration\/security\/active-sessions#sessions$/)
  await page.goto('/public-access')
  await expect(page.getByRole('tab', { name: 'Request limits', exact: true })).toHaveCount(0)
})

test('metadata catalogs and credential creation links have consistent navigation', async ({
  page,
}) => {
  for (const [section, label] of [
    ['videos', 'Videos'],
    ['games', 'Games'],
    ['software', 'Software'],
    ['music', 'Music'],
    ['books', 'Books'],
  ]) {
    await page.goto(`/metadata/${section}/settings`)
    await expect(
      page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('listitem'),
    ).toHaveText(['Metadata/', 'Catalogs/', label])
    await expect(page.getByRole('button', { name: 'Catalogs', exact: true })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    if (section !== 'books') {
      const link = page.getByRole('link', {
        name:
          section === 'videos'
            ? 'Get TMDB read access token'
            : section === 'games'
              ? 'Get Twitch credentials'
              : section === 'software'
                ? 'Get NVD API key'
                : 'Get MetaBrainz Live Data Feed token',
        exact: true,
      })
      await expect(link).toHaveAttribute(
        'href',
        section === 'videos'
          ? 'https://www.themoviedb.org/settings/api/request'
          : section === 'games'
            ? 'https://dev.twitch.tv/console/apps/create'
            : section === 'software'
              ? 'https://nvd.nist.gov/developers/request-an-api-key'
              : 'https://metabrainz.org/profile',
      )
      await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    }
  }
  await page.goto('/metadata/identification/results')
  await expect(
    page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('listitem'),
  ).toHaveText(['Metadata/', 'Identification'])
})

test('the failed Stremio statistic selects every failed or interrupted outcome', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO aiosources.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results)
      SELECT now(),'movie','synthetic-'||outcome,1,outcome,0,0,0,0,0,0,0,false,'[]'
      FROM unnest(ARRAY['results','empty','failed','cancelled','invalid','busy','pending']) AS outcome`)
  } finally {
    await db.end()
  }
  await page.goto('/addon')
  await page.getByRole('link', { name: 'View failed or interrupted requests', exact: true }).click()
  await expect(page).toHaveURL(/status=failed,cancelled,invalid,busy$/)
  await expect(page.locator('main').getByText('4 requests', { exact: true })).toBeVisible()
  for (const outcome of ['failed', 'cancelled', 'invalid', 'busy'])
    await expect(
      page.getByRole('link', { name: `synthetic-${outcome}`, exact: true }),
    ).toBeVisible()
  await expect(page.getByRole('link', { name: 'synthetic-results', exact: true })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'synthetic-empty', exact: true })).toHaveCount(0)
})

test('occurrence windows retain identification filters, navigation, browser history and focus', async ({
  page,
  server,
}) => {
  const config = await (await page.request.get('/api/aiosource')).json()
  expect(
    (
      await page.request.put('/api/aiosource/config', {
        headers: await headers(page),
        data: { ...config.config, auto_match: false },
      })
    ).status(),
  ).toBe(204)
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('overlay-fixture','one','{"title":"Overlay Example"}');
      INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,revision,status,reason)
      SELECT provider_id,source_id,md5(fields::text),1,'ambiguous','multiple plausible works' FROM aiosources.torrents;
      DELETE FROM aiosources.aio_match_queue`)
  } finally {
    await db.end()
  }
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/metadata/identification/results?match_q=Overlay&match_limit=25')
    const originalEntry = await page.evaluate(() => history.state.idx)
    const link = page.getByRole('link', { name: 'Overlay Example', exact: true })
    await link.click()
    const dialog = page.getByRole('dialog', { name: 'Source occurrence', exact: true })
    await expect(dialog).toBeVisible()
    expect(new URL(page.url()).pathname).toBe('/metadata/identification/results')
    expect(new URL(page.url()).searchParams.get('match_q')).toBe('Overlay')
    await expect(
      page
        .getByRole('navigation', { name: 'Breadcrumb', includeHidden: true })
        .getByRole('listitem', { includeHidden: true }),
    ).toHaveText(['Metadata/', 'Identification'])
    await dialog.getByRole('tab', { name: 'Related occurrences', exact: true }).click()
    await expect(dialog.getByText('No related occurrences', { exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Close', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    await expect(link).toBeFocused()
    expect(await page.evaluate(() => history.state.idx)).toBe(originalEntry)
    expect(new URL(page.url()).searchParams.get('match_q')).toBe('Overlay')
    await link.click()
    await expect(dialog).toBeVisible()
    await page.goBack()
    await expect(dialog).toHaveCount(0)
    await page.goForward()
    await expect(dialog).toBeVisible()
    await page.reload()
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Close', exact: true }).click()
    expect(new URL(page.url()).pathname).toBe('/metadata/identification/results')
  }
  await page.goto('/torrents?q=Overlay')
  await expect(page).toHaveURL(/\/sources\/files\/explorer\?q=Overlay$/)
  await page.getByRole('link', { name: 'Overlay Example', exact: true }).click()
  await expect(
    page.getByRole('dialog').getByRole('tab', { name: 'Published fields', exact: true }),
  ).toHaveAttribute('data-state', 'active')
})

test('optional pagination controls are spaced on desktop and mobile', async ({ page, server }) => {
  expect(
    (
      await page.request.put('/api/providers/layout-fixture', {
        headers: await headers(page),
        data: {
          revision: '',
          json: JSON.stringify({
            version: 1,
            id: 'layout-fixture',
            name: 'Layout fixture',
            adapter: 'json',
            url: server.sourceURL,
            enabled: false,
            http: { items_path: '/items' },
            pagination: { type: 'page', page_param: 'page', size_param: 'limit' },
            mapping: { id: '/id', fields: { title: '/title' } },
          }),
        },
      })
    ).status(),
  ).toBe(200)
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/providers/id/layout-fixture')
    await page.getByRole('tab', { name: /^Pagination/ }).click()
    const other = page.locator('details[data-settings-section="Other pagination parameters"]')
    const checks = page.locator('details[data-settings-section="Response checks"]')
    await expect(other).toBeVisible()
    await expect(checks).toBeVisible()
    const a = await other.boundingBox(),
      b = await checks.boundingBox()
    expect(b!.y - (a!.y + a!.height)).toBeGreaterThanOrEqual(16)
    await openDisclosure(page, 'Response checks')
    await expect(page.getByLabel('Total pointer', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Current page pointer', { exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
})
