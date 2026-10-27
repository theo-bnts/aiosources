import { test } from './fixtures'
import { exerciseBookSearch, exerciseBookSynchronizationControls } from './book-fixture'
test('local book discovery, file choice and video covers stay coherent', async ({ page, server }) =>
  exerciseBookSearch(page, server))
test('book synchronization settings and recovery status stay clear', async ({ page, server }) =>
  exerciseBookSynchronizationControls(page, server))
