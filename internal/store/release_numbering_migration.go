package store

// Revisit only numbering affected by the parser correction. Keep imported
// torrents, manual reviews and every already queued version intact.
const releaseNumberingRecheckSchema = `
INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
SELECT t.provider_id,t.source_id FROM aiosources.torrents t
JOIN aiosources.aio_matches m USING(provider_id,source_id)
WHERE translate(t.fields->>'title','._','  ') ~* '(\m(s[0-9]{1,3}|(season|saison)[[:space:]]*[0-9]{1,3})[[:space:]]+[-–][[:space:]]+[0-9]|\m[0-9]{1,3}(st|nd|rd|th)[[:space:]]+season\M|\mS[0-9]{1,3}[[:space:]]*E[0-9].*(episode|ep)[[:space:]]*[0-9]| - (18|19|20|21)[0-9]{2}\M| - [0-9]{1,4}[[:space:]]*[-–][[:space:]]*[0-9]{1,4})'
AND NOT EXISTS(SELECT 1 FROM aiosources.aio_match_reviews r WHERE r.provider_id=t.provider_id AND r.source_id=t.source_id AND r.active)
ON CONFLICT DO NOTHING;
`
