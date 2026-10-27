import { test } from './fixtures'
import { exerciseBookSearch } from './book-fixture'
test('local book discovery, file choice and video covers stay coherent', async ({ page, server }) =>
  exerciseBookSearch(page, server))
