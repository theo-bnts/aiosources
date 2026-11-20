package aiosource

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMediaInfoMetadataEnrichmentCompactStorageAndCrossSourcePropagation(t *testing.T) {
	ctx, s, conn := testService(t)

	// Create a matched movie work.
	if err := s.DB.SaveAIOWork(ctx, model.AIOWork{
		Type:             "movie",
		TMDB:             "707",
		IMDb:             "tt7070707",
		DisplayTitle:     "Signal Example",
		Titles:           []string{"Signal Example"},
		Years:            []int{2025},
		OriginalLanguage: "ja",
	}, []string{"signal example"}); err != nil {
		t.Fatal(err)
	}

	// 1. JSON source with incremental listing + detail metadata enrichment (metadata.nfoContent) in compact mode.
	detailProvider := model.Provider{
		ID:      "detail-source",
		Adapter: "http_json",
		Mapping: model.Mapping{
			ID: "/id",
			Fields: map[string]string{
				"title":     "/name",
				"info_hash": "/infoHash",
				"size":      "/size",
				"languages": "/languages",
			},
		},
		Traversal: &model.JSONTraversal{
			EnrichFields: []string{"external_ids", "metadata"},
			IDRecovery: &model.JSONIDRecovery{
				Mapping: model.Mapping{
					ID: "/id",
					Fields: map[string]string{
						"title":        "/name",
						"info_hash":    "/infoHash",
						"external_ids": "/externalIds",
						"metadata":     "/metadata",
					},
				},
			},
		},
	}

	hashA := strings.Repeat("a", 40)
	listingPayload, err := json.Marshal(map[string]any{
		"id":        "101",
		"name":      "Signal.Example.2025.MULTI.2160p.WEB-DL.H265-GRP",
		"infoHash":  hashA,
		"size":      int64(14_500_000_000),
		"languages": []string{"MULTI"},
	})
	if err != nil {
		t.Fatal(err)
	}
	listID, listFields, err := connectors.FieldsFromJSON(listingPayload, detailProvider.Mapping, "")
	if err != nil {
		t.Fatal(err)
	}

	incRun, err := s.DB.CreateRun(ctx, model.Run{
		ProviderID:  detailProvider.ID,
		Config:      detailProvider,
		Mode:        model.ModeIncremental,
		StorageMode: model.StorageCompact,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.DB.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != incRun.ID {
		t.Fatalf("claim incremental run failed: %+v %v", claimed, err)
	}
	if _, err := s.DB.SavePage(ctx, *claimed, model.Page{
		Items: []model.Record{{SourceID: listID, Fields: listFields, Raw: listingPayload, ContentType: "application/json"}},
		Done:  true,
	}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.FinishRun(ctx, claimed.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}

	// Detail enrichment response carrying MediaInfo inside metadata.nfoContent.
	mediaInfo := `General
Complete name                            : Signal.Example.2025.MULTI.2160p.WEB-DL.H265-GRP.mkv
Duration                                 : 2 h 12 min
Overall bit rate                         : 14.6 Mb/s

Video
Format                                   : HEVC
HDR format                               : SMPTE ST 2094 App 4, Version 1, HDR10+ Profile B compatible
Width                                    : 3 840 pixels
Height                                   : 1 600 pixels

Audio #1
Format                                   : MLP FBA 16-ch
Commercial name                          : Dolby TrueHD with Dolby Atmos
Channel(s)                               : 8 channels
Language                                 : French (France)

Audio #2
Format                                   : E-AC-3
Channel(s)                               : 6 channels
Language                                 : English

Audio #3
Format                                   : AC-3
Channel(s)                               : 2 channels
Title                                    : Director's Commentary
Language                                 : German

Text #1
Language                                 : French
Forced                                   : Yes

Text #2
Language                                 : French
Forced                                   : No

Text #3
Language                                 : Spanish
Forced                                   : No`

	detailPayload, err := json.Marshal(map[string]any{
		"id":          "101",
		"name":        "Signal.Example.2025.MULTI.2160p.WEB-DL.H265-GRP",
		"infoHash":    hashA,
		"externalIds": map[string]any{"tmdb": "707"},
		"metadata":    map[string]any{"nfoContent": mediaInfo},
	})
	if err != nil {
		t.Fatal(err)
	}
	detID, detFields, err := connectors.FieldsFromJSON(detailPayload, detailProvider.Traversal.IDRecovery.Mapping, "")
	if err != nil {
		t.Fatal(err)
	}

	metaRun, err := s.DB.CreateRun(ctx, model.Run{
		ProviderID:  detailProvider.ID,
		Config:      detailProvider,
		Mode:        model.ModeMetadata,
		StorageMode: model.StorageCompact,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimedMeta, err := s.DB.ClaimNext(ctx)
	if err != nil || claimedMeta == nil || claimedMeta.ID != metaRun.ID {
		t.Fatalf("claim metadata run failed: %+v %v", claimedMeta, err)
	}
	if _, err := s.DB.SavePage(ctx, *claimedMeta, model.Page{
		Items: []model.Record{{SourceID: detID, Fields: detFields, Raw: detailPayload, ContentType: "application/json"}},
		Done:  true,
	}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.FinishRun(ctx, claimedMeta.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}

	// Verify compact storage dropped raw nfoContent while persisting structured extracted fields.
	var storedRaw []byte
	if err := conn.QueryRow(ctx, `SELECT fields FROM aiosources.torrents WHERE provider_id='detail-source' AND source_id='101'`).Scan(&storedRaw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(storedRaw), "nfoContent") || strings.Contains(string(storedRaw), "Complete name") {
		t.Fatalf("compact storage retained raw nfoContent in DB: %s", string(storedRaw))
	}
	var stored map[string]any
	if err := json.Unmarshal(storedRaw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["hdr"] != "HDR10+" || stored["audio_codec"] != "TRUEHD ATMOS" || stored["audio_channels"] != "7.1" || stored["resolution"] != "2160p" {
		t.Fatalf("expected technical fields persisted from MediaInfo, got %+v", stored)
	}

	// 2. UNIT3D listing with BDInfo + bare Torznab peer sharing the same info_hash.
	hashB := strings.Repeat("b", 40)
	bdinfo := `QUICK SUMMARY:
Disc Title: SIGNAL_EXAMPLE_UHD
Length: 2:12:00.000
Total Bitrate: 61.50 Mbps
Video: MPEG-H HEVC Video / 50100 kbps / 2160p / 23.976 fps / HDR10 / Dolby Vision
Audio: Japanese / DTS-HD Master Audio / 5.1 / 48 kHz / 3800 kbps
Audio: English / Dolby Digital Plus Audio / 5.1 / 48 kHz / 768 kbps
Subtitle: French (Forced) / 0.50 kbps
Subtitle: French / 25.00 kbps`

	unit3dPayload, err := json.Marshal(map[string]any{
		"id": "202",
		"attributes": map[string]any{
			"name":      "Signal.Example.2025.2160p.UHD.BluRay.REMUX-PEER",
			"info_hash": hashB,
			"size":      int64(58_000_000_000),
			"tmdb_id":   "707",
			"bd_info":   bdinfo,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	unit3dMapping := model.Mapping{
		ID: "/id",
		Fields: map[string]string{
			"title":     "/attributes/name",
			"info_hash": "/attributes/info_hash",
			"size":      "/attributes/size",
			"tmdb_id":   "/attributes/tmdb_id",
			"bd_info":   "/attributes/bd_info",
		},
	}
	_, uFields, err := connectors.FieldsFromJSON(unit3dPayload, unit3dMapping, "")
	if err != nil {
		t.Fatal(err)
	}
	uCompact := model.StoredTorrentFields(uFields, model.StorageCompact)
	if uCompact["bd_info"] != nil {
		t.Fatalf("compact storage retained raw bd_info: %+v", uCompact)
	}
	uJSON, err := json.Marshal(uCompact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('unit3d-source','202',$1)`, uJSON); err != nil {
		t.Fatal(err)
	}

	// Bare Torznab peer with identical hashB and zero language hints in its title.
	peerJSON, err := json.Marshal(map[string]any{
		"title":     "Signal.Example.2025.2160p.UHD.BluRay.REMUX-PEER",
		"info_hash": hashB,
		"size":      int64(58_000_000_000),
		"tmdb_id":   "707",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('torznab-peer','303',$1)`, peerJSON); err != nil {
		t.Fatal(err)
	}

	// Run matching + release correspondence propagation.
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.drainCorrespondences(ctx, nil); err != nil {
		t.Fatal(err)
	}

	// Verify detail-source (enriched from metadata.nfoContent) allows fr & en, rejects de (commentary).
	detailRec, err := s.DB.AIOTorrent(ctx, "detail-source", "101")
	if err != nil {
		t.Fatal(err)
	}
	if !AllowsLanguage(detailRec, model.StreamPreferences{Language: "fr", Mode: "preferred"}) ||
		!AllowsLanguage(detailRec, model.StreamPreferences{Language: "en", Mode: "preferred"}) ||
		AllowsLanguage(detailRec, model.StreamPreferences{Language: "de", Mode: "preferred"}) {
		t.Fatalf("unexpected MediaInfo language filtering for detail-source: langs=%v subs=%v", detailRec.Languages, detailRec.SubtitleLanguages)
	}
	if !slices.Equal(detailRec.SubtitleLanguages, []string{"fr", "es"}) {
		t.Fatalf("expected [fr es] subtitles from MediaInfo, got %v", detailRec.SubtitleLanguages)
	}

	// Verify torznab-peer automatically inherited ja/en audio and fr subtitles from unit3d-source's BDInfo!
	peerRec, err := s.DB.AIOTorrent(ctx, "torznab-peer", "303")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(peerRec.PropagatedLanguages, []string{"en", "ja", "multi"}) || !slices.Equal(peerRec.PropagatedSubtitleLanguages, []string{"fr"}) {
		t.Fatalf("expected BDInfo languages propagated to torznab-peer, got audio=%v subs=%v", peerRec.PropagatedLanguages, peerRec.PropagatedSubtitleLanguages)
	}

	// Verify Stremio Streams renders French preference cleanly with MediaInfo presentation tags.
	tokenFR, err := testPrivateInstallation(t, ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	setTestInstallationPreferences(t, ctx, s, tokenFR, model.StreamPreferences{Language: "fr", Mode: "preferred"})
	streams, err := s.Streams(ctx, "http://localhost", tokenFR, "movie", "tmdb:707")
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 1 || streams[0].InfoHash != hashA {
		t.Fatalf("expected only French-audio detail-source stream for preferred=fr, got %+v", streams)
	}
	if !strings.Contains(streams[0].Name, "2h 12m") || !strings.Contains(streams[0].Name, "French") || !strings.Contains(streams[0].Name, "English") || !strings.Contains(streams[0].Name, "French / Spanish subtitles") {
		t.Fatalf("expected MediaInfo duration, languages, and subtitles in Stremio stream name, got %q", streams[0].Name)
	}
	if !strings.Contains(streams[0].Title, "HDR10+") || !strings.Contains(streams[0].Title, "7.1") {
		t.Fatalf("expected MediaInfo HDR10+ and 7.1 tags in Stremio stream title, got %q", streams[0].Title)
	}
}
