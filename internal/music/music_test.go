package music

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func buildTarBZ2(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for name, body := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
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
		t.Skip("bzip2 binary not available for archive compression test")
	}
	cmd := exec.Command(bzipPath, "-c")
	cmd.Stdin = &tarBuf
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("bzip2 compress test archive: %v", err)
	}
	return out.Bytes()
}

func TestIdentifiersAndExternalURLs(t *testing.T) {
	id := "B10BBBFC-CF9E-42E0-BE17-E2C3E1D2600D"
	if got := MBID(id); got != "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d" {
		t.Fatalf("MBID(%q) = %q", id, got)
	}
	if got := MBID("https://musicbrainz.org/release-group/b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"); got != "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d" {
		t.Fatalf("MBID URL = %q", got)
	}
	if got := MBID("https://evil.example/release-group/b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"); got != "" {
		t.Fatalf("unexpected untrusted host MBID = %q", got)
	}
	if got := Barcode("0 7243-496917-2-8"); got != "0724349691728" {
		t.Fatalf("Barcode = %q", got)
	}
	if got := Barcode("123"); got != "" {
		t.Fatalf("short barcode accepted: %q", got)
	}
	if kind, did := DiscogsURL("https://www.discogs.com/master/26647-Daft-Punk-Discovery"); kind != "master" || did != "26647" {
		t.Fatalf("DiscogsURL master = (%q, %q)", kind, did)
	}
	if kind, did := DiscogsURL("https://www.discogs.com/fr/release/123456-Album"); kind != "release" || did != "123456" {
		t.Fatalf("DiscogsURL localized release = (%q, %q)", kind, did)
	}
	if kind, did := DiscogsURL("https://example.com/master/12345"); kind != "" || did != "" {
		t.Fatalf("non-discogs URL accepted: (%q, %q)", kind, did)
	}
	if got := DiscogsID("m26647"); got != "master:26647" {
		t.Fatalf("DiscogsID m-prefix = %q", got)
	}
	if got := WikidataURL("https://www.wikidata.org/wiki/Q43228"); got != "Q43228" {
		t.Fatalf("WikidataURL = %q", got)
	}
	if got := WikidataID("q43228"); got != "Q43228" {
		t.Fatalf("WikidataID = %q", got)
	}
	if got := CatalogNumber("WARP CD 123"); got != "warpcd123" {
		t.Fatalf("CatalogNumber = %q", got)
	}
	if snap, ok := ExportSnapshot("20261114-002230/"); !ok || snap != "20261114-002230" {
		t.Fatalf("ExportSnapshot = (%q, %v)", snap, ok)
	}
	if _, ok := ExportSnapshot("../etc/passwd"); ok {
		t.Fatal("accepted path traversal snapshot")
	}
}

