package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

const (
	aioPropagatedJoin    = ` LEFT JOIN aiosources.aio_release_group_members rgm ON rgm.provider_id=t.provider_id AND rgm.source_id=t.source_id AND rgm.fingerprint=t.fields_fingerprint`
	aioPropagatedColumns = `COALESCE(rgm.contributed_audio,ARRAY[]::text[]),COALESCE(rgm.contributed_subtitles,ARRAY[]::text[])`
)

type CorrespondenceBuilder func(scope, scopeKey string, workID *int64, records []model.AIORecord, exclusions map[string]map[string]bool) []model.ComputedReleaseGroup

// enqueueReleaseCorrespondencesTx enqueues only scopes that currently span multiple
// sources or already have persisted correspondence groups that may need pruning.
func enqueueReleaseCorrespondencesTx(ctx context.Context, tx pgx.Tx, workIDs []int64, hashes []string) error {
	cleanWorks := []int64{}
	for _, id := range workIDs {
		if id > 0 && !slices.Contains(cleanWorks, id) {
			cleanWorks = append(cleanWorks, id)
		}
	}
	if len(cleanWorks) > 0 {
		if _, err := tx.Exec(ctx, `WITH target(work_id) AS (SELECT DISTINCT unnest($1::bigint[])),
eligible AS (
 SELECT t.work_id FROM target t
 WHERE EXISTS(
  SELECT 1 FROM aiosources.aio_matches m JOIN aiosources.aio_works w ON w.id=m.work_id
  WHERE m.work_id=t.work_id AND m.status='matched' AND w.media_type IN ('movie','tv') AND NOT w.adult
  GROUP BY m.work_id HAVING count(DISTINCT m.provider_id) >= 2
 )
 OR EXISTS(SELECT 1 FROM aiosources.aio_release_groups g WHERE g.scope='work' AND g.scope_key=t.work_id::text)
)
INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT 'work',work_id::text FROM eligible
ON CONFLICT(scope,scope_key) DO UPDATE SET version=aiosources.aio_release_correspondence_queue.version+1`, cleanWorks); err != nil {
			return databaseError("queue work correspondences", err)
		}
	}

	cleanHashes := []string{}
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if len(h) == 40 && !slices.Contains(cleanHashes, h) {
			cleanHashes = append(cleanHashes, h)
		}
	}
	if len(cleanHashes) > 0 {
		if _, err := tx.Exec(ctx, `WITH target(info_hash) AS (SELECT DISTINCT unnest($1::text[])),
eligible AS (
 SELECT t.info_hash FROM target t
 WHERE EXISTS(
  SELECT 1 FROM aiosources.torrents tr WHERE tr.search_info_hash=t.info_hash
  GROUP BY tr.search_info_hash HAVING count(DISTINCT tr.provider_id) >= 2
 )
 OR EXISTS(SELECT 1 FROM aiosources.aio_release_groups g WHERE g.scope='hash' AND g.scope_key=t.info_hash)
)
INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT 'hash',info_hash FROM eligible
ON CONFLICT(scope,scope_key) DO UPDATE SET version=aiosources.aio_release_correspondence_queue.version+1`, cleanHashes); err != nil {
			return databaseError("queue hash correspondences", err)
		}
	}
	return nil
}

