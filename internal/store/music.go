package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
)

func MusicSelection(c model.AIOConfig) music.ImportSelection {
	types, ok := music.ImportTypes(c.MusicBrainzTypes)
	if !ok {
		types = music.DefaultPrimaryTypes
	}
	langs, _ := music.ImportLanguages(c.MusicBrainzLanguages)
	return music.ImportSelection{
		OfficialOnly:  c.MusicBrainzOfficialOnly,
		ExcludeSpoken: c.MusicBrainzExcludeSpoken,
		IncludeCovers: c.MusicBrainzIncludeCovers,
		Types:         types,
		Languages:     langs,
	}
}

func MusicSelectionKey(c model.AIOConfig) string {
	return MusicSelection(c).Key()
}

func (s *Store) MusicStatus(ctx context.Context) (model.MusicStatus, error) {
	var v model.MusicStatus
	err := s.pool.QueryRow(ctx, `SELECT complete,works,releases,artists,labels,covers,imported,position,filtered,skipped,
 phase,selection,snapshot,snapshot_at,replication_sequence,schema_sequence,generation,last_import,last_sync,
 changes_through,last_packet_at,needs_export,recovery_reason,schema_unsupported,retry_at,error
 FROM aiosources.mb_state WHERE singleton`).Scan(
		&v.Complete, &v.Works, &v.Releases, &v.Artists, &v.Labels, &v.Covers, &v.Imported, &v.Position, &v.Filtered, &v.Skipped,
		&v.Phase, &v.Selection, &v.Snapshot, &v.SnapshotAt, &v.ReplicationSequence, &v.SchemaSequence, &v.Generation,
		&v.LastImport, &v.LastSync, &v.ChangesThrough, &v.LastPacketAt, &v.NeedsExport, &v.RecoveryReason,
		&v.SchemaUnsupported, &v.RetryAt, &v.Error,
	)
	return v, databaseError("read music information status", err)
}

func (s *Store) NextMusicUpdate(ctx context.Context, minutes int, live bool) (time.Time, error) {
	if minutes < 15 || minutes > 1440 {
		return time.Time{}, model.ErrInvalid
	}
	var next time.Time
	// An export-only catalog follows the bulk polling interval until it has
	// both a Live Data Feed token and a published replication sequence.
	err := s.pool.QueryRow(ctx, `SELECT GREATEST(COALESCE(retry_at,to_timestamp(0)),
 CASE WHEN NOT complete THEN now()
      WHEN needs_export THEN COALESCE(retry_at,now())
      WHEN $2 AND replication_sequence>0 THEN COALESCE(GREATEST(last_sync,checked_at)+make_interval(mins=>$1),now())
      ELSE COALESCE(GREATEST(last_sync,checked_at)+make_interval(mins=>GREATEST($1,360)),now())
 END) FROM aiosources.mb_state WHERE singleton`, minutes, live).Scan(&next)
	return next, databaseError("read music update schedule", err)
}

func (s *Store) MusicDue(ctx context.Context, minutes int, live bool) (bool, error) {
	next, err := s.NextMusicUpdate(ctx, minutes, live)
	if err != nil {
		return false, err
	}
	return !next.After(time.Now()), nil
}

func prepareMusicImport(ctx context.Context, tx pgx.Tx, c model.AIOConfig) error {
	key := MusicSelectionKey(c)
	_, err := tx.Exec(ctx, `UPDATE aiosources.mb_state SET selection=$1,complete=FALSE,
 retry_at=NULL,error='' WHERE singleton`, key)
	return databaseError("prepare music catalog filters", err)
}

func (s *Store) TouchMusicCheck(ctx context.Context) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.mb_state SET phase='idle',checked_at=now(),error='',retry_at=NULL WHERE singleton`)
	return databaseError("record music check time", err)
}

func (s *Store) BeginMusicDump(ctx context.Context, sel music.ImportSelection, recoveryReason string) (int64, error) {
	var gen int64
	phase := "dump"
	if recoveryReason != "" {
		phase = "recovery"
	}
	err := s.workerPool().QueryRow(ctx, `UPDATE aiosources.mb_state SET generation=generation+1,phase=$1,
 complete=complete AND (selection='' OR selection=$2),selection=$2,
 imported=0,position=0,filtered=0,skipped=0,needs_export=TRUE,
 recovery_reason=$3,schema_unsupported=FALSE,error='',retry_at=NULL,checked_at=now()
 WHERE singleton RETURNING generation`, phase, sel.Key(), recoveryReason).Scan(&gen)
	return gen, databaseError("begin music catalog import", err)
}

func (s *Store) SetMusicPhase(ctx context.Context, phase string) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.mb_state SET phase=$1 WHERE singleton`, phase)
	return databaseError("update music import phase", err)
}

