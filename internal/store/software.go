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
	"github.com/theo-bnts/aiosources/internal/software"
)

// SoftwareSyncMeta records checkpoints updated when a Software catalog sync
// finishes cleanly.
type SoftwareSyncMeta struct {
	NVDSnapshot       string
	NVDSnapshotSHA256 string
	NVDSnapshotAt     *time.Time
	NVDChangesThrough *time.Time
	WinGetETag        string
	WinGetSyncedAt    *time.Time
	HomebrewETag      string
	HomebrewSyncedAt  *time.Time
	IconsETag         string
	IconsSyncedAt     *time.Time
	FullImport        bool
}

func SoftwareSelection(c model.AIOConfig) software.ImportSelection {
	platforms, ok := software.ImportPlatforms(c.SoftwarePlatforms)
	if !ok {
		platforms = slices.Clone(software.DefaultPlatforms)
	}
	return software.ImportSelection{
		IncludeNVD:     c.SoftwareIncludeNVD,
		ExcludePlugins: c.SoftwareExcludePlugins,
		IncludeOS:      c.SoftwareIncludeOS,
		IncludeIcons:   c.SoftwareIncludeIcons,
		Platforms:      platforms,
	}
}

func SoftwareSelectionKey(c model.AIOConfig) string {
	return SoftwareSelection(c).Key()
}

func (s *Store) SoftwareStatus(ctx context.Context) (model.SoftwareStatus, error) {
	var v model.SoftwareStatus
	err := s.pool.QueryRow(ctx, `SELECT complete,works,nvd_entries,winget_entries,homebrew_entries,icons,icons_indexed,
 imported,position,filtered,skipped,phase,selection,generation,
 nvd_snapshot,nvd_snapshot_sha256,nvd_snapshot_at,nvd_changes_through,
 winget_etag,winget_synced_at,homebrew_etag,homebrew_synced_at,icons_etag,icons_synced_at,
 last_import,last_sync,needs_full_import,recovery_reason,retry_at,error
 FROM aiosources.sw_sync_state WHERE singleton`).Scan(
		&v.Complete, &v.Works, &v.NVDEntries, &v.WinGetEntries, &v.HomebrewEntries, &v.Icons, &v.IconsIndexed,
		&v.Imported, &v.Position, &v.Filtered, &v.Skipped, &v.Phase, &v.Selection, &v.Generation,
		&v.NVDSnapshot, &v.NVDSnapshotSHA256, &v.NVDSnapshotAt, &v.NVDChangesThrough,
		&v.WinGetETag, &v.WinGetSyncedAt, &v.HomebrewETag, &v.HomebrewSyncedAt, &v.IconsETag, &v.IconsSyncedAt,
		&v.LastImport, &v.LastSync, &v.NeedsFullImport, &v.RecoveryReason, &v.RetryAt, &v.Error,
	)
	return v, databaseError("read software information status", err)
}

func (s *Store) NextSoftwareUpdate(ctx context.Context, minutes int) (time.Time, error) {
	if minutes < 15 || minutes > 1440 {
		return time.Time{}, model.ErrInvalid
	}
	var next time.Time
	err := s.pool.QueryRow(ctx, `SELECT GREATEST(COALESCE(retry_at,to_timestamp(0)),
 CASE WHEN NOT complete THEN now()
      WHEN needs_full_import THEN COALESCE(checked_at+interval '6 hours',now())
      ELSE COALESCE(GREATEST(last_sync,checked_at)+make_interval(mins=>$1),now())
 END) FROM aiosources.sw_sync_state WHERE singleton`, minutes).Scan(&next)
	return next, databaseError("read software update schedule", err)
}

func (s *Store) SoftwareDue(ctx context.Context, minutes int) (bool, error) {
	next, err := s.NextSoftwareUpdate(ctx, minutes)
	if err != nil {
		return false, err
	}
	return !next.After(time.Now()), nil
}

func prepareSoftwareImport(ctx context.Context, tx pgx.Tx, c model.AIOConfig) error {
	key := SoftwareSelectionKey(c)
	_, err := tx.Exec(ctx, `UPDATE aiosources.sw_sync_state SET selection=$1,
 complete=CASE WHEN selection<>'' AND selection<>$1 THEN FALSE ELSE complete END,
 phase=CASE WHEN selection<>'' AND selection<>$1 THEN 'idle' ELSE phase END,
 retry_at=NULL,error='' WHERE singleton`, key)
	return databaseError("prepare software catalog filters", err)
}

