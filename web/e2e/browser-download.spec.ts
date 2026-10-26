import { createServer } from 'node:http'
import { test, expect } from '@playwright/test'
import { prepareBrowserDownload } from '../src/lib/browser-download'
import { listen, closeListener } from './server'

test('attachment downloads retain authentication, omit referrers and keep the application open', async ({
  page,
}) => {
  const deliveries: { authenticated: boolean; referrer: string | undefined }[] = []
  const server = createServer((request, response) => {
    if (request.url === '/') {
      response.setHeader(
        'Set-Cookie',
        'download_session=synthetic; HttpOnly; SameSite=Strict; Path=/',
      )
      response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }).end(`
        <!doctype html><title>Attachment test</title>
        <button id="private">Download file</button><button id="public">Download public file</button>
        <script>
          const prepare = (${prepareBrowserDownload.toString()});
          for (const button of document.querySelectorAll('button')) button.onclick = async () => {
            const response = await fetch('/prepare/' + button.id);
            prepare().start(await response.text());
          };
        </script>`)
    } else if (request.url === '/prepare/private' || request.url === '/prepare/public') {
      response
        .writeHead(200, { 'Content-Type': 'text/plain' })
        .end(
          request.url === '/prepare/private'
            ? '/api/catalog-downloads/synthetic-ticket'
            : '/api/public/catalog-downloads/synthetic-ticket',
        )
    } else if (
      request.url === '/api/catalog-downloads/synthetic-ticket' ||
      request.url === '/api/public/catalog-downloads/synthetic-ticket'
    ) {
      const authenticated = request.headers.cookie === 'download_session=synthetic'
      deliveries.push({ authenticated, referrer: request.headers.referer })
      if (!authenticated) {
        response.writeHead(401).end()
        return
      }
      response
        .writeHead(200, {
          'Content-Type': 'application/octet-stream',
          'Content-Disposition': 'attachment; filename="example.txt"',
          'Content-Security-Policy': "default-src 'none'; frame-ancestors 'self'",
          'X-Frame-Options': 'SAMEORIGIN',
        })
        .end('Synthetic attachment')
    } else response.writeHead(404).end()
  })
  const port = await listen(server)
  const origin = `http://127.0.0.1:${port}`
  try {
    await page.goto(origin)
    for (const id of ['private', 'public']) {
      const result = page.waitForEvent('download')
      await page.locator(`#${id}`).click()
      const download = await result
      expect(download.suggestedFilename()).toBe('example.txt')
      expect(await download.failure()).toBeNull()
      expect(page.context().pages()).toHaveLength(1)
      await expect(page).toHaveURL(origin + '/')
    }
    expect(deliveries).toEqual([
      { authenticated: true, referrer: undefined },
      { authenticated: true, referrer: undefined },
    ])
  } finally {
    await closeListener(server)
  }
})
