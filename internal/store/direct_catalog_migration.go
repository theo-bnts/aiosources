package store

// Zero denotes a direct published-occurrence download without a metadata work.
// Its saved input/fingerprint still bind it to the current local source row.
const directCatalogSchema = `
ALTER TABLE aiosources.operation_downloads DROP CONSTRAINT operation_downloads_work_id_check;
ALTER TABLE aiosources.operation_downloads ADD CHECK(work_id>=0);
CREATE FUNCTION aiosources.catalog_has_download_file(files JSONB) RETURNS BOOLEAN
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $files$
 SELECT EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(files)='array' THEN files ELSE '[]'::jsonb END) item
 CROSS JOIN LATERAL(SELECT replace(item->>'path',chr(92),'/') AS path) normalized
 WHERE jsonb_typeof(item->'path')='string' AND octet_length(normalized.path) BETWEEN 1 AND 4096
 AND (item->'size' IS NULL OR item->'size'='null'::jsonb OR aiosources.torrent_integer(item->'size')>=0)
 AND normalized.path !~* '(?:^|[/ ._-])sample(?:[ ._-]|$)'
 AND regexp_replace(regexp_replace(normalized.path,'^.*/',''),'\.[^.]*$','')
  !~* '^(?:read[ ._-]?me(?:[ ._-]?first)?|read[ ._-]?first|licen[cs]e|changelog|release[ ._-]?notes|file[ ._-]?list|info|cover|sample|preview|extrait)$');
$files$;
`
