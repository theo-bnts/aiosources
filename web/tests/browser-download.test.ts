import test from 'node:test'
import assert from 'node:assert/strict'
import { prepareBrowserDownload } from '../src/lib/browser-download.ts'

test('browser downloads accept only authenticated application attachment URLs and never open a tab', () => {
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window')
  const originalDocument = Object.getOwnPropertyDescriptor(globalThis, 'document')
  const frames: any[] = []
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: { location: { origin: 'https://admin.example' }, setTimeout: () => 0 },
  })
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: {
      createElement: () => ({
        dataset: {},
        sandbox: {
          add: (...flags: string[]) => assert.equal(flags.includes('allow-scripts'), false),
        },
      }),
      body: { append: (frame: any) => frames.push(frame) },
    },
  })
  try {
    for (const url of [
      'https://outside.example/file',
      'http://admin.example/api/catalog-downloads/ticket',
      'https://admin.example:444/api/catalog-downloads/ticket',
      'https://user:password@admin.example/api/catalog-downloads/ticket',
      '/api/secrets',
      '/api/catalog-downloads/%2e%2e/secrets',
      '/api/catalog-downloads/ticket?target=external',
      '/api/catalog-downloads/ticket#fragment',
      '/api/public/catalog-downloads/%2e%2e/secrets',
      '/api/public/catalog-downloads/ticket?target=external',
      '/api/public/catalog-downloads/ticket#fragment',
      'javascript:alert(1)',
      'data:text/html,example',
    ])
      assert.equal(prepareBrowserDownload().start(url), false)
    const cancelled = prepareBrowserDownload()
    cancelled.cancel()
    assert.equal(cancelled.start('/api/catalog-downloads/ticket'), false)
    assert.equal(frames.length, 0)
    assert.equal(prepareBrowserDownload().start('/api/catalog-downloads/ticket'), true)
    assert.equal(frames.length, 1)
    assert.equal(frames[0].src, 'https://admin.example/api/catalog-downloads/ticket')
    assert.equal(frames[0].referrerPolicy, 'no-referrer')
    assert.equal(prepareBrowserDownload().start('/api/public/catalog-downloads/ticket'), true)
    assert.equal(frames[1].src, 'https://admin.example/api/public/catalog-downloads/ticket')
  } finally {
    if (originalWindow) Object.defineProperty(globalThis, 'window', originalWindow)
    else delete (globalThis as any).window
    if (originalDocument) Object.defineProperty(globalThis, 'document', originalDocument)
    else delete (globalThis as any).document
  }
})
