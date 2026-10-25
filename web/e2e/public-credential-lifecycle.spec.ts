import { test, expect } from './fixtures'

for (const change of ['forget', 'replace'] as const) {
  test(`public credentials ${change} across tabs while the administration view is open`, async ({
    page,
    server,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const access = await (await page.request.get('/api/public-access')).json()
    expect(
      (
        await page.request.put('/api/public-access', {
          headers: { Origin: server.origin, 'X-CSRF-Token': session.csrf_token },
          data: { ...access, enabled: true, stremio_enabled: true },
        })
      ).status(),
    ).toBe(204)
    await page.evaluate(() => {
      localStorage.setItem(
        'aiosources.public.credentials',
        JSON.stringify({
          credential: 'synthetic-encrypted-browser-configuration-original-012345',
          services: ['torbox'],
          announces: [],
        }),
      )
    })
    await page.goto('/public')
    await expect(page.getByLabel('TorBox API key', { exact: true })).toHaveAttribute(
      'placeholder',
      /Saved on this browser/,
    )
    await page.evaluate(() => {
      Object.assign(window, { credentialStorageEvents: 0 })
      window.addEventListener('storage', (event) => {
        if (event.key === 'aiosources.public.credentials') {
          const probe = window as unknown as { credentialStorageEvents: number }
          probe.credentialStorageEvents++
        }
      })
    })
    await page.getByRole('link', { name: 'Administration', exact: true }).click()
    await expect(page).toHaveURL(`${server.origin}/providers`)
    await expect(page.getByLabel('TorBox API key', { exact: true })).toHaveCount(0)

    const other = await page.context().newPage()
    try {
      await other.goto(`${server.origin}/public`)
      await expect(other.getByLabel('TorBox API key', { exact: true })).toHaveAttribute(
        'placeholder',
        /Saved on this browser/,
      )
      if (change === 'forget') {
        await other.getByRole('button', { name: 'Forget saved credentials', exact: true }).click()
      } else {
        await other.evaluate(() => {
          localStorage.setItem(
            'aiosources.public.credentials',
            JSON.stringify({
              credential: 'synthetic-encrypted-browser-configuration-replacement-012345',
              services: ['premiumize'],
              announces: [],
            }),
          )
        })
      }
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              (window as unknown as { credentialStorageEvents: number }).credentialStorageEvents,
          ),
        )
        .toBeGreaterThan(0)
      await page.goBack()
      await expect(page).toHaveURL(`${server.origin}/public`)
      await expect(page.getByLabel('TorBox API key', { exact: true })).not.toHaveAttribute(
        'placeholder',
        /Saved on this browser/,
      )
      if (change === 'replace') {
        await expect(page.getByLabel('Premiumize API key', { exact: true })).toHaveAttribute(
          'placeholder',
          /Saved on this browser/,
        )
      } else {
        await expect(
          page.getByRole('button', { name: 'Forget saved credentials', exact: true }),
        ).toHaveCount(0)
      }
    } finally {
      await other.close()
    }
  })
}
