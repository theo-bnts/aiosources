package store

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
)

func matchedMusicKind(record model.AIORecord, work *model.AIOWork) string {
	if work != nil && work.Type != "music" {
		return ""
	}
	kind := music.ParseRecord(record).Kind
	if kind == "" && work != nil && work.Type == "music" {
		return "music"
	}
	return kind
}

// Music candidate discovery prioritizes verified identifiers (MBID, Discogs ID,
// Wikidata ID, Barcode, CatalogNumber) before title/alias lookups, keeping
// queries fast even with millions of release groups.
const musicCandidateIDsSQL = `WITH candidate_ids AS (
 SELECT id FROM aiosources.aio_works WHERE $2<>'' AND media_type='music' AND musicbrainz_id<>'' AND musicbrainz_id=aiosources.mb_resolve_rg($2)
 UNION SELECT w.id FROM aiosources.mb_releases r JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid
  WHERE $2<>'' AND (r.gid=$2 OR r.id IN (SELECT target_id FROM aiosources.mb_redirects WHERE entity='release' AND source_gid=$2))
 UNION SELECT w.id FROM aiosources.mb_urls u JOIN aiosources.mb_entity_urls eu ON eu.url_id=u.id
  LEFT JOIN aiosources.mb_releases r ON eu.entity='release' AND r.id=eu.entity_id
  JOIN aiosources.mb_release_groups rg ON rg.id=CASE WHEN eu.entity='release_group' THEN eu.entity_id ELSE r.release_group_id END
  JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid
  WHERE $3<>'' AND u.discogs_id<>'' AND ((u.discogs_id=split_part($3,':',2) AND split_part($3,':',2)<>'' AND u.discogs_kind=split_part($3,':',1)) OR u.discogs_id=$3)
 UNION SELECT w.id FROM aiosources.mb_urls u JOIN aiosources.mb_entity_urls eu ON eu.url_id=u.id AND eu.entity='release_group'
  JOIN aiosources.mb_release_groups rg ON rg.id=eu.entity_id
  JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid
  WHERE $4<>'' AND u.wikidata_id<>'' AND u.wikidata_id=$4
 UNION SELECT w.id FROM aiosources.mb_releases r JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid
  WHERE $5<>'' AND r.barcode<>'' AND r.barcode=$5
 UNION SELECT w.id FROM aiosources.mb_release_labels rl JOIN aiosources.mb_releases r ON r.id=rl.release_id JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid
  WHERE $6<>'' AND rl.catalog_normalized<>'' AND rl.catalog_normalized=$6
 UNION SELECT id FROM aiosources.aio_works WHERE $2='' AND $3='' AND $4='' AND $5='' AND $6='' AND media_type='music' AND music_normalized<>'' AND (
  music_normalized=ANY($7::text[]) OR (NOT $8::boolean AND $1<>'' AND music_normalized % $1)
 )
 UNION SELECT t.work_id FROM aiosources.aio_titles t JOIN aiosources.aio_works w ON w.id=t.work_id
  WHERE $2='' AND $3='' AND $4='' AND $5='' AND $6='' AND w.media_type='music' AND (
   t.normalized=ANY($7::text[]) OR (NOT $8::boolean AND $1<>'' AND t.normalized % $1)
  )
)
SELECT ` + aioWorkColumns + ` FROM candidate_ids c
CROSS JOIN LATERAL (SELECT * FROM aiosources.aio_works WHERE id=c.id OFFSET 0) w
WHERE w.media_type='music' AND NOT w.adult AND ($9::int IS NULL OR $9=ANY(w.years))
ORDER BY w.id LIMIT 101`

