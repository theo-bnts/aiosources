import { test, expect } from './fixtures'

test('navigation and page tabs remain coherent on every current and legacy route', async ({
  page,
}) => {
  test.setTimeout(90_000)
  const destinations = [
    // Canonical hierarchical URLs
    ['Sources', 'Torrents', '/sources/indexers/torrents/statistics'],
    ['Sources', 'Torrents', '/sources/indexers/torrents/settings'],
    ['Sources', 'Torrents', '/sources/indexers/torrents/settings/new'],
    ['Sources', 'Torrents', '/sources/indexers/torrents/history'],
    ['Sources', 'Torrents', '/sources/indexers/torrents/history/missing'],
    ['Sources', 'Torrents', '/sources/indexers/torrents/history/raw/999999'],
    ['Sources', 'Torrents', '/sources/indexers/torrents/history/pages/999999'],
    ['Sources', 'Remote catalogs', '/sources/remote-catalogs/statistics'],
    ['Sources', 'Remote catalogs', '/sources/remote-catalogs/settings'],
    ['Sources', 'Remote catalogs', '/sources/remote-catalogs/history'],
    ['Sources', 'Remote catalogs', '/sources/remote-catalogs/history/missing'],
    ['Sources', 'Files', '/sources/files/statistics'],
    ['Sources', 'Files', '/sources/files/results'],
    ['Metadata', 'Videos', '/metadata/catalogs/videos/statistics'],
    ['Metadata', 'Videos', '/metadata/catalogs/videos/settings'],
    ['Metadata', 'Videos', '/metadata/catalogs/videos/history'],
    ['Metadata', 'Games', '/metadata/catalogs/games/statistics'],
    ['Metadata', 'Games', '/metadata/catalogs/games/settings'],
    ['Metadata', 'Games', '/metadata/catalogs/games/history'],
    ['Metadata', 'Software', '/metadata/catalogs/software/statistics'],
    ['Metadata', 'Software', '/metadata/catalogs/software/settings'],
    ['Metadata', 'Software', '/metadata/catalogs/software/history'],
    ['Metadata', 'Music', '/metadata/catalogs/music/statistics'],
    ['Metadata', 'Music', '/metadata/catalogs/music/settings'],
    ['Metadata', 'Music', '/metadata/catalogs/music/history'],
    ['Metadata', 'Books', '/metadata/catalogs/books/statistics'],
    ['Metadata', 'Books', '/metadata/catalogs/books/settings'],
    ['Metadata', 'Books', '/metadata/catalogs/books/history'],
    ['Metadata', 'Identification', '/metadata/identification/statistics'],
    ['Metadata', 'Identification', '/metadata/identification/results'],
    ['Metadata', 'Identification', '/metadata/identification/correspondences'],
    ['Metadata', 'Identification', '/metadata/identification/settings'],
    ['Metadata', 'Identification', '/metadata/identification/history'],
    ['Debrid', 'Debrid', '/debrid/statistics'],
    ['Debrid', 'Debrid', '/debrid/settings'],
    ['Debrid', 'Debrid', '/debrid/history'],
    ['Debrid', 'Debrid', '/debrid/history/missing'],
    ['Browse', 'Trending', '/browse/trending'],
    ['Browse', 'Search', '/browse/search'],
    ['Browse', 'Search', '/browse/search/torrents'],
    ['Browse', 'Search', '/browse/search/999999'],
    ['Connections', 'Torznab', '/connections/torznab'],
    ['Connections', 'Stremio', '/connections/stremio/statistics'],
    ['Connections', 'Stremio', '/connections/stremio/settings'],
    ['Connections', 'Stremio', '/connections/stremio/history'],
    ['Connections', 'Shared catalogs', '/connections/shared-catalogs/statistics'],
    ['Connections', 'Shared catalogs', '/connections/shared-catalogs/settings'],
    ['Administration', 'Secrets', '/administration/security/secrets'],
    ['Administration', 'Limits', '/administration/security/limits'],
    ['Administration', 'Active sessions', '/administration/security/active-sessions'],
    ['Administration', 'Incoming', '/administration/network/incoming'],
    ['Administration', 'Outgoing', '/administration/network/outgoing/proxies'],
    ['Administration', 'Outgoing', '/administration/network/outgoing/routing'],
    ['Administration', 'Trusted', '/administration/users/trusted'],
    ['Administration', 'Public', '/administration/users/public'],
    ['Administration', 'Storage', '/administration/storage/statistics'],
    ['Administration', 'Storage', '/administration/storage/settings'],
    ['Administration', 'Logs', '/administration/logs'],
    // Legacy bookmark redirects
    ['Sources', 'Torrents', '/providers'],
    ['Sources', 'Torrents', '/providers/settings'],
    ['Sources', 'Torrents', '/schedules'],
    ['Sources', 'Torrents', '/settings'],
    ['Sources', 'Torrents', '/'],
    ['Sources', 'Torrents', '/import-settings'],
    ['Sources', 'Torrents', '/providers/new'],
    ['Sources', 'Torrents', '/runs'],
    ['Sources', 'Torrents', '/raw'],
    ['Sources', 'Torrents', '/raw/999999'],
    ['Sources', 'Torrents', '/pages/999999'],
    ['Sources', 'Torrents', '/runs/missing'],
    ['Sources', 'Files', '/torrents'],
    ['Sources', 'Files', '/files'],
    ['Sources', 'Files', '/files/results'],
    ['Metadata', 'Videos', '/metadata'],
    ['Metadata', 'Videos', '/metadata/settings'],
    ['Metadata', 'Videos', '/metadata/videos'],
    ['Metadata', 'Videos', '/metadata/videos/settings'],
    ['Metadata', 'Videos', '/metadata/videos/history'],
    ['Metadata', 'Games', '/metadata/games'],
    ['Metadata', 'Games', '/metadata/games/settings'],
    ['Metadata', 'Games', '/metadata/games/history'],
    ['Metadata', 'Software', '/metadata/software'],
    ['Metadata', 'Software', '/metadata/software/settings'],
    ['Metadata', 'Software', '/metadata/software/history'],
    ['Metadata', 'Music', '/metadata/music'],
    ['Metadata', 'Music', '/metadata/music/settings'],
    ['Metadata', 'Music', '/metadata/music/history'],
    ['Metadata', 'Books', '/metadata/books'],
    ['Metadata', 'Books', '/metadata/books/settings'],
    ['Metadata', 'Books', '/metadata/books/history'],
    ['Metadata', 'Identification', '/metadata/identification'],
    ['Metadata', 'Videos', '/metadata/history'],
    ['Metadata', 'Videos', '/metadata/history/missing'],
    ['Debrid', 'Debrid', '/debrid'],
    ['Browse', 'Trending', '/trending'],
    ['Browse', 'Search', '/search'],
    ['Browse', 'Search', '/search/999999'],
    ['Connections', 'Stremio', '/addon'],
    ['Connections', 'Stremio', '/addon/settings'],
    ['Connections', 'Stremio', '/addon/history'],
    ['Connections', 'Shared catalogs', '/sharing'],
    ['Connections', 'Torznab', '/torznab'],
    ['Administration', 'Public', '/public-access'],
    ['Administration', 'Trusted', '/trusted-users'],
    ['Sources', 'Remote catalogs', '/sharing/remotes'],
    ['Sources', 'Remote catalogs', '/remotes'],
    ['Sources', 'Remote catalogs', '/remotes/settings'],
    ['Sources', 'Remote catalogs', '/remotes/history'],
    ['Sources', 'Remote catalogs', '/remotes/history/missing'],
    ['Connections', 'Shared catalogs', '/sharing/settings'],
    ['Administration', 'Incoming', '/network'],
    ['Administration', 'Limits', '/network/limits'],
    ['Administration', 'Outgoing', '/network/proxies'],
    ['Administration', 'Outgoing', '/network/routing'],
    ['Administration', 'Incoming', '/network/incoming'],
    ['Administration', 'Storage', '/health'],
    ['Administration', 'Secrets', '/security'],
    ['Administration', 'Secrets', '/secrets'],
    ['Administration', 'Secrets', '/security/secrets'],
    ['Administration', 'Limits', '/security/limits'],
    ['Administration', 'Active sessions', '/security/sessions'],
    ['Administration', 'Logs', '/logs'],
    ['Administration', 'Logs', '/notifications'],
    ['Administration', 'Logs', '/webhooks'],
  ]
  for (const [group, label, url] of destinations) {
    await page.goto(url)
    await expect(
      page.getByRole('navigation', { name: 'Breadcrumb', exact: true }).getByRole('listitem'),
    ).toHaveText(
      ['Sources', 'Metadata', 'Browse', 'Connections', 'Administration'].includes(group)
        ? label === 'Torrents'
          ? ['Sources/', 'Indexers/', label]
          : ['Incoming', 'Outgoing'].includes(label)
            ? [`${group}/`, 'Network/', label]
            : ['Secrets', 'Limits', 'Active sessions'].includes(label)
              ? [`${group}/`, 'Security/', label]
              : ['Public', 'Trusted'].includes(label)
                ? [`${group}/`, 'Users/', label]
                : ['Videos', 'Games', 'Software', 'Music', 'Books'].includes(label)
                  ? [`${group}/`, 'Catalogs/', label]
                  : [`${group}/`, label]
        : [label],
    )
    const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
    await expect(navigation.locator('a[aria-current="page"]')).toHaveText(label)
    if (['Sources', 'Metadata', 'Browse', 'Connections', 'Administration'].includes(group))
      await expect(navigation.getByRole('button', { name: group, exact: true })).toHaveAttribute(
        'aria-expanded',
        'true',
      )
    await expect(page.getByRole('heading', { name: label, exact: true, level: 1 })).toBeVisible()
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
    await expect(
      page
        .getByRole('navigation', { name: 'Breadcrumb' })
        .getByRole('button', { name: `About ${label}`, exact: true }),
    ).toHaveCount(0)
    await expect(page.locator('#page-description')).toBeVisible()
    const layout = await page.evaluate(() => ({
      overflow: document.documentElement.scrollWidth - innerWidth,
      tabOverflow: [...document.querySelectorAll<HTMLElement>('[role="tablist"]')]
        .filter(
          (node) =>
            node.getClientRects().length > 0 &&
            node.getAttribute('aria-orientation') !== 'vertical',
        )
        .map((node) => node.scrollHeight - node.clientHeight),
    }))
    expect(layout.overflow, url).toBeLessThanOrEqual(0)
    for (const overflow of layout.tabOverflow) expect(overflow, url).toBeLessThanOrEqual(1)
  }
  await page.goto('/providers')
  const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  const security = navigation.getByRole('button', { name: 'Security', exact: true })
  if ((await security.getAttribute('aria-expanded')) === 'true') await security.click()
  const network = navigation.getByRole('button', { name: 'Network', exact: true })
  if ((await network.getAttribute('aria-expanded')) === 'true') await network.click()
  const users = navigation.getByRole('button', { name: 'Users', exact: true })
  if ((await users.getAttribute('aria-expanded')) !== 'true') await users.click()
  await expect(navigation.getByRole('button')).toHaveText([
    'Sources',
    'Indexers',
    'Metadata',
    'Catalogs',
    'Browse',
    'Connections',
    'Administration',
    'Security',
    'Network',
    'Users',
  ])
  await expect(navigation.getByRole('link')).toHaveText([
    'Torrents',
    'Remote catalogs',
    'Files',
    'Videos',
    'Games',
    'Software',
    'Music',
    'Books',
    'Identification',
    'Debrid',
    'Trending',
    'Search',
    'Torznab',
    'Stremio',
    'Shared catalogs',
    'Trusted',
    'Public',
    'Storage',
    'Logs',
  ])
  for (const name of [
    'Overview',
    'Preferences',
    'Schedules',
    'History',
    'Secrets',
    'Backups',
    'Notifications',
    'External notifications',
  ])
    await expect(navigation.getByRole('link', { name, exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /notification/i })).toHaveCount(0)
  await expect(
    page.getByRole('tablist', { name: 'Torrents views', exact: true }).getByRole('tab'),
  ).toHaveText(['Statistics', 'Settings', 'History'])
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(page).toHaveURL(/\/sources\/indexers\/torrents\/history$/)
  await expect(page.getByRole('heading', { name: 'History', exact: true })).toHaveCount(0)
  await page.goto('/runs?provider=example&status=failed&offset=50&limit=25')
  await expect(page).toHaveURL(
    /\/sources\/indexers\/torrents\/history\?provider=example&status=failed&offset=50&limit=25$/,
  )
  await expect(page.getByRole('navigation', { name: 'History views', exact: true })).toHaveCount(0)
  await page.goto('/raw?provider=example&limit=25')
  await expect(page).toHaveURL(/\/sources\/indexers\/torrents\/history\?provider=example$/)
  await expect(page.getByLabel('Processing result', { exact: true })).toHaveCount(0)
})