func (s *Store) PrepareSoftwareImport(ctx context.Context, c model.AIOConfig) error {
	key := SoftwareSelectionKey(c)
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.sw_sync_state SET selection=$1,
 complete=CASE WHEN selection<>'' AND selection<>$1 THEN FALSE ELSE complete END WHERE singleton`, key)
	return databaseError("prepare software catalog filters", err)
}

func (s *Store) TouchSoftwareCheck(ctx context.Context, meta SoftwareSyncMeta) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.sw_sync_state SET
 phase='idle',
 nvd_snapshot=CASE WHEN $1<>'' THEN $1 ELSE nvd_snapshot END,
 nvd_snapshot_sha256=CASE WHEN $2<>'' THEN $2 ELSE nvd_snapshot_sha256 END,
 nvd_snapshot_at=COALESCE($3,nvd_snapshot_at),
 nvd_changes_through=COALESCE($4,nvd_changes_through),
 recovery_reason='',
 checked_at=now(),
 error='',
 retry_at=NULL
 WHERE singleton`,
		meta.NVDSnapshot, meta.NVDSnapshotSHA256, meta.NVDSnapshotAt, meta.NVDChangesThrough)
	return databaseError("record software check time", err)
}

func (s *Store) BeginSoftwareSync(ctx context.Context, selectionKey, recoveryReason string) (int64, error) {
	var gen int64
	phase := "nvd_meta"
	if recoveryReason != "" {
		phase = "recovery"
	}
	err := s.workerPool().QueryRow(ctx, `UPDATE aiosources.sw_sync_state SET
 generation=generation+1,phase=$1,selection=$2,
 recovery_reason=$3,error='',retry_at=NULL,checked_at=now()
 WHERE singleton RETURNING generation`, phase, selectionKey, recoveryReason).Scan(&gen)
	return gen, databaseError("begin software catalog sync", err)
}

func (s *Store) ResetSoftwareProgress(ctx context.Context) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.sw_sync_state SET imported=0,position=0,skipped=0 WHERE singleton`)
	return databaseError("reset software sync progress", err)
}

func (s *Store) SetSoftwarePhase(ctx context.Context, phase string) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.sw_sync_state SET phase=$1 WHERE singleton`, phase)
	return databaseError("update software sync phase", err)
}

