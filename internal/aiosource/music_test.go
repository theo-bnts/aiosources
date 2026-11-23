package aiosource

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
)

func buildMusicTarBZ2(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	bzipPath, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("bzip2 binary not available")
	}
	cmd := exec.Command(bzipPath, "-c")
	cmd.Stdin = &tarBuf
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("bzip2: %v", err)
	}
	return out.Bytes()
}

func TestTrustedMusicExportRedirectValidation(t *testing.T) {
	for _, allowed := range []string{
		"https://data.metabrainz.org/pub/musicbrainz/data/fullexport/LATEST",
		"https://metabrainz.org/api/musicbrainz/replication-100-v2.tar.bz2",
		"https://musicbrainz.org/pub/data",
		"https://archive.org/download/mbdump/mbdump.tar.bz2",
	} {
		u, _ := url.Parse(allowed)
		if err := trustedMusicExport(&http.Request{URL: u}, nil); err != nil {
			t.Fatalf("trusted URL rejected %q: %v", allowed, err)
		}
	}
	for _, blocked := range []string{
		"http://data.metabrainz.org/pub/musicbrainz/data/fullexport/LATEST",
		"https://evil.example/mbdump.tar.bz2",
		"https://data.metabrainz.org:8443/LATEST",
		"https://user:pass@data.metabrainz.org/LATEST",
		"https://127.0.0.1/LATEST",
	} {
		u, _ := url.Parse(blocked)
		if err := trustedMusicExport(&http.Request{URL: u}, nil); err == nil {
			t.Fatalf("untrusted URL accepted %q", blocked)
		}
	}
}

func TestMusicExportMustCoverThePublishedSnapshot(t *testing.T) {
	const published = "20261118-002230"
	for _, tc := range []struct {
		previous   string
		latest     string
		continuity bool
		needsNewer bool
	}{
		{"", published, true, false},
		{published, "20261114-002230", false, true},
		{published, published, false, false},
		{published, published, true, true},
		{published, "20261121-002230", true, false},
	} {
		if got := musicNeedsNewerExport(tc.previous, tc.latest, tc.continuity); got != tc.needsNewer {
			t.Errorf("published %q, latest %q, recovering continuity %v: needs newer = %v", tc.previous, tc.latest, tc.continuity, got)
		}
	}
}

