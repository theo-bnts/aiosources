import pg from 'pg'
import { test, expect, selectMultiple } from './fixtures'
import { openDisclosure } from './disclosures'

test('real book identification publishes ebooks and audiobooks to the same Search filters', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO aiosources.ol_authors(id,name,normalized) VALUES('OL200A','Morgan Vale','morgan vale');
    INSERT INTO aiosources.aio_works(media_type,openlibrary_id,display_title,book_normalized,years,book_data)
    VALUES('book','OL100W','The Amber Orchard','the amber orchard',ARRAY[2024],'{"author_keys":["OL200A"]}');
    INSERT INTO aiosources.ol_editions(id,work_keys,title,normalized,author_keys,year)
    VALUES('OL300M',ARRAY['OL100W'],'The Amber Orchard','the amber orchard',ARRAY['OL200A'],2024);
    UPDATE aiosources.ol_import_state SET complete=TRUE,data_complete=TRUE,works=1,last_sync=now();
    INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES
    ('fixture','ebook','{"title":"Morgan.Vale.The.Amber.Orchard.2024.FRENCH.EPUB","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":100}'),
    ('fixture','audio','{"title":"Morgan Vale - The Amber Orchard [MP3.128]","categories":[7020],"info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":200}');`)
  } finally {
    await db.end()
  }
  await page.goto('/metadata/identification/settings')
  await openDisclosure(page, 'Manual actions')
  await page.getByRole('button', { name: 'Identify torrents', exact: true }).click()
  await expect
    .poll(
      async () =>
        (await (await page.request.get('/api/operations?kind=matching&status=succeeded')).json())
          .total,
    )
    .toBe(1)
  await page.goto('/search')
  await page.getByLabel('Title, author or identifier', { exact: true }).fill('Orchard')
  for (const types of [['ebook'], ['audiobook'], ['book', 'ebook', 'audiobook']]) {
    await openDisclosure(page, 'Filters')
    await selectMultiple(page, 'Type', types)
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('main tbody tr')).toHaveCount(1)
    await expect(page.locator('main tbody')).toContainText('The Amber Orchard')
    await expect(page.getByText('No matching titles', { exact: true })).toHaveCount(0)
  }
})
