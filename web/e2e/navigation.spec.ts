import { test, expect } from './fixtures'

test('navigation and page tabs remain coherent on every current and legacy route', async ({
  page,
}) => {
  test.setTimeout(90_000)
  const destinations = [
    ['Sources', 'Sources', '/providers'],
    ['Sources', 'Sources', '/providers/settings'],
    ['Sources', 'Sources', '/schedules'],
    ['Sources', 'Sources', '/settings'],
    ['Sources', 'Sources', '/'],
    ['Sources', 'Sources', '/import-settings'],
    ['Sources', 'Sources', '/providers/new'],
    ['Sources', 'Sources', '/runs'],
    ['Sources', 'Sources', '/raw'],
    ['Sources', 'Sources', '/raw/999999'],
    ['Sources', 'Sources', '/pages/999999'],
    ['Sources', 'Sources', '/runs/missing'],
    ['Sources', 'Torrent search', '/torrents'],
    ['Metadata', 'Metadata', '/metadata'],
    ['Metadata', 'Metadata', '/metadata/settings'],
    ['Metadata', 'Identification', '/metadata/identification'],
    ['Metadata', 'Metadata', '/metadata/history'],
    ['Metadata', 'Metadata', '/metadata/history/missing'],
    ['Debrid', 'Debrid', '/debrid'],
    ['Debrid', 'Debrid', '/debrid/settings'],
    ['Debrid', 'Debrid', '/debrid/history'],
    ['Debrid', 'Debrid', '/debrid/history/missing'],
    ['Search', 'Search', '/search'],
    ['Search', 'Search', '/search/999999'],
    ['Connections', 'Stremio', '/addon'],
    ['Connections', 'Stremio', '/addon/settings'],
    ['Connections', 'Stremio', '/addon/history'],
    ['Connections', 'Shared catalogs', '/sharing'],
    ['Connections', 'Torznab', '/torznab'],
    ['Connections', 'Public access', '/public-access'],
    ['Sources', 'Remote catalogs', '/sharing/remotes'],
    ['Sources', 'Remote catalogs', '/remotes'],
    ['Sources', 'Remote catalogs', '/remotes/settings'],
    ['Sources', 'Remote catalogs', '/remotes/history'],
    ['Sources', 'Remote catalogs', '/remotes/history/missing'],
    ['Connections', 'Shared catalogs', '/sharing/settings'],
    ['Administration', 'Network', '/network'],
    ['Administration', 'Network', '/network/routing'],
    ['Administration', 'Storage', '/health'],
    ['Administration', 'Security', '/security'],
    ['Administration', 'Security', '/secrets'],
    ['Administration', 'Security', '/security/secrets'],
    ['Administration', 'Security', '/security/verification'],
    ['Administration', 'Security', '/security/sessions'],
    ['Administration', 'Logs', '/logs'],
    ['Administration', 'Logs', '/notifications'],
    ['Administration', 'Logs', '/webhooks'],
  ]
  for (const [group, label, url] of destinations) {
    await page.goto(url)
    await expect(
      page.getByRole('navigation', { name: 'Breadcrumb', exact: true }).getByRole('listitem'),
    ).toHaveText(
      ['Sources', 'Metadata', 'Connections', 'Administration'].includes(group)
        ? [`${group}/`, label]
        : [label],
    )
    const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
    await expect(navigation.locator('a[aria-current="page"]')).toHaveText(label)
    if (['Sources', 'Metadata', 'Connections', 'Administration'].includes(group))
      await expect(navigation.getByRole('button', { name: group, exact: true })).toHaveAttribute(
        'aria-expanded',
        'true',
      )
    await expect(page.getByRole('heading', { name: label, exact: true, level: 1 })).toBeVisible()
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
    await expect(page.locator('#page-description')).toBeVisible()
  }
  await page.goto('/providers')
  const navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  await expect(navigation.getByRole('button')).toHaveText([
    'Sources',
    'Metadata',
    'Connections',
    'Administration',
  ])
  await expect(navigation.getByRole('link')).toHaveText([
    'Sources',
    'Remote catalogs',
    'Torrent search',
    'Metadata',
    'Identification',
    'Debrid',
    'Search',
    'Stremio',
    'Torznab',
    'Shared catalogs',
    'Public access',
    'Network',
    'Security',
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
    page.getByRole('tablist', { name: 'Sources views', exact: true }).getByRole('tab'),
  ).toHaveText(['Statistics', 'Settings', 'History'])
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await expect(page).toHaveURL(/\/runs$/)
  await expect(page.getByRole('heading', { name: 'History', exact: true })).toHaveCount(0)
  await page.goto('/runs?provider=example&status=failed&offset=50&limit=25')
  await expect(page.getByRole('navigation', { name: 'History views', exact: true })).toHaveCount(0)
  await page.goto('/raw?provider=example&limit=25')
  await expect(page).toHaveURL(/\/runs\?provider=example$/)
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
  await navigation.getByRole('link', { name: 'Metadata', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.getByRole('tab', { name: 'History', exact: true }).click()
  await page.reload()
  await expect(page.getByRole('tab', { name: 'History', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(breadcrumb.getByRole('listitem')).toHaveText(['Metadata/', 'Metadata'])
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
  await expect(page.getByRole('heading', { name: 'Data retention', exact: true })).toBeVisible()
  await expect(breadcrumb.getByRole('listitem')).toHaveText(['Administration/', 'Storage'])
  await expect(
    page.getByRole('button', { name: 'Reset all source data', exact: true }),
  ).toBeHidden()
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  navigation = page.getByRole('navigation', { name: 'Main navigation', exact: true })
  await navigation.getByRole('link', { name: 'Security', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  const tabs = page.getByRole('tablist', { name: 'Security views', exact: true })
  await expect(tabs).toHaveAttribute('aria-orientation', 'horizontal')
  await expect(tabs.getByRole('tab', { name: 'Statistics', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('heading', { name: 'Secrets', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Create secret', exact: true })).toHaveCount(0)
  await tabs.getByRole('tab', { name: 'Settings', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Create secret', exact: true })).toBeVisible()
  await expect(page.getByText('This session', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Active sessions', exact: true })).toBeVisible()
  await expect(page.getByText('Security audit', { exact: true })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
