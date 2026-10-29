package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func DefaultAIOConfig() model.AIOConfig {
	return model.AIOConfig{Revision: 1, OpenLibraryMinutes: 15, AutoSync: true, AutoMatch: true, SyncMinutes: 15, MatchSeconds: 30, TMDBRequestsPerSecond: 35, CacheMinutes: 4320, ResponseSeconds: 5, Trackers: map[string]model.AIOTracker{}}
}
func (s *Store) AIOConfig(ctx context.Context) (model.AIOConfig, error) {
	return readAIOConfig(ctx, s.pool)
}

type aioConfigReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// A query already holding a transaction must read configuration through that
// transaction instead of waiting for another connection from the same pool.
func readAIOConfig(ctx context.Context, reader aioConfigReader) (model.AIOConfig, error) {
	c := DefaultAIOConfig()
	var v string
	err := reader.QueryRow(ctx, "SELECT value FROM ingest.settings WHERE key=$1", "aiosource.config.v2").Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, databaseError("read application setting", err)
	}
	if err = json.Unmarshal([]byte(v), &c); err != nil {
		return c, errors.New("invalid AIOSources configuration")
	}
	if c.Trackers == nil {
		c.Trackers = map[string]model.AIOTracker{}
	}
	return c, nil
}
func (s *Store) SetAIOConfig(ctx context.Context, c model.AIOConfig) error {
	return s.SetAIOConfigAndSecrets(ctx, c, nil)
}