func (s *Store) QueueAllReleaseCorrespondences(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin correspondence queue refresh", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT 'work', m.work_id::text
FROM aiosources.aio_matches m
JOIN aiosources.aio_works w ON w.id=m.work_id
WHERE m.status='matched' AND m.work_id IS NOT NULL AND w.media_type IN ('movie','tv') AND NOT w.adult
GROUP BY m.work_id
HAVING count(DISTINCT m.provider_id) >= 2
ON CONFLICT(scope,scope_key) DO UPDATE SET version=aiosources.aio_release_correspondence_queue.version+1`); err != nil {
		return databaseError("queue multi-source works", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT 'hash', t.search_info_hash
FROM aiosources.torrents t
WHERE t.search_info_hash IS NOT NULL AND length(t.search_info_hash) = 40
GROUP BY t.search_info_hash
HAVING count(DISTINCT t.provider_id) >= 2
ON CONFLICT(scope,scope_key) DO UPDATE SET version=aiosources.aio_release_correspondence_queue.version+1`); err != nil {
		return databaseError("queue multi-source hashes", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT DISTINCT scope, scope_key FROM aiosources.aio_release_groups
ON CONFLICT(scope,scope_key) DO UPDATE SET version=aiosources.aio_release_correspondence_queue.version+1`); err != nil {
		return databaseError("queue existing correspondence groups", err)
	}
	return databaseError("commit correspondence queue refresh", tx.Commit(ctx))
}

func (s *Store) ClaimCorrespondenceScopes(ctx context.Context, limit int) ([]model.CorrespondenceScope, error) {
	if limit <= 0 {
		limit = 50
	}
	// Process 'work' scopes before 'hash' scopes ('work' > 'hash' lexicographically)
	// so work-scoped groups claim their members before hash-only fallback groups run.
	rows, err := s.pool.Query(ctx, `SELECT scope,scope_key,version FROM aiosources.aio_release_correspondence_queue ORDER BY scope DESC,scope_key LIMIT $1`, limit)
	if err != nil {
		return nil, databaseError("read correspondence queue", err)
	}
	defer rows.Close()
	out := []model.CorrespondenceScope{}
	for rows.Next() {
		var item model.CorrespondenceScope
		if err = rows.Scan(&item.Scope, &item.ScopeKey, &item.Version); err != nil {
			return nil, databaseError("scan correspondence queue", err)
		}
		out = append(out, item)
	}
	return out, databaseError("read correspondence queue", rows.Err())
}

func (s *Store) ProcessCorrespondenceBatch(ctx context.Context, scopes []model.CorrespondenceScope, build CorrespondenceBuilder) (groupsCount, membersCount, enrichedCount int, err error) {
	if len(scopes) == 0 {
		return 0, 0, 0, nil
	}
	if build == nil {
		return 0, 0, 0, model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, 0, databaseError("begin correspondence batch", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR SHARE`); err != nil {
		return 0, 0, 0, databaseError("lock correspondence generation", err)
	}
	for _, item := range scopes {
		g, m, e, err := recomputeCorrespondenceScopeTx(ctx, tx, item.Scope, item.ScopeKey, build)
		if err != nil {
			return 0, 0, 0, err
		}
		groupsCount += g
		membersCount += m
		enrichedCount += e
		if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_release_correspondence_queue WHERE scope=$1 AND scope_key=$2 AND version=$3`, item.Scope, item.ScopeKey, item.Version); err != nil {
			return 0, 0, 0, databaseError("acknowledge correspondence scope", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, 0, 0, databaseError("publish correspondence batch", err)
	}
	return groupsCount, membersCount, enrichedCount, nil
}

func recomputeCorrespondenceScopeTx(ctx context.Context, tx pgx.Tx, scope, scopeKey string, build CorrespondenceBuilder) (groupsCount, membersCount, enrichedCount int, err error) {
	var workIDPtr *int64
	records := []model.AIORecord{}

	switch scope {
	case "work":
		wid, parseErr := strconv.ParseInt(scopeKey, 10, 64)
		if parseErr != nil || wid <= 0 {
			return 0, 0, 0, nil
		}
		workIDPtr = &wid
		rows, err := tx.Query(ctx, `SELECT `+aioRecordColumns+`
FROM aiosources.aio_matches m
JOIN aiosources.aio_works w ON w.id=m.work_id
JOIN aiosources.torrents t USING(provider_id,source_id)
WHERE m.work_id=$1 AND w.media_type IN ('movie','tv') AND `+aioPublishedMatch+`
ORDER BY t.provider_id,t.source_id`, wid)
		if err != nil {
			return 0, 0, 0, databaseError("load work correspondence candidates", err)
		}
		for rows.Next() {
			rec, scanErr := scanAIORecord(rows)
			if scanErr != nil {
				rows.Close()
				return 0, 0, 0, scanErr
			}
			records = append(records, rec)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, 0, 0, databaseError("read work correspondence candidates", err)
		}
	case "hash":
		if len(scopeKey) != 40 {
			return 0, 0, 0, nil
		}
		rows, err := tx.Query(ctx, `SELECT `+aioRecordColumns+`,m.work_id
FROM aiosources.torrents t
LEFT JOIN aiosources.aio_matches m ON m.provider_id=t.provider_id AND m.source_id=t.source_id AND m.status='matched' AND m.fingerprint=t.fields_fingerprint
LEFT JOIN aiosources.aio_release_group_members existing ON existing.provider_id=t.provider_id AND existing.source_id=t.source_id
LEFT JOIN aiosources.aio_release_groups existing_g ON existing_g.id=existing.group_id
WHERE t.search_info_hash=$1
AND (existing_g.scope IS NULL OR existing_g.scope='hash')
ORDER BY t.provider_id,t.source_id`, scopeKey)
		if err != nil {
			return 0, 0, 0, databaseError("load hash correspondence candidates", err)
		}
		seenWorks := map[int64]bool{}
		for rows.Next() {
			var wid *int64
			rec, scanErr := scanAIORecord(rows, &wid)
			if scanErr != nil {
				rows.Close()
				return 0, 0, 0, scanErr
			}
			if wid != nil && *wid > 0 {
				seenWorks[*wid] = true
			}
			records = append(records, rec)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, 0, 0, databaseError("read hash correspondence candidates", err)
		}
		// Never merge torrents that point to two different published works.
		if len(seenWorks) > 1 {
			records = nil
		} else if len(seenWorks) == 1 {
			for wid := range seenWorks {
				w := wid
				workIDPtr = &w
			}
		}
	default:
		return 0, 0, 0, model.ErrInvalid
	}

	if err = applyAIOEpisodeReviewsTx(ctx, tx, records); err != nil {
		return 0, 0, 0, err
	}

	exclusions := map[string]map[string]bool{}
	if len(records) >= 2 {
		providers := make([]string, len(records))
		sourceIDs := make([]string, len(records))
		for i, rec := range records {
			providers[i] = rec.Provider
			sourceIDs[i] = rec.SourceID
		}
		exRows, err := tx.Query(ctx, `SELECT provider_id,source_id,match_key
FROM aiosources.aio_release_correspondence_exclusions
WHERE (provider_id,source_id) IN (SELECT * FROM unnest($1::text[],$2::text[]))`, providers, sourceIDs)
		if err != nil {
			return 0, 0, 0, databaseError("load correspondence exclusions", err)
		}
		for exRows.Next() {
			var p, sid, key string
			if scanErr := exRows.Scan(&p, &sid, &key); scanErr != nil {
				exRows.Close()
				return 0, 0, 0, databaseError("scan correspondence exclusion", scanErr)
			}
			if exclusions[key] == nil {
				exclusions[key] = map[string]bool{}
			}
			exclusions[key][p+"\x00"+sid] = true
		}
		err = exRows.Err()
		exRows.Close()
		if err != nil {
			return 0, 0, 0, databaseError("read correspondence exclusions", err)
		}
	}

	computed := build(scope, scopeKey, workIDPtr, records, exclusions)

	// Remove previous groups owned by this (scope, scope_key).
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_release_groups WHERE scope=$1 AND scope_key=$2`, scope, scopeKey); err != nil {
		return 0, 0, 0, databaseError("clear previous correspondence groups", err)
	}

	for _, g := range computed {
		if len(g.Members) < 2 {
			continue
		}
		// If any member belonged to a superseded scope (e.g. a hash-only group now superseded by work scope),
		// detach it cleanly and re-queue the old scope so it updates/dissolves cleanly.
		mProviders := make([]string, len(g.Members))
		mSources := make([]string, len(g.Members))
		for i, m := range g.Members {
			mProviders[i] = m.ProviderID
			mSources[i] = m.SourceID
		}
		if scope == "work" {
			if _, err = tx.Exec(ctx, `WITH displaced AS (
 DELETE FROM aiosources.aio_release_group_members m
 USING aiosources.aio_release_groups old_g
 WHERE m.group_id=old_g.id AND old_g.scope='hash'
 AND (m.provider_id,m.source_id) IN (SELECT * FROM unnest($1::text[],$2::text[]))
 RETURNING old_g.scope,old_g.scope_key
)
INSERT INTO aiosources.aio_release_correspondence_queue(scope,scope_key)
SELECT DISTINCT scope,scope_key FROM displaced
ON CONFLICT(scope,scope_key) DO UPDATE SET version=aiosources.aio_release_correspondence_queue.version+1`, mProviders, mSources); err != nil {
				return 0, 0, 0, databaseError("rebalance displaced hash correspondence", err)
			}
		}

		// Delete any stale conflicting match_key before inserting.
		if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_release_groups WHERE match_key=$1`, g.MatchKey); err != nil {
			return 0, 0, 0, databaseError("clear conflicting correspondence key", err)
		}

		var groupID int64
		if err = tx.QueryRow(ctx, `INSERT INTO aiosources.aio_release_groups(
 scope,scope_key,work_id,match_rule,match_key,confidence,canonical_size,canonical_title,
 member_count,provider_count,enriched_count,propagated_audio,propagated_subtitles,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now())
RETURNING id`,
			g.Scope, g.ScopeKey, g.WorkID, g.MatchRule, g.MatchKey, g.Confidence,
			g.CanonicalSize, g.CanonicalTitle, g.MemberCount, g.ProviderCount, g.EnrichedCount,
			g.PropagatedAudio, g.PropagatedSubtitles,
		).Scan(&groupID); err != nil {
			return 0, 0, 0, databaseError("save correspondence group", err)
		}

		insertedMembers := 0
		for _, m := range g.Members {
			tag, execErr := tx.Exec(ctx, `INSERT INTO aiosources.aio_release_group_members(
 group_id,provider_id,source_id,fingerprint,match_rule,own_audio,own_subtitles,contributed_audio,contributed_subtitles)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(provider_id,source_id) DO NOTHING`,
				groupID, m.ProviderID, m.SourceID, m.Fingerprint, m.MatchRule,
				m.OwnAudio, m.OwnSubtitles, m.ContributedAudio, m.ContributedSubtitles,
			)
			if execErr != nil {
				return 0, 0, 0, databaseError("save correspondence member", execErr)
			}
			insertedMembers += int(tag.RowsAffected())
		}
		if insertedMembers < 2 {
			if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_release_groups WHERE id=$1`, groupID); err != nil {
				return 0, 0, 0, databaseError("prune incomplete correspondence group", err)
			}
			continue
		}
		groupsCount++
		membersCount += insertedMembers
		enrichedCount += g.EnrichedCount
	}
	return groupsCount, membersCount, enrichedCount, nil
}

