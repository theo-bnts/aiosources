package store

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *Store) GameStatus(ctx context.Context) (model.GameStatus, error) {
	var v model.GameStatus
	err := s.pool.QueryRow(ctx, `SELECT complete,works,processed,total,phase,after_id,generation,started_at,last_sync,changes_through,last_reconciliation,retry_at,error FROM aiosources.igdb_state WHERE singleton`).Scan(&v.Complete, &v.Works, &v.Processed, &v.Total, &v.Phase, &v.After, &v.Generation, &v.StartedAt, &v.LastSync, &v.ChangesThrough, &v.LastReconciliation, &v.RetryAt, &v.Error)
	v.Pending = v.StartedAt != nil
	return v, databaseError("read game information status", err)
}

// A fixed time boundary and an ID cursor avoid both offset drift and a lossy
// timestamp-only checkpoint when several hundred games change in one second.
func (s *Store) BeginGameUpdate(ctx context.Context, reconcileDays int) (model.GameStatus, error) {
	_, err := s.pool.Exec(ctx, `UPDATE aiosources.igdb_state SET
 phase=CASE WHEN NOT complete THEN 'initial' WHEN last_reconciliation IS NULL OR last_reconciliation<now()-make_interval(days=>$1) THEN 'reconciliation' ELSE 'changes' END,
 started_at=date_trunc('second',clock_timestamp()-interval '2 minutes'),after_id=0,processed=0,generation=generation+1,error='',retry_at=NULL WHERE singleton AND started_at IS NULL`, reconcileDays)
	if err != nil {
		return model.GameStatus{}, databaseError("begin game information update", err)
	}
	return s.GameStatus(ctx)
}