func searchMusicCandidates(ctx context.Context, tx pgx.Tx, f model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
	titles := []string{}
	for _, t := range append(slices.Clone(f.Titles), f.Title) {
		n := model.NormalizeTitle(t)
		if n != "" && !slices.Contains(titles, n) {
			titles = append(titles, n)
		}
	}
	rows, err := tx.Query(ctx, musicCandidateIDsSQL, f.Title, f.MusicBrainz, f.DiscogsID, f.WikidataID, f.Barcode, f.CatalogNumber, titles, f.ExactOnly, f.Year)
	if err != nil {
		return nil, false, databaseError("find music candidates", err)
	}
	defer rows.Close()
	out := []model.AIOWork{}
	for rows.Next() {
		w, e := scanAIOWork(rows)
		if e != nil {
			return nil, false, databaseError("read music candidate", e)
		}
		if f.MusicArtist != "" {
			sig := music.ArtistSignature(f.MusicArtist)
			matched := music.ArtistSignature(w.Music.ArtistCredit) == sig
			if !matched {
				for _, a := range w.Music.Artists {
					if music.ArtistSignature(a) == sig {
						matched = true
						break
					}
				}
			}
			if !matched {
				for _, al := range w.Music.ArtistAliases {
					if music.ArtistSignature(al) == sig {
						matched = true
						break
					}
				}
			}
			if !matched && f.MusicBrainz == "" && f.DiscogsID == "" && f.WikidataID == "" && f.Barcode == "" && f.CatalogNumber == "" {
				continue
			}
		}
		w.Music.YearVerified = f.Year != nil && slices.Contains(w.Years, *f.Year)
		out = append(out, w)
		if len(out) > aioCandidateLimit {
			break
		}
	}
	return out, len(out) > aioCandidateLimit, databaseError("find music candidates", rows.Err())
}

// Scope catalog music search to works present in the local source catalog,
// mirroring catalogBookScopeCTE so artist, alias and label substring filters
// never scan millions of unrelated MusicBrainz rows.
const catalogMusicScopeCTE = `catalog_music_works AS MATERIALIZED(
 SELECT w.id,w.musicbrainz_id FROM catalog_matched_ids m JOIN aiosources.aio_works w ON w.id=m.id WHERE w.media_type='music' AND w.musicbrainz_id<>''),
catalog_music_rgs AS MATERIALIZED(
 SELECT w.id AS work_id,rg.id AS rg_id,rg.artist_credit FROM catalog_music_works w JOIN aiosources.mb_release_groups rg ON rg.gid=w.musicbrainz_id),
catalog_music_artists AS MATERIALIZED(
 SELECT DISTINCT rg.work_id,a.id AS artist_id,a.normalized FROM catalog_music_rgs rg JOIN aiosources.mb_artist_credit_names acn ON acn.artist_credit=rg.artist_credit JOIN aiosources.mb_artists a ON a.id=acn.artist_id WHERE a.normalized<>''),
catalog_music_aliases AS MATERIALIZED(
 SELECT DISTINCT ma.work_id,al.normalized FROM catalog_music_artists ma JOIN aiosources.mb_aliases al ON al.entity='artist' AND al.entity_id=ma.artist_id WHERE al.normalized<>''),
catalog_music_labels AS MATERIALIZED(
 SELECT DISTINCT rg.work_id,l.normalized FROM catalog_music_rgs rg JOIN aiosources.mb_releases r ON r.release_group_id=rg.rg_id JOIN aiosources.mb_release_labels rl ON rl.release_id=r.id JOIN aiosources.mb_labels l ON l.id=rl.label_id WHERE l.normalized<>'')`

func musicDiscogsPredicate(queryArg string) string {
	return `u.discogs_id<>'' AND ((u.discogs_id=split_part(` + queryArg + `,':',2) AND split_part(` + queryArg + `,':',2)<>'' AND u.discogs_kind=split_part(` + queryArg + `,':',1)) OR u.discogs_id=` + queryArg + `)`
}

