package store

// Replace untouched defaults and cap values above the new limit. Keep the
// other saved choices and increment the revision only when a value changes.
const connectionTimeoutsSchema = `
WITH updated AS (
 SELECT CASE WHEN revision=1 AND values='{"stremio_seconds":70,"torznab_seconds":20,"catalog_seconds":20}'::jsonb
  THEN '{"stremio_seconds":5,"torznab_seconds":5,"catalog_seconds":5}'::jsonb
  ELSE jsonb_build_object(
   'stremio_seconds',LEAST((values->>'stremio_seconds')::int,300),
   'torznab_seconds',LEAST((values->>'torznab_seconds')::int,300),
   'catalog_seconds',LEAST((values->>'catalog_seconds')::int,300))
 END AS values
 FROM aiosources.runtime_settings WHERE section='connections'
)
UPDATE aiosources.runtime_settings settings SET values=updated.values,revision=settings.revision+1
FROM updated WHERE settings.section='connections' AND settings.values IS DISTINCT FROM updated.values;
`
