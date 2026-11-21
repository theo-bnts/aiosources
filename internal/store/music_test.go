package store

import (
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
)

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
	if err = db.PrepareMusicImport(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	gen, err := db.BeginMusicDump(ctx, "20261114-002230", &now, sel.Key(), "")
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
	if err = db.FinishMusicDump(ctx, gen, music.DumpHeader{SchemaSequence: 30, ReplicationSequence: 100, Timestamp: now}, "20261114-002230", &now, true); err != nil {
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
		},
	}
	if _, err = db.ApplyMusicReplicationPacket(ctx, packet, sel); err != nil {
		t.Fatalf("replication apply failed: %v", err)
	}
	st, err := db.MusicStatus(ctx)
	if err != nil || st.ReplicationSequence != 101 || st.Works != 1 {
		t.Fatalf("unexpected music status after replication: %+v %v", st, err)
	}
}