func (s *Store) SaveMusicDumpBatch(ctx context.Context, generation int64, b music.DumpTableBatch) error {
	tx, err := s.workerPool().Begin(ctx)
	if err != nil {
		return databaseError("begin music dump batch", err)
	}
	defer rollback(tx)

	batch := &pgx.Batch{}
	for _, r := range b.Lookups {
		batch.Queue(`INSERT INTO aiosources.mb_lookups(kind,id,name) VALUES($1,$2,$3) ON CONFLICT(kind,id) DO UPDATE SET name=EXCLUDED.name`, r.Kind, r.ID, r.Name)
	}
	for _, r := range b.Languages {
		batch.Queue(`INSERT INTO aiosources.mb_lookups(kind,id,name) VALUES('language',$1,$2) ON CONFLICT(kind,id) DO UPDATE SET name=EXCLUDED.name`, r.ID, r.Code)
	}
	for _, r := range b.Artists {
		batch.Queue(`INSERT INTO aiosources.mb_artists(id,gid,name,sort_name,normalized,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,name=EXCLUDED.name,sort_name=EXCLUDED.sort_name,normalized=EXCLUDED.normalized,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.GID, r.Name, r.SortName, model.NormalizeTitle(r.Name), generation)
	}
	for _, r := range b.Aliases {
		batch.Queue(`INSERT INTO aiosources.mb_aliases(entity,id,entity_id,name,normalized,locale,mb_seen) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(entity,id) DO UPDATE SET entity_id=EXCLUDED.entity_id,name=EXCLUDED.name,normalized=EXCLUDED.normalized,locale=EXCLUDED.locale,mb_seen=EXCLUDED.mb_seen`,
			r.Entity, r.ID, r.EntityID, r.Name, model.NormalizeTitle(r.Name), r.Locale, generation)
	}
	for _, r := range b.ArtistCredits {
		batch.Queue(`INSERT INTO aiosources.mb_artist_credits(id,name,normalized,mb_seen) VALUES($1,$2,$3,$4)
 ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,normalized=EXCLUDED.normalized,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.Name, model.NormalizeTitle(r.Name), generation)
	}
	for _, r := range b.CreditNames {
		batch.Queue(`INSERT INTO aiosources.mb_artist_credit_names(artist_credit,position,artist_id,name,join_phrase,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(artist_credit,position) DO UPDATE SET artist_id=EXCLUDED.artist_id,name=EXCLUDED.name,join_phrase=EXCLUDED.join_phrase,mb_seen=EXCLUDED.mb_seen`,
			r.ArtistCredit, r.Position, r.ArtistID, r.Name, r.JoinPhrase, generation)
	}
	for _, r := range b.ReleaseGroups {
		batch.Queue(`INSERT INTO aiosources.mb_release_groups(id,gid,name,normalized,artist_credit,primary_type,mb_seen) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,name=EXCLUDED.name,normalized=EXCLUDED.normalized,artist_credit=EXCLUDED.artist_credit,primary_type=EXCLUDED.primary_type,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.GID, r.Name, model.NormalizeTitle(r.Name), r.ArtistCredit, r.PrimaryType, generation)
	}
	for _, r := range b.SecondaryTypes {
		batch.Queue(`INSERT INTO aiosources.mb_release_group_secondary_types(release_group_id,secondary_type_id,mb_seen) VALUES($1,$2,$3)
 ON CONFLICT(release_group_id,secondary_type_id) DO UPDATE SET mb_seen=EXCLUDED.mb_seen`, r.ReleaseGroupID, r.SecondaryTypeID, generation)
	}
	for _, r := range b.Releases {
		batch.Queue(`INSERT INTO aiosources.mb_releases(id,gid,release_group_id,name,normalized,artist_credit,status,language_id,barcode,mb_seen) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,release_group_id=EXCLUDED.release_group_id,name=EXCLUDED.name,normalized=EXCLUDED.normalized,artist_credit=EXCLUDED.artist_credit,status=EXCLUDED.status,language_id=EXCLUDED.language_id,barcode=EXCLUDED.barcode,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.GID, r.ReleaseGroupID, r.Name, model.NormalizeTitle(r.Name), r.ArtistCredit, r.Status, r.LanguageID, r.Barcode, generation)
	}
	for _, r := range b.ReleaseDates {
		batch.Queue(`INSERT INTO aiosources.mb_release_years(release_id,year,mb_seen,occurrences) VALUES($1,$2,$3,1)
 ON CONFLICT(release_id,year) DO UPDATE SET mb_seen=EXCLUDED.mb_seen,
 occurrences=CASE WHEN aiosources.mb_release_years.mb_seen=EXCLUDED.mb_seen THEN aiosources.mb_release_years.occurrences+1 ELSE 1 END`, r.ReleaseID, r.Year, generation)
	}
	for _, r := range b.Labels {
		batch.Queue(`INSERT INTO aiosources.mb_labels(id,gid,name,normalized,mb_seen) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,name=EXCLUDED.name,normalized=EXCLUDED.normalized,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.GID, r.Name, model.NormalizeTitle(r.Name), generation)
	}
	for _, r := range b.ReleaseLabels {
		batch.Queue(`INSERT INTO aiosources.mb_release_labels(id,release_id,label_id,catalog_number,catalog_normalized,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET release_id=EXCLUDED.release_id,label_id=EXCLUDED.label_id,catalog_number=EXCLUDED.catalog_number,catalog_normalized=EXCLUDED.catalog_normalized,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.ReleaseID, r.LabelID, r.CatalogNumber, music.CatalogNumber(r.CatalogNumber), generation)
	}
	for _, r := range b.Mediums {
		batch.Queue(`INSERT INTO aiosources.mb_mediums(id,release_id,position,format_id,track_count,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET release_id=EXCLUDED.release_id,position=EXCLUDED.position,format_id=EXCLUDED.format_id,track_count=EXCLUDED.track_count,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.ReleaseID, r.Position, r.FormatID, r.TrackCount, generation)
	}
	for _, r := range b.URLs {
		batch.Queue(`INSERT INTO aiosources.mb_urls(id,discogs_kind,discogs_id,wikidata_id,mb_seen) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(id) DO UPDATE SET discogs_kind=EXCLUDED.discogs_kind,discogs_id=EXCLUDED.discogs_id,wikidata_id=EXCLUDED.wikidata_id,mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.DiscogsKind, r.DiscogsID, r.WikidataID, generation)
	}
	for _, r := range b.EntityURLs {
		batch.Queue(`INSERT INTO aiosources.mb_entity_urls(entity,link_id,entity_id,url_id,mb_seen) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(entity,link_id) DO UPDATE SET entity_id=EXCLUDED.entity_id,url_id=EXCLUDED.url_id,mb_seen=EXCLUDED.mb_seen`,
			r.Entity, r.LinkID, r.EntityID, r.URLID, generation)
	}
	for _, r := range b.Redirects {
		batch.Queue(`INSERT INTO aiosources.mb_redirects(entity,source_gid,target_id,mb_seen) VALUES($1,$2,$3,$4)
 ON CONFLICT(entity,source_gid) DO UPDATE SET target_id=EXCLUDED.target_id,mb_seen=EXCLUDED.mb_seen`,
			r.Entity, r.SourceGID, r.TargetID, generation)
	}
	for _, r := range b.CoverArts {
		batch.Queue(`INSERT INTO aiosources.mb_cover_art(id,release_id,ordering,mb_seen) VALUES($1,$2,$3,$4)
 ON CONFLICT(id) DO UPDATE SET release_id=EXCLUDED.release_id,ordering=EXCLUDED.ordering,
  is_front=CASE WHEN aiosources.mb_cover_art.mb_seen=EXCLUDED.mb_seen THEN aiosources.mb_cover_art.is_front ELSE FALSE END,
  mb_seen=EXCLUDED.mb_seen`,
			r.ID, r.ReleaseID, r.Ordering, generation)
	}
	for _, r := range b.CoverArtTypes {
		batch.Queue(`INSERT INTO aiosources.mb_cover_art(id,release_id,is_front,mb_seen) VALUES($1,0,TRUE,$2)
 ON CONFLICT(id) DO UPDATE SET is_front=TRUE,mb_seen=EXCLUDED.mb_seen`, r.CoverArtID, generation)
	}
	for _, r := range b.RGCoverArts {
		batch.Queue(`INSERT INTO aiosources.mb_rg_cover_art(release_group_id,release_id,mb_seen) VALUES($1,$2,$3)
 ON CONFLICT(release_group_id) DO UPDATE SET release_id=EXCLUDED.release_id,mb_seen=EXCLUDED.mb_seen`,
			r.ReleaseGroupID, r.ReleaseID, generation)
	}

	if batch.Len() > 0 {
		results := tx.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err = results.Exec(); err != nil {
				_ = results.Close()
				return databaseError("save music dump batch", err)
			}
		}
		if err = results.Close(); err != nil {
			return databaseError("close music dump batch", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE aiosources.mb_state SET position=position+$1,imported=imported+$2,skipped=skipped+$3 WHERE singleton`, b.Read, b.Len(), b.Skipped); err != nil {
		return databaseError("advance music dump progress", err)
	}
	return databaseError("commit music dump batch", tx.Commit(ctx))
}

// PruneUnlinkedMusicURLs removes entity_url links whose url_id is not a kept
// Discogs/Wikidata URL in bounded batches so mb_entity_urls stays small after
// streaming a dump without holding a single multi-million-row transaction.
func (s *Store) pruneUnlinkedMusicURLs(ctx context.Context) error {
	const batchSize = 50000
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tag, err := s.workerPool().Exec(ctx, `DELETE FROM aiosources.mb_entity_urls WHERE ctid IN (
 SELECT eu.ctid FROM aiosources.mb_entity_urls eu
 WHERE NOT EXISTS(SELECT 1 FROM aiosources.mb_urls u WHERE u.id=eu.url_id)
 LIMIT $1
)`, batchSize)
		if err != nil {
			return databaseError("prune non-external music urls", err)
		}
		if tag.RowsAffected() < batchSize {
			return nil
		}
	}
}

type projectedReleaseGroup struct {
	ID            int
	GID           string
	Name          string
	ArtistCredit  string
	Artists       []string
	ArtistAliases []string
	PrimaryType   string
	Secondary     []string
	Labels        []string
	CatalogNos    []string
	Barcodes      []string
	Formats       []string
	TrackCounts   []int
	DiscogsIDs    []string
	WikidataID    string
	Languages     []string
	Years         []int
	Titles        []string
	OfficialCount int
	HasCover      bool
	Reviewed      bool
}

const projectMusicChunkSQL = `
SELECT rg.id, rg.gid, rg.name,
 COALESCE(ac.name,''),
 COALESCE(ARRAY(SELECT DISTINCT a.name FROM aiosources.mb_artist_credit_names acn JOIN aiosources.mb_artists a ON a.id=acn.artist_id WHERE acn.artist_credit=rg.artist_credit ORDER BY a.name),'{}'::text[]),
 COALESCE(ARRAY(SELECT DISTINCT al.name FROM aiosources.mb_artist_credit_names acn JOIN aiosources.mb_aliases al ON al.entity='artist' AND al.entity_id=acn.artist_id WHERE acn.artist_credit=rg.artist_credit ORDER BY al.name LIMIT 100),'{}'::text[]),
 COALESCE(lower(pt.name),''),
 COALESCE(ARRAY(SELECT DISTINCT lower(st.name) FROM aiosources.mb_release_group_secondary_types rgs JOIN aiosources.mb_lookups st ON st.kind='release_group_secondary_type' AND st.id=rgs.secondary_type_id WHERE rgs.release_group_id=rg.id ORDER BY lower(st.name)),'{}'::text[]),
 COALESCE(ARRAY(SELECT DISTINCT l.name FROM aiosources.mb_releases r JOIN aiosources.mb_release_labels rl ON rl.release_id=r.id JOIN aiosources.mb_labels l ON l.id=rl.label_id WHERE r.release_group_id=rg.id ORDER BY l.name LIMIT 40),'{}'::text[]),
 COALESCE(ARRAY(SELECT DISTINCT rl.catalog_number FROM aiosources.mb_releases r JOIN aiosources.mb_release_labels rl ON rl.release_id=r.id WHERE r.release_group_id=rg.id AND rl.catalog_number<>'' ORDER BY rl.catalog_number LIMIT 60),'{}'::text[]),
 COALESCE(ARRAY(SELECT DISTINCT r.barcode FROM aiosources.mb_releases r WHERE r.release_group_id=rg.id AND r.barcode<>'' ORDER BY r.barcode LIMIT 60),'{}'::text[]),
 COALESCE(ARRAY(SELECT DISTINCT mf.name FROM aiosources.mb_releases r JOIN aiosources.mb_mediums m ON m.release_id=r.id JOIN aiosources.mb_lookups mf ON mf.kind='medium_format' AND mf.id=m.format_id WHERE r.release_group_id=rg.id ORDER BY mf.name LIMIT 20),'{}'::text[]),
 COALESCE(ARRAY(SELECT DISTINCT m.track_count FROM aiosources.mb_releases r JOIN aiosources.mb_mediums m ON m.release_id=r.id WHERE r.release_group_id=rg.id AND m.track_count>0 ORDER BY m.track_count LIMIT 20),'{}'::int[]),
 COALESCE(ARRAY(SELECT DISTINCT discogs FROM (
  SELECT u.discogs_kind||':'||u.discogs_id AS discogs FROM aiosources.mb_entity_urls eu JOIN aiosources.mb_urls u ON u.id=eu.url_id
   WHERE eu.entity='release_group' AND eu.entity_id=rg.id AND u.discogs_id<>''
  UNION ALL
  SELECT u.discogs_kind||':'||u.discogs_id AS discogs FROM aiosources.mb_releases r JOIN aiosources.mb_entity_urls eu ON eu.entity='release' AND eu.entity_id=r.id JOIN aiosources.mb_urls u ON u.id=eu.url_id
   WHERE r.release_group_id=rg.id AND u.discogs_id<>''
 ) d ORDER BY 1 LIMIT 30),'{}'::text[]),
 COALESCE((SELECT u.wikidata_id FROM aiosources.mb_entity_urls eu JOIN aiosources.mb_urls u ON u.id=eu.url_id WHERE eu.entity='release_group' AND eu.entity_id=rg.id AND u.wikidata_id<>'' ORDER BY u.id LIMIT 1),''),
 COALESCE(ARRAY(SELECT DISTINCT lang.name FROM aiosources.mb_releases r JOIN aiosources.mb_lookups lang ON lang.kind='language' AND lang.id=r.language_id WHERE r.release_group_id=rg.id ORDER BY lang.name LIMIT 20),'{}'::text[]),
 COALESCE(ARRAY(SELECT DISTINCT ry.year FROM aiosources.mb_releases r JOIN aiosources.mb_release_years ry ON ry.release_id=r.id WHERE r.release_group_id=rg.id ORDER BY ry.year),'{}'::int[]),
 COALESCE(ARRAY(SELECT DISTINCT title FROM (
  SELECT rg.name AS title
  UNION ALL SELECT name FROM aiosources.mb_aliases WHERE entity='release_group' AND entity_id=rg.id
  UNION ALL SELECT name FROM aiosources.mb_releases WHERE release_group_id=rg.id
  UNION ALL SELECT al.name FROM aiosources.mb_releases r JOIN aiosources.mb_aliases al ON al.entity='release' AND al.entity_id=r.id WHERE r.release_group_id=rg.id
 ) t WHERE title<>'' ORDER BY title LIMIT 100),'{}'::text[]),
 (SELECT count(*) FROM aiosources.mb_releases r LEFT JOIN aiosources.mb_lookups rs ON rs.kind='release_status' AND rs.id=r.status WHERE r.release_group_id=rg.id AND lower(COALESCE(rs.name,''))='official')::int,
 ($1::boolean AND (
  EXISTS(SELECT 1 FROM aiosources.mb_rg_cover_art c JOIN aiosources.mb_cover_art ca ON ca.release_id=c.release_id AND ca.is_front WHERE c.release_group_id=rg.id)
  OR EXISTS(SELECT 1 FROM aiosources.mb_releases r JOIN aiosources.mb_cover_art ca ON ca.release_id=r.id AND ca.is_front WHERE r.release_group_id=rg.id)
 )),
 EXISTS(SELECT 1 FROM aiosources.aio_match_reviews rev WHERE rev.active AND rev.media_type='music' AND rev.musicbrainz_id=rg.gid)
FROM aiosources.mb_release_groups rg
LEFT JOIN aiosources.mb_artist_credits ac ON ac.id=rg.artist_credit
LEFT JOIN aiosources.mb_lookups pt ON pt.kind='release_group_primary_type' AND pt.id=rg.primary_type
`

func (rg projectedReleaseGroup) eligible(sel music.ImportSelection) bool {
	if rg.Reviewed {
		return true
	}
	if sel.ExcludeSpoken {
		for _, sec := range rg.Secondary {
			if slices.Contains(music.SpokenSecondaryTypes, sec) {
				return false
			}
		}
	}
	if len(sel.Types) > 0 {
		pt := rg.PrimaryType
		if pt == "" {
			pt = "other"
		}
		if !slices.Contains(sel.Types, pt) {
			return false
		}
	}
	if sel.OfficialOnly && rg.OfficialCount == 0 {
		return false
	}
	if len(sel.Languages) > 0 {
		matchedLang := false
		for _, l := range rg.Languages {
			if slices.Contains(sel.Languages, l) {
				matchedLang = true
				break
			}
		}
		if !matchedLang {
			return false
		}
	}
	return true
}

func (rg projectedReleaseGroup) toWork() model.AIOWork {
	poster := ""
	if rg.HasCover {
		poster = rg.GID
	}
	years := rg.Years
	if years == nil {
		years = []int{}
	}
	return model.AIOWork{
		Type:         "music",
		MusicBrainz:  rg.GID,
		DisplayTitle: rg.Name,
		LocalizedTitles: map[string]string{
			"en": rg.Name,
		},
		LocalizedPosters: map[string]string{},
		PosterPath:       poster,
		Years:            years,
		Titles:           projectedCatalogTitles(rg.Name, rg.Titles),
		Music: model.MusicInformation{
			ArtistCredit:   rg.ArtistCredit,
			Artists:        rg.Artists,
			ArtistAliases:  rg.ArtistAliases,
			PrimaryType:    rg.PrimaryType,
			SecondaryTypes: rg.Secondary,
			Labels:         rg.Labels,
			CatalogNumbers: rg.CatalogNos,
			Barcodes:       rg.Barcodes,
			Formats:        rg.Formats,
			TrackCounts:    rg.TrackCounts,
			DiscogsIDs:     rg.DiscogsIDs,
			WikidataID:     rg.WikidataID,
			Languages:      rg.Languages,
			HasCover:       rg.HasCover,
		},
	}
}

type sqlQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func readProjectedMusicChunk(ctx context.Context, q sqlQueryer, includeCovers bool, rgIDs []int) ([]projectedReleaseGroup, error) {
	rows, err := q.Query(ctx, projectMusicChunkSQL+` WHERE rg.id=ANY($2::int[]) ORDER BY rg.id`, includeCovers, rgIDs)
	if err != nil {
		return nil, databaseError("read music projection rows", err)
	}
	ordered := make([]projectedReleaseGroup, 0, len(rgIDs))
	for rows.Next() {
		var rg projectedReleaseGroup
		if err = rows.Scan(
			&rg.ID, &rg.GID, &rg.Name, &rg.ArtistCredit, &rg.Artists, &rg.ArtistAliases,
			&rg.PrimaryType, &rg.Secondary, &rg.Labels, &rg.CatalogNos, &rg.Barcodes,
			&rg.Formats, &rg.TrackCounts, &rg.DiscogsIDs, &rg.WikidataID, &rg.Languages,
			&rg.Years, &rg.Titles, &rg.OfficialCount, &rg.HasCover, &rg.Reviewed,
		); err != nil {
			rows.Close()
			return nil, err
		}
		ordered = append(ordered, rg)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return ordered, nil
}

// ProjectMusicCatalog materializes the validated snapshot into searchable works.
func (s *Store) ProjectMusicCatalog(ctx context.Context, generation int64, sel music.ImportSelection, onChunk func(int64)) (worksCount, filteredCount, coversCount int64, err error) {
	if err = s.pruneMusicDumpRows(ctx, generation, sel.IncludeCovers); err != nil {
		return 0, 0, 0, err
	}

	after := 0
	for {
		if err := ctx.Err(); err != nil {
			return worksCount, filteredCount, coversCount, err
		}
		rows, err := s.workerPool().Query(ctx, `SELECT id FROM aiosources.mb_release_groups WHERE id>$1 ORDER BY id LIMIT 500`, after)
		if err != nil {
			return worksCount, filteredCount, coversCount, databaseError("scan music release groups", err)
		}
		chunk := make([]int, 0, 500)
		for rows.Next() {
			var id int
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return worksCount, filteredCount, coversCount, err
			}
			chunk = append(chunk, id)
			after = id
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return worksCount, filteredCount, coversCount, err
		}
		if len(chunk) == 0 {
			break
		}
		// Read and assemble the projection chunk before acquiring aio_state FOR UPDATE
		// so heavy staging joins never block matching or other metadata writers.
		ordered, err := readProjectedMusicChunk(ctx, s.workerPool(), sel.IncludeCovers, chunk)
		if err != nil {
			return worksCount, filteredCount, coversCount, err
		}
		tx, err := s.workerPool().Begin(ctx)
		if err != nil {
			return worksCount, filteredCount, coversCount, err
		}
		w, f, c, _, _, err := writeProjectedMusicTx(ctx, tx, generation, sel, ordered, false)
		if err != nil {
			rollback(tx)
			return worksCount, filteredCount, coversCount, err
		}
		if f > 0 {
			if _, err = tx.Exec(ctx, `UPDATE aiosources.mb_state SET filtered=filtered+$1 WHERE singleton`, f); err != nil {
				rollback(tx)
				return worksCount, filteredCount, coversCount, err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return worksCount, filteredCount, coversCount, err
		}
		worksCount += w
		filteredCount += f
		coversCount += c
		if onChunk != nil {
			onChunk(w)
		}
	}
	return worksCount, filteredCount, coversCount, nil
}

func writeProjectedMusicTx(ctx context.Context, tx pgx.Tx, generation int64, sel music.ImportSelection, ordered []projectedReleaseGroup, removeExcluded bool) (worksCount, filteredCount, coversCount, worksDelta, coversDelta int64, err error) {
	works := make([]model.AIOWork, 0, len(ordered))
	gids := make([]string, 0, len(ordered))
	excludedGIDs := []string{}
	for _, rg := range ordered {
		if !rg.eligible(sel) {
			filteredCount++
			excludedGIDs = append(excludedGIDs, rg.GID)
			continue
		}
		w := rg.toWork()
		works = append(works, w)
		gids = append(gids, w.MusicBrainz)
		worksCount++
		if rg.HasCover {
			coversCount++
		}
	}
	if len(works) == 0 && !(removeExcluded && len(excludedGIDs) > 0) {
		return worksCount, filteredCount, coversCount, 0, 0, nil
	}
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE`); err != nil {
		return 0, 0, 0, 0, 0, err
	}

	if len(works) > 0 {
		prevRows, err := tx.Query(ctx, `SELECT `+aioWorkColumns+` FROM aiosources.aio_works w WHERE media_type='music' AND musicbrainz_id<>'' AND musicbrainz_id=ANY($1::text[])`, gids)
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}
		previous := map[string]model.AIOWork{}
		for prevRows.Next() {
			w, e := scanAIOWork(prevRows)
			if e != nil {
				prevRows.Close()
				return 0, 0, 0, 0, 0, e
			}
			previous[w.MusicBrainz] = w
		}
		err = prevRows.Err()
		prevRows.Close()
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}

		changed := []string{}
		aliases := []string{}
		worksByGID := make(map[string]model.AIOWork, len(works))
		for _, w := range works {
			worksByGID[w.MusicBrainz] = w
			old, exists := previous[w.MusicBrainz]
			if !exists {
				worksDelta++
				if w.PosterPath != "" {
					coversDelta++
				}
			} else {
				if old.PosterPath == "" && w.PosterPath != "" {
					coversDelta++
				} else if old.PosterPath != "" && w.PosterPath == "" {
					coversDelta--
				}
			}
			if !exists || musicSignature(old) != musicSignature(w) {
				changed = append(changed, w.MusicBrainz)
				for _, t := range append(append([]string{}, old.Titles...), w.Titles...) {
					if n := model.NormalizeTitle(t); n != "" {
						aliases = append(aliases, n)
					}
				}
			}
		}

		type workRow struct {
			MusicBrainz     string                 `json:"musicbrainz"`
			DisplayTitle    string                 `json:"display_title"`
			Normalized      string                 `json:"normalized"`
			LocalizedTitles map[string]string      `json:"localized_titles"`
			PosterPath      string                 `json:"poster_path"`
			Years           []int                  `json:"years"`
			Music           model.MusicInformation `json:"music"`
		}
		payload := make([]workRow, len(works))
		for i, w := range works {
			payload[i] = workRow{
				MusicBrainz:     w.MusicBrainz,
				DisplayTitle:    w.DisplayTitle,
				Normalized:      model.NormalizeTitle(w.DisplayTitle),
				LocalizedTitles: w.LocalizedTitles,
				PosterPath:      w.PosterPath,
				Years:           w.Years,
				Music:           w.Music,
			}
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}
		retRows, err := tx.Query(ctx, `INSERT INTO aiosources.aio_works(media_type,musicbrainz_id,display_title,music_normalized,localized_titles,localized_posters,poster_path,years,music_data,mb_generation)
 SELECT 'music',musicbrainz,display_title,normalized,localized_titles,'{}'::jsonb,COALESCE(poster_path,''),years,COALESCE(music,'{}'::jsonb),$2
 FROM jsonb_to_recordset($1::jsonb) AS i(musicbrainz text,display_title text,normalized text,localized_titles jsonb,poster_path text,years int[],music jsonb) ORDER BY musicbrainz
 ON CONFLICT(musicbrainz_id) WHERE musicbrainz_id<>'' DO UPDATE SET
  display_title=EXCLUDED.display_title,music_normalized=EXCLUDED.music_normalized,localized_titles=EXCLUDED.localized_titles,
  poster_path=EXCLUDED.poster_path,years=EXCLUDED.years,music_data=EXCLUDED.music_data,mb_generation=EXCLUDED.mb_generation,
  revision=aiosources.aio_works.revision+CASE WHEN EXCLUDED.musicbrainz_id=ANY($3::text[]) THEN 1 ELSE 0 END,updated_at=now()
 RETURNING id,musicbrainz_id`, body, generation, changed)
		if err != nil {
			return 0, 0, 0, 0, 0, databaseError("publish music information", err)
		}
		byGID := map[string]int64{}
		for retRows.Next() {
			var id int64
			var gid string
			if err = retRows.Scan(&id, &gid); err != nil {
				retRows.Close()
				return 0, 0, 0, 0, 0, err
			}
			byGID[gid] = id
		}
		err = retRows.Err()
		retRows.Close()
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}
		if len(changed) > 0 {
			changedIDs := make([]int64, 0, len(changed))
			titleRows := []map[string]any{}
			for _, gid := range changed {
				wid := byGID[gid]
				changedIDs = append(changedIDs, wid)
				w := worksByGID[gid]
				for _, t := range w.Titles {
					titleRows = append(titleRows, map[string]any{"work_id": wid, "title": t, "normalized": model.NormalizeTitle(t)})
				}
			}
			if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_titles WHERE work_id=ANY($1::bigint[])`, changedIDs); err != nil {
				return 0, 0, 0, 0, 0, err
			}
			if len(titleRows) > 0 {
				encoded, _ := json.Marshal(titleRows)
				if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_titles SELECT * FROM jsonb_populate_recordset(NULL::aiosources.aio_titles,$1::jsonb) ON CONFLICT DO NOTHING`, encoded); err != nil {
					return 0, 0, 0, 0, 0, err
				}
			}
			if err = queueMusicAliases(ctx, tx, changedIDs, changed, aliases); err != nil {
				return 0, 0, 0, 0, 0, err
			}
			if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
				return 0, 0, 0, 0, 0, err
			}
		}
	}

	if removeExcluded && len(excludedGIDs) > 0 {
		delRows, err := tx.Query(ctx, `SELECT id,poster_path<>'' FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id<>'' AND musicbrainz_id=ANY($1::text[])`, excludedGIDs)
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}
		delIDs := []int64{}
		var delCovers int64
		for delRows.Next() {
			var id int64
			var hasCover bool
			if err = delRows.Scan(&id, &hasCover); err != nil {
				delRows.Close()
				return 0, 0, 0, 0, 0, err
			}
			delIDs = append(delIDs, id)
			if hasCover {
				delCovers++
			}
		}
		err = delRows.Err()
		delRows.Close()
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}
		if len(delIDs) > 0 {
			if err = queueAIOManyAliases(ctx, tx, delIDs, nil); err != nil {
				return 0, 0, 0, 0, 0, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_works WHERE id=ANY($1::bigint[])`, delIDs); err != nil {
				return 0, 0, 0, 0, 0, err
			}
			if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
				return 0, 0, 0, 0, 0, err
			}
			worksDelta -= int64(len(delIDs))
			coversDelta -= delCovers
		}
	}
	return worksCount, filteredCount, coversCount, worksDelta, coversDelta, nil
}

func queueMusicAliases(ctx context.Context, tx pgx.Tx, workIDs []int64, gids, aliases []string) error {
	_, err := tx.Exec(ctx, `INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
 SELECT provider_id,source_id FROM aiosources.aio_matches WHERE work_id=ANY($1::bigint[]) OR normalized=ANY($3::text[])
 UNION SELECT provider_id,source_id FROM aiosources.aio_match_reviews WHERE active AND media_type='music' AND musicbrainz_id=ANY($2::text[])
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1`, workIDs, gids, aliases)
	return err
}

func musicSignature(w model.AIOWork) string {
	aliases := make([]string, 0, len(w.Titles))
	for _, t := range w.Titles {
		aliases = append(aliases, model.NormalizeTitle(t))
	}
	base := aioWorkSignature(w, aliases)
	data, _ := json.Marshal(w.Music)
	return base + "|" + w.DisplayTitle + "|" + w.PosterPath + "|" + string(data)
}

func (s *Store) pruneMusicDumpRows(ctx context.Context, generation int64, includeCovers bool) error {
	pool := s.workerPool()
	pruneTables := []string{
		"mb_artists", "mb_aliases", "mb_artist_credits", "mb_artist_credit_names",
		"mb_labels", "mb_release_groups", "mb_release_group_secondary_types",
		"mb_releases", "mb_release_years", "mb_release_labels", "mb_mediums",
		"mb_urls", "mb_entity_urls", "mb_redirects",
	}
	if includeCovers {
		pruneTables = append(pruneTables, "mb_cover_art", "mb_rg_cover_art")
	} else {
		if _, err := pool.Exec(ctx, `TRUNCATE aiosources.mb_cover_art,aiosources.mb_rg_cover_art`); err != nil {
			return databaseError("clear music cover art", err)
		}
	}
	const pruneBatch = 20000
	for _, table := range pruneTables {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			tag, err := pool.Exec(ctx, `DELETE FROM aiosources.`+table+` WHERE ctid IN (SELECT ctid FROM aiosources.`+table+` WHERE mb_seen<$1 LIMIT $2)`, generation, pruneBatch)
			if err != nil {
				return databaseError("prune "+table, err)
			}
			if tag.RowsAffected() < pruneBatch {
				break
			}
		}
	}
	return s.pruneUnlinkedMusicURLs(ctx)
}

// FinishMusicDump removes stale works and publishes the snapshot and its cursor.
func (s *Store) FinishMusicDump(ctx context.Context, generation int64, header music.DumpHeader, snapshot string, snapshotAt *time.Time, sel music.ImportSelection) error {
	pool := s.workerPool()
	// Remove stale music works in bounded short transactions so aio_state FOR UPDATE
	// is never held across a large full-catalog cleanup.
	const staleBatch = 1000
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		staleRows, err := pool.Query(ctx, `SELECT id FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id<>'' AND mb_generation<>$1 ORDER BY id LIMIT $2`, generation, staleBatch)
		if err != nil {
			return databaseError("scan stale music works", err)
		}
		staleIDs := []int64{}
		for staleRows.Next() {
			var id int64
			if err = staleRows.Scan(&id); err != nil {
				staleRows.Close()
				return err
			}
			staleIDs = append(staleIDs, id)
		}
		err = staleRows.Err()
		staleRows.Close()
		if err != nil {
			return err
		}
		if len(staleIDs) == 0 {
			break
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return databaseError("begin stale music cleanup", err)
		}
		if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE`); err != nil {
			rollback(tx)
			return err
		}
		confirmRows, err := tx.Query(ctx, `SELECT id FROM aiosources.aio_works WHERE id=ANY($1::bigint[]) AND media_type='music' AND musicbrainz_id<>'' AND mb_generation<>$2`, staleIDs, generation)
		if err != nil {
			rollback(tx)
			return err
		}
		confirmed := []int64{}
		for confirmRows.Next() {
			var id int64
			if err = confirmRows.Scan(&id); err != nil {
				confirmRows.Close()
				rollback(tx)
				return err
			}
			confirmed = append(confirmed, id)
		}
		err = confirmRows.Err()
		confirmRows.Close()
		if err != nil {
			rollback(tx)
			return err
		}
		if len(confirmed) > 0 {
			if err = queueAIOManyAliases(ctx, tx, confirmed, nil); err != nil {
				rollback(tx)
				return err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_works WHERE id=ANY($1::bigint[])`, confirmed); err != nil {
				rollback(tx)
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
				rollback(tx)
				return err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return databaseError("commit stale music cleanup", err)
		}
		if len(confirmed) == 0 || len(staleIDs) < staleBatch {
			break
		}
	}

	var wasComplete bool
	if err := pool.QueryRow(ctx, `SELECT complete FROM aiosources.mb_state WHERE singleton`).Scan(&wasComplete); err != nil {
		return databaseError("read music completion state", err)
	}
	_, err := pool.Exec(ctx, `WITH backfill AS (
 INSERT INTO aiosources.settings(key,value) VALUES('musicbrainz.matching.v1','1') ON CONFLICT DO NOTHING RETURNING 1
)
INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
 SELECT t.provider_id,t.source_id FROM aiosources.torrents t
 WHERE (NOT $1::boolean OR EXISTS(SELECT 1 FROM backfill)) AND (
  lower(COALESCE(t.fields->>'media_type',t.fields->>'type','')) IN ('music','audio','album','flac','mp3','lossless')
  OR t.fields ?| ARRAY['musicbrainz','musicbrainz_id','mbid','musicbrainz_release_group_id','discogs','discogs_id','discogs_master_id','barcode','upc','ean','catalog_number','catno','artist','album']
  OR COALESCE(t.fields->'categories',t.fields->'category','null')::text ~ '\m(3000|3010|3040|3050|3060)\M'
  OR concat_ws(' ',t.fields->>'category_name',t.fields->>'subcategory_name') ~* '(musi[cq]|flac|lossless|albums?|mp3)'
  OR t.fields->>'title' ~* '\m(FLAC|ALAC|LOSSLESS|MP3|320\s?kbps|24bit|16bit|SACD|DSD|WEB-FLAC)\M'
	 ) ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1`, wasComplete)
	if err != nil {
		return databaseError("queue music torrents for matching", err)
	}

	changesThrough := snapshotAt
	if !header.Timestamp.IsZero() {
		ts := header.Timestamp
		changesThrough = &ts
	}
	tag, err := pool.Exec(ctx, `UPDATE aiosources.mb_state SET
 complete=TRUE,
 phase='idle',
 works=(SELECT count(*) FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id<>''),
 releases=(SELECT count(*) FROM aiosources.mb_releases),
 artists=(SELECT count(*) FROM aiosources.mb_artists),
 labels=(SELECT count(*) FROM aiosources.mb_labels),
 covers=(SELECT count(*) FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id<>'' AND poster_path<>''),
 snapshot=$1,
 snapshot_at=$2,
 replication_sequence=$3,
 schema_sequence=$4,
 last_import=now(),
 last_sync=now(),
 changes_through=COALESCE($5,now()),
 last_packet_at=NULL,
 needs_export=FALSE,
 recovery_reason='',
 schema_unsupported=FALSE,
 retry_at=NULL,
 error=''
 WHERE singleton AND selection=$6 AND generation=$7`, snapshot, snapshotAt, header.ReplicationSequence, header.SchemaSequence, changesThrough, sel.Key(), generation)
	if err != nil {
		return databaseError("complete music catalog import", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

type musicReplicationDelta struct {
	works    int64
	releases int64
	artists  int64
	labels   int64
	covers   int64
}

// ApplyMusicReplicationPacket applies a single dbmirror2 hourly packet
// atomically, re-projects only affected release groups, and advances the
// durable replication cursor.
func (s *Store) ApplyMusicReplicationPacket(ctx context.Context, packet music.ReplicationPacket, sel music.ImportSelection) (int, error) {
	tx, err := s.workerPool().Begin(ctx)
	if err != nil {
		return 0, databaseError("begin music replication packet", err)
	}
	defer rollback(tx)

	var currentSeq, gen int64
	var needsExport bool
	if err = tx.QueryRow(ctx, `SELECT replication_sequence,generation,needs_export FROM aiosources.mb_state WHERE singleton FOR UPDATE`).Scan(&currentSeq, &gen, &needsExport); err != nil {
		return 0, err
	}
	if packet.ReplicationSequence <= currentSeq {
		return 0, tx.Commit(ctx)
	}
	if needsExport || packet.ReplicationSequence != currentSeq+1 {
		return 0, model.ErrConflict
	}

	affectedRGs := map[int]bool{}
	affectedReleases := map[int]bool{}
	affectedArtists := map[int]bool{}
	affectedCredits := map[int]bool{}
	affectedLabels := map[int]bool{}
	affectedURLs := map[int]bool{}
	deletedRGGIDs := map[string]bool{}
	var delta musicReplicationDelta

	for _, ch := range packet.Changes {
		if err = applyReplicationChangeTx(ctx, tx, gen, ch, affectedRGs, affectedReleases, affectedArtists, affectedCredits, affectedLabels, affectedURLs, deletedRGGIDs, &delta); err != nil {
			return 0, databaseError("apply music replication change", err)
		}
	}

	// Expand indirect dependencies into affected release_group IDs.
	expandIntSet := func(query string, ids map[int]bool, target map[int]bool) error {
		if len(ids) == 0 {
			return nil
		}
		list := make([]int, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		rows, e := tx.Query(ctx, query, list)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v int
			if e = rows.Scan(&v); e != nil {
				return e
			}
			target[v] = true
		}
		return rows.Err()
	}

	if err = expandIntSet(`SELECT DISTINCT artist_credit FROM aiosources.mb_artist_credit_names WHERE artist_id=ANY($1::int[])`, affectedArtists, affectedCredits); err != nil {
		return 0, err
	}
	if err = expandIntSet(`SELECT DISTINCT id FROM aiosources.mb_release_groups WHERE artist_credit=ANY($1::int[])`, affectedCredits, affectedRGs); err != nil {
		return 0, err
	}
	if err = expandIntSet(`SELECT DISTINCT release_id FROM aiosources.mb_release_labels WHERE label_id=ANY($1::int[])`, affectedLabels, affectedReleases); err != nil {
		return 0, err
	}
	if err = expandIntSet(`SELECT DISTINCT entity_id FROM aiosources.mb_entity_urls WHERE entity='release_group' AND url_id=ANY($1::int[])`, affectedURLs, affectedRGs); err != nil {
		return 0, err
	}
	if err = expandIntSet(`SELECT DISTINCT entity_id FROM aiosources.mb_entity_urls WHERE entity='release' AND url_id=ANY($1::int[])`, affectedURLs, affectedReleases); err != nil {
		return 0, err
	}
	if err = expandIntSet(`SELECT DISTINCT release_group_id FROM aiosources.mb_releases WHERE id=ANY($1::int[])`, affectedReleases, affectedRGs); err != nil {
		return 0, err
	}

	// Remove explicitly deleted release groups from aio_works.
	if len(deletedRGGIDs) > 0 {
		gids := make([]string, 0, len(deletedRGGIDs))
		for g := range deletedRGGIDs {
			gids = append(gids, g)
		}
		rows, err := tx.Query(ctx, `SELECT id,poster_path<>'' FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id<>'' AND musicbrainz_id=ANY($1::text[])`, gids)
		if err != nil {
			return 0, err
		}
		delIDs := []int64{}
		var delCovers int64
		for rows.Next() {
			var id int64
			var hasCover bool
			if err = rows.Scan(&id, &hasCover); err != nil {
				rows.Close()
				return 0, err
			}
			delIDs = append(delIDs, id)
			if hasCover {
				delCovers++
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		if len(delIDs) > 0 {
			if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE`); err != nil {
				return 0, err
			}
			if err = queueAIOManyAliases(ctx, tx, delIDs, nil); err != nil {
				return 0, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_works WHERE id=ANY($1::bigint[])`, delIDs); err != nil {
				return 0, err
			}
			if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
				return 0, err
			}
			delta.works -= int64(len(delIDs))
			delta.covers -= delCovers
		}
	}

	rgList := make([]int, 0, len(affectedRGs))
	for id := range affectedRGs {
		rgList = append(rgList, id)
	}
	slices.Sort(rgList)
	for start := 0; start < len(rgList); start += 500 {
		chunk := rgList[start:min(start+500, len(rgList))]
		ordered, err := readProjectedMusicChunk(ctx, tx, sel.IncludeCovers, chunk)
		if err != nil {
			return 0, err
		}
		_, _, _, wd, cd, err := writeProjectedMusicTx(ctx, tx, gen, sel, ordered, true)
		if err != nil {
			return 0, err
		}
		delta.works += wd
		delta.covers += cd
	}

	var ts *time.Time
	if !packet.Timestamp.IsZero() {
		t := packet.Timestamp
		ts = &t
	}
	_, err = tx.Exec(ctx, `UPDATE aiosources.mb_state SET
 phase='idle',
 replication_sequence=$1,
 schema_sequence=$2,
 works=GREATEST(0,works+$4),
 releases=GREATEST(0,releases+$5),
 artists=GREATEST(0,artists+$6),
 labels=GREATEST(0,labels+$7),
 covers=GREATEST(0,covers+$8),
 last_sync=now(),
 last_packet_at=now(),
 checked_at=now(),
 changes_through=COALESCE($3,now()),
 needs_export=FALSE,
 recovery_reason='',
 schema_unsupported=FALSE,
 retry_at=NULL,
 error=''
 WHERE singleton`, packet.ReplicationSequence, packet.SchemaSequence, ts, delta.works, delta.releases, delta.artists, delta.labels, delta.covers)
	if err != nil {
		return 0, databaseError("advance music replication cursor", err)
	}
	return len(rgList), databaseError("commit music replication packet", tx.Commit(ctx))
}

func applyReplicationChangeTx(
	ctx context.Context, tx pgx.Tx, gen int64, ch music.ReplicationChange,
	affectedRGs, affectedReleases, affectedArtists, affectedCredits, affectedLabels, affectedURLs map[int]bool,
	deletedRGGIDs map[string]bool, delta *musicReplicationDelta,
) error {
	row := ch.NewData
	if ch.Op == "d" {
		row = ch.OldData
	}
	if row == nil {
		return nil
	}

	switch ch.Schema + "." + ch.Table {
	case "musicbrainz.release_group_primary_type", "musicbrainz.release_group_secondary_type", "musicbrainz.release_status", "musicbrainz.medium_format":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_lookups WHERE kind=$1 AND id=$2`, ch.Table, id)
			return err
		}
		name := music.JSONString(row, "name")
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_lookups(kind,id,name) VALUES($1,$2,$3) ON CONFLICT(kind,id) DO UPDATE SET name=EXCLUDED.name`, ch.Table, id, name)
		return err

	case "musicbrainz.language":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_lookups WHERE kind='language' AND id=$1`, id)
			return err
		}
		iso3 := music.JSONString(row, "iso_code_3")
		if iso3 == "" {
			iso3 = music.JSONString(row, "iso_code_2t")
		}
		if code := music.NormalizeLanguage(music.JSONString(row, "iso_code_1"), iso3); code != "" {
			_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_lookups(kind,id,name) VALUES('language',$1,$2) ON CONFLICT(kind,id) DO UPDATE SET name=EXCLUDED.name`, id, code)
			return err
		}
		return nil

	case "musicbrainz.artist":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		affectedArtists[id] = true
		if ch.Op == "d" {
			tag, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_artists WHERE id=$1`, id)
			if err == nil {
				delta.artists -= tag.RowsAffected()
			}
			return err
		}
		gid := music.MBID(music.JSONString(row, "gid"))
		name := music.JSONString(row, "name")
		if gid == "" || name == "" {
			return nil
		}
		var inserted bool
		err := tx.QueryRow(ctx, `INSERT INTO aiosources.mb_artists(id,gid,name,sort_name,normalized,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,name=EXCLUDED.name,sort_name=EXCLUDED.sort_name,normalized=EXCLUDED.normalized,mb_seen=EXCLUDED.mb_seen
 RETURNING (xmax = 0)`,
			id, gid, name, music.JSONString(row, "sort_name"), model.NormalizeTitle(name), gen).Scan(&inserted)
		if err == nil && inserted {
			delta.artists++
		}
		return err

	case "musicbrainz.artist_alias", "musicbrainz.release_group_alias", "musicbrainz.release_alias", "musicbrainz.label_alias":
		entity := strings.TrimSuffix(ch.Table, "_alias")
		id, ok := music.JSONInt(row, "id")
		entityID, ok2 := music.JSONInt(row, entity)
		if !ok || !ok2 {
			return nil
		}
		switch entity {
		case "artist":
			affectedArtists[entityID] = true
		case "release_group":
			affectedRGs[entityID] = true
		case "release":
			affectedReleases[entityID] = true
		case "label":
			affectedLabels[entityID] = true
		}
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_aliases WHERE entity=$1 AND id=$2`, entity, id)
			return err
		}
		name := music.JSONString(row, "name")
		if name == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_aliases(entity,id,entity_id,name,normalized,locale,mb_seen) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(entity,id) DO UPDATE SET entity_id=EXCLUDED.entity_id,name=EXCLUDED.name,normalized=EXCLUDED.normalized,locale=EXCLUDED.locale,mb_seen=EXCLUDED.mb_seen`,
			entity, id, entityID, name, model.NormalizeTitle(name), music.JSONString(row, "locale"), gen)
		return err

	case "musicbrainz.artist_credit":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		affectedCredits[id] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_artist_credits WHERE id=$1`, id)
			return err
		}
		name := music.JSONString(row, "name")
		if name == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_artist_credits(id,name,normalized,mb_seen) VALUES($1,$2,$3,$4)
 ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,normalized=EXCLUDED.normalized,mb_seen=EXCLUDED.mb_seen`, id, name, model.NormalizeTitle(name), gen)
		return err

	case "musicbrainz.artist_credit_name":
		ac, ok := music.JSONInt(row, "artist_credit")
		pos, ok2 := music.JSONInt(row, "position")
		if !ok || !ok2 {
			return nil
		}
		affectedCredits[ac] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_artist_credit_names WHERE artist_credit=$1 AND position=$2`, ac, pos)
			return err
		}
		artistID, ok3 := music.JSONInt(row, "artist")
		if !ok3 {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_artist_credit_names(artist_credit,position,artist_id,name,join_phrase,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(artist_credit,position) DO UPDATE SET artist_id=EXCLUDED.artist_id,name=EXCLUDED.name,join_phrase=EXCLUDED.join_phrase,mb_seen=EXCLUDED.mb_seen`,
			ac, pos, artistID, music.JSONString(row, "name"), music.JSONString(row, "join_phrase"), gen)
		return err

	case "musicbrainz.release_group":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		if ch.Op == "d" {
			if gid := music.MBID(music.JSONString(row, "gid")); gid != "" {
				deletedRGGIDs[gid] = true
			}
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_release_groups WHERE id=$1`, id)
			return err
		}
		affectedRGs[id] = true
		gid := music.MBID(music.JSONString(row, "gid"))
		name := music.JSONString(row, "name")
		ac, _ := music.JSONInt(row, "artist_credit")
		pt, _ := music.JSONInt(row, "type")
		if gid == "" || name == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_release_groups(id,gid,name,normalized,artist_credit,primary_type,mb_seen) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,name=EXCLUDED.name,normalized=EXCLUDED.normalized,artist_credit=EXCLUDED.artist_credit,primary_type=EXCLUDED.primary_type,mb_seen=EXCLUDED.mb_seen`,
			id, gid, name, model.NormalizeTitle(name), ac, pt, gen)
		return err

	case "musicbrainz.release_group_secondary_type_join":
		rgID, ok := music.JSONInt(row, "release_group")
		secID, ok2 := music.JSONInt(row, "secondary_type")
		if !ok || !ok2 {
			return nil
		}
		affectedRGs[rgID] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_release_group_secondary_types WHERE release_group_id=$1 AND secondary_type_id=$2`, rgID, secID)
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_release_group_secondary_types(release_group_id,secondary_type_id,mb_seen) VALUES($1,$2,$3)
 ON CONFLICT(release_group_id,secondary_type_id) DO UPDATE SET mb_seen=EXCLUDED.mb_seen`, rgID, secID, gen)
		return err

	case "musicbrainz.release":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		if oldRG, okOld := music.JSONInt(ch.OldData, "release_group"); okOld {
			affectedRGs[oldRG] = true
		}
		if newRG, okNew := music.JSONInt(ch.NewData, "release_group"); okNew {
			affectedRGs[newRG] = true
		}
		if ch.Op == "d" {
			tag, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_releases WHERE id=$1`, id)
			if err == nil {
				delta.releases -= tag.RowsAffected()
			}
			return err
		}
		gid := music.MBID(music.JSONString(row, "gid"))
		name := music.JSONString(row, "name")
		rgID, okRG := music.JSONInt(row, "release_group")
		ac, _ := music.JSONInt(row, "artist_credit")
		status, _ := music.JSONInt(row, "status")
		langID, _ := music.JSONInt(row, "language")
		if gid == "" || name == "" || !okRG {
			return nil
		}
		var inserted bool
		err := tx.QueryRow(ctx, `INSERT INTO aiosources.mb_releases(id,gid,release_group_id,name,normalized,artist_credit,status,language_id,barcode,mb_seen) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,release_group_id=EXCLUDED.release_group_id,name=EXCLUDED.name,normalized=EXCLUDED.normalized,artist_credit=EXCLUDED.artist_credit,status=EXCLUDED.status,language_id=EXCLUDED.language_id,barcode=EXCLUDED.barcode,mb_seen=EXCLUDED.mb_seen
 RETURNING (xmax = 0)`,
			id, gid, rgID, name, model.NormalizeTitle(name), ac, status, langID, music.Barcode(music.JSONString(row, "barcode")), gen).Scan(&inserted)
		if err == nil && inserted {
			delta.releases++
		}
		return err

	case "musicbrainz.release_country", "musicbrainz.release_unknown_country":
		return applyMusicReleaseDate(ctx, tx, gen, ch, affectedReleases)

	case "musicbrainz.label":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		affectedLabels[id] = true
		if ch.Op == "d" {
			tag, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_labels WHERE id=$1`, id)
			if err == nil {
				delta.labels -= tag.RowsAffected()
			}
			return err
		}
		gid := music.MBID(music.JSONString(row, "gid"))
		name := music.JSONString(row, "name")
		if gid == "" || name == "" {
			return nil
		}
		var inserted bool
		err := tx.QueryRow(ctx, `INSERT INTO aiosources.mb_labels(id,gid,name,normalized,mb_seen) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(id) DO UPDATE SET gid=EXCLUDED.gid,name=EXCLUDED.name,normalized=EXCLUDED.normalized,mb_seen=EXCLUDED.mb_seen
 RETURNING (xmax = 0)`, id, gid, name, model.NormalizeTitle(name), gen).Scan(&inserted)
		if err == nil && inserted {
			delta.labels++
		}
		return err

	case "musicbrainz.release_label":
		id, ok := music.JSONInt(row, "id")
		relID, ok2 := music.JSONInt(row, "release")
		if !ok || !ok2 {
			return nil
		}
		affectedReleases[relID] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_release_labels WHERE id=$1`, id)
			return err
		}
		labelID, _ := music.JSONInt(row, "label")
		catNo := music.JSONString(row, "catalog_number")
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_release_labels(id,release_id,label_id,catalog_number,catalog_normalized,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET release_id=EXCLUDED.release_id,label_id=EXCLUDED.label_id,catalog_number=EXCLUDED.catalog_number,catalog_normalized=EXCLUDED.catalog_normalized,mb_seen=EXCLUDED.mb_seen`,
			id, relID, labelID, catNo, music.CatalogNumber(catNo), gen)
		return err

	case "musicbrainz.medium":
		id, ok := music.JSONInt(row, "id")
		relID, ok2 := music.JSONInt(row, "release")
		if !ok || !ok2 {
			return nil
		}
		affectedReleases[relID] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_mediums WHERE id=$1`, id)
			return err
		}
		pos, _ := music.JSONInt(row, "position")
		fmtID, _ := music.JSONInt(row, "format")
		tracks, _ := music.JSONInt(row, "track_count")
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_mediums(id,release_id,position,format_id,track_count,mb_seen) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET release_id=EXCLUDED.release_id,position=EXCLUDED.position,format_id=EXCLUDED.format_id,track_count=EXCLUDED.track_count,mb_seen=EXCLUDED.mb_seen`,
			id, relID, pos, fmtID, max(0, tracks), gen)
		return err

	case "musicbrainz.url":
		id, ok := music.JSONInt(row, "id")
		if !ok {
			return nil
		}
		affectedURLs[id] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_urls WHERE id=$1`, id)
			return err
		}
		rawURL := music.JSONString(row, "url")
		dKind, dID := music.DiscogsURL(rawURL)
		wd := music.WikidataURL(rawURL)
		if dID == "" && wd == "" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_urls WHERE id=$1`, id)
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_urls(id,discogs_kind,discogs_id,wikidata_id,mb_seen) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(id) DO UPDATE SET discogs_kind=EXCLUDED.discogs_kind,discogs_id=EXCLUDED.discogs_id,wikidata_id=EXCLUDED.wikidata_id,mb_seen=EXCLUDED.mb_seen`,
			id, dKind, dID, wd, gen)
		return err

	case "musicbrainz.l_release_group_url", "musicbrainz.l_release_url", "musicbrainz.l_artist_url":
		entity := strings.TrimSuffix(strings.TrimPrefix(ch.Table, "l_"), "_url")
		linkID, ok := music.JSONInt(row, "id")
		entityID, ok2 := music.JSONInt(row, "entity0")
		urlID, ok3 := music.JSONInt(row, "entity1")
		if !ok || !ok2 || !ok3 {
			return nil
		}
		switch entity {
		case "release_group":
			affectedRGs[entityID] = true
		case "release":
			affectedReleases[entityID] = true
		case "artist":
			affectedArtists[entityID] = true
		}
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_entity_urls WHERE entity=$1 AND link_id=$2`, entity, linkID)
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.mb_urls WHERE id=$1)`, urlID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_entity_urls WHERE entity=$1 AND link_id=$2`, entity, linkID)
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_entity_urls(entity,link_id,entity_id,url_id,mb_seen) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(entity,link_id) DO UPDATE SET entity_id=EXCLUDED.entity_id,url_id=EXCLUDED.url_id,mb_seen=EXCLUDED.mb_seen`, entity, linkID, entityID, urlID, gen)
		return err

	case "musicbrainz.artist_gid_redirect", "musicbrainz.release_group_gid_redirect", "musicbrainz.release_gid_redirect", "musicbrainz.label_gid_redirect":
		entity := strings.TrimSuffix(ch.Table, "_gid_redirect")
		gid := music.MBID(music.JSONString(row, "gid"))
		target, ok := music.JSONInt(row, "new_id")
		if gid == "" || !ok {
			return nil
		}
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_redirects WHERE entity=$1 AND source_gid=$2`, entity, gid)
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_redirects(entity,source_gid,target_id,mb_seen) VALUES($1,$2,$3,$4)
 ON CONFLICT(entity,source_gid) DO UPDATE SET target_id=EXCLUDED.target_id,mb_seen=EXCLUDED.mb_seen`, entity, gid, target, gen)
		return err

	case "cover_art_archive.cover_art":
		id, ok := music.JSONInt64(row, "id")
		relID, ok2 := music.JSONInt(row, "release")
		if !ok || !ok2 {
			return nil
		}
		affectedReleases[relID] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_cover_art WHERE id=$1`, id)
			return err
		}
		ordering, _ := music.JSONInt(row, "ordering")
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_cover_art(id,release_id,ordering,mb_seen) VALUES($1,$2,$3,$4)
 ON CONFLICT(id) DO UPDATE SET release_id=EXCLUDED.release_id,ordering=EXCLUDED.ordering,mb_seen=EXCLUDED.mb_seen`, id, relID, ordering, gen)
		return err

	case "cover_art_archive.cover_art_type":
		id, ok := music.JSONInt64(row, "id")
		typeID, ok2 := music.JSONInt(row, "type_id")
		if !ok || !ok2 || typeID != 1 {
			return nil
		}
		var relID int
		if err := tx.QueryRow(ctx, `SELECT release_id FROM aiosources.mb_cover_art WHERE id=$1`, id).Scan(&relID); err == nil && relID > 0 {
			affectedReleases[relID] = true
		}
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `UPDATE aiosources.mb_cover_art SET is_front=FALSE WHERE id=$1`, id)
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_cover_art(id,release_id,is_front,mb_seen) VALUES($1,0,TRUE,$2)
 ON CONFLICT(id) DO UPDATE SET is_front=TRUE,mb_seen=EXCLUDED.mb_seen`, id, gen)
		return err

	case "cover_art_archive.release_group_cover_art":
		rgID, ok := music.JSONInt(row, "release_group")
		relID, ok2 := music.JSONInt(row, "release")
		if !ok || !ok2 {
			return nil
		}
		affectedRGs[rgID] = true
		if ch.Op == "d" {
			_, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_rg_cover_art WHERE release_group_id=$1`, rgID)
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_rg_cover_art(release_group_id,release_id,mb_seen) VALUES($1,$2,$3)
 ON CONFLICT(release_group_id) DO UPDATE SET release_id=EXCLUDED.release_id,mb_seen=EXCLUDED.mb_seen`, rgID, relID, gen)
		return err
	}
	return nil
}