// SetAIOConfigAndSecrets publishes migrated encrypted credentials and their
// references atomically. A stale revision never leaves unreferenced secrets.
func (s *Store) SetAIOConfigAndSecrets(ctx context.Context, c model.AIOConfig, encrypted map[string][]byte) error {
	expected := c.Revision
	c.Revision++
	if expected < 1 {
		return model.ErrInvalid
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	refs := c.SecretReferences()
	for _, t := range c.Trackers {
		refs = append(refs, t.AnnounceSecret, t.PasskeySecret)
	}
	for name, ciphertext := range encrypted {
		if len(ciphertext) == 0 {
			return model.ErrInvalid
		}
		referenced := false
		for _, ref := range refs {
			referenced = referenced || ref == name
		}
		if !referenced {
			return model.ErrInvalid
		}
		tag, err := tx.Exec(ctx, `INSERT INTO ingest.secrets(name,ciphertext) VALUES($1,$2) ON CONFLICT DO NOTHING`, name, ciphertext)
		if err != nil {
			return databaseError("create migrated announce secret", err)
		}
		if tag.RowsAffected() != 1 {
			return model.ErrConflict
		}
	}
	sort.Strings(refs)
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		var name string
		if err = tx.QueryRow(ctx, `SELECT name FROM ingest.secrets WHERE name=$1 FOR KEY SHARE`, ref).Scan(&name); err != nil {
			return databaseError("lock configured secret", err)
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO ingest.settings(key,value) SELECT 'aiosource.config.v2',$1 WHERE $2::bigint=1 OR EXISTS(SELECT 1 FROM ingest.settings WHERE key='aiosource.config.v2') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value WHERE (ingest.settings.value::jsonb->>'revision')::bigint=$2`, string(b), expected)
	if err != nil {
		return databaseError("save AIOSources configuration", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrConflict
	}
	return databaseError("commit AIOSources configuration", tx.Commit(ctx))
}

const aioUpdateStatusColumns = `(SELECT count(*) FROM ingest.aio_metadata_jobs),EXISTS(SELECT 1 FROM ingest.aio_metadata_jobs WHERE next_attempt<=now()),(SELECT count(*) FROM ingest.aio_cleanup WHERE NOT failed),(SELECT count(*) FROM ingest.aio_cleanup WHERE failed),last_sync,changes_through,inventory_date,sync_retry_at,sync_failures,sync_error,last_match,recovery_started_at,applied_through,provider_retry_at`

const aioVideoCount = `SELECT count(*) AS total FROM ingest.aio_works WHERE media_type IN ('movie','tv')`

// One aggregate consumes current decisions once. Referencing a large CTE from
// four separate counts forces PostgreSQL to spool and reread the whole catalog.
const aioStatusQuery = `WITH decisions AS (SELECT ` + aioDiagnosticStatus + ` AS status` + aioDiagnosticSource + `), grouped AS (
SELECT status,count(*) AS total FROM decisions GROUP BY status
), counts AS (
SELECT COALESCE(sum(total) FILTER(WHERE status='matched'),0)::bigint AS matched,
COALESCE(sum(total) FILTER(WHERE status='ambiguous'),0)::bigint AS ambiguous,
COALESCE(sum(total) FILTER(WHERE status='unmatched'),0)::bigint AS unmatched,
COALESCE(sum(total) FILTER(WHERE status='pending'),0)::bigint AS pending FROM grouped
), video_counts AS MATERIALIZED (
` + aioVideoCount + `
) SELECT
video_counts.total+(SELECT works FROM ingest.ol_import_state WHERE singleton),video_counts.total,
counts.matched,counts.ambiguous,counts.unmatched,counts.pending,
` + aioUpdateStatusColumns + ` FROM ingest.aio_state CROSS JOIN counts CROSS JOIN video_counts`

func aioUpdateStatusTargets(v *model.AIOStatus) []any {
	return []any{&v.Pending, &v.MetadataDue, &v.CleanupPending, &v.CleanupFailed, &v.LastSync, &v.ChangesThrough, &v.InventoryDate, &v.SyncRetryAt, &v.SyncFailures, &v.SyncError, &v.LastMatch, &v.RecoveryStartedAt, &v.AppliedThrough, &v.ProviderRetryAt}
}

func (s *Store) AIOStatus(ctx context.Context) (model.AIOStatus, error) {
	var v model.AIOStatus
	targets := append([]any{&v.Works, &v.VideoWorks, &v.Matched, &v.Ambiguous, &v.Unmatched, &v.MatchingPending}, aioUpdateStatusTargets(&v)...)
	err := s.pool.QueryRow(ctx, aioStatusQuery).Scan(targets...)
	return v, databaseError("read AIOSources status", err)
}

// Information pages need exact metadata counts and checkpoints, without deriving
// identification decisions for every imported torrent. Keep those scans scoped
// to Identification, which actually displays their results.
func (s *Store) AIOInformationStatus(ctx context.Context) (model.AIOStatus, error) {
	var v model.AIOStatus
	targets := append([]any{&v.Works, &v.VideoWorks}, aioUpdateStatusTargets(&v)...)
	err := s.pool.QueryRow(ctx, `WITH video_counts AS (`+aioVideoCount+`)
SELECT video_counts.total+(SELECT works FROM ingest.ol_import_state WHERE singleton),video_counts.total,`+aioUpdateStatusColumns+`
FROM ingest.aio_state CROSS JOIN video_counts`).Scan(targets...)
	return v, databaseError("read information statistics", err)
}
func (s *Store) SaveAIOWork(ctx context.Context, w model.AIOWork, normalized []string) error {
	return s.SaveAIOMetadataBatch(ctx, []AIOMetadataPublication{{Work: w, Normalized: normalized}})
}

func queueAIOAliases(ctx context.Context, tx pgx.Tx, workID int64, aliases []string) error {
	return queueAIOManyAliases(ctx, tx, []int64{workID}, aliases)
}

const aioWorkColumns = `w.id,w.media_type,COALESCE(w.tmdb_id,''),w.imdb_id,w.tvdb_id,` + bookWorkYears + `,w.revision,w.adult,w.display_title,w.localized_titles,
ARRAY(SELECT title FROM ingest.aio_titles WHERE work_id=w.id UNION SELECT w.display_title WHERE w.media_type='book'),
COALESCE((SELECT jsonb_object_agg(season,year) FROM ingest.aio_seasons WHERE work_id=w.id),'{}'),
COALESCE((SELECT jsonb_object_agg(season::text||':'||episode::text,year) FROM ingest.aio_episodes WHERE work_id=w.id),'{}'),w.openlibrary_id,w.poster_path,
w.book_data || jsonb_build_object('author_aliases',COALESCE((SELECT jsonb_agg(alias) FROM (SELECT DISTINCT unnest(a.aliases) AS alias FROM ingest.ol_authors a WHERE w.media_type='book' AND a.id IN(SELECT ingest.ol_resolve(jsonb_array_elements_text(COALESCE(w.book_data->'author_keys','[]')))) LIMIT 200) names),'[]'), 'authors',COALESCE((SELECT jsonb_agg(a.name ORDER BY a.id) FROM ingest.ol_authors a WHERE a.id IN(SELECT ingest.ol_resolve(jsonb_array_elements_text(COALESCE(w.book_data->'author_keys','[]'))))),'[]'),
'cover',COALESCE(NULLIF((w.book_data->>'cover')::bigint,0),(SELECT cover FROM ingest.ol_editions WHERE w.media_type='book' AND work_keys && ingest.ol_aliases(w.openlibrary_id) AND cover>0 ORDER BY id LIMIT 1),0))`

func scanAIOWork(row scanner) (model.AIOWork, error) {
	var w model.AIOWork
	var seasons, episodes, book []byte
	err := row.Scan(&w.ID, &w.Type, &w.TMDB, &w.IMDb, &w.TVDB, &w.Years, &w.Revision, &w.Adult, &w.DisplayTitle, &w.LocalizedTitles, &w.Titles, &seasons, &episodes, &w.OpenLibrary, &w.PosterPath, &book)
	if err != nil {
		return w, err
	}
	if err = json.Unmarshal(seasons, &w.SeasonYears); err == nil {
		err = json.Unmarshal(episodes, &w.EpisodeYears)
	}
	if err == nil && w.Type == "book" {
		err = json.Unmarshal(book, &w.Book)
	}
	return w, err
}

const aioCandidatesQuery = `WITH candidate_ids AS (
 SELECT id FROM ingest.aio_works WHERE $2<>'' AND tmdb_id=$2
 UNION SELECT id FROM ingest.aio_works WHERE $3<>'' AND imdb_id=$3
 UNION SELECT id FROM ingest.aio_works WHERE $4<>'' AND tvdb_id=$4
 UNION SELECT work_id FROM ingest.aio_titles WHERE normalized=$1
 UNION SELECT work_id FROM ingest.aio_titles WHERE NOT $9::boolean AND normalized OPERATOR(public.%) $1 AND public.similarity(normalized,$1)>=0.5
) SELECT ` + aioWorkColumns + ` FROM candidate_ids c JOIN ingest.aio_works w ON w.id=c.id
WHERE NOT w.adult AND w.media_type<>'book' AND ($5='' OR w.media_type=$5) AND ($6::int IS NULL OR $6=ANY(w.years)
OR (w.media_type='tv' AND EXISTS(SELECT 1 FROM ingest.aio_seasons s WHERE s.work_id=w.id AND s.season=ANY($7::int[]) AND s.year=$6))
OR (w.media_type='tv' AND EXISTS(SELECT 1 FROM ingest.aio_episodes e WHERE e.work_id=w.id AND e.season=ANY($7::int[]) AND e.year=$6 AND ($8::int IS NULL OR (cardinality($7::int[])=1 AND e.episode=$8)))))
ORDER BY w.id LIMIT 101`

func (s *Store) AIOCandidates(ctx context.Context, title, tmdb, imdb, tvdb string) ([]model.AIOWork, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, false, databaseError("find metadata candidates", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.5',true)`); err != nil {
		return nil, false, err
	}
	out, overflow, err := searchAIOCandidates(ctx, tx, model.AIOCandidateFilter{Title: title, TMDB: tmdb, IMDb: imdb, TVDB: tvdb})
	if err != nil {
		return nil, false, err
	}
	return out, overflow, databaseError("read metadata candidates", tx.Commit(ctx))
}

const aioRecordColumns = `t.provider_id,t.source_id,t.fields,t.fields_fingerprint`

func scanAIORecord(row scanner) (model.AIORecord, error) { return scanAIORecordQueue(row, false) }
func scanAIORecordQueue(row scanner, queue bool) (model.AIORecord, error) {
	var r model.AIORecord
	var b []byte
	dest := []any{&r.Provider, &r.SourceID, &b, &r.Fingerprint}
	if queue {
		dest = append(dest, &r.QueueVersion)
	}
	if err := row.Scan(dest...); err != nil {
		return r, err
	}
	f, err := decodeObject(b)
	if err != nil {
		return r, err
	}
	text := func(names ...string) string {
		for _, name := range names {
			if v := stringValue(f[name]); v != "" {
				return v
			}
		}
		return ""
	}
	r.Title = text("title", "name")
	r.BookHints = model.BookSourceHints(f)
	r.Hash = strings.ToLower(text("info_hash", "hash", "infohash"))
	r.Size, _ = strconv.ParseInt(text("size"), 10, 64)
	if published, err := time.Parse(time.RFC3339Nano, text("published_at")); err == nil {
		published = published.UTC()
		r.PublishedAt = &published
	}
	if seconds, err := strconv.ParseFloat(text("duration"), 64); err == nil && seconds > 0 && !math.IsInf(seconds, 0) && seconds <= float64(1<<53-1)/1000 {
		r.DurationSeconds = seconds
	}
	for kind, dst := range map[string]*string{"imdb": &r.IMDb, "tmdb": &r.TMDB, "tvdb": &r.TVDB, "mal": &r.MAL} {
		values := []any{f[kind+"_id"], f[kind]}
		if kind == "imdb" {
			values = append(values, f["tt_id"])
		}
		if ext, ok := f["external_ids"].(map[string]any); ok {
			values = append(values, ext[kind], ext[kind+"_id"])
		}
		if attrs, ok := f["attributes"].(map[string]any); ok {
			values = append(values, attrs[kind], attrs[kind+"id"])
		}
		established := ""
		for _, value := range values {
			v := stringValue(value)
			if v == "invalid conflicting values" {
				established = v
				break
			}
			if v == "" || v == "0" || v == "null" {
				continue
			}
			if kind == "imdb" {
				v = "tt" + strings.TrimPrefix(strings.ToLower(v), "tt")
			} else if n, err := strconv.ParseUint(v, 10, 53); err == nil {
				v = strconv.FormatUint(n, 10)
			}
			if established != "" && established != v {
				established = "invalid conflicting values"
				break
			}
			established = v
		}
		*dst = established
	}
	index := func(key string) *int {
		v := text(key)
		if v == "" {
			if attrs, ok := f["attributes"].(map[string]any); ok {
				v = stringValue(attrs[key])
			}
		}
		if v == "" {
			return nil
		}
		n, e := strconv.ParseUint(v, 10, 31)
		if e != nil {
			i := -1
			return &i
		}
		i := int(n)
		return &i
	}
	r.Season = index("season")
	r.Episode = index("episode")
	// Source adapters use both ordinary fields and Torznab attributes. Only
	// explicit audio language fields belong here; raw/private fields stay private.
	var addLanguages func(any)
	addLanguages = func(value any) {
		switch value := value.(type) {
		case string:
			for _, language := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '/' || r == ';' }) {
				language = strings.TrimSpace(language)
				if language != "" && !slices.Contains(r.Languages, language) {
					r.Languages = append(r.Languages, language)
				}
			}
		case []any:
			for _, language := range value {
				addLanguages(language)
			}
		}
	}
	for _, key := range []string{"audiolangs", "languages", "language"} {
		addLanguages(f[key])
	}
	if attributes, ok := f["attributes"].(map[string]any); ok {
		for _, key := range []string{"audiolangs", "languages", "language"} {
			addLanguages(attributes[key])
		}
	}

	// Matching queues do not need display-only metadata.
	if !queue {
		readAIOPresentation(f, &r)
	}
	return r, nil
}
func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case []any:
		if len(x) == 0 {
			return ""
		}
		first := stringValue(x[0])
		for _, v := range x {
			if stringValue(v) != first {
				return "invalid conflicting values"
			}
		}
		return first
	default:
		return ""
	}
}
func (s *Store) AIOMatchBatch(ctx context.Context, limit int) ([]model.AIORecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+aioRecordColumns+`,q.version FROM ingest.aio_match_queue q JOIN ingest.torrents t USING(provider_id,source_id) ORDER BY q.provider_id,q.source_id LIMIT $1`, limit)
	if err != nil {
		return nil, databaseError("read matching queue", err)
	}
	defer rows.Close()
	out := []model.AIORecord{}
	for rows.Next() {
		v, e := scanAIORecordQueue(rows, true)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, databaseError("read matching queue", rows.Err())
}
func (s *Store) SaveAIOMatch(ctx context.Context, r model.AIORecord, status, reason string, work *model.AIOWork) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR SHARE`).Scan(&generation); err != nil {
		return databaseError("verify candidate generation", err)
	}
	if generation != r.CandidateGeneration {
		return nil
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT md5(fields::text) FROM ingest.torrents WHERE provider_id=$1 AND source_id=$2 FOR SHARE`, r.Provider, r.SourceID).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return databaseError("lock matched torrent", err)
	}
	if current != r.Fingerprint {
		return nil
	}
	if reviewed, err := reviewedAIOMatch(ctx, tx, r); err != nil {
		return err
	} else if reviewed != nil {
		status, reason, work = reviewed.Status, reviewed.Reason, reviewed.Work
	}
	var id any
	var rev int64
	if work != nil {
		var actual int64
		if err = tx.QueryRow(ctx, `SELECT revision FROM ingest.aio_works WHERE id=$1 FOR SHARE`, work.ID).Scan(&actual); err != nil {
			return databaseError("verify metadata revision", err)
		}
		if actual != work.Revision {
			return nil
		}
		id = work.ID
		rev = work.Revision
	}
	if err = lockCatalogEntityWrites(ctx, tx); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason,normalized) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(provider_id,source_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,work_id=EXCLUDED.work_id,revision=EXCLUDED.revision,status=EXCLUDED.status,reason=EXCLUDED.reason,normalized=EXCLUDED.normalized,updated_at=now()`, r.Provider, r.SourceID, r.Fingerprint, id, rev, status, reason, r.Normalized)
	if err != nil {
		return databaseError("save correspondence", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM ingest.aio_match_queue WHERE provider_id=$1 AND source_id=$2 AND version=$3`, r.Provider, r.SourceID, r.QueueVersion); err != nil {
		return databaseError("finish matching queue", err)
	}
	return databaseError("publish correspondence", tx.Commit(ctx))
}
func (s *Store) AIOStreams(ctx context.Context, kind, id, mediaType string) ([]model.AIORecord, error) {
	// The identifier column is selected from a closed allowlist, never user SQL.
	column := map[string]string{"imdb": "imdb_id", "tmdb": "tmdb_id", "tvdb": "tvdb_id", "openlibrary": "openlibrary_id"}[kind]
	typ := "tv"
	if mediaType == "movie" || mediaType == "book" {
		typ = mediaType
	}
	order := ` ORDER BY COALESCE(t.search_published_at,t.first_seen_at) DESC,t.provider_id,t.source_id`
	query := `SELECT ` + aioRecordColumns + `,w.imdb_id,COALESCE(w.tmdb_id,''),w.tvdb_id FROM ingest.torrents t JOIN ingest.aio_matches m USING(provider_id,source_id) JOIN ingest.aio_works w ON w.id=m.work_id WHERE ` + aioPublishedMatch + ` AND w.media_type=$2 AND w.` + column + `=$1` + order
	if kind == "mal" {
		query = `SELECT ` + aioRecordColumns + ` FROM ingest.torrents t WHERE (` + aioMALAssertionPredicate + `) AND ingest.aio_native_mal_candidates(t.fields) @> ARRAY[$1::text]` + order
		rows, err := s.pool.Query(ctx, query, id)
		out, err := collectAIORecords(rows, err, false)
		// An indexed candidate is not an established assertion: every native
		// representation must agree. Never publish conflicting MAL values.
		return slices.DeleteFunc(out, func(r model.AIORecord) bool { return r.MAL != id }), err
	}
	if column == "" {
		return []model.AIORecord{}, nil
	}
	rows, err := s.pool.Query(ctx, query, id, typ)
	records, err := collectAIORecords(rows, err, true)
	if err == nil {
		err = s.applyAIOEpisodeReviews(ctx, records)
	}
	return records, err
}

