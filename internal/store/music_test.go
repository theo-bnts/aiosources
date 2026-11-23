package store

import (
	"slices"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
)

func TestProjectedCatalogTitlesUseTheStoredAliasIdentity(t *testing.T) {
	got := projectedCatalogTitles("Neon Horizon", []string{"NEON-HORIZON", "Horizon néon", "horizon neon", "..."})
	if !slices.Equal(got, []string{"Neon Horizon", "Horizon néon"}) {
		t.Fatalf("unexpected projected titles: %v", got)
	}
}

func TestMusicSelectionKeyChangesOnlyOnMeaningfulFilterChanges(t *testing.T) {
	base := model.AIOConfig{
		MusicBrainzOfficialOnly:  true,
		MusicBrainzExcludeSpoken: true,
		MusicBrainzIncludeCovers: true,
		MusicBrainzTypes:         []string{"album", "ep", "single"},
		MusicBrainzLanguages:     []string{"en", "fr"},
	}
	reordered := base
	reordered.MusicBrainzTypes = []string{"Single", "ALBUM", "ep"}
	reordered.MusicBrainzLanguages = []string{"FR", "eng"}
	if MusicSelectionKey(base) != MusicSelectionKey(reordered) {
		t.Fatalf("reordered filters produced different keys: %q vs %q", MusicSelectionKey(base), MusicSelectionKey(reordered))
	}

	noCovers := base
	noCovers.MusicBrainzIncludeCovers = false
	if MusicSelectionKey(base) == MusicSelectionKey(noCovers) {
		t.Fatal("toggling cover import kept the same selection key")
	}

	withSpoken := base
	withSpoken.MusicBrainzExcludeSpoken = false
	if MusicSelectionKey(base) == MusicSelectionKey(withSpoken) {
		t.Fatal("toggling spokenword exclusion kept the same selection key")
	}
}

func TestMusicDumpProjectionReplicationAndCandidateSearch(t *testing.T) {
	ctx, db := sharingDatabase(t)
	cfg, err := db.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sel := MusicSelection(cfg)
	now := time.Now().UTC()
	gen, err := db.BeginMusicDump(ctx, sel, "")
	if err != nil {
		t.Fatal(err)
	}
	rgID := "11111111-2222-3333-4444-555555555555"
	batch := music.DumpTableBatch{
		Lookups: []music.LookupRow{
			{Kind: "release_group_primary_type", ID: 1, Name: "Album"},
			{Kind: "release_status", ID: 1, Name: "Official"},
		},
		Artists: []music.ArtistRow{
			{ID: 10, GID: "aaaaaaaa-2222-3333-4444-555555555555", Name: "Synthetic Duo", SortName: "Duo, Synthetic"},
		},
		ArtistCredits: []music.ArtistCreditRow{
			{ID: 20, Name: "Synthetic Duo"},
		},
		CreditNames: []music.ArtistCreditNameRow{
			{ArtistCredit: 20, Position: 0, ArtistID: 10, Name: "Synthetic Duo"},
		},
		ReleaseGroups: []music.ReleaseGroupRow{
			{ID: 30, GID: rgID, Name: "Neon Horizon", ArtistCredit: 20, PrimaryType: 1},
		},
		Releases: []music.ReleaseRow{
			{ID: 40, GID: "bbbbbbbb-2222-3333-4444-555555555555", ReleaseGroupID: 30, Name: "Neon Horizon", ArtistCredit: 20, Status: 1, Barcode: "0724349691728"},
		},
		ReleaseDates: []music.ReleaseDateRow{{ReleaseID: 40, Year: 2024}, {ReleaseID: 40, Year: 2024}},
		CoverArts: []music.CoverArtRow{
			{ID: 900, ReleaseID: 40, Ordering: 1},
		},
		CoverArtTypes: []music.CoverArtTypeRow{
			{CoverArtID: 900, TypeID: 1},
		},
	}
	if err = db.SaveMusicDumpBatch(ctx, gen, batch); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = db.ProjectMusicCatalog(ctx, gen, sel, nil); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishMusicDump(ctx, gen, music.DumpHeader{SchemaSequence: 30, ReplicationSequence: 100, Timestamp: now}, "20261114-002230", &now, sel); err != nil {
		t.Fatal(err)
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	works, overflow, err := searchAIOCandidates(ctx, tx, model.AIOCandidateFilter{
		Type:        "music",
		Title:       "neon horizon",
		MusicArtist: "Synthetic Duo",
		ExactOnly:   true,
	})
	if err != nil || overflow || len(works) != 1 || works[0].MusicBrainz != rgID || !works[0].Music.HasCover {
		t.Fatalf("music candidate search failed: works=%+v overflow=%v err=%v", works, overflow, err)
	}

	packet := music.ReplicationPacket{
		DumpHeader: music.DumpHeader{
			SchemaSequence:      30,
			ReplicationSequence: 101,
			Timestamp:           now,
		},
		Changes: []music.ReplicationChange{
			{
				Schema:  "musicbrainz",
				Table:   "artist_credit",
				Op:      "u",
				NewData: map[string]any{"id": float64(20), "name": "Synthetic Duo Reborn"},
			},
			{
				Schema:  "musicbrainz",
				Table:   "release_country",
				Op:      "d",
				OldData: map[string]any{"release": 40, "country": 1, "date_year": 2024},
			},
		},
	}
	if _, err = db.ApplyMusicReplicationPacket(ctx, packet, sel); err != nil {
		t.Fatalf("replication apply failed: %v", err)
	}
	st, err := db.MusicStatus(ctx)
	if err != nil || st.ReplicationSequence != 101 || st.Works != 1 {
		t.Fatalf("unexpected music status after replication: %+v %v", st, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.pool.QueryRow(ctx, `SELECT occurrences FROM aiosources.mb_release_years WHERE release_id=40 AND year=2024`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("removing a country removed another country's matching year: count=%d %v", count, err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE aiosources.mb_state SET selection='legacy-selection' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	second, err := db.BeginMusicDump(ctx, sel, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FailMusicUpdate(ctx, "Download interrupted", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	st, err = db.MusicStatus(ctx)
	if err != nil || !st.NeedsExport || st.Complete || st.Selection != sel.Key() || st.Snapshot != "20261114-002230" || st.ReplicationSequence != 101 {
		t.Fatalf("interrupted dump changed the published checkpoint: %+v %v", st, err)
	}
	batch.CoverArts, batch.CoverArtTypes = nil, nil
	if err = db.SaveMusicDumpBatch(ctx, second, batch); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = db.ProjectMusicCatalog(ctx, second, sel, nil); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishMusicDump(ctx, second, music.DumpHeader{SchemaSequence: 30, ReplicationSequence: 102}, "20261118-002230", &now, sel); err != nil {
		t.Fatal(err)
	}
	st, err = db.MusicStatus(ctx)
	if err != nil || st.NeedsExport || !st.Complete || st.Snapshot != "20261118-002230" || st.ReplicationSequence != 102 || st.Covers != 0 {
		t.Fatalf("replacement dump kept stale staging data: %+v %v", st, err)
	}
	live, err := db.NextMusicUpdate(ctx, 60, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE aiosources.mb_state SET replication_sequence=0 WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	export, err := db.NextMusicUpdate(ctx, 60, true)
	if err != nil || export.Sub(live) != 5*time.Hour {
		t.Fatalf("a token without a sequence should keep the full-export interval: %v %v %v", live, export, err)
	}
}
