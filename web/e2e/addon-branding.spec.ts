import { test, expect, createStremioAccess } from './fixtures'

test('installed addons advertise their public PNG without exposing administrator access', async ({
  page,
  browser,
  server,
}) => {
  const { manifestURL } = await createStremioAccess(page)
  const visitor = await browser.newContext({ baseURL: server.origin })
  try {
    const response = await visitor.request.get(manifestURL)
    expect(response.status()).toBe(200)
    expect(response.headers()['access-control-allow-origin']).toBe('*')
    const manifest = await response.json()
    expect(manifest.version).toBe('2.2.0')
    expect(manifest.logo).toBe(
      'https://raw.githubusercontent.com/theo-bnts/aiosources/main/web/public/logo.png',
    )
    expect(manifest.id).toMatch(/^org\.aiosource\.v2\.connection\.[a-f0-9]+$/)

    const logo = await visitor.request.get('/logo.png')
    expect(logo.status()).toBe(200)
    expect(logo.headers()['content-type']).toMatch(/^image\/png/)
    const bytes = await logo.body()
    expect(Array.from(bytes.subarray(0, 8))).toEqual([137, 80, 78, 71, 13, 10, 26, 10])
    expect(bytes.readUInt32BE(16)).toBe(512)
    expect(bytes.readUInt32BE(20)).toBe(512)
    expect(
      await page.evaluate(async () => {
        const image = new Image()
        image.src = '/logo.png'
        await image.decode()
        return [image.naturalWidth, image.naturalHeight]
      }),
    ).toEqual([512, 512])
    const head = await visitor.request.head('/logo.png')
    expect(head.status()).toBe(200)
    expect(await head.body()).toHaveLength(0)
    expect((await visitor.request.get('/api/providers')).status()).toBe(401)
  } finally {
    await visitor.close()
  }
})