func applyMusicReleaseDate(ctx context.Context, tx pgx.Tx, gen int64, ch music.ReplicationChange, affectedReleases map[int]bool) error {
	oldRelease, _ := music.JSONInt(ch.OldData, "release")
	newRelease, _ := music.JSONInt(ch.NewData, "release")
	oldYear, _ := music.JSONInt(ch.OldData, "date_year")
	newYear, _ := music.JSONInt(ch.NewData, "date_year")
	if oldRelease == newRelease && oldYear == newYear {
		return nil
	}
	if oldRelease > 0 && oldYear >= 1850 && oldYear <= 2200 {
		affectedReleases[oldRelease] = true
		// Older imports kept one row regardless of how many countries shared a
		// date. Their unknown counts are recovered by the next full snapshot.
		if _, err := tx.Exec(ctx, `DELETE FROM aiosources.mb_release_years WHERE release_id=$1 AND year=$2 AND occurrences=1`, oldRelease, oldYear); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE aiosources.mb_release_years SET occurrences=occurrences-1 WHERE release_id=$1 AND year=$2 AND occurrences>1`, oldRelease, oldYear); err != nil {
			return err
		}
	}
	if newRelease > 0 && newYear >= 1850 && newYear <= 2200 {
		affectedReleases[newRelease] = true
		_, err := tx.Exec(ctx, `INSERT INTO aiosources.mb_release_years(release_id,year,mb_seen,occurrences) VALUES($1,$2,$3,1)
 ON CONFLICT(release_id,year) DO UPDATE SET occurrences=aiosources.mb_release_years.occurrences+1,mb_seen=EXCLUDED.mb_seen`, newRelease, newYear, gen)
		return err
	}
	return nil
}

func (s *Store) MarkMusicNeedsExport(ctx context.Context, reason, message string, schemaUnsupported bool, retryAt time.Time) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.mb_state SET phase='idle',needs_export=TRUE,recovery_reason=$1,schema_unsupported=$2,error=$3,retry_at=$4,checked_at=now() WHERE singleton`,
		reason, schemaUnsupported, message, retryAt)
	return databaseError("mark music recovery needed", err)
}