test('mobile navigation and horizontal page tabs retain their current section', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/addon')
  const breadcrumb = page.getByRole('navigation', { name: 'Breadcrumb', exact: true })
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  let navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  await navigation.getByRole('button', { name: 'Sources', exact: true }).click()
  await expect(navigation.getByRole('button', { name: 'Sources', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await navigation.getByRole('button', { name: 'Catalogs', exact: true }).click()
  await navigation.getByRole('link', { name: 'Videos', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await page.reload()
  await expect(page.getByRole('tab', { name: 'History', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(breadcrumb.getByRole('listitem')).toHaveText(['Metadata/', 'Catalogs/', 'Videos'])
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  await expect(navigation.getByRole('button', { name: 'Sources', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await navigation.getByRole('link', { name: 'Storage', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  const storageTabs = page.getByRole('tablist', { name: 'Storage views', exact: true })
  await expect(storageTabs.getByRole('tab', { name: 'Statistics', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(breadcrumb.getByRole('listitem')).toHaveText(['Administration/', 'Storage'])
  await storageTabs.getByRole('tab', { name: 'Settings', exact: true }).click()
  await expect(page.getByText('Torrent data', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Stored data', exact: true })).toHaveCount(0)
  await expect(breadcrumb.getByRole('listitem')).toHaveText(['Administration/', 'Storage'])
  await expect(
    page.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toBeHidden()
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  await navigation.getByRole('button', { name: 'Security', exact: true }).click()
  await navigation.getByRole('link', { name: 'Secrets', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.getByRole('tablist', { name: 'Security views', exact: true })).toHaveCount(0)
  const createSecret = page.getByRole('button', { name: 'Create secret', exact: true })
  await expect(createSecret).toBeVisible()
  const createPosition = (await createSecret.boundingBox())!
  await expect(page.getByRole('region', { name: 'Active sessions', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  await page
    .getByRole('navigation', { name: 'Main navigation', exact: true })
    .getByRole('link', { name: 'Active sessions', exact: true })
    .click()
  await expect(page.getByText('This session', { exact: true })).toBeVisible()
  const revokeOthers = page.getByRole('button', { name: 'Revoke others', exact: true })
  await expect(revokeOthers).toBeDisabled()
  const revokePosition = (await revokeOthers.boundingBox())!
  expect(
    Math.abs(createPosition.x + createPosition.width - revokePosition.x - revokePosition.width),
  ).toBeLessThan(1)
  await expect(
    page.getByRole('heading', { name: 'Active sessions', exact: true, level: 1 }),
  ).toBeVisible()
  await expect(page.getByText('Security audit', { exact: true })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