type aioStreamRow struct {
	scanner
	ids []any
}

func (r aioStreamRow) Scan(dest ...any) error { return r.scanner.Scan(append(dest, r.ids...)...) }

func collectAIORecords(rows pgx.Rows, err error, publishedIDs bool) ([]model.AIORecord, error) {
	if err != nil {
		return nil, databaseError("search local streams", err)
	}
	defer rows.Close()
	out := []model.AIORecord{}
	for rows.Next() {
		var imdb, tmdb, tvdb string
		var row scanner = rows
		if publishedIDs {
			row = aioStreamRow{scanner: rows, ids: []any{&imdb, &tmdb, &tvdb}}
		}
		v, e := scanAIORecord(row)
		if e != nil {
			return nil, e
		}
		if publishedIDs {
			// This projection is already fenced by work revision, source
			// fingerprint and an empty queue. Use its verified identifiers,
			// including those absent from the unchanged native source fields.
			v.IMDb, v.TMDB, v.TVDB = imdb, tmdb, tvdb
		}
		out = append(out, v)
	}
	return out, databaseError("search local streams", rows.Err())
}
func (s *Store) AIOTorrent(ctx context.Context, provider, id string) (model.AIORecord, error) {
	v, err := scanAIORecord(s.pool.QueryRow(ctx, `SELECT `+aioRecordColumns+` FROM ingest.torrents t WHERE provider_id=$1 AND source_id=$2`, provider, id))
	if err == nil {
		records := []model.AIORecord{v}
		err = s.applyAIOEpisodeReviews(ctx, records)
		v = records[0]
	}
	return v, databaseError("read playback torrent", err)
}
func (s *Store) QueueAIOMetadata(ctx context.Context, typ string, ids []string, refresh bool) error {
	q := `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) SELECT $1,id FROM(SELECT DISTINCT unnest($2::text[]) id) i WHERE NOT EXISTS(SELECT 1 FROM ingest.aio_works w WHERE w.media_type=$1 AND w.tmdb_id=i.id) ON CONFLICT DO NOTHING`
	if refresh {
		q = `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) SELECT $1,id FROM(SELECT DISTINCT unnest($2::text[]) id) i ON CONFLICT(media_type,tmdb_id) DO UPDATE SET version=ingest.aio_metadata_jobs.version+1,next_attempt=now()`
	}
	_, err := s.pool.Exec(ctx, q, typ, ids)
	return databaseError("queue metadata", err)
}
func (s *Store) QueueAIOInventory(ctx context.Context, typ string, ids []string, date string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `INSERT INTO ingest.aio_inventory(media_type,tmdb_id,export_date) SELECT $1,id,$3 FROM(SELECT DISTINCT unnest($2::text[]) id) i ON CONFLICT(media_type,tmdb_id) DO UPDATE SET export_date=EXCLUDED.export_date WHERE ingest.aio_inventory.export_date IS DISTINCT FROM EXCLUDED.export_date`, typ, ids, date); err != nil {
		return databaseError("store inventory", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) SELECT $1,id FROM(SELECT DISTINCT unnest($2::text[]) id) i WHERE NOT EXISTS(SELECT 1 FROM ingest.aio_works w WHERE w.media_type=$1 AND w.tmdb_id=i.id) ON CONFLICT DO NOTHING`, typ, ids); err != nil {
		return databaseError("queue inventory", err)
	}
	return databaseError("commit inventory page", tx.Commit(ctx))
}
func (s *Store) ReconcileAIOInventory(ctx context.Context, date string) error {
	for {
		// An export is a dated snapshot, not the current catalog. Keep works
		// refreshed since its UTC day began; a subsequent export can reconcile them.
		rows, err := s.pool.Query(ctx, `SELECT media_type,tmdb_id FROM ingest.aio_works w WHERE w.media_type IN ('movie','tv') AND w.updated_at < ($1::text::date::timestamp AT TIME ZONE 'UTC') AND NOT EXISTS(SELECT 1 FROM ingest.aio_inventory i WHERE i.media_type=w.media_type AND i.tmdb_id=w.tmdb_id AND i.export_date=$1) LIMIT 100`, date)
		if err != nil {
			return databaseError("reconcile metadata inventory", err)
		}
		jobs := []AIOJob{}
		for rows.Next() {
			var j AIOJob
			if err = rows.Scan(&j.Type, &j.ID); err != nil {
				rows.Close()
				return err
			}
			jobs = append(jobs, j)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			break
		}
		for _, j := range jobs {
			if err = s.DeleteAIOWork(ctx, j.Type, j.ID); err != nil {
				return err
			}
		}
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM ingest.aio_inventory WHERE export_date<>$1`, date)
	return databaseError("finish inventory reconciliation", err)
}

