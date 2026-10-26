import pg from 'pg'
import { test, expect } from './fixtures'

test.use({ collectionSource: true })

test('returning through a processed record and its run retains the original history filters', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  let recordID: string
  try {
    await db.query(`INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
      VALUES('nested-run','nested-source','Nested source','preview','succeeded','','{}',now())`)
    const result = await db.query(`WITH saved_page AS (
      INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type)
      VALUES('nested-run','nested-source',1,''::bytea,'application/json') RETURNING id
    )
    INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields)
    SELECT 'nested-run','nested-source','record',id,1,''::bytea,'application/json','{"title":"Example record"}'
    FROM saved_page RETURNING id::text`)
    recordID = result.rows[0].id
  } finally {
    await db.end()
  }
  await page.goto('/runs?provider=nested-source&status=succeeded')
  const origin = page.url()
  await page.getByRole('link', { name: 'Open run nested-run', exact: true }).click()
  await page.getByRole('tab', { name: 'Processed records', exact: true }).click()
  await page.getByRole('link', { name: `View record ${recordID}`, exact: true }).click()
  await page.reload()
  await page.locator('main').getByRole('link', { name: 'Processed records', exact: true }).click()
  await expect(page).toHaveURL(/\/runs\/nested-run\?tab=raw$/)
  await page
    .locator('main')
    .getByRole('link', { name: /^(Sources history|History)$/ })
    .click()
  await expect(page).toHaveURL(origin)
})