func (s *Store) FailMusicUpdate(ctx context.Context, message string, retryAt time.Time) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.mb_state SET phase='idle',error=$1,retry_at=GREATEST(COALESCE(retry_at,to_timestamp(0)),$2),checked_at=now() WHERE singleton`, message, retryAt)
	return databaseError("retain music update retry", err)
}

func (s *Store) RecordMusicInformationActivity(ctx context.Context, updateErr error, processed int64) error {
	level, kind, message := "info", "musicbrainz.sync_completed", "Music information is up to date."
	if updateErr != nil {
		level, kind, message = "error", "musicbrainz.sync_failed", "Music information update failed. Saved progress is preserved."
		if errors.Is(updateErr, context.Canceled) {
			level, kind, message = "warn", "musicbrainz.sync_interrupted", "Music information update interrupted. Saved progress is preserved."
		}
	} else {
		state, err := s.MusicStatus(ctx)
		if err != nil {
			return err
		}
		if state.SchemaUnsupported {
			level, kind, message = "error", "musicbrainz.schema_unsupported", "MusicBrainz updated its database schema. Update AIOSources to resume music updates."
		} else if state.NeedsExport {
			level, kind, message = "warn", "musicbrainz.recovery_pending", "A newer MusicBrainz export is needed to recover missing changes. Existing music information is preserved."
		} else if state.Skipped > 0 {
			level, kind, message = "warn", "musicbrainz.records_skipped", "Music information updated. Unreadable or oversized export records were skipped; review the update history."
		}
	}
	id, err := newRunID()
	if err != nil {
		return err
	}
	_, err = s.workerPool().Exec(ctx, `INSERT INTO aiosources.activity_logs(event_id,level,kind,message,title,service,data) VALUES($1,$2,$3,$4,'Music information update','musicbrainz',jsonb_build_object('records',$5::bigint))`,
		"activity-"+id, level, kind, message, processed)
	return databaseError("record music information activity", err)
}

func (s *Store) ResetMusicInformation(ctx context.Context) error {
	return s.resetInformation(ctx, "music")
}
