import pg from 'pg'
import { test, expect } from './fixtures'

test.use({ collectionSource: true })

test('source display names stay consistent while routes and historical identity remain stable', async ({
  page,
  server,
}) => {
  test.setTimeout(60_000)
  const session = await (await page.request.get('/api/session')).json()
  const headers = { 'X-CSRF-Token': session.csrf_token, Origin: server.origin }
  const source = {
    version: 1,
    id: 'machine-source',
    name: 'Example Library',
    adapter: 'http_json',
    enabled: false,
    url: server.sourceURL,
  }
  const save = await page.request.put(`/api/providers/${source.id}`, {
    headers,
    data: { revision: '', json: JSON.stringify(source) },
  })
  expect(save.status()).toBe(200)
  const document = await save.json()
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  let rawID = '',
    requestID = ''
  try {
    await db.query(
      `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,finished_at)
       VALUES('display-run',$1,$2,'full','succeeded','',$3,now());
       `,
      [source.id, source.name, JSON.stringify(source)],
    )
    const response = await db.query(
      `INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type,payload_retained)
       VALUES('display-run',$1,1,''::bytea,'application/json',false) RETURNING id`,
      [source.id],
    )
    const record = await db.query(
      `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,payload_retained,fields)
       VALUES('display-run',$1,'release-1',$2,1,''::bytea,'application/json',false,'{"title":"Example Release"}') RETURNING id`,
      [source.id, response.rows[0].id],
    )
    rawID = String(record.rows[0].id)
    await db.query(
      `INSERT INTO ingest.torrents(provider_id,source_id,fields,raw_id)
       VALUES($1,'release-1','{"title":"Example Release"}',$2);
       `,
      [source.id, rawID],
    )
    await db.query(`INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years)
      VALUES('movie','1','tt1234567',ARRAY[2021]);
      INSERT INTO ingest.aio_titles(work_id,title,normalized)
      SELECT id,'Example Release','example release' FROM ingest.aio_works;
      INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
      SELECT provider_id,source_id,md5(fields::text),(SELECT id FROM ingest.aio_works),1,'matched','exact alias with compatible evidence'
      FROM ingest.torrents;
      DELETE FROM ingest.aio_match_queue;`)
    await db.query(
      `INSERT INTO ingest.activity_logs(event_id,level,kind,message,provider_id,run_id)
       VALUES('display-log','info','system.fixture','Example source activity',$1,'display-run')`,
      [source.id],
    )
    const result = await db.query(
      `INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results)
       VALUES(now(),'movie','imdb:tt1234567',1,'results',1,1,0,0,0,0,1,false,$1) RETURNING id`,
      [
        JSON.stringify([
          {
            name: '［AIO Example Library］',
            provider: source.id,
            provider_name: source.name,
            source_id: 'release-1',
            title: 'Example Release',
            service: 'direct',
            cache: 'direct',
            filename: 'Example Release',
            size: 1000,
          },
        ]),
      ],
    )
    requestID = String(result.rows[0].id)
  } finally {
    await db.end()
  }
  await page.reload()
  await expect(page.getByRole('link', { name: source.name, exact: true })).toBeVisible()
  await expect(
    page.getByRole('region', { name: `Source ${source.name}`, exact: true }),
  ).not.toContainText(source.id)

  for (const name of [source.name, 'Renamed Library é']) {
    if (name !== source.name) {
      expect(
        (
          await page.request.put(`/api/providers/${source.id}`, {
            headers,
            data: { revision: document.revision, json: JSON.stringify({ ...source, name }) },
          })
        ).status(),
      ).toBe(200)
      await page.reload()
    }
    await page.goto(`/torrents?provider=${source.id}`)
    await expect(page.getByRole('table')).toContainText(name)
    await expect(page.getByRole('table')).not.toContainText(source.id)
    await page.getByRole('link', { name: 'Example Release', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText(`${name} · release-1`)
    await page.getByRole('button', { name: 'Close', exact: true }).click()
    await page.goto(`/raw/${rawID}`)
    await expect(page.locator('.page').first()).toContainText(`${name} · release-1`)
    await page.goto(`/runs?provider=${source.id}`)
    await expect(page.getByRole('link', { name, exact: true })).toHaveAttribute(
      'href',
      '/runs/display-run',
    )
    await page.getByRole('link', { name, exact: true }).click()
    await expect(page.locator('.page').first()).toContainText(name)
    await page.goto('/logs?q=Example%20source%20activity')
    await expect(page.getByRole('link', { name: `Source: ${name}`, exact: true })).toHaveAttribute(
      'href',
      `/providers/${source.id}`,
    )
    await page.goto(`/metadata/identification?match_status=matched&match_provider=${source.id}`)
    await expect(page.getByRole('table')).toContainText(`${name} · release-1`)
    await page.getByRole('button', { name: 'Review Example Release', exact: true }).last().click()
    await expect(page.getByRole('dialog')).toContainText(`${name} · release-1`)
    await page.getByRole('button', { name: 'Close', exact: true }).click()
    await page.goto(`/addon/history/${requestID}`)
    await expect(
      page.getByRole('link', { name: `${name} · release-1`, exact: true }),
    ).toHaveAttribute('href', `/torrents?provider=${source.id}&q=release-1`)
  }
  const renamed = await (await page.request.get(`/api/providers/${source.id}`)).json()
  expect(
    (
      await page.request.delete(`/api/providers/${source.id}`, {
        headers,
        data: { revision: renamed.revision },
      })
    ).status(),
  ).toBe(204)
  await page.reload()
  // Explicit source deletion also removes the associated history and results.
  await expect(
    page.getByRole('link', { name: `${source.name} · release-1`, exact: true }),
  ).toHaveCount(0)
  expect((await page.request.get('/api/runs/display-run')).status()).toBe(404)
  await page.goto(`/runs?provider=${source.id}`)
  await expect(page.getByRole('link', { name: 'Renamed Library é', exact: true })).toHaveCount(0)
  expect(server.sourceRequests()).toBe(0)
})