type AIOJob struct {
	Type, ID string
	Version  int64
	Attempts int
}

func (s *Store) AIOMetadataJobs(ctx context.Context) ([]AIOJob, error) {
	limit, err := s.RuntimeValue(ctx, "metadata", "queue_batch")
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT media_type,tmdb_id,version,attempts FROM ingest.aio_metadata_jobs WHERE next_attempt<=now() ORDER BY next_attempt,media_type,tmdb_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIOJob{}
	for rows.Next() {
		var v AIOJob
		if err := rows.Scan(&v.Type, &v.ID, &v.Version, &v.Attempts); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) FinishAIOMetadata(ctx context.Context, j AIOJob, success bool) error {
	var err error
	if success {
		_, err = s.pool.Exec(ctx, `DELETE FROM ingest.aio_metadata_jobs WHERE media_type=$1 AND tmdb_id=$2 AND version=$3`, j.Type, j.ID, j.Version)
	} else {
		policy, e := s.RuntimeValues(ctx, "metadata_retries")
		if e != nil {
			return e
		}
		_, err = s.pool.Exec(ctx, `UPDATE ingest.aio_metadata_jobs SET attempts=attempts+1,next_attempt=now()+make_interval(secs=>LEAST($5::int,$4::int*power(2,LEAST(attempts,20)))::int) WHERE media_type=$1 AND tmdb_id=$2 AND version=$3`, j.Type, j.ID, j.Version, policy["detail_retry_seconds"], policy["detail_retry_max_seconds"])
	}
	return databaseError("finish metadata job", err)
}
func (s *Store) DeleteAIOWork(ctx context.Context, typ, id string) error {
	return s.deleteAIOWork(ctx, typ, id, nil)
}

// A confirmed deletion and its versioned acknowledgement publish together.
// A later change notification must remain queued even if an older GET was 404.
func (s *Store) DeleteAIOMetadataJob(ctx context.Context, job AIOJob) error {
	return s.deleteAIOWork(ctx, job.Type, job.ID, &job)
}

func (s *Store) deleteAIOWork(ctx context.Context, typ, id string, job *AIOJob) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		return databaseError("lock metadata removal", err)
	}
	var workID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM ingest.aio_works WHERE media_type=$1 AND tmdb_id=$2 FOR UPDATE`, typ, id).Scan(&workID); errors.Is(err, pgx.ErrNoRows) {
		if job == nil {
			return nil
		}
		if err = finishAIOJobs(ctx, tx, []AIOJob{*job}); err != nil {
			return err
		}
		return databaseError("acknowledge missing metadata", tx.Commit(ctx))
	} else if err != nil {
		return databaseError("lock removed metadata", err)
	}
	var aliases []string
	if err = tx.QueryRow(ctx, `SELECT ARRAY(SELECT normalized FROM ingest.aio_titles WHERE work_id=$1)`, workID).Scan(&aliases); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
		return err
	}
	if err = queueAIOAliases(ctx, tx, workID, aliases); err != nil {
		return err
	}
	if err = lockCatalogEntityWrites(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM ingest.aio_works WHERE id=$1`, workID); err != nil {
		return databaseError("remove deleted metadata", err)
	}
	if job != nil {
		if err = finishAIOJobs(ctx, tx, []AIOJob{*job}); err != nil {
			return err
		}
	}
	return databaseError("publish metadata removal", tx.Commit(ctx))
}
func (s *Store) AIOInventoryDate(ctx context.Context, date string) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.aio_state SET inventory_date=$1`, date)
	return databaseError("save inventory checkpoint", err)
}
func (s *Store) AIOChangesThrough(ctx context.Context, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.aio_state SET changes_through=$1`, at)
	return databaseError("save change checkpoint", err)
}
func (s *Store) ResetAIOMapping(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;DELETE FROM ingest.aio_matches;DELETE FROM ingest.aio_match_queue;INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM ingest.torrents;UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1,last_match=NULL`)
	return databaseError("reset correspondence", err)
}
func (s *Store) ResetAIOMetadata(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;SELECT ingest.lock_catalog_entity_writes();TRUNCATE ingest.ol_redirects,ingest.ol_authors,ingest.ol_editions,ingest.aio_works,ingest.aio_titles,ingest.aio_seasons,ingest.aio_episodes,ingest.aio_matches,ingest.aio_match_queue,ingest.aio_metadata_jobs,ingest.aio_season_requests,ingest.aio_inventory;INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM ingest.torrents;DELETE FROM ingest.catalog_entity_changes WHERE kind='metadata';UPDATE ingest.ol_import_state SET snapshot='',etag='',position=0,imported=0,works=0,editions=0,authors=0,skipped=0,last_sync=NULL,error='',complete=FALSE,retry_at=NULL,checked_at=NULL;UPDATE ingest.aio_state SET last_sync=NULL,last_match=NULL,changes_through=NULL,inventory_date='',sync_retry_at=CASE WHEN provider_retry_at>now() THEN provider_retry_at END,sync_failures=0,sync_error='',recovery_started_at=NULL,applied_through=NULL,metadata_generation=metadata_generation+1;`+resetBookChangesSQL)
	return databaseError("reset local metadata", err)
}

