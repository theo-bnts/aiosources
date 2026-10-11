import pg from 'pg'
import { test, expect } from './fixtures'

test('provider limits have the same safe labels in Stremio and debrid history', async ({
  page,
  server,
}) => {
  const db = new pg.Client({ connectionString: server.databaseURL })
  await db.connect()
  try {
    await db.query(`INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results,origin,kind,service,calls)
      VALUES(now(),'movie','imdb:tt1234567',20,'failed',1,1,0,0,0,0,0,false,'[]','public','playback','alldebrid',
      '[{"phase":"provider_add","duration_ms":10,"http_status":200,"error_code":"account_limit"},{"phase":"provider_playback","duration_ms":10,"http_status":429,"error_code":"rate_limit"}]')`)
    await db.query(`INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,processed,errors,results,finished_at,error)
      VALUES('provider-limits','cache','alldebrid','playback',0,'failed','availability',0,1,'{}',now(),
      'Cache verification failed. Saved availability is preserved and the check can be retried. The provider account reached its torrent or download limit.')`)
    await db.query(`INSERT INTO ingest.operation_events(run_id,phase,processed,errors,duration_ms,http_status,error_code)
      VALUES('provider-limits','provider_add',0,1,10,200,'account_limit'),
      ('provider-limits','provider_playback',0,1,10,429,'rate_limit')`)
  } finally {
    await db.end()
  }
  await page.goto('/addon/history')
  await page.getByRole('link', { name: 'imdb:tt1234567', exact: true }).click()
  const calls = page.getByRole('region', { name: 'Provider calls', exact: true })
  await expect(calls).toContainText('Torrent or download limit reached')
  await expect(calls).toContainText('Request rate limit reached')
  await expect(calls).toContainText('HTTP 429')
  await page.goto('/debrid/history')
  await page.getByRole('link', { name: 'Cache verification', exact: true }).click()
  const events = page.getByRole('region', { name: 'Operation events', exact: true })
  await expect(events).toContainText('Torrent or download limit reached')
  await expect(events).toContainText('Request rate limit reached')
  await expect(events).toContainText('HTTP 429')
  await expect(page.getByRole('alert')).toContainText('torrent or download limit')
})