// SaveSoftwareSourceBatch upserts normalized entries from NVD, WinGet or
// Homebrew Cask into sw_source_entries. When incremental is true (or within the
// same generation), array fields merge with prior entries instead of overwriting.
func (s *Store) SaveSoftwareSourceBatch(ctx context.Context, generation int64, entries []software.SourceEntry, readCount, skippedCount int64, incremental bool) error {
	tx, err := s.workerPool().Begin(ctx)
	if err != nil {
		return databaseError("begin software source batch", err)
	}
	defer rollback(tx)

	if len(entries) > 0 {
		batch := &pgx.Batch{}
		for _, e := range entries {
			part := e.Part
			if part != "o" {
				part = "a"
			}
			platforms := nonNilStrings(e.Platforms)
			tags := nonNilStrings(e.Tags)
			languages := nonNilStrings(e.Languages)
			aliases := nonNilStrings(e.Aliases)
			batch.Queue(`INSERT INTO aiosources.sw_source_entries(
 source,external_id,part,name,vendor,vendor_norm,product_norm,description,license,latest_version,homepage_key,
 platforms,tags,languages,aliases,is_plugin,last_modified_at,sw_seen
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT(source,external_id) DO UPDATE SET
 part=CASE WHEN EXCLUDED.part='a' THEN 'a' ELSE aiosources.sw_source_entries.part END,
 name=CASE WHEN EXCLUDED.name<>'' THEN EXCLUDED.name ELSE aiosources.sw_source_entries.name END,
 vendor=CASE WHEN EXCLUDED.vendor<>'' THEN EXCLUDED.vendor ELSE aiosources.sw_source_entries.vendor END,
 vendor_norm=CASE WHEN EXCLUDED.vendor_norm<>'' THEN EXCLUDED.vendor_norm ELSE aiosources.sw_source_entries.vendor_norm END,
 product_norm=CASE WHEN EXCLUDED.product_norm<>'' THEN EXCLUDED.product_norm ELSE aiosources.sw_source_entries.product_norm END,
 description=CASE WHEN EXCLUDED.description<>'' THEN EXCLUDED.description ELSE aiosources.sw_source_entries.description END,
 license=CASE WHEN EXCLUDED.license<>'' THEN EXCLUDED.license ELSE aiosources.sw_source_entries.license END,
 latest_version=CASE WHEN EXCLUDED.latest_version<>'' THEN EXCLUDED.latest_version ELSE aiosources.sw_source_entries.latest_version END,
 homepage_key=CASE WHEN EXCLUDED.homepage_key<>'' THEN EXCLUDED.homepage_key ELSE aiosources.sw_source_entries.homepage_key END,
 platforms=CASE WHEN $19::boolean OR aiosources.sw_source_entries.sw_seen=EXCLUDED.sw_seen
  THEN ARRAY(SELECT DISTINCT x FROM unnest(aiosources.sw_source_entries.platforms || EXCLUDED.platforms) AS x ORDER BY x)
  ELSE EXCLUDED.platforms END,
 tags=CASE WHEN $19::boolean OR aiosources.sw_source_entries.sw_seen=EXCLUDED.sw_seen
  THEN ARRAY(SELECT DISTINCT x FROM unnest(aiosources.sw_source_entries.tags || EXCLUDED.tags) AS x ORDER BY x)
  ELSE EXCLUDED.tags END,
 languages=CASE WHEN $19::boolean OR aiosources.sw_source_entries.sw_seen=EXCLUDED.sw_seen
  THEN ARRAY(SELECT DISTINCT x FROM unnest(aiosources.sw_source_entries.languages || EXCLUDED.languages) AS x ORDER BY x)
  ELSE EXCLUDED.languages END,
 aliases=CASE WHEN $19::boolean OR aiosources.sw_source_entries.sw_seen=EXCLUDED.sw_seen
  THEN ARRAY(SELECT DISTINCT x FROM unnest(aiosources.sw_source_entries.aliases || EXCLUDED.aliases) AS x ORDER BY x)
  ELSE EXCLUDED.aliases END,
 is_plugin=CASE WHEN $19::boolean OR aiosources.sw_source_entries.sw_seen=EXCLUDED.sw_seen
  THEN (aiosources.sw_source_entries.is_plugin AND EXCLUDED.is_plugin)
  ELSE EXCLUDED.is_plugin END,
 last_modified_at=COALESCE(GREATEST(aiosources.sw_source_entries.last_modified_at,EXCLUDED.last_modified_at),EXCLUDED.last_modified_at,aiosources.sw_source_entries.last_modified_at),
 sw_seen=EXCLUDED.sw_seen`,
				e.Source, e.ExternalID, part, e.Name, e.Vendor, e.VendorNorm, e.ProductNorm, e.Description, e.License, e.LatestVersion, e.HomepageKey,
				platforms, tags, languages, aliases, e.IsPlugin, e.LastModifiedAt, generation, incremental)
		}
		results := tx.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err = results.Exec(); err != nil {
				_ = results.Close()
				return databaseError("save software source batch", err)
			}
		}
		if err = results.Close(); err != nil {
			return databaseError("close software source batch", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE aiosources.sw_sync_state SET position=position+$1,imported=imported+$2,skipped=skipped+$3 WHERE singleton`,
		readCount, int64(len(entries)), skippedCount); err != nil {
		return databaseError("advance software sync progress", err)
	}
	return databaseError("commit software source batch", tx.Commit(ctx))
}

// PruneSoftwareSourceEntries deletes stale rows for one fully refreshed source
// without touching untouched sources or live searchable aio_works.
func (s *Store) PruneSoftwareSourceEntries(ctx context.Context, source string, generation int64) error {
	_, err := s.workerPool().Exec(ctx, `DELETE FROM aiosources.sw_source_entries WHERE source=$1 AND sw_seen<$2`, source, generation)
	return databaseError("prune software source entries", err)
}

// SaveSoftwareIcons replaces the lightweight selfh.st/icons metadata index
// atomically (zero remote image bytes are ever downloaded or stored).
func (s *Store) SaveSoftwareIcons(ctx context.Context, generation int64, icons []software.IconEntry, skipped int64) error {
	tx, err := s.workerPool().Begin(ctx)
	if err != nil {
		return databaseError("begin software icons save", err)
	}
	defer rollback(tx)

	if len(icons) > 0 {
		batch := &pgx.Batch{}
		for _, ic := range icons {
			batch.Queue(`INSERT INTO aiosources.sw_icons(reference,name,normalized,sw_seen) VALUES($1,$2,$3,$4)
 ON CONFLICT(reference) DO UPDATE SET name=EXCLUDED.name,normalized=EXCLUDED.normalized,sw_seen=EXCLUDED.sw_seen`,
				ic.Reference, ic.Name, ic.Normalized, generation)
		}
		results := tx.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err = results.Exec(); err != nil {
				_ = results.Close()
				return databaseError("save software icons", err)
			}
		}
		if err = results.Close(); err != nil {
			return databaseError("close software icons batch", err)
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.sw_icons WHERE sw_seen<$1`, generation); err != nil {
		return databaseError("prune stale software icons", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE aiosources.sw_sync_state SET icons_indexed=(SELECT count(*) FROM aiosources.sw_icons),skipped=skipped+$1 WHERE singleton`, skipped); err != nil {
		return databaseError("update software icons count", err)
	}
	return databaseError("commit software icons", tx.Commit(ctx))
}

// ClearSoftwareIcons drops the remote icon metadata index when disabled.
func (s *Store) ClearSoftwareIcons(ctx context.Context) error {
	tx, err := s.workerPool().Begin(ctx)
	if err != nil {
		return databaseError("begin clear software icons", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `TRUNCATE aiosources.sw_icons`); err != nil {
		return databaseError("clear software icons", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE aiosources.sw_sync_state SET icons_indexed=0,icons_etag='',icons_synced_at=NULL WHERE singleton`); err != nil {
		return databaseError("reset software icon state", err)
	}
	return databaseError("commit clear software icons", tx.Commit(ctx))
}

// LoadSoftwareStaging reads staged source entries and optional selfh.st icons
// for deterministic merging and projection.
func (s *Store) LoadSoftwareStaging(ctx context.Context, includeNVD, includeIcons bool) ([]software.SourceEntry, []software.IconEntry, error) {
	query := `SELECT source,external_id,part,name,vendor,vendor_norm,product_norm,description,license,latest_version,homepage_key,platforms,tags,languages,aliases,is_plugin,last_modified_at
 FROM aiosources.sw_source_entries`
	if !includeNVD {
		query += ` WHERE source<>'nvd'`
	}
	rows, err := s.workerPool().Query(ctx, query)
	if err != nil {
		return nil, nil, databaseError("load software staging entries", err)
	}
	entries := []software.SourceEntry{}
	for rows.Next() {
		var e software.SourceEntry
		if err = rows.Scan(
			&e.Source, &e.ExternalID, &e.Part, &e.Name, &e.Vendor, &e.VendorNorm, &e.ProductNorm,
			&e.Description, &e.License, &e.LatestVersion, &e.HomepageKey,
			&e.Platforms, &e.Tags, &e.Languages, &e.Aliases, &e.IsPlugin, &e.LastModifiedAt,
		); err != nil {
			rows.Close()
			return nil, nil, databaseError("scan software staging entry", err)
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, databaseError("read software staging entries", err)
	}

	var icons []software.IconEntry
	if includeIcons {
		iconRows, err := s.workerPool().Query(ctx, `SELECT reference,name,normalized FROM aiosources.sw_icons ORDER BY reference`)
		if err != nil {
			return nil, nil, databaseError("load software icons", err)
		}
		for iconRows.Next() {
			var ic software.IconEntry
			if err = iconRows.Scan(&ic.Reference, &ic.Name, &ic.Normalized); err != nil {
				iconRows.Close()
				return nil, nil, databaseError("scan software icon", err)
			}
			icons = append(icons, ic)
		}
		err = iconRows.Err()
		iconRows.Close()
		if err != nil {
			return nil, nil, databaseError("read software icons", err)
		}
	}
	return entries, icons, nil
}

// ProjectSoftwareCatalog writes merged Software catalog entries into sw_catalog,
// sw_aliases, sw_external_ids, aio_works and aio_titles in bounded chunks while
// keeping existing searchable works intact until FinishSoftwareSync swaps/prunes.
func (s *Store) ProjectSoftwareCatalog(ctx context.Context, generation int64, items []software.MergedSoftware, filtered int64, onProgress func(int64)) error {
	const chunkSize = 500
	for start := 0; start < len(items); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := items[start:min(start+chunkSize, len(items))]
		if err := s.projectSoftwareChunk(ctx, generation, chunk); err != nil {
			return err
		}
		if onProgress != nil {
			onProgress(int64(len(chunk)))
		}
	}
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.sw_sync_state SET filtered=$1 WHERE singleton`, filtered)
	return databaseError("update software filtered count", err)
}

func (s *Store) projectSoftwareChunk(ctx context.Context, generation int64, chunk []software.MergedSoftware) error {
	tx, err := s.workerPool().Begin(ctx)
	if err != nil {
		return databaseError("begin software projection chunk", err)
	}
	defer rollback(tx)

	keys := make([]string, len(chunk))
	for i, item := range chunk {
		keys[i] = item.SoftwareKey
	}

	// Upsert dedicated sw_catalog, sw_aliases and sw_external_ids tables before
	// acquiring the aio_state metadata fence so catalog writes stay brief.
	catBatch := &pgx.Batch{}
	for _, item := range chunk {
		catBatch.Queue(`INSERT INTO aiosources.sw_catalog(
 software_key,name,normalized,vendor,vendor_norm,part,description,license,latest_version,
 platforms,tags,languages,nvd_cpe,winget_id,homebrew_cask,icon_ref,sw_generation
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT(software_key) DO UPDATE SET
 name=EXCLUDED.name,normalized=EXCLUDED.normalized,vendor=EXCLUDED.vendor,vendor_norm=EXCLUDED.vendor_norm,
 part=EXCLUDED.part,description=EXCLUDED.description,license=EXCLUDED.license,latest_version=EXCLUDED.latest_version,
 platforms=EXCLUDED.platforms,tags=EXCLUDED.tags,languages=EXCLUDED.languages,
 nvd_cpe=EXCLUDED.nvd_cpe,winget_id=EXCLUDED.winget_id,homebrew_cask=EXCLUDED.homebrew_cask,
 icon_ref=EXCLUDED.icon_ref,sw_generation=EXCLUDED.sw_generation`,
			item.SoftwareKey, item.Name, item.Normalized, item.Info.Vendor, software.VendorNorm(item.Info.Vendor),
			item.Part, item.Info.Description, item.Info.License, item.Info.LatestVersion,
			nonNilStrings(item.Info.Platforms), nonNilStrings(item.Info.Tags), nonNilStrings(item.Info.Languages),
			item.Info.NVDCPE, item.Info.WinGetID, item.Info.HomebrewCask, item.Info.IconRef, generation)
	}
	catRes := tx.SendBatch(ctx, catBatch)
	for i := 0; i < catBatch.Len(); i++ {
		if _, err = catRes.Exec(); err != nil {
			_ = catRes.Close()
			return databaseError("upsert sw_catalog", err)
		}
	}
	if err = catRes.Close(); err != nil {
		return databaseError("close sw_catalog batch", err)
	}

	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.sw_aliases WHERE software_key=ANY($1::text[])`, keys); err != nil {
		return databaseError("clear sw_aliases", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.sw_external_ids WHERE software_key=ANY($1::text[])`, keys); err != nil {
		return databaseError("clear sw_external_ids", err)
	}
	subBatch := &pgx.Batch{}
	for _, item := range chunk {
		for _, alias := range item.Aliases {
			norm := model.NormalizeTitle(alias)
			if norm == "" {
				continue
			}
			subBatch.Queue(`INSERT INTO aiosources.sw_aliases(software_key,alias,normalized) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
				item.SoftwareKey, alias, norm)
		}
		for _, ext := range item.ExternalIDs {
			subBatch.Queue(`INSERT INTO aiosources.sw_external_ids(kind,external_id,external_lower,software_key) VALUES($1,$2,$3,$4)
 ON CONFLICT(kind,external_lower) DO UPDATE SET external_id=EXCLUDED.external_id,software_key=EXCLUDED.software_key`,
				ext.Kind, ext.Value, strings.ToLower(ext.Value), item.SoftwareKey)
		}
	}
	if subBatch.Len() > 0 {
		subRes := tx.SendBatch(ctx, subBatch)
		for i := 0; i < subBatch.Len(); i++ {
			if _, err = subRes.Exec(); err != nil {
				_ = subRes.Close()
				return databaseError("upsert software aliases/external ids", err)
			}
		}
		if err = subRes.Close(); err != nil {
			return databaseError("close software sub batch", err)
		}
	}

	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE`); err != nil {
		return err
	}

	// Read previous aio_works for signature diffing so unchanged rows keep their revision.
	prevRows, err := tx.Query(ctx, `SELECT `+aioWorkColumns+` FROM aiosources.aio_works w WHERE media_type='software' AND software_key<>'' AND software_key=ANY($1::text[])`, keys)
	if err != nil {
		return err
	}
	previous := make(map[string]model.AIOWork, len(chunk))
	for prevRows.Next() {
		w, e := scanAIOWork(prevRows)
		if e != nil {
			prevRows.Close()
			return e
		}
		previous[w.SoftwareKey] = w
	}
	err = prevRows.Err()
	prevRows.Close()
	if err != nil {
		return err
	}

	works := make([]model.AIOWork, len(chunk))
	changed := []string{}
	changedAliases := []string{}
	for i, item := range chunk {
		titles := item.Aliases
		if len(titles) == 0 {
			titles = []string{item.Name}
		}
		w := model.AIOWork{
			Type:             "software",
			SoftwareKey:      item.SoftwareKey,
			DisplayTitle:     item.Name,
			LocalizedTitles:  map[string]string{"en": item.Name},
			LocalizedPosters: map[string]string{},
			PosterPath:       item.Info.IconRef,
			Years:            []int{},
			Titles:           titles,
			Software:         item.Info,
		}
		works[i] = w
		old, exists := previous[w.SoftwareKey]
		if !exists || softwareSignature(old) != softwareSignature(w) {
			changed = append(changed, w.SoftwareKey)
			for _, t := range append(append([]string{}, old.Titles...), w.Titles...) {
				if n := model.NormalizeTitle(t); n != "" {
					changedAliases = append(changedAliases, n)
				}
			}
		}
	}

	type workRow struct {
		SoftwareKey     string                    `json:"software_key"`
		DisplayTitle    string                    `json:"display_title"`
		Normalized      string                    `json:"normalized"`
		LocalizedTitles map[string]string         `json:"localized_titles"`
		PosterPath      string                    `json:"poster_path"`
		Software        model.SoftwareInformation `json:"software"`
	}
	payload := make([]workRow, len(works))
	for i, w := range works {
		payload[i] = workRow{
			SoftwareKey:     w.SoftwareKey,
			DisplayTitle:    w.DisplayTitle,
			Normalized:      model.NormalizeTitle(w.DisplayTitle),
			LocalizedTitles: w.LocalizedTitles,
			PosterPath:      w.PosterPath,
			Software:        w.Software,
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	retRows, err := tx.Query(ctx, `INSERT INTO aiosources.aio_works(media_type,software_key,display_title,software_normalized,localized_titles,localized_posters,poster_path,years,software_data,sw_generation)
 SELECT 'software',software_key,display_title,normalized,localized_titles,'{}'::jsonb,COALESCE(poster_path,''),'{}'::int[],COALESCE(software,'{}'::jsonb),$2
 FROM jsonb_to_recordset($1::jsonb) AS i(software_key text,display_title text,normalized text,localized_titles jsonb,poster_path text,software jsonb) ORDER BY software_key
 ON CONFLICT(software_key) WHERE software_key<>'' DO UPDATE SET
  display_title=EXCLUDED.display_title,software_normalized=EXCLUDED.software_normalized,localized_titles=EXCLUDED.localized_titles,
  poster_path=EXCLUDED.poster_path,software_data=EXCLUDED.software_data,sw_generation=EXCLUDED.sw_generation,
  revision=aiosources.aio_works.revision+CASE WHEN EXCLUDED.software_key=ANY($3::text[]) THEN 1 ELSE 0 END,updated_at=now()
 RETURNING id,software_key`, body, generation, changed)
	if err != nil {
		return databaseError("publish software works", err)
	}
	byKey := make(map[string]int64, len(works))
	for retRows.Next() {
		var id int64
		var key string
		if err = retRows.Scan(&id, &key); err != nil {
			retRows.Close()
			return err
		}
		byKey[key] = id
	}
	err = retRows.Err()
	retRows.Close()
	if err != nil {
		return err
	}

	if len(changed) > 0 {
		changedIDs := make([]int64, 0, len(changed))
		changedSet := make(map[string]struct{}, len(changed))
		for _, k := range changed {
			changedSet[k] = struct{}{}
			changedIDs = append(changedIDs, byKey[k])
		}
		if _, err = tx.Exec(ctx, `DELETE FROM aiosources.aio_titles WHERE work_id=ANY($1::bigint[])`, changedIDs); err != nil {
			return err
		}
		titleRows := []map[string]any{}
		for _, w := range works {
			if _, ok := changedSet[w.SoftwareKey]; !ok {
				continue
			}
			wid := byKey[w.SoftwareKey]
			seen := map[string]bool{}
			for _, t := range w.Titles {
				n := model.NormalizeTitle(t)
				if n == "" || seen[n] {
					continue
				}
				seen[n] = true
				titleRows = append(titleRows, map[string]any{"work_id": wid, "title": t, "normalized": n})
			}
		}
		if len(titleRows) > 0 {
			encoded, _ := json.Marshal(titleRows)
			if _, err = tx.Exec(ctx, `INSERT INTO aiosources.aio_titles SELECT * FROM jsonb_populate_recordset(NULL::aiosources.aio_titles,$1::jsonb) ON CONFLICT DO NOTHING`, encoded); err != nil {
				return err
			}
		}
		if err = queueSoftwareAliases(ctx, tx, changedIDs, changed, changedAliases); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
			return err
		}
	}
	return databaseError("commit software projection chunk", tx.Commit(ctx))
}

func queueSoftwareAliases(ctx context.Context, tx pgx.Tx, workIDs []int64, keys, aliases []string) error {
	_, err := tx.Exec(ctx, `INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
 SELECT provider_id,source_id FROM aiosources.aio_matches WHERE work_id=ANY($1::bigint[]) OR normalized=ANY($3::text[])
 UNION SELECT provider_id,source_id FROM aiosources.aio_match_reviews WHERE active AND media_type='software' AND software_key=ANY($2::text[])
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1`, workIDs, keys, aliases)
	return err
}

func softwareSignature(w model.AIOWork) string {
	aliases := make([]string, 0, len(w.Titles))
	for _, t := range w.Titles {
		aliases = append(aliases, model.NormalizeTitle(t))
	}
	base := aioWorkSignature(w, aliases)
	data, _ := json.Marshal(w.Software)
	return base + "|" + w.PosterPath + "|" + string(data)
}

// FinishSoftwareSync swaps/prunes stale software works and catalog rows after
// projection finishes, queues existing software torrents once, and publishes
// fresh source checkpoints and counters without holding aio_state FOR UPDATE
// across full scans or counts.
func (s *Store) FinishSoftwareSync(ctx context.Context, generation int64, meta SoftwareSyncMeta) error {
	pool := s.workerPool()
	const staleBatch = 1000
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		staleRows, err := pool.Query(ctx, `SELECT id FROM aiosources.aio_works w
 WHERE w.media_type='software' AND w.software_key<>'' AND w.sw_generation<>$1
  AND NOT EXISTS(SELECT 1 FROM aiosources.aio_match_reviews rev WHERE rev.active AND rev.media_type='software' AND rev.software_key=w.software_key)
 ORDER BY w.id LIMIT $2`, generation, staleBatch)
		if err != nil {
			return databaseError("scan stale software works", err)
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
			return databaseError("begin software cleanup", err)
		}
		if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE`); err != nil {
			rollback(tx)
			return err
		}
		confirmRows, err := tx.Query(ctx, `SELECT id FROM aiosources.aio_works w
 WHERE w.id=ANY($1::bigint[]) AND w.media_type='software' AND w.software_key<>'' AND w.sw_generation<>$2
  AND NOT EXISTS(SELECT 1 FROM aiosources.aio_match_reviews rev WHERE rev.active AND rev.media_type='software' AND rev.software_key=w.software_key)`, staleIDs, generation)
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
			return databaseError("commit software cleanup", err)
		}
		if len(confirmed) == 0 || len(staleIDs) < staleBatch {
			break
		}
	}

	if _, err := pool.Exec(ctx, `DELETE FROM aiosources.sw_catalog c
 WHERE c.sw_generation<>$1
  AND NOT EXISTS(SELECT 1 FROM aiosources.aio_match_reviews rev WHERE rev.active AND rev.media_type='software' AND rev.software_key=c.software_key)`, generation); err != nil {
		return databaseError("prune stale sw_catalog", err)
	}

	var wasComplete bool
	if err := pool.QueryRow(ctx, `SELECT complete FROM aiosources.sw_sync_state WHERE singleton`).Scan(&wasComplete); err != nil {
		return databaseError("read software completion state", err)
	}
	backfill, err := pool.Exec(ctx, `INSERT INTO aiosources.settings(key,value) VALUES('software.matching.v1','1') ON CONFLICT DO NOTHING`)
	if err != nil {
		return databaseError("record software matching backfill", err)
	}
	if !wasComplete || backfill.RowsAffected() == 1 {
		if _, err = pool.Exec(ctx, `INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
 SELECT t.provider_id,t.source_id FROM aiosources.torrents t
 WHERE NOT (COALESCE(t.fields->'categories',t.fields->'category','null')::text ~ '\m4050\M')
  AND (
   lower(COALESCE(t.fields->>'media_type',t.fields->>'type','')) IN ('software','app','application','apps','0day','iso','mac','macos','mobile','ios','android','apk')
   OR t.fields ?| ARRAY['software_key','winget_id','winget','homebrew_cask','brew_cask','nvd_cpe','cpe']
   OR COALESCE(t.fields->'categories',t.fields->'category','null')::text ~ '\m(4000|4010|4020|4030|4040|4060|4070)\M'
   OR concat_ws(' ',t.fields->>'category_name',t.fields->>'subcategory_name') ~* '(software|logiciel|application|0day|macos|android|apk)'
   OR t.fields->>'title' ~* '\m(Win64|Win32|macOS|OSX|AppImage|DMG|APK|IPA|VST3?|AAX|Multilingual|Portable)\M'
  )
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1`); err != nil {
			return databaseError("queue software torrents for matching", err)
		}
	}

	_, err = pool.Exec(ctx, `UPDATE aiosources.sw_sync_state SET
 complete=TRUE,
 phase='idle',
 works=(SELECT count(*) FROM aiosources.aio_works WHERE media_type='software' AND software_key<>''),
 nvd_entries=(SELECT count(*) FROM aiosources.sw_source_entries WHERE source='nvd'),
 winget_entries=(SELECT count(*) FROM aiosources.sw_source_entries WHERE source='winget'),
 homebrew_entries=(SELECT count(*) FROM aiosources.sw_source_entries WHERE source='homebrew'),
 icons=(SELECT count(*) FROM aiosources.aio_works WHERE media_type='software' AND software_key<>'' AND poster_path<>''),
 icons_indexed=(SELECT count(*) FROM aiosources.sw_icons),
 nvd_snapshot=CASE WHEN $1<>'' THEN $1 ELSE nvd_snapshot END,
 nvd_snapshot_sha256=CASE WHEN $2<>'' THEN $2 ELSE nvd_snapshot_sha256 END,
 nvd_snapshot_at=COALESCE($3,nvd_snapshot_at),
 nvd_changes_through=COALESCE($4,nvd_changes_through),
 winget_etag=CASE WHEN $5<>'' THEN $5 ELSE winget_etag END,
 winget_synced_at=COALESCE($6,winget_synced_at),
 homebrew_etag=CASE WHEN $7<>'' THEN $7 ELSE homebrew_etag END,
 homebrew_synced_at=COALESCE($8,homebrew_synced_at),
 icons_etag=CASE WHEN $9<>'' THEN $9 ELSE icons_etag END,
 icons_synced_at=COALESCE($10,icons_synced_at),
 last_import=CASE WHEN $11::boolean OR last_import IS NULL THEN now() ELSE last_import END,
 last_sync=now(),
 needs_full_import=FALSE,
 recovery_reason='',
 retry_at=NULL,
 checked_at=now(),
 error=''
 WHERE singleton`,
		meta.NVDSnapshot, meta.NVDSnapshotSHA256, meta.NVDSnapshotAt, meta.NVDChangesThrough,
		meta.WinGetETag, meta.WinGetSyncedAt, meta.HomebrewETag, meta.HomebrewSyncedAt,
		meta.IconsETag, meta.IconsSyncedAt, meta.FullImport)
	return databaseError("complete software catalog sync", err)
}

