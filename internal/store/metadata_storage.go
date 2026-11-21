package store

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Dedicated per-catalog metadata relations grouped by catalog type. Shared
// catalog tables (aio_works, aio_titles) are attributed proportionally via
// PostgreSQL ANALYZE distributions on aio_works.media_type so measurement never
// scans catalog rows or takes collection leases.
const (
	videoMetadataRelations    = `'aio_seasons','aio_episodes','aio_inventory','aio_metadata_jobs','aio_title_variants','aio_season_requests'`
	gameMetadataRelations     = `'igdb_state'`
	softwareMetadataRelations = `'sw_sync_state','sw_source_entries','sw_icons','sw_catalog','sw_aliases','sw_external_ids'`
	musicMetadataRelations    = `'mb_state','mb_lookups','mb_artists','mb_aliases','mb_artist_credits','mb_artist_credit_names','mb_labels','mb_release_groups','mb_release_group_secondary_types','mb_releases','mb_release_years','mb_release_labels','mb_mediums','mb_urls','mb_entity_urls','mb_redirects','mb_cover_art','mb_rg_cover_art'`
	bookMetadataRelations     = `'ol_import_references','ol_redirects','ol_authors','ol_editions','ol_import_state','ol_changes_state','ol_change_jobs','ol_deleted'`
)

var canonicalMetadataStorageOrder = []string{"videos", "games", "software", "music", "books"}

func metadataStorageOrderIndex(kind string) int {
	if idx := slices.Index(canonicalMetadataStorageOrder, kind); idx >= 0 {
		return idx
	}
	return len(canonicalMetadataStorageOrder)
}

func metadataStorageEstimates(ctx context.Context, tx pgx.Tx) ([]model.MetadataStorageEstimate, error) {
	byType := make(map[string]*model.MetadataStorageEstimate, len(canonicalMetadataStorageOrder))
	for _, kind := range canonicalMetadataStorageOrder {
		byType[kind] = &model.MetadataStorageEstimate{Type: kind}
	}

	rows, err := tx.Query(ctx, `SELECT CASE
 WHEN c.relname IN (`+videoMetadataRelations+`) THEN 'videos'
 WHEN c.relname IN (`+gameMetadataRelations+`) THEN 'games'
 WHEN c.relname IN (`+softwareMetadataRelations+`) THEN 'software'
 WHEN c.relname IN (`+musicMetadataRelations+`) THEN 'music'
 WHEN c.relname IN (`+bookMetadataRelations+`) THEN 'books'
 WHEN c.relname IN ('aio_works','aio_titles') THEN 'shared'
 ELSE '' END AS kind,
COALESCE(SUM(pg_table_size(c.oid)),0)::bigint,
COALESCE(SUM(pg_indexes_size(c.oid)),0)::bigint
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname='aiosources' AND c.relkind IN ('r','m') AND c.relname IN (
 `+videoMetadataRelations+`,`+gameMetadataRelations+`,`+softwareMetadataRelations+`,`+musicMetadataRelations+`,`+bookMetadataRelations+`,'aio_works','aio_titles'
) GROUP BY kind`)
	if err != nil {
		return nil, databaseError("measure metadata catalog storage", err)
	}
	var sharedTable, sharedIndex int64
	for rows.Next() {
		var kind string
		var tableBytes, indexBytes int64
		if err := rows.Scan(&kind, &tableBytes, &indexBytes); err != nil {
			rows.Close()
			return nil, databaseError("read metadata catalog storage", err)
		}
		if kind == "shared" {
			sharedTable += tableBytes
			sharedIndex += indexBytes
			continue
		}
		if entry, ok := byType[kind]; ok {
			entry.TableBytes += tableBytes
			entry.IndexBytes += indexBytes
			entry.Bytes = entry.TableBytes + entry.IndexBytes
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, databaseError("read metadata catalog storage", err)
	}

	// Apportion shared aio_works and aio_titles allocation across metadata types
	// using PostgreSQL's pg_stats distribution on aio_works.media_type.
	if sharedTable > 0 || sharedIndex > 0 {
		if statsTx, err := tx.Begin(ctx); err == nil {
			shares := map[string]float64{}
			var totalShare float64
			srows, qerr := statsTx.Query(ctx, `SELECT CASE d.value
 WHEN 'movie' THEN 'videos'
 WHEN 'tv' THEN 'videos'
 WHEN 'game' THEN 'games'
 WHEN 'software' THEN 'software'
 WHEN 'music' THEN 'music'
 WHEN 'book' THEN 'books'
 ELSE '' END AS kind,
SUM(GREATEST(d.frequency,0))::double precision
FROM pg_stats s`+storageDistributionSQL+`
WHERE s.schemaname='aiosources' AND s.tablename='aio_works' AND s.attname='media_type' AND NOT s.inherited
GROUP BY kind`)
			if qerr == nil {
				for srows.Next() {
					var kind string
					var share float64
					if scanErr := srows.Scan(&kind, &share); scanErr == nil && share > 0 {
						if _, ok := byType[kind]; ok {
							shares[kind] += share
							totalShare += share
						}
					}
				}
				qerr = srows.Err()
				srows.Close()
			}
			if qerr == nil {
				if commitErr := statsTx.Commit(ctx); commitErr == nil && totalShare > 0 {
					for kind, share := range shares {
						entry := byType[kind]
						ratio := share / totalShare
						entry.TableBytes += int64(math.Round(float64(sharedTable) * ratio))
						entry.IndexBytes += int64(math.Round(float64(sharedIndex) * ratio))
						entry.Bytes = entry.TableBytes + entry.IndexBytes
						entry.Estimated = true
					}
				}
			} else {
				rollback(statsTx)
			}
		}
	}

	items := make([]model.MetadataStorageEstimate, 0, len(canonicalMetadataStorageOrder))
	for _, kind := range canonicalMetadataStorageOrder {
		items = append(items, *byType[kind])
	}
	slices.SortFunc(items, func(a, b model.MetadataStorageEstimate) int {
		if order := cmp.Compare(b.Bytes, a.Bytes); order != 0 {
			return order
		}
		return cmp.Compare(metadataStorageOrderIndex(a.Type), metadataStorageOrderIndex(b.Type))
	})
	return items, nil
}
