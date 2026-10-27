import { test } from './fixtures'
import { exerciseBookSearch, exerciseBookSynchronizationControls } from './book-fixture'
test.use({
  viewport: { width: 390, height: 844 },
  isMobile: true,
  hasTouch: true,
  deviceScaleFactor: 3,
})
test('mobile book discovery, file choice and video covers stay coherent', async ({
  page,
  server,
}) => exerciseBookSearch(page, server))
test('mobile book synchronization settings and recovery status stay clear', async ({
  page,
  server,
}) => exerciseBookSynchronizationControls(page, server))