func catalogMusicCandidates(queryArg, normalizedArg, matching string) string {
	resolved := `(SELECT aiosources.mb_resolve_rg(` + queryArg + `))`
	return `
 UNION ALL SELECT id,9,1::real FROM catalog_music_works WHERE musicbrainz_id=` + resolved + `
 UNION ALL SELECT w.id,9,1::real FROM aiosources.mb_releases r JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN catalog_music_works w ON w.musicbrainz_id=rg.gid WHERE r.gid=` + queryArg + ` OR (r.barcode<>'' AND r.barcode=` + queryArg + `)
 UNION ALL SELECT w.id,9,1::real FROM aiosources.mb_urls u JOIN aiosources.mb_entity_urls eu ON eu.url_id=u.id LEFT JOIN aiosources.mb_releases r ON eu.entity='release' AND r.id=eu.entity_id JOIN aiosources.mb_release_groups rg ON rg.id=CASE WHEN eu.entity='release_group' THEN eu.entity_id ELSE r.release_group_id END JOIN catalog_music_works w ON w.musicbrainz_id=rg.gid WHERE (u.wikidata_id<>'' AND u.wikidata_id=` + queryArg + `) OR (` + musicDiscogsPredicate(queryArg) + `)
 UNION ALL SELECT w.id,9,1::real FROM aiosources.mb_release_labels rl JOIN aiosources.mb_releases r ON r.id=rl.release_id JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN catalog_music_works w ON w.musicbrainz_id=rg.gid WHERE rl.catalog_normalized<>'' AND rl.catalog_normalized=` + queryArg + `
 UNION ALL SELECT work_id,CASE WHEN normalized=` + normalizedArg + ` THEN 5 ELSE 2 END,similarity(` + normalizedArg + `,normalized) FROM catalog_music_artists WHERE ` + matching + `
 UNION ALL SELECT work_id,CASE WHEN normalized=` + normalizedArg + ` THEN 4 ELSE 2 END,similarity(` + normalizedArg + `,normalized) FROM catalog_music_aliases WHERE ` + matching + `
 UNION ALL SELECT work_id,2,similarity(` + normalizedArg + `,normalized) FROM catalog_music_labels WHERE ` + matching
}

// musicCatalogCandidates returns SQL UNION clauses for full-catalog review
// candidate discovery, using indexed lookups so searches never seq-scan
// millions of MusicBrainz artist, alias, label or URL rows.
func musicCatalogCandidates(queryArg, normalizedArg string) string {
	resolved := `(SELECT aiosources.mb_resolve_rg(` + queryArg + `))`
	return `
 UNION ALL SELECT id,9,1::real FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id<>'' AND musicbrainz_id=` + resolved + `
 UNION ALL SELECT w.id,9,1::real FROM aiosources.mb_releases r JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid WHERE r.gid=` + queryArg + ` OR (r.barcode<>'' AND r.barcode=` + queryArg + `)
 UNION ALL SELECT w.id,9,1::real FROM aiosources.mb_urls u JOIN aiosources.mb_entity_urls eu ON eu.url_id=u.id LEFT JOIN aiosources.mb_releases r ON eu.entity='release' AND r.id=eu.entity_id JOIN aiosources.mb_release_groups rg ON rg.id=CASE WHEN eu.entity='release_group' THEN eu.entity_id ELSE r.release_group_id END JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid WHERE (u.wikidata_id<>'' AND u.wikidata_id=` + queryArg + `) OR (` + musicDiscogsPredicate(queryArg) + `)
 UNION ALL SELECT w.id,9,1::real FROM aiosources.mb_release_labels rl JOIN aiosources.mb_releases r ON r.id=rl.release_id JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid WHERE rl.catalog_normalized<>'' AND rl.catalog_normalized=` + queryArg + `
 UNION ALL SELECT w.id,5,1::real FROM aiosources.mb_artists a JOIN aiosources.mb_artist_credit_names acn ON acn.artist_id=a.id JOIN aiosources.mb_release_groups rg ON rg.artist_credit=acn.artist_credit JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid WHERE a.normalized<>'' AND a.normalized=` + normalizedArg + `
 UNION ALL SELECT w.id,4,1::real FROM aiosources.mb_aliases al JOIN aiosources.mb_artist_credit_names acn ON acn.artist_id=al.entity_id JOIN aiosources.mb_release_groups rg ON rg.artist_credit=acn.artist_credit JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid WHERE al.entity='artist' AND al.normalized<>'' AND al.normalized=` + normalizedArg + `
 UNION ALL SELECT w.id,2,1::real FROM aiosources.mb_labels l JOIN aiosources.mb_release_labels rl ON rl.label_id=l.id JOIN aiosources.mb_releases r ON r.id=rl.release_id JOIN aiosources.mb_release_groups rg ON rg.id=r.release_group_id JOIN aiosources.aio_works w ON w.media_type='music' AND w.musicbrainz_id<>'' AND w.musicbrainz_id=rg.gid WHERE l.normalized<>'' AND l.normalized=` + normalizedArg
}
