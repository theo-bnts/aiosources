import { test, expect } from './fixtures'

const secondLibrary = 'Second' + 'Library'.repeat(24)

for (const connection of ['stremio', 'search', 'torznab'] as const) {
  test(`public ${connection} removal discards a pending replacement without clearing other fields`, async ({
    page,
    server,
    browser,
  }) => {
    const session = await (await page.request.get('/api/session')).json()
    const settings = await (await page.request.get('/api/public-access')).json()
    expect(
      (
        await page.request.put('/api/public-access', {
          headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
          data: {
            ...settings,
            enabled: true,
            stremio_enabled: true,
            search_enabled: true,
            torznab_enabled: true,
          },
        })
      ).status(),
    ).toBe(204)
    const context = await browser.newContext({ viewport: page.viewportSize() })
    const visitor = await context.newPage()
    const original = 'synthetic-encrypted-browser-configuration-original-012345'
    await context.addInitScript((credential) => {
      localStorage.setItem(
        'aiosources.public.credentials',
        JSON.stringify({
          credential,
          services: ['alldebrid', 'torbox'],
          announces: ['private-one', 'private-two'],
        }),
      )
    }, original)
    let writes = 0
    await context.route('**/*', async (route) => {
      const url = new URL(route.request().url())
      if (url.origin !== server.origin) return route.abort('blockedbyclient')
      if (url.pathname === '/api/public/config')
        return route.fulfill({
          json: {
            enabled: true,
            stremio_enabled: true,
            search_enabled: true,
            torznab_enabled: true,
            providers: [
              { id: 'alldebrid', name: 'AllDebrid' },
              { id: 'torbox', name: 'TorBox' },
            ],
            sources: [
              { id: 'private-one', name: 'First library' },
              { id: 'private-two', name: secondLibrary },
            ],
            private_sources: [
              { id: 'private-one', name: 'First library' },
              { id: 'private-two', name: secondLibrary },
            ],
          },
        })
      const endpoint =
        connection === 'stremio'
          ? '/api/public/install'
          : connection === 'search'
            ? '/api/public/credentials'
            : '/api/public/torznab/install'
      if (url.pathname === endpoint) {
        const body = route.request().postDataJSON()
        writes++
        expect(body.credential).toBe(original)
        expect(body.announces).toEqual({
          'private-two': 'https://announce.example/second-replacement/announce',
        })
        expect(body.remove_announces).toEqual(['private-one'])
        if (connection !== 'torznab') {
          expect(body.services).toEqual({ torbox: 'synthetic-retained-torbox-replacement' })
          expect(body.remove_services).toEqual(['alldebrid'])
        }
        return route.fulfill({
          json: {
            credential: original + '-updated',
            services: connection === 'torznab' ? ['alldebrid', 'torbox'] : ['torbox'],
            announces: ['private-two'],
            manifest_url: `${server.origin}/public/stremio/synthetic-installation/manifest.json`,
            url: `${server.origin}/public/torznab/api`,
            api_key: 'synthetic-encrypted-connection-key',
          },
        })
      }
      return route.continue()
    })
    try {
      await visitor.goto(connection === 'stremio' ? '/public' : `/public/${connection}`)
      if (connection === 'search')
        await visitor.locator('summary', { hasText: /^Debrid services$/ }).click()
      if (connection !== 'torznab') {
        const removed = visitor.getByLabel('AllDebrid API key', { exact: true })
        const retained = visitor.getByLabel('TorBox API key', { exact: true })
        await removed.fill('synthetic-discarded-alldebrid-replacement')
        await retained.fill('synthetic-retained-torbox-replacement')
        await visitor.getByRole('button', { name: 'Reveal TorBox API key', exact: true }).click()
        await expect(retained).toHaveAttribute('type', 'text')
        await expect(removed).toHaveAttribute('type', 'password')
        await visitor.getByRole('button', { name: 'Hide TorBox API key', exact: true }).click()
        await visitor.getByRole('button', { name: 'Reveal AllDebrid API key', exact: true }).click()
        await visitor
          .locator('.field')
          .filter({ has: removed })
          .getByRole('button', { name: 'Remove key', exact: true })
          .click()
        await expect(removed).toHaveValue('')
        await expect(removed).toHaveAttribute('type', 'password')
        await expect(removed).toBeFocused()
        await expect(retained).toHaveValue('synthetic-retained-torbox-replacement')
      }
      await visitor.locator('summary', { hasText: /^Advanced$/ }).click()
      const removed = visitor.getByLabel('First library announce URL', { exact: true })
      const retained = visitor.getByLabel(`${secondLibrary} announce URL`, { exact: true })
      expect(
        await visitor
          .locator('label[for="public-announce-private-two"]')
          .evaluate((element) => element.scrollWidth <= element.clientWidth),
      ).toBe(true)
      await removed.fill('https://announce.example/discarded-replacement/announce')
      await retained.fill('https://announce.example/second-replacement/announce')
      await visitor
        .getByRole('button', { name: `Reveal ${secondLibrary} announce URL`, exact: true })
        .click()
      await expect(retained).toHaveAttribute('type', 'text')
      await expect(removed).toHaveAttribute('type', 'password')
      await visitor
        .getByRole('button', { name: `Hide ${secondLibrary} announce URL`, exact: true })
        .click()
      await visitor
        .getByRole('button', { name: 'Reveal First library announce URL', exact: true })
        .click()
      await visitor
        .locator('.field')
        .filter({ has: removed })
        .getByRole('button', { name: 'Remove announce', exact: true })
        .click()
      await expect(removed).toHaveValue('')
      await expect(removed).toHaveAttribute('type', 'password')
      await expect(removed).toBeFocused()
      await expect(retained).toHaveValue('https://announce.example/second-replacement/announce')
      await visitor
        .getByRole('button', {
          name:
            connection === 'stremio'
              ? 'Create installation link'
              : connection === 'search'
                ? 'Update services'
                : 'Create connection link',
          exact: true,
        })
        .click()
      await expect.poll(() => writes).toBe(1)
      if (connection === 'stremio') {
        const manifest = visitor.getByLabel('Manifest URL', { exact: true })
        await expect(manifest).toHaveAttribute('type', 'password')
        await expect(manifest).toHaveValue(
          `${server.origin}/public/stremio/synthetic-installation/manifest.json`,
        )
        await visitor.getByRole('button', { name: 'Reveal manifest URL', exact: true }).click()
        await expect(manifest).toHaveAttribute('type', 'text')
      } else if (connection === 'torznab') {
        const key = visitor.getByLabel('API key', { exact: true })
        await expect(key).toHaveAttribute('type', 'password')
        await visitor.getByRole('button', { name: 'Reveal API key', exact: true }).click()
        await expect(key).toHaveAttribute('type', 'text')
        await visitor.locator('summary', { hasText: /^Full endpoint URL$/ }).click()
        const endpoint = visitor.getByLabel('Endpoint URL', { exact: true })
        await expect(endpoint).toHaveAttribute('type', 'password')
        await visitor.getByRole('button', { name: 'Reveal endpoint URL', exact: true }).click()
        await expect(endpoint).toHaveAttribute('type', 'text')
        expect(new URL(await endpoint.inputValue()).searchParams.get('apikey')).toBe(
          'synthetic-encrypted-connection-key',
        )
      }
      expect(await visitor.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    } finally {
      await context.close()
    }
  })
}