func TestStreamCoreAndCoverArtDumpArchives(t *testing.T) {
	core := buildTarBZ2(t, map[string]string{
		"TIMESTAMP":                           "2026-11-14 00:22:30.000000+00\n",
		"REPLICATION_SEQUENCE":                "182450\n",
		"SCHEMA_SEQUENCE":                     "31\n",
		"mbdump/release_group_primary_type":   "1\tAlbum\n2\tSingle\n",
		"mbdump/release_group_secondary_type": "1\tCompilation\n11\tAudiobook\n",
		"mbdump/release_status":               "1\tOfficial\n3\tBootleg\n",
		"mbdump/medium_format":                "1\tCD\n12\tDigital Media\n",
		"mbdump/language":                     "120\teng\teng\ten\tEnglish\t2\teng\n",
		"mbdump/artist":                       "10\t056e4f3e-d505-4dad-8ec1-d04f521cbb56\tDaft Punk\tDaft Punk\n",
		"mbdump/artist_alias":                 "1\t10\tダフト・パンク\tja\n",
		"mbdump/artist_credit":                "20\tDaft Punk\t1\t10\t2020-01-01\t0\taaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa\n",
		"mbdump/artist_credit_name":           "20\t0\t10\tDaft Punk\t\n",
		"mbdump/release_group":                "30\t46f5c7ea-410c-3769-838a-81d80d7e3962\tDiscovery\t20\t1\n",
		"mbdump/release":                      "40\t11111111-1111-1111-1111-111111111111\tDiscovery\t20\t30\t1\t\\N\t120\t28\t724384960629\n",
		"mbdump/release_country":              "40\t73\t2001\t3\t12\n",
		"mbdump/label":                        "50\t22222222-2222-2222-2222-222222222222\tVirgin\n",
		"mbdump/release_label":                "60\t40\t50\tCDV2940\n",
		"mbdump/medium":                       "70\t40\t1\t1\t\t0\t2020-01-01\t14\n",
		"mbdump/url":                          "80\t33333333-3333-3333-3333-333333333333\thttps://www.discogs.com/master/26647\n81\t44444444-4444-4444-4444-444444444444\thttps://www.wikidata.org/wiki/Q738624\n82\t55555555-5555-5555-5555-555555555555\thttps://unrelated.example/page\n",
		"mbdump/l_release_group_url":          "90\t1\t30\t80\n91\t2\t30\t81\n",
		"mbdump/release_group_gid_redirect":   "99999999-9999-9999-9999-999999999999\t30\n",
		"mbdump/track":                        "999999\tignored-track-data\n",
	})

	counts := map[string]int{}
	header, err := StreamDumpArchive(context.Background(), bytes.NewReader(core), WatchedCoreTables, 100, func(b DumpTableBatch) error {
		counts[b.Table] += b.Len()
		if b.Table == "track" {
			t.Fatal("heavy track table must be skipped")
		}
		if b.Table == "url" && len(b.URLs) != 2 {
			t.Fatalf("expected only Discogs + Wikidata URLs, got %d", len(b.URLs))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamDumpArchive core failed: %v", err)
	}
	if header.ReplicationSequence != 182450 || header.SchemaSequence != 31 {
		t.Fatalf("unexpected header: %+v", header)
	}
	for _, table := range []string{"artist", "release_group", "release", "release_country", "label", "release_label", "medium", "url", "l_release_group_url", "release_group_gid_redirect"} {
		if counts[table] == 0 {
			t.Fatalf("expected decoded rows for %s", table)
		}
	}

	covers := buildTarBZ2(t, map[string]string{
		"SCHEMA_SEQUENCE":                "31\n",
		"mbdump/cover_art":               "123456789\t40\t\t1\n",
		"mbdump/cover_art_type":          "123456789\t1\n123456789\t2\n",
		"mbdump/release_group_cover_art": "30\t40\n",
	})
	coverCounts := map[string]int{}
	_, err = StreamDumpArchive(context.Background(), bytes.NewReader(covers), WatchedCoverTables, 100, func(b DumpTableBatch) error {
		coverCounts[b.Table] += b.Len()
		return nil
	})
	if err != nil {
		t.Fatalf("StreamDumpArchive covers failed: %v", err)
	}
	if coverCounts["cover_art"] != 1 || coverCounts["cover_art_type"] != 1 || coverCounts["release_group_cover_art"] != 1 {
		t.Fatalf("unexpected cover counts: %+v", coverCounts)
	}
}

func TestStreamDumpArchiveRejectsUnsupportedSchema(t *testing.T) {
	future := buildTarBZ2(t, map[string]string{
		"SCHEMA_SEQUENCE": "99\n",
		"mbdump/artist":   "1\t056e4f3e-d505-4dad-8ec1-d04f521cbb56\tTest\tTest\n",
	})
	_, err := StreamDumpArchive(context.Background(), bytes.NewReader(future), WatchedCoreTables, 100, func(DumpTableBatch) error {
		t.Fatal("must not emit rows for unsupported schema")
		return nil
	})
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("expected ErrUnsupportedSchema, got %v", err)
	}
}

func TestParseReplicationPacketFiltersWatchedAndCoverTables(t *testing.T) {
	pending := strings.Join([]string{
		`1` + "\t" + `"musicbrainz"."release_group"` + "\t" + `i` + "\t" + `100` + "\t" + `\N` + "\t" + `{"id":30,"gid":"46f5c7ea-410c-3769-838a-81d80d7e3962","name":"Discovery","artist_credit":20,"type":1}` + "\t" + `0`,
		`2` + "\t" + `"musicbrainz"."track"` + "\t" + `i` + "\t" + `100` + "\t" + `\N` + "\t" + `{"id":999,"name":"One More Time"}` + "\t" + `0`,
		`3` + "\t" + `"cover_art_archive"."cover_art"` + "\t" + `i` + "\t" + `101` + "\t" + `\N` + "\t" + `{"id":123456,"release":40,"ordering":1}` + "\t" + `0`,
		`4` + "\t" + `"cover_art_archive"."cover_art_type"` + "\t" + `i` + "\t" + `101` + "\t" + `\N` + "\t" + `{"id":123456,"type_id":1}` + "\t" + `0`,
	}, "\n") + "\n"

	archive := buildTarBZ2(t, map[string]string{
		"TIMESTAMP":                     "2026-11-21 12:00:00+00\n",
		"REPLICATION_SEQUENCE":          "182451\n",
		"SCHEMA_SEQUENCE":               "31\n",
		"DBMIRROR_VERSION":              "2\n",
		"mbdump/dbmirror2.pending_data": pending,
	})

	pkt, err := ParseReplicationPacket(context.Background(), bytes.NewReader(archive), true)
	if err != nil {
		t.Fatalf("ParseReplicationPacket failed: %v", err)
	}
	if pkt.ReplicationSequence != 182451 || pkt.SchemaSequence != 31 {
		t.Fatalf("unexpected packet header: %+v", pkt.DumpHeader)
	}
	if len(pkt.Changes) != 3 {
		t.Fatalf("expected 3 watched changes (skipping track), got %d", len(pkt.Changes))
	}
	if pkt.Changes[0].Table != "release_group" || pkt.Changes[1].Schema != "cover_art_archive" {
		t.Fatalf("unexpected changes: %+v", pkt.Changes)
	}

	// Without covers enabled, cover_art_archive mutations are skipped.
	pktNoCovers, err := ParseReplicationPacket(context.Background(), bytes.NewReader(archive), false)
	if err != nil {
		t.Fatalf("ParseReplicationPacket(no covers) failed: %v", err)
	}
	if len(pktNoCovers.Changes) != 1 {
		t.Fatalf("expected 1 change without covers, got %d", len(pktNoCovers.Changes))
	}
}

func TestParseRecordMusicAndExclusions(t *testing.T) {
	flac := ParseRecord(model.AIORecord{
		Title: "Daft Punk - Discovery (2001) [FLAC 24bit-96kHz]",
	})
	if flac.Kind != "lossless" || flac.Artist != "Daft Punk" || flac.Title != "Discovery" || flac.Year != 2001 {
		t.Fatalf("unexpected lossless parse: %+v", flac)
	}

	mp3 := ParseRecord(model.AIORecord{
		Title:      "VA - French Touch Classics (2020) [MP3 320]",
		MusicHints: model.MusicHints{Categories: []int{3010}},
	})
	if mp3.Kind != "mp3" || mp3.Artist != "Various Artists" || mp3.Title != "French Touch Classics" || mp3.Year != 2020 {
		t.Fatalf("unexpected mp3 parse: %+v", mp3)
	}

	discog := ParseRecord(model.AIORecord{
		Title: "Daft Punk - Complete Discography (1997-2023) [FLAC]",
	})
	if !discog.Collection {
		t.Fatalf("expected collection flag for discography: %+v", discog)
	}

	// Audiobooks (category 3030 or audiobook keyword) must remain owned by Books.
	audiobook := ParseRecord(model.AIORecord{
		Title:      "Frank Herbert - Dune (Audiobook) [MP3]",
		MusicHints: model.MusicHints{Categories: []int{3030}},
	})
	if audiobook.Kind != "" {
		t.Fatalf("audiobook must not be classified as music: %+v", audiobook)
	}

	// Video releases with FLAC audio tracks must not be classified as music.
	video := ParseRecord(model.AIORecord{
		Title: "Interstella.5555.2003.1080p.BluRay.x264.FLAC-GROUP",
	})
	if video.Kind != "" {
		t.Fatalf("video release must not be classified as music: %+v", video)
	}
}