func TestMusicFullDumpCoversLiveReplicationLongPauseRecoveryAndReset(t *testing.T) {
	ctx, s, conn := testService(t)
	rgID := "11111111-2222-3333-4444-555555555555"
	rgSpokenID := "22222222-2222-3333-4444-555555555555"
	rgNewID := "33333333-2222-3333-4444-555555555555"
	artistID := "aaaaaaaa-2222-3333-4444-555555555555"
	relID := "bbbbbbbb-2222-3333-4444-555555555555"
	relSpokenID := "cccccccc-2222-3333-4444-555555555555"

	now := time.Now().UTC()
	dumpTime := now.Add(-2 * time.Hour)
	replTime := now.Add(-1 * time.Hour)
	snap1 := dumpTime.Add(-48 * time.Hour).Format("20060102-150405")
	snap2 := dumpTime.Add(-24 * time.Hour).Format("20060102-150405")
	snap3 := dumpTime.Format("20060102-150405")

	coreDump := buildMusicTarBZ2(t, map[string]string{
		"SCHEMA_SEQUENCE":                          "30\n",
		"REPLICATION_SEQUENCE":                     "100\n",
		"TIMESTAMP":                                dumpTime.Format("2006-01-02 15:04:05+00") + "\n",
		"mbdump/language":                          "120\teng\t\t\tEnglish\t2\t\ten\n",
		"mbdump/release_group_primary_type":        "1\tAlbum\t\\N\t0\t\t00000000-0000-0000-0000-000000000001\n",
		"mbdump/release_group_secondary_type":      "5\tAudiobook\t\\N\t0\t\t00000000-0000-0000-0000-000000000005\n",
		"mbdump/release_status":                    "1\tOfficial\t\\N\t0\t\t00000000-0000-0000-0000-000000000011\n",
		"mbdump/artist":                            fmt.Sprintf("10\t%s\tSynthetic Duo\tDuo, Synthetic\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\t0\t2026-11-14\t\\N\t\\N\tf\n", artistID),
		"mbdump/artist_credit":                     "20\tSynthetic Duo\t1\t1\t2026-11-14\t0\t00000000-0000-0000-0000-000000000020\n",
		"mbdump/artist_credit_name":                "20\t0\t10\tSynthetic Duo\t\n",
		"mbdump/release_group":                     fmt.Sprintf("30\t%s\tNeon Horizon\t20\t1\t\t0\t2026-11-14\n31\t%s\tSpoken Story\t20\t1\t\t0\t2026-11-14\n", rgID, rgSpokenID),
		"mbdump/release_group_secondary_type_join": "31\t5\t2026-11-14\n",
		"mbdump/release":                           fmt.Sprintf("40\t%s\tNeon Horizon\t20\t30\t1\t\\N\t120\t\\N\t0724349691728\t\t0\t1\t2026-11-14\n41\t%s\tSpoken Story\t20\t31\t1\t\\N\t120\t\\N\t\t\t0\t1\t2026-11-14\n", relID, relSpokenID),
		"mbdump/release_country":                   "40\t1\t2024\t5\t10\n",
		"mbdump/label":                             "50\tdddddddd-2222-3333-4444-555555555555\tOrbit Records\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\\N\t\t0\t2026-11-14\tf\n",
		"mbdump/release_label":                     "60\t40\t50\tORB-042\t2026-11-14\n",
		"mbdump/url":                               "70\teeeeeeee-2222-3333-4444-555555555555\thttps://www.discogs.com/master/26647\t0\t2026-11-14\n71\tffffffff-2222-3333-4444-555555555555\thttps://www.wikidata.org/wiki/Q43228\t0\t2026-11-14\n72\t99999999-2222-3333-4444-555555555555\thttps://unrelated.example/page\t0\t2026-11-14\n",
		"mbdump/l_release_group_url":               "80\t1\t30\t70\t0\t2026-11-14\t0\t\n81\t1\t30\t71\t0\t2026-11-14\t0\t\n82\t1\t30\t72\t0\t2026-11-14\t0\t\n",
	})
	coverDump := buildMusicTarBZ2(t, map[string]string{
		"SCHEMA_SEQUENCE":                         "30\n",
		"REPLICATION_SEQUENCE":                    "100\n",
		"mbdump/cover_art_archive.cover_art":      "900\t40\t\t1\t1\t2026-11-14\t1\timage/jpeg\t\n",
		"mbdump/cover_art_archive.cover_art_type": "900\t1\n",
	})
	repl101 := buildMusicTarBZ2(t, map[string]string{
		"SCHEMA_SEQUENCE":      "30\n",
		"REPLICATION_SEQUENCE": "101\n",
		"TIMESTAMP":            replTime.Format("2006-01-02 15:04:05+00") + "\n",
		"mbdump/dbmirror2.pending_data": strings.Join([]string{
			fmt.Sprintf("1\t\"musicbrainz\".\"release_group\"\ti\t101\t\\N\t{\"id\":32,\"gid\":%q,\"name\":\"Solar Echoes\",\"artist_credit\":20,\"type\":1}\t0", rgNewID),
			"2\t\"musicbrainz\".\"release\"\ti\t101\t\\N\t{\"id\":42,\"gid\":\"44444444-2222-3333-4444-555555555555\",\"name\":\"Solar Echoes\",\"artist_credit\":20,\"release_group\":32,\"status\":1,\"language\":120,\"barcode\":\"\"}\t0",
		}, "\n") + "\n",
	})

	badSchemaDump := buildMusicTarBZ2(t, map[string]string{"SCHEMA_SEQUENCE": "99\n"})
	incompletePacket := buildMusicTarBZ2(t, map[string]string{"SCHEMA_SEQUENCE": "30\n", "REPLICATION_SEQUENCE": "102\n"})

	var mode atomic.Int32 // 0 = initial dump, 1 = long pause, 2 = unsupported schema, 3 = incomplete replication
	var exportCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fullexport/LATEST":
			switch mode.Load() {
			case 1:
				fmt.Fprintln(w, snap2)
			case 2:
				fmt.Fprintln(w, snap3)
			default:
				fmt.Fprintln(w, snap1)
			}
		case "/fullexport/" + snap1 + "/mbdump.tar.bz2", "/fullexport/" + snap2 + "/mbdump.tar.bz2", "/fullexport/" + snap3 + "/mbdump.tar.bz2":
			exportCalls.Add(1)
			if mode.Load() == 2 {
				_, _ = w.Write(badSchemaDump)
				return
			}
			_, _ = w.Write(coreDump)
		case "/fullexport/" + snap1 + "/mbdump-cover-art-archive.tar.bz2", "/fullexport/" + snap2 + "/mbdump-cover-art-archive.tar.bz2", "/fullexport/" + snap3 + "/mbdump-cover-art-archive.tar.bz2":
			_, _ = w.Write(coverDump)
		case "/replication/replication-101-v2.tar.bz2":
			if r.URL.Query().Get("token") != "synthetic-mb-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if mode.Load() == 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(repl101)
		case "/replication/replication-102-v2.tar.bz2":
			if mode.Load() == 3 {
				_, _ = w.Write(incompletePacket)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	s.MusicExportBase = upstream.URL + "/fullexport"
	s.MusicReplicationBase = upstream.URL + "/replication"
	s.MusicHTTP = upstream.Client()

	if err := s.Vault.Put(ctx, "mb_token_secret", "synthetic-mb-token"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.MusicBrainzSecret = "mb_token_secret"
	c.MusicBrainzOfficialOnly = true
	c.MusicBrainzExcludeSpoken = true
	c.MusicBrainzIncludeCovers = true
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.DeleteSecret(ctx, "mb_token_secret"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("configured MusicBrainz token could be deleted: %v", err)
	}

	// 1a. Initial sync downloads core + cover dumps.
	if err := s.SyncMusic(ctx, "manual"); err != nil {
		t.Fatalf("initial music dump sync failed: %v", err)
	}
	st, err := s.DB.MusicStatus(ctx)
	if err != nil || !st.Complete || st.Works != 1 || st.Covers != 1 || st.ReplicationSequence != 100 {
		t.Fatalf("unexpected music status after dump: %+v %v", st, err)
	}
	// 1b. Next sync follows hourly Live Data Feed replication-101.
	if err := s.SyncMusic(ctx, "manual"); err != nil {
		t.Fatalf("live replication sync failed: %v", err)
	}
	st, err = s.DB.MusicStatus(ctx)
	if err != nil || !st.Complete || st.Works != 2 || st.Covers != 1 || st.ReplicationSequence != 101 {
		t.Fatalf("unexpected music status after dump + repl: %+v %v", st, err)
	}

	// Verify Discogs, Wikidata, Cover Art Archive poster_path, and spokenword exclusion.
	var poster, musicJSON string
	if err = conn.QueryRow(ctx, `SELECT poster_path,music_data::text FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id=$1`, rgID).Scan(&poster, &musicJSON); err != nil {
		t.Fatalf("projected release group missing: %v", err)
	}
	if poster != rgID || !strings.Contains(musicJSON, "master:26647") || !strings.Contains(musicJSON, "Q43228") || strings.Contains(musicJSON, "unrelated.example") {
		t.Fatalf("unexpected projected music work: poster=%q json=%s", poster, musicJSON)
	}
	var spokenCount int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM aiosources.aio_works WHERE media_type='music' AND musicbrainz_id=$1`, rgSpokenID).Scan(&spokenCount); err != nil || spokenCount != 0 {
		t.Fatalf("spokenword/audiobook leaked into music catalog: %d %v", spokenCount, err)
	}

	// With no newer export, an incomplete packet must leave the last cursor in place.
	mode.Store(3)
	if err = s.SyncMusic(ctx, "manual"); err != nil {
		t.Fatalf("incomplete replication did not wait for an export: %v", err)
	}
	st, err = s.DB.MusicStatus(ctx)
	if err != nil || !st.NeedsExport || st.RecoveryReason != "packet_invalid" || st.ReplicationSequence != 101 || st.Snapshot != snap1 || st.RetryAt == nil {
		t.Fatalf("incomplete replication changed the published snapshot: %+v %v", st, err)
	}

	// 2. Simulate a long pause without a pending recovery.
	if _, err = conn.Exec(ctx, `UPDATE aiosources.mb_state SET changes_through=$1,last_packet_at=$1,replication_sequence=100,needs_export=FALSE,recovery_reason='',retry_at=NULL WHERE singleton`, now.Add(-90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	beforeExports := exportCalls.Load()
	if err = s.SyncMusic(ctx, "manual"); err != nil {
		t.Fatalf("long-pause recovery failed: %v", err)
	}
	if exportCalls.Load() <= beforeExports {
		t.Fatal("3-month pause did not trigger fullexport recovery")
	}

	// 3. Unsupported future SCHEMA_SEQUENCE fails closed and never leaks token.
	mode.Store(2)
	if _, err = conn.Exec(ctx, `UPDATE aiosources.mb_state SET needs_export=TRUE,recovery_reason='schema_upgrade' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	err = s.SyncMusic(ctx, "manual")
	if !errors.Is(err, music.ErrUnsupportedSchema) {
		t.Fatalf("expected ErrUnsupportedSchema, got %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-mb-token") {
		t.Fatal("secret token leaked in error")
	}
	st, _ = s.DB.MusicStatus(ctx)
	if !st.SchemaUnsupported {
		t.Fatalf("schema_unsupported flag not set: %+v", st)
	}

	// 4. Isolated music reset leaves unrelated video/game/book works intact.
	if _, err = conn.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,years) VALUES('game','777','Keep Game','{2024}')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Reset(ctx, "music"); err != nil {
		t.Fatalf("music reset failed: %v", err)
	}
	var musicWorks, gameWorks int
	if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.aio_works WHERE media_type='music'),(SELECT count(*) FROM aiosources.aio_works WHERE media_type='game')`).Scan(&musicWorks, &gameWorks); err != nil || musicWorks != 0 || gameWorks != 1 {
		t.Fatalf("music reset affected wrong works: music=%d game=%d err=%v", musicWorks, gameWorks, err)
	}
}