// A dedicated connection holds the session lock for the whole worker pass.
func (s *Store) AIOLock(ctx context.Context, name string) (func(), bool, error) {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, false, err
	}
	key := providerLockKey("aiosource:" + name)
	var locked bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil || !locked {
		closeConnection(conn)
		return nil, false, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			// Confirm release before returning: closing the client socket alone
			// can leave a successor seeing the old server session's lock.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", key)
			closeConnection(conn)
		})
	}, true, nil
}
func (s *Store) AIORequestedSeasons(ctx context.Context, tmdb, imdb, tvdb string) ([]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT season FROM ingest.aio_season_requests WHERE tmdb_id=$1 UNION SELECT DISTINCT COALESCE(fields->>'season',(regexp_match(fields->>'title','[sS]([0-9]{1,3})[eE]'))[1])::int FROM ingest.torrents WHERE COALESCE(fields->>'season',(regexp_match(fields->>'title','[sS]([0-9]{1,3})[eE]'))[1]) ~ '^[0-9]{1,3}$' AND (fields->>'tmdb_id'=$1 OR ($2<>'' AND regexp_replace(fields->>'imdb_id','^tt','','i')=regexp_replace($2,'^tt','')) OR ($3<>'' AND fields->>'tvdb_id'=$3)) ORDER BY 1`, tmdb, imdb, tvdb)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func (s *Store) RequestAIOSeason(ctx context.Context, tmdb string, season int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	tag, err := tx.Exec(ctx, `INSERT INTO ingest.aio_season_requests(tmdb_id,season) VALUES($1,$2) ON CONFLICT DO NOTHING`, tmdb, season)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) VALUES('tv',$1) ON CONFLICT(media_type,tmdb_id) DO UPDATE SET version=ingest.aio_metadata_jobs.version+1,next_attempt=now()`, tmdb); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) AIOResources(ctx context.Context, service, account string, after time.Time) ([]model.AIOResource, error) {
	rows, err := s.pool.Query(ctx, `SELECT resource,hashes,checked_at,last_attempt,retry FROM ingest.aio_resources WHERE service=$1 AND account=$2 AND checked_at>$3 AND EXISTS(SELECT 1 FROM unnest(hashes) h WHERE NOT EXISTS(SELECT 1 FROM ingest.aio_availability a WHERE a.service=$1 AND a.account=$2 AND a.hash=h AND a.cached AND a.checked_at>$3 AND a.checked_at<=now())) ORDER BY last_attempt LIMIT 64`, service, account, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AIOResource{}
	for rows.Next() {
		v := model.AIOResource{Service: service, Account: account}
		if err = rows.Scan(&v.Key, &v.Hashes, &v.CheckedAt, &v.LastAttempt, &v.Retry); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Ping(ctx context.Context) error {
	return databaseError("check database", s.pool.Ping(ctx))
}

func (s *Store) AIOCandidateGeneration(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT metadata_generation FROM ingest.aio_state`).Scan(&n)
	return n, databaseError("read candidate generation", err)
}