func (s *Store) MarkSoftwareNeedsFullImport(ctx context.Context, reason, message string, retryAt time.Time) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.sw_sync_state SET phase='idle',needs_full_import=TRUE,recovery_reason=$1,error=$2,retry_at=$3,checked_at=now() WHERE singleton`,
		reason, message, retryAt)
	return databaseError("mark software recovery needed", err)
}

func (s *Store) FailSoftwareUpdate(ctx context.Context, message string, retryAt time.Time) error {
	_, err := s.workerPool().Exec(ctx, `UPDATE aiosources.sw_sync_state SET phase='idle',error=$1,retry_at=GREATEST(COALESCE(retry_at,to_timestamp(0)),$2),checked_at=now() WHERE singleton`,
		message, retryAt)
	return databaseError("retain software update retry", err)
}

func (s *Store) RecordSoftwareInformationActivity(ctx context.Context, updateErr error, errorMessage string, processed int64) error {
	level, kind, message := "info", "software.sync_completed", "Software information is up to date."
	if updateErr != nil {
		level, kind, message = "error", "software.sync_failed", "Software information update failed. Saved progress is preserved."
		if errors.Is(updateErr, context.Canceled) {
			level, kind, message = "warn", "software.sync_interrupted", "Software information update interrupted. Saved progress is preserved."
		} else if errorMessage != "" {
			message = errorMessage
		}
	} else {
		state, err := s.SoftwareStatus(ctx)
		if err != nil {
			return err
		}
		if state.NeedsFullImport {
			level, kind, message = "warn", "software.recovery_pending", "A full NVD CPE snapshot re-import is scheduled to recover software updates. Existing software information is preserved."
		} else if state.Skipped > 0 {
			level, kind, message = "warn", "software.records_skipped", "Software information updated. Deprecated or unreadable source records were skipped."
		}
	}
	id, err := newRunID()
	if err != nil {
		return err
	}
	_, err = s.workerPool().Exec(ctx, `INSERT INTO aiosources.activity_logs(event_id,level,kind,message,title,service,data) VALUES($1,$2,$3,$4,'Software information update','software',jsonb_build_object('records',$5::bigint))`,
		"activity-"+id, level, kind, message, processed)
	return databaseError("record software information activity", err)
}

func (s *Store) ResetSoftwareInformation(ctx context.Context) error {
	return s.resetInformation(ctx, "software")
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