func applyAIOEpisodeReviewsTx(ctx context.Context, tx pgx.Tx, records []model.AIORecord) error {
	if len(records) == 0 {
		return nil
	}
	providers, ids := make([]string, len(records)), make([]string, len(records))
	for i, record := range records {
		providers[i], ids[i] = record.Provider, record.SourceID
	}
	rows, err := tx.Query(ctx, `SELECT r.provider_id,r.source_id,r.season,r.episode
FROM aiosources.aio_match_reviews r
JOIN aiosources.torrents t USING(provider_id,source_id)
JOIN aiosources.aio_matches m USING(provider_id,source_id)
JOIN aiosources.aio_works w ON w.id=m.work_id
WHERE (r.provider_id,r.source_id) IN (SELECT * FROM unnest($1::text[],$2::text[]))
AND r.active AND r.override_episode AND r.media_type=w.media_type AND r.tmdb_id=w.tmdb_id
AND r.evidence_fingerprint=md5(aiosources.aio_review_evidence(t.fields)::text)
AND `+aioPublishedMatch, providers, ids)
	if err != nil {
		return databaseError("read reviewed episode overrides", err)
	}
	defer rows.Close()
	type override struct {
		season, episode *int
	}
	overrides := map[string]override{}
	for rows.Next() {
		var p, sid string
		var o override
		if err = rows.Scan(&p, &sid, &o.season, &o.episode); err != nil {
			return databaseError("read reviewed episode override", err)
		}
		overrides[p+"\x00"+sid] = o
	}
	if err = rows.Err(); err != nil {
		return databaseError("read reviewed episode overrides", err)
	}
	for i := range records {
		if o, ok := overrides[records[i].Provider+"\x00"+records[i].SourceID]; ok {
			records[i].Season, records[i].Episode, records[i].ReviewedEpisode = o.season, o.episode, true
		}
	}
	return nil
}

