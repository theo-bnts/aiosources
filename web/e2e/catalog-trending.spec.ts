import pg from 'pg'
import { test, expect } from './fixtures'

test('trending ranks categories across all periods, scrolls horizontally and returns cleanly from title details', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    const now = Date.now()
    const isoDaysAgo = (days: number, hours = 0) =>
      new Date(now - (days * 24 + hours) * 3600 * 1000).toISOString()

    const insertWork = async (
      kind: 'movie' | 'tv' | 'game' | 'software' | 'music' | 'book',
      key: string,
      title: string,
      years: number[],
      extra = '',
    ) => {
      const tmdb = kind === 'movie' || kind === 'tv' ? key : null
      const igdb = kind === 'game' ? key : ''
      const ol = kind === 'book' ? key : ''
      const mb = kind === 'music' ? key : ''
      const sw = kind === 'software' ? key : ''
      const res = await db.query(
        `INSERT INTO aiosources.aio_works(
          media_type,tmdb_id,igdb_id,openlibrary_id,musicbrainz_id,software_key,display_title,years
        ) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
        [kind, tmdb, igdb, ol, mb, sw, title, years],
      )
      const id = Number(res.rows[0].id)
      if (extra) await db.query(extra, [id])
      return id
    }

    let seq = 0
    const addTorrents = async (
      workID: number,
      count: number,
      daysAgo: number,
      hoursAgo = 1,
      titleForIndex?: (index: number) => string,
    ) => {
      for (let i = 0; i < count; i++) {
        seq++
        const sourceID = `trend-${seq}`
        const publishedAt = isoDaysAgo(daysAgo, hoursAgo)
        await db.query(
          `INSERT INTO aiosources.torrents(provider_id,source_id,fields,first_seen_at) VALUES('fixture',$1,$2::jsonb,$3)`,
          [
            sourceID,
            JSON.stringify({
              title: titleForIndex ? titleForIndex(i) : `Release ${seq}`,
              info_hash: seq.toString(16).padStart(40, '0'),
              published_at: publishedAt,
            }),
            publishedAt,
          ],
        )
        await db.query(
          `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
           SELECT t.provider_id,t.source_id,t.fields_fingerprint,$2,w.revision,'matched','fixture'
           FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id=$2
           WHERE t.provider_id='fixture' AND t.source_id=$1`,
          [sourceID, workID],
        )
      }
    }

    const dailyMovie = await insertWork('movie', '91001', 'Daily Spotlight', [2026])
    const weeklySeries = await insertWork('tv', '91002', 'Weekly Saga', [2025, 2026])
    const dailySoap = await insertWork('tv', '91006', 'Endless Daily Soap', [2024, 2026])
    const monthlyMovie = await insertWork('movie', '91003', 'Monthly Hit', [2024])
    const quarterMovie = await insertWork('movie', '91004', 'Quarter Favorite', [2023])
    const yearlyMovie = await insertWork('movie', '91005', 'Yearly Champion', [2022])

    await addTorrents(dailyMovie, 2, 0, 2)
    // Weekly Saga: 2 episodes x 4 torrents = 8 total -> 4/ep (top in week).
    await addTorrents(weeklySeries, 8, 3, 1, (i) => `Weekly.Saga.S02E0${(i % 2) + 1}.R${i}.1080p`)
    // Endless Daily Soap: 10 episodes x 1.5 avg (15 torrents across 10 eps = 1.5/ep) -> more raw torrents than Weekly Saga and Daily Spotlight, yet ranked below both!
    await addTorrents(
      dailySoap,
      15,
      3,
      1,
      (i) => `Endless.Daily.Soap.S08E${String((i % 10) + 1).padStart(2, '0')}.R${i}.1080p`,
    )
    await addTorrents(monthlyMovie, 6, 15)
    await addTorrents(quarterMovie, 8, 60)
    await addTorrents(yearlyMovie, 10, 200)

    // Add enough weekly video titles to require horizontal scrolling on desktop.
    for (let i = 1; i <= 12; i++) {
      const filler = await insertWork('movie', `${92000 + i}`, `Extra Film ${i}`, [2026])
      await addTorrents(filler, 1, 2)
    }

    const game = await insertWork('game', '81001', 'Arcade Horizon', [2026])
    const software = await insertWork(
      'software',
      'winget:videolan.vlc',
      'VLC Media Player',
      [],
      `UPDATE aiosources.aio_works SET software_data='{"vendor":"VideoLAN"}'::jsonb WHERE id=$1`,
    )
    const music = await insertWork(
      'music',
      '11111111-1111-4111-8111-111111111111',
      'Neon Echoes',
      [2025],
      `UPDATE aiosources.aio_works SET music_data='{"artists":["Synthwave"],"primary_type":"Album"}'::jsonb WHERE id=$1`,
    )
    const book = await insertWork('book', 'OL81001W', 'Orbital Manual', [2024])

    await addTorrents(game, 3, 2)
    await addTorrents(software, 2, 2)
    await addTorrents(music, 2, 2)
    await addTorrents(book, 2, 2)
    await db.query('DELETE FROM aiosources.aio_match_queue')
  } finally {
    await db.end()
  }

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/trending')
  await expect(page.getByRole('heading', { name: 'Trending', exact: true, level: 1 })).toBeVisible()

  // All 5 categories appear in order from top to bottom.
  const headings = page.locator('main h2')
  await expect(headings).toHaveText(['Videos', 'Games', 'Software', 'Music', 'Books'])

  const period = page.getByLabel('Period', { exact: true })
  await expect(period).toHaveValue('week')
  await expect(period.locator('option')).toHaveText([
    'Past day',
    'Past week',
    'Past month',
    'Past quarter',
    'Past year',
  ])

  const videosSection = page.locator('main section').first()
  const videoLinks = videosSection.getByRole('link')
  await expect(videoLinks.first()).toContainText('Weekly Saga')
  await expect(videoLinks.first()).toContainText('Series · 2025')
  await expect(videoLinks.first()).toContainText('4 torrents/episode')
  await expect(
    videoLinks.first().locator('[title="8 torrents · 2 episodes (4 torrents/episode)"]'),
  ).toBeVisible()
  await expect(videoLinks.nth(1)).toContainText('Daily Spotlight')
  await expect(videoLinks.nth(1)).toContainText('Movie · 2026')
  await expect(videoLinks.nth(1)).toContainText('2 torrents')
  await expect(videoLinks.nth(2)).toContainText('Endless Daily Soap')
  await expect(videoLinks.nth(2)).toContainText('Series · 2024')
  await expect(videoLinks.nth(2)).toContainText('2 torrents/episode')

  // Cards sit side by side horizontally (same row y, increasing x) without page overflow.
  const firstBox = (await videoLinks.nth(0).boundingBox())!
  const secondBox = (await videoLinks.nth(1).boundingBox())!
  expect(Math.abs(firstBox.y - secondBox.y)).toBeLessThan(2)
  expect(secondBox.x).toBeGreaterThan(firstBox.x + firstBox.width / 2)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)

  // Horizontal scroll button scrolls the Videos rail.
  const scrollRight = videosSection.getByRole('button', {
    name: 'Scroll Videos right',
    exact: true,
  })
  await expect(scrollRight).toBeEnabled()
  await scrollRight.click()
  await expect
    .poll(() => videosSection.locator('.overflow-x-auto').evaluate((element) => element.scrollLeft))
    .toBeGreaterThan(0)

  // Other categories display their top titles with subtype before year/context.
  await expect(page.locator('main section').nth(1)).toContainText('Arcade Horizon')
  await expect(page.locator('main section').nth(1)).toContainText('Game · 2026')
  await expect(page.locator('main section').nth(2)).toContainText('VLC Media Player')
  await expect(page.locator('main section').nth(2)).toContainText('Software · VideoLAN')
  await expect(page.locator('main section').nth(2)).toContainText('2 torrents')
  await expect(page.locator('main section').nth(3)).toContainText('Neon Echoes')
  await expect(page.locator('main section').nth(3)).toContainText('Album · 2025')
  await expect(page.locator('main section').nth(4)).toContainText('Orbital Manual')
  await expect(page.locator('main section').nth(4)).toContainText('Book · 2024')

  // Switching periods updates URL and rankings accurately.
  await period.selectOption('day')
  await expect(page).toHaveURL(/\/browse\/trending\?period=day$/)
  await expect(videoLinks.first()).toContainText('Daily Spotlight')
  await expect(videoLinks).toHaveCount(1)

  await period.selectOption('month')
  await expect(page).toHaveURL(/\/browse\/trending\?period=month$/)
  await expect(videoLinks.first()).toContainText('Monthly Hit')

  await period.selectOption('quarter')
  await expect(page).toHaveURL(/\/browse\/trending\?period=quarter$/)
  await expect(videoLinks.first()).toContainText('Quarter Favorite')

  await period.selectOption('year')
  await expect(page).toHaveURL(/\/browse\/trending\?period=year$/)
  await expect(videoLinks.first()).toContainText('Yearly Champion')

  // Clicking a poster opens the title detail page and returns cleanly to Trending with its period.
  await videoLinks.first().click()
  await expect(page).toHaveURL(/\/browse\/search\/\d+$/)
  await expect(page.getByRole('heading', { name: 'Yearly Champion', exact: true })).toBeVisible()
  const backLink = page.getByRole('main').getByRole('link', { name: 'Trending', exact: true })
  await expect(backLink).toBeVisible()
  await backLink.click()
  await expect(page).toHaveURL(/\/browse\/trending\?period=year$/)
  await expect(period).toHaveValue('year')

  // Mobile layout keeps zero horizontal document overflow.
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('public and trusted trending use the dedicated Trending permission under Browse independently of Search', async ({
  page,
  server,
  browser,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    const res = await db.query(
      `INSERT INTO aiosources.aio_works(media_type,tmdb_id,display_title,years)
       VALUES('movie','95001','Public Trending Film',ARRAY[2026]) RETURNING id`,
    )
    const id = Number(res.rows[0].id)
    await db.query(
      `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','pub-1',$1::jsonb)`,
      [
        JSON.stringify({
          title: 'Public.Trending.Film.2026',
          info_hash: 'c'.repeat(40),
          published_at: new Date().toISOString(),
        }),
      ],
    )
    await db.query(
      `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
       SELECT t.provider_id,t.source_id,t.fields_fingerprint,$1,w.revision,'matched','fixture'
       FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id=$1
       WHERE t.provider_id='fixture' AND t.source_id='pub-1'`,
      [id],
    )
    await db.query('DELETE FROM aiosources.aio_match_queue')
  } finally {
    await db.end()
  }

  // First disable Trending while keeping Search enabled: /public/trending must redirect away.
  await page.goto('/public-access')
  const access = page.getByRole('form', { name: 'Public', exact: true })
  await access.getByLabel('Enable public access', { exact: true }).check()
  await access.locator('summary').getByText('Permissions', { exact: true }).click()
  await access.getByLabel('Allow Trending', { exact: true }).uncheck()
  await access.getByLabel('Allow Search', { exact: true }).check()
  await access.getByLabel('Allow Stremio connections', { exact: true }).uncheck()
  await access.getByLabel('Allow Torznab connections', { exact: true }).uncheck()
  await access.getByLabel('Allow shared catalogs', { exact: true }).uncheck()
  await access.locator('summary').getByText('Credentials', { exact: true }).click()
  await access.getByLabel('Use instance debrid accounts', { exact: true }).check()
  await access.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  const context = await browser.newContext({ baseURL: server.origin })
  const visitor = await context.newPage()
  try {
    await visitor.goto('/public/trending')
    await expect(visitor).toHaveURL(/\/public\/browse\/search$/)
    const nav = visitor.getByRole('navigation', { name: 'Main navigation' })
    await expect(nav.getByRole('button', { name: 'Browse', exact: true })).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Trending', exact: true })).toHaveCount(0)
    await expect(nav.getByRole('link', { name: 'Search', exact: true })).toBeVisible()

    // Now enable Trending while disabling Search: Trending works standalone under Browse.
    await access.getByLabel('Allow Trending', { exact: true }).check()
    await access.getByLabel('Allow Search', { exact: true }).uncheck()
    await access.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(access.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

    await expect(visitor).toHaveURL(/\/public\/browse\/trending$/)
    await expect(
      visitor.getByRole('heading', { name: 'Trending', exact: true, level: 1 }),
    ).toBeVisible()
    await expect(
      visitor.getByRole('navigation', { name: 'Breadcrumb', exact: true }).getByRole('listitem'),
    ).toHaveText(['Browse/', 'Trending'])
    await expect(nav.getByRole('link')).toHaveText(['Trending'])
    await expect(nav.locator('a[aria-current="page"]')).toHaveText('Trending')

    const filmLink = visitor
      .locator('main section')
      .first()
      .getByRole('link', { name: /Public Trending Film/ })
    await expect(filmLink).toBeVisible()
    await filmLink.click()
    await expect(visitor).toHaveURL(/\/public\/browse\/search\/\d+$/)
    await expect(
      visitor.getByRole('heading', { name: 'Public Trending Film', exact: true }),
    ).toBeVisible()
    // Reloading title detail while Search is disabled still keeps Browse / Trending navigation & back link.
    await visitor.reload()
    await expect(
      visitor.getByRole('heading', { name: 'Public Trending Film', exact: true }),
    ).toBeVisible()
    await expect(nav.locator('a[aria-current="page"]')).toHaveText('Trending')
    await visitor.getByRole('main').getByRole('link', { name: 'Trending', exact: true }).click()
    await expect(visitor).toHaveURL(/\/public\/browse\/trending$/)
  } finally {
    await context.close()
  }

  // Trusted user with Trending enabled and Search disabled.
  await page.goto('/administration/users/trusted')
  await page.getByRole('button', { name: 'Add user', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Add trusted user', exact: true })
  await dialog.getByLabel('Email', { exact: true }).fill('trending.reader@example.invalid')
  await dialog.getByLabel('Password', { exact: true }).fill('reader-' + server.password)
  await dialog.getByRole('switch', { name: 'Enable account', exact: true }).check()
  await dialog.locator('summary').getByText('Permissions', { exact: true }).click()
  await dialog.getByRole('switch', { name: 'Allow Trending', exact: true }).check()
  for (const name of [
    'Allow Search',
    'Allow Torznab connections',
    'Allow Stremio connections',
    'Allow shared catalogs',
  ])
    await dialog.getByRole('switch', { name, exact: true }).uncheck()
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByText('Trending', { exact: true }).first()).toBeVisible()

  const trustedContext = await browser.newContext({ baseURL: server.origin })
  const trustedPage = await trustedContext.newPage()
  try {
    await trustedPage.goto('/login')
    await trustedPage.getByLabel('Email', { exact: true }).fill('trending.reader@example.invalid')
    await trustedPage.getByLabel('Password', { exact: true }).fill('reader-' + server.password)
    await trustedPage.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(trustedPage).toHaveURL(/\/trusted\/browse\/trending$/)
    const trustedNav = trustedPage.getByRole('navigation', { name: 'Main navigation' })
    await expect(trustedNav.getByRole('button')).toHaveText(['Browse'])
    await expect(trustedNav.getByRole('link')).toHaveText(['Trending'])
    await expect(
      trustedPage
        .locator('main section')
        .first()
        .getByRole('link', { name: /Public Trending Film/ }),
    ).toBeVisible()
    await trustedPage.goto('/trusted/search')
    await expect(trustedPage).toHaveURL(/\/trusted\/browse\/trending$/)
  } finally {
    await trustedContext.close()
  }
})