// Work publication, affected identifications and the provider cursor commit
// together. Restarting never acknowledges data which did not reach PostgreSQL.
func (s *Store) PublishGames(ctx context.Context, state model.GameStatus, works []model.AIOWork, after int64) error {
	if len(works) == 0 || len(works) > 100 || after <= state.After || state.StartedAt == nil {
		return model.ErrInvalid
	}
	ids := make([]string, len(works))
	last := state.After
	for i := range works {
		w := &works[i]
		id, e := strconv.ParseInt(w.IGDB, 10, 53)
		if e != nil || strconv.FormatInt(id, 10) != w.IGDB || id <= last || id > after || w.Type != "game" || len(w.Titles) == 0 || len(w.Titles) > 200 {
			return model.ErrInvalid
		}
		last = id
		ids[i] = w.IGDB
		if w.LocalizedTitles == nil {
			w.LocalizedTitles = map[string]string{"en": w.DisplayTitle}
		}
		if w.LocalizedPosters == nil {
			w.LocalizedPosters = map[string]string{}
		}
		if w.Years == nil {
			w.Years = []int{}
		}
	}
	if last != after {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT `+aioWorkColumns+` FROM aiosources.aio_works w WHERE media_type='game' AND igdb_id=ANY($1::text[])`, ids)
	if err != nil {
		return err
	}
	previous := map[string]model.AIOWork{}
	for rows.Next() {
		w, e := scanAIOWork(rows)
		if e != nil {
			rows.Close()
			return e
		}
		previous[w.IGDB] = w
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	changed := []string{}
	aliases := []string{}
	inserted := 0
	for _, w := range works {
		old, exists := previous[w.IGDB]
		if !exists {
			inserted++
		}
		if !exists || gameSignature(old) != gameSignature(w) {
			changed = append(changed, w.IGDB)
			for _, title := range append(append([]string{}, old.Titles...), w.Titles...) {
				if n := model.NormalizeTitle(title); n != "" {
					aliases = append(aliases, n)
				}
			}
		}
	}
	body, err := json.Marshal(works)
	if err != nil {
		return model.ErrInvalid
	}
	rows, err = tx.Query(ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,localized_titles,localized_posters,poster_path,years,game_data,igdb_generation)
 SELECT 'game',igdb,display_title,localized_titles,COALESCE(localized_posters,'{}'::jsonb),COALESCE(poster_path,''),years,COALESCE(game,'{}'::jsonb),$2
 FROM jsonb_to_recordset($1::jsonb) AS i(igdb text,display_title text,localized_titles jsonb,localized_posters jsonb,poster_path text,years int[],game jsonb) ORDER BY igdb
 ON CONFLICT(igdb_id) WHERE igdb_id<>'' DO UPDATE SET display_title=EXCLUDED.display_title,localized_titles=EXCLUDED.localized_titles,localized_posters=EXCLUDED.localized_posters,poster_path=EXCLUDED.poster_path,years=EXCLUDED.years,game_data=EXCLUDED.game_data,igdb_generation=EXCLUDED.igdb_generation,
 revision=aiosources.aio_works.revision+CASE WHEN EXCLUDED.igdb_id=ANY($3::text[]) THEN 1 ELSE 0 END,updated_at=now() RETURNING id,igdb_id`, body, state.Generation, changed)
	if err != nil {
		return databaseError("publish game information", err)
	}
	byID := map[string]int64{}
	workIDs := []int64{}
	for rows.Next() {
		var id int64
		var key string
		if err = rows.Scan(&id, &key); err != nil {
			rows.Close()
			return err
		}
		byID[key] = id
		workIDs = append(workIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_titles WHERE work_id IN(SELECT id FROM aiosources.aio_works WHERE media_type='game' AND igdb_id=ANY($1::text[]))`, changed); err != nil {
			return err
		}
		titles := []map[string]any{}
		for _, key := range changed {
			for _, w := range works {
				if w.IGDB == key {
					for _, title := range w.Titles {
						titles = append(titles, map[string]any{"work_id": byID[key], "title": title, "normalized": model.NormalizeTitle(title)})
					}
				}
			}
		}
		encoded, _ := json.Marshal(titles)
		if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_titles SELECT * FROM jsonb_populate_recordset(NULL::aiosources.aio_titles,$1::jsonb) ON CONFLICT DO NOTHING`, encoded); err != nil {
			return err
		}
		if err = queueGameAliases(ctx, tx, workIDs, ids, aliases); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE aiosources.igdb_state SET after_id=$1,processed=processed+$2,works=works+$3,error='',retry_at=NULL WHERE singleton AND generation=$4 AND after_id=$5 AND started_at=$6`, after, len(works), inserted, state.Generation, state.After, state.StartedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return databaseError("commit game information checkpoint", tx.Commit(ctx))
}

// Game identification accepts exact complete aliases only. Rechecking fuzzy
// video neighbours for every new game wastes work without creating a match.
func queueGameAliases(ctx context.Context, tx pgx.Tx, workIDs []int64, ids, aliases []string) error {
	_, err := tx.Exec(ctx, `INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
 SELECT provider_id,source_id FROM aiosources.aio_matches WHERE work_id=ANY($1::bigint[]) OR normalized=ANY($3::text[])
 UNION SELECT provider_id,source_id FROM aiosources.aio_match_reviews WHERE active AND media_type='game' AND igdb_id=ANY($2::text[])
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1`, workIDs, ids, aliases)
	return err
}

func gameSignature(w model.AIOWork) string {
	aliases := []string{}
	for _, t := range w.Titles {
		aliases = append(aliases, model.NormalizeTitle(t))
	}
	base := aioWorkSignature(w, aliases)
	game, _ := json.Marshal(w.Game)
	return base + string(game)
}

func (s *Store) CompleteGameUpdate(ctx context.Context, state model.GameStatus) error {
	if state.StartedAt == nil {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE`); err != nil {
		return err
	}
	if state.Phase == "initial" {
		if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_match_queue(provider_id,source_id) SELECT t.provider_id,t.source_id FROM aiosources.torrents t WHERE COALESCE(t.fields->>'media_type',t.fields->>'type','') IN ('game','games','videogame') OR t.fields ?| ARRAY['igdb','igdb_id'] OR t.fields->'external_ids' ?| ARRAY['igdb','igdb_id'] OR t.fields->'attributes' ?| ARRAY['igdb','igdbid','igdb_id'] OR COALESCE(t.fields->'categories',t.fields->'category','null')::text ~ '\m(1[0-9]{3}|4050)\M' OR COALESCE(t.fields->>'category_name','') ~* '\m(games?|jeux)\M' OR t.fields->>'title' ~* '\m(PS[1-5]|Nintendo[ ._-]?Switch|NSW|Win32|Win64|XBOX[ ._-]?(360|ONE))\M' OR t.fields->>'title' ~* '-(CODEX|RUNE|TENOKE|FLT|SKIDROW|RELOADED|GOG|DARKSIDERS|TINYISO|DINOBYTES|PLAZA|EMPRESS|ELAMIGOS|FITGIRL|DODI)\M' ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1`); err != nil {
			return err
		}
	}
	if state.Phase != "changes" {
		rows, err := tx.Query(ctx, `SELECT id FROM aiosources.aio_works WHERE media_type='game' AND igdb_generation<>$1`, state.Generation)
		if err != nil {
			return err
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			if err = queueAIOManyAliases(ctx, tx, ids, nil); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_works WHERE id=ANY($1::bigint[]);`, ids); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
				return err
			}
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE aiosources.igdb_state SET complete=TRUE,works=(SELECT count(*) FROM aiosources.aio_works WHERE media_type='game'),last_sync=now(),changes_through=started_at,
 last_reconciliation=CASE WHEN phase<>'changes' THEN now() ELSE last_reconciliation END,started_at=NULL,after_id=0,error='',retry_at=NULL
 WHERE singleton AND generation=$1 AND after_id=$2 AND started_at=$3`, state.Generation, state.After, state.StartedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return databaseError("finish game information update", tx.Commit(ctx))
}

func (s *Store) GameUpdateFailure(ctx context.Context, message string, retry time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE aiosources.igdb_state SET error=$1,retry_at=GREATEST(retry_at,$2) WHERE singleton`, message, retry)
	return databaseError("retain game information retry", err)
}
func (s *Store) SetGameTotal(ctx context.Context, total int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE aiosources.igdb_state SET total=$1 WHERE singleton`, total)
	return err
}
func (s *Store) ResetGameInformation(ctx context.Context) error {
	return s.resetInformation(ctx, "games")
}

func matchedMediaType(r model.AIORecord, w *model.AIOWork) string {
	if w != nil {
		return w.Type
	}
	return r.IdentificationType
}