func (s *Store) ListReleaseCorrespondences(ctx context.Context, opts model.ReleaseCorrespondenceOptions) (model.List[model.ReleaseCorrespondenceGroup], error) {
	opts.ListOptions = pageOptions(opts.ListOptions)
	result := model.List[model.ReleaseCorrespondenceGroup]{
		Items:  []model.ReleaseCorrespondenceGroup{},
		Limit:  opts.Limit,
		Offset: opts.Offset,
	}
	if !validSearchText(opts.Query, 200) || !model.ValidSelection(opts.Rule, model.ValidCorrespondenceRule) {
		return result, model.ErrInvalid
	}
	locale := "en"
	if opts.Language != "" {
		locale = opts.Language
	}
	clauses := []string{"TRUE"}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if opts.Rule != "" {
		clauses = append(clauses, "g.match_rule=ANY(string_to_array("+bind(opts.Rule)+",','))")
	}
	if opts.ProviderID != "" {
		clauses = append(clauses, "EXISTS(SELECT 1 FROM aiosources.aio_release_group_members mp WHERE mp.group_id=g.id AND mp.provider_id=ANY(string_to_array("+bind(opts.ProviderID)+",',')))")
	}
	if opts.EnrichedOnly {
		clauses = append(clauses, "g.enriched_count > 0")
	}
	if q := strings.TrimSpace(opts.Query); q != "" {
		lowerQ := strings.ToLower(q)
		like := "%" + escapeSearchFragment.Replace(lowerQ) + "%"
		p := bind(like)
		tmdbTerm := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(lowerQ, "tmdb:"), "tmdb "))
		tmdbClause := ""
		if tmdbTerm != "" {
			tp := bind("%" + escapeSearchFragment.Replace(tmdbTerm) + "%")
			tmdbClause = ` OR lower(COALESCE(w.tmdb_id,'')) LIKE ` + tp + ` ESCAPE '\'`
		}
		clauses = append(clauses, `(lower(g.canonical_title) LIKE `+p+` ESCAPE '\'
 OR lower(COALESCE(w.display_title,'')) LIKE `+p+` ESCAPE '\'
 OR lower(COALESCE(w.localized_titles::text,'')) LIKE `+p+` ESCAPE '\'
 OR lower(COALESCE(w.imdb_id,'')) LIKE `+p+` ESCAPE '\'`+tmdbClause+`
 OR EXISTS(
  SELECT 1 FROM aiosources.aio_release_group_members mq
  JOIN aiosources.torrents tq USING(provider_id,source_id)
  WHERE mq.group_id=g.id AND (
   lower(COALESCE(tq.fields->>'title','')) LIKE `+p+` ESCAPE '\'
   OR lower(tq.source_id) LIKE `+p+` ESCAPE '\'
   OR lower(COALESCE(tq.search_info_hash,'')) LIKE `+p+` ESCAPE '\'
  )
 ))`)
	}

	where := " WHERE " + strings.Join(clauses, " AND ")
	from := ` FROM aiosources.aio_release_groups g LEFT JOIN aiosources.aio_works w ON w.id=g.work_id`

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin correspondence list", err)
	}
	defer rollback(tx)

	if err = tx.QueryRow(ctx, `SELECT count(*)`+from+where, args...).Scan(&result.Total); err != nil {
		return result, databaseError("count correspondences", err)
	}
	if result.Total <= int64(opts.Offset) {
		return result, databaseError("finish correspondence list", tx.Commit(ctx))
	}

	localeArg := bind(locale)
	limitArg := bind(opts.Limit)
	offsetArg := bind(opts.Offset)
	workLabel := `CASE WHEN w.id IS NULL THEN '' ELSE ` + catalogTitleLabel(localeArg, "NULL") + ` END`

	listSQL := `SELECT g.id,g.scope,g.scope_key,g.work_id,COALESCE(w.media_type,''),` + workLabel + `,
COALESCE(w.imdb_id,''),COALESCE(w.tmdb_id,''),g.match_rule,g.match_key,g.confidence,
g.canonical_size,g.canonical_title,g.member_count,g.provider_count,g.enriched_count,
COALESCE(ARRAY(SELECT DISTINCT m.provider_id FROM aiosources.aio_release_group_members m WHERE m.group_id=g.id ORDER BY m.provider_id),ARRAY[]::text[]),
g.propagated_audio,g.propagated_subtitles,g.updated_at` + from + where + `
ORDER BY g.enriched_count DESC,g.updated_at DESC,g.id DESC LIMIT ` + limitArg + ` OFFSET ` + offsetArg

	rows, err := tx.Query(ctx, listSQL, args...)
	if err != nil {
		return result, databaseError("list correspondences", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item model.ReleaseCorrespondenceGroup
		if err = rows.Scan(
			&item.ID, &item.Scope, &item.ScopeKey, &item.WorkID, &item.WorkType, &item.WorkTitle,
			&item.IMDb, &item.TMDB, &item.MatchRule, &item.MatchKey, &item.Confidence,
			&item.CanonicalSize, &item.CanonicalTitle, &item.MemberCount, &item.ProviderCount, &item.EnrichedCount,
			&item.Providers, &item.PropagatedAudio, &item.PropagatedSubtitles, &item.UpdatedAt,
		); err != nil {
			return result, databaseError("read correspondence group", err)
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return result, databaseError("list correspondences", err)
	}
	return result, databaseError("finish correspondence list", tx.Commit(ctx))
}

func (s *Store) ReleaseCorrespondenceDetail(ctx context.Context, groupID int64, locale string) (model.ReleaseCorrespondenceDetail, error) {
	var detail model.ReleaseCorrespondenceDetail
	if groupID < 1 {
		return detail, model.ErrNotFound
	}
	if locale == "" {
		locale = "en"
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return detail, databaseError("begin correspondence detail", err)
	}
	defer rollback(tx)

	workLabel := `CASE WHEN w.id IS NULL THEN '' ELSE ` + catalogTitleLabel("$2", "NULL") + ` END`
	err = tx.QueryRow(ctx, `SELECT g.id,g.scope,g.scope_key,g.work_id,COALESCE(w.media_type,''),`+workLabel+`,
COALESCE(w.imdb_id,''),COALESCE(w.tmdb_id,''),g.match_rule,g.match_key,g.confidence,
g.canonical_size,g.canonical_title,g.member_count,g.provider_count,g.enriched_count,
COALESCE(ARRAY(SELECT DISTINCT m.provider_id FROM aiosources.aio_release_group_members m WHERE m.group_id=g.id ORDER BY m.provider_id),ARRAY[]::text[]),
g.propagated_audio,g.propagated_subtitles,g.updated_at
FROM aiosources.aio_release_groups g
LEFT JOIN aiosources.aio_works w ON w.id=g.work_id
WHERE g.id=$1`, groupID, locale).Scan(
		&detail.Group.ID, &detail.Group.Scope, &detail.Group.ScopeKey, &detail.Group.WorkID,
		&detail.Group.WorkType, &detail.Group.WorkTitle, &detail.Group.IMDb, &detail.Group.TMDB,
		&detail.Group.MatchRule, &detail.Group.MatchKey, &detail.Group.Confidence,
		&detail.Group.CanonicalSize, &detail.Group.CanonicalTitle,
		&detail.Group.MemberCount, &detail.Group.ProviderCount, &detail.Group.EnrichedCount,
		&detail.Group.Providers, &detail.Group.PropagatedAudio, &detail.Group.PropagatedSubtitles,
		&detail.Group.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return detail, model.ErrNotFound
	}
	if err != nil {
		return detail, databaseError("read correspondence detail", err)
	}

	detail.Members = []model.ReleaseCorrespondenceMember{}
	rows, err := tx.Query(ctx, `SELECT m.group_id,t.occurrence_id,m.provider_id,m.source_id,m.fingerprint,
COALESCE(t.fields->>'title',''),COALESCE(t.search_info_hash,''),
CASE WHEN t.search_size>=0 AND t.search_size<=9223372036854775807 THEN t.search_size::bigint ELSE 0 END,
m.match_rule,m.own_audio,m.own_subtitles,m.contributed_audio,m.contributed_subtitles
FROM aiosources.aio_release_group_members m
JOIN aiosources.torrents t USING(provider_id,source_id)
WHERE m.group_id=$1
ORDER BY m.provider_id,m.source_id`, groupID)
	if err != nil {
		return detail, databaseError("read correspondence members", err)
	}
	for rows.Next() {
		var m model.ReleaseCorrespondenceMember
		if err = rows.Scan(
			&m.GroupID, &m.OccurrenceID, &m.ProviderID, &m.SourceID, &m.Fingerprint,
			&m.Title, &m.Hash, &m.Size, &m.MatchRule,
			&m.OwnAudio, &m.OwnSubtitles, &m.ContributedAudio, &m.ContributedSubtitles,
		); err != nil {
			rows.Close()
			return detail, databaseError("scan correspondence member", err)
		}
		detail.Members = append(detail.Members, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return detail, databaseError("read correspondence members", err)
	}

	detail.Exclusions = []model.ReleaseCorrespondenceExclusion{}
	exRows, err := tx.Query(ctx, `SELECT t.occurrence_id,e.provider_id,e.source_id,COALESCE(t.fields->>'title',''),e.match_key,e.reason,e.created_at
FROM aiosources.aio_release_correspondence_exclusions e
JOIN aiosources.torrents t USING(provider_id,source_id)
WHERE e.match_key=$1
ORDER BY e.created_at DESC,e.provider_id,e.source_id`, detail.Group.MatchKey)
	if err != nil {
		return detail, databaseError("read correspondence exclusions", err)
	}
	for exRows.Next() {
		var ex model.ReleaseCorrespondenceExclusion
		if err = exRows.Scan(&ex.OccurrenceID, &ex.ProviderID, &ex.SourceID, &ex.Title, &ex.MatchKey, &ex.Reason, &ex.CreatedAt); err != nil {
			exRows.Close()
			return detail, databaseError("scan correspondence exclusion", err)
		}
		detail.Exclusions = append(detail.Exclusions, ex)
	}
	err = exRows.Err()
	exRows.Close()
	if err != nil {
		return detail, databaseError("read correspondence exclusions", err)
	}
	return detail, databaseError("finish correspondence detail", tx.Commit(ctx))
}

func (s *Store) UnlinkReleaseCorrespondenceMember(ctx context.Context, groupID int64, occurrenceID string, build CorrespondenceBuilder) (int64, error) {
	if groupID < 1 || !ValidOccurrenceID(occurrenceID) || build == nil {
		return 0, model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, databaseError("begin unlink correspondence", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR SHARE`); err != nil {
		return 0, databaseError("lock unlink correspondence", err)
	}
	var scope, scopeKey, matchKey, providerID, sourceID, fingerprint string
	err = tx.QueryRow(ctx, `SELECT g.scope,g.scope_key,g.match_key,m.provider_id,m.source_id,t.fields_fingerprint
FROM aiosources.aio_release_groups g
JOIN aiosources.aio_release_group_members m ON m.group_id=g.id
JOIN aiosources.torrents t USING(provider_id,source_id)
WHERE g.id=$1 AND t.occurrence_id=$2 FOR UPDATE OF g,t`, groupID, occurrenceID).Scan(&scope, &scopeKey, &matchKey, &providerID, &sourceID, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, model.ErrNotFound
	}
	if err != nil {
		return 0, databaseError("load correspondence member for unlink", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_release_correspondence_exclusions(provider_id,source_id,match_key,fingerprint,reason)
VALUES($1,$2,$3,$4,'manually unlinked')
ON CONFLICT(provider_id,source_id,match_key) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,reason=EXCLUDED.reason,created_at=now()`, providerID, sourceID, matchKey, fingerprint); err != nil {
		return 0, databaseError("save correspondence exclusion", err)
	}
	if _, _, _, err = recomputeCorrespondenceScopeTx(ctx, tx, scope, scopeKey, build); err != nil {
		return 0, err
	}
	var nextGroupID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM aiosources.aio_release_groups WHERE match_key=$1`, matchKey).Scan(&nextGroupID); errors.Is(err, pgx.ErrNoRows) {
		nextGroupID = 0
	} else if err != nil {
		return 0, databaseError("read updated correspondence group", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO aiosources.activity_logs(event_id,level,kind,message,provider_id,service,data)
VALUES('correspondence-unlink-'||gen_random_uuid()::text,'info','metadata.correspondence_unlinked','Release unlinked from cross-source correspondence.',$2,'',jsonb_build_object('match_key',$3::text,'occurrence_id',$1::text))`, occurrenceID, providerID, matchKey); err != nil {
		return 0, databaseError("record correspondence unlink", err)
	}
	return nextGroupID, databaseError("commit correspondence unlink", tx.Commit(ctx))
}

func (s *Store) RestoreReleaseCorrespondenceExclusion(ctx context.Context, groupID int64, occurrenceID string, build CorrespondenceBuilder) (int64, error) {
	if groupID < 1 || !ValidOccurrenceID(occurrenceID) || build == nil {
		return 0, model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, databaseError("begin restore correspondence", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR SHARE`); err != nil {
		return 0, databaseError("lock restore correspondence", err)
	}
	var scope, scopeKey, matchKey, providerID, sourceID string
	err = tx.QueryRow(ctx, `SELECT g.scope,g.scope_key,g.match_key,e.provider_id,e.source_id
FROM aiosources.aio_release_groups g
JOIN aiosources.aio_release_correspondence_exclusions e ON e.match_key=g.match_key
JOIN aiosources.torrents t USING(provider_id,source_id)
WHERE g.id=$1 AND t.occurrence_id=$2 FOR UPDATE OF g,e`, groupID, occurrenceID).Scan(&scope, &scopeKey, &matchKey, &providerID, &sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, model.ErrNotFound
	}
	if err != nil {
		return 0, databaseError("load correspondence exclusion", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_release_correspondence_exclusions WHERE provider_id=$1 AND source_id=$2 AND match_key=$3`, providerID, sourceID, matchKey); err != nil {
		return 0, databaseError("delete correspondence exclusion", err)
	}
	if _, _, _, err = recomputeCorrespondenceScopeTx(ctx, tx, scope, scopeKey, build); err != nil {
		return 0, err
	}
	var nextGroupID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM aiosources.aio_release_groups WHERE match_key=$1`, matchKey).Scan(&nextGroupID); errors.Is(err, pgx.ErrNoRows) {
		nextGroupID = 0
	} else if err != nil {
		return 0, databaseError("read restored correspondence group", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO aiosources.activity_logs(event_id,level,kind,message,provider_id,service,data)
VALUES('correspondence-restore-'||gen_random_uuid()::text,'info','metadata.correspondence_restored','Release returned to automatic cross-source correspondence.',$2,'',jsonb_build_object('match_key',$3::text,'occurrence_id',$1::text))`, occurrenceID, providerID, matchKey); err != nil {
		return 0, databaseError("record correspondence restore", err)
	}
	return nextGroupID, databaseError("commit correspondence restore", tx.Commit(ctx))
}
