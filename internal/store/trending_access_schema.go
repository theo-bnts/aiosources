package store

// Separate Trending from Search permissions for public visitors and trusted
// accounts. Existing installations inherit their current Search choice so
// upgrades neither withdraw active access nor grant disabled browse features.
const trendingAccessSchema = `
ALTER TABLE aiosources.aio_public_access
 ADD COLUMN trending_enabled boolean NOT NULL DEFAULT true;
UPDATE aiosources.aio_public_access SET trending_enabled=search_enabled WHERE singleton;
UPDATE aiosources.trusted_users
 SET permissions=jsonb_set(permissions,'{trending_enabled}',to_jsonb(COALESCE((permissions->>'search_enabled')::boolean,false)),true)
 WHERE NOT (permissions ? 'trending_enabled');
`
