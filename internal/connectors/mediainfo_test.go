package connectors

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestParseMediaInfoEnglishFullReportWithGuards(t *testing.T) {
	sample := `[code]
General
Unique ID                                : 1234567890
Complete name                            : Example.Movie.2024.2160p.UHD.BluRay.Remux.mkv
Format                                   : Matroska
File size                                : 52.4 GiB
Duration                                 : 2 h 14 min
Overall bit rate                         : 55.9 Mb/s

Video
ID                                       : 1
Format                                   : HEVC
HDR format                               : Dolby Vision, Version 1.0, dvhe.08.06, BL+RPU, HDR10 compatible / SMPTE ST 2086, HDR10 compatible
Width                                    : 3 840 pixels
Height                                   : 1 600 pixels
Scan type                                : Progressive

Audio #1
ID                                       : 2
Format                                   : MLP FBA 16-ch
Commercial name                          : Dolby TrueHD with Dolby Atmos
Channel(s)                               : 8 channels
Language                                 : English
Title                                    : English TrueHD Atmos 7.1

Audio #2
ID                                       : 3
Format                                   : E-AC-3
Commercial name                          : Dolby Digital Plus
Channel(s)                               : 6 channels
Language                                 : French (France)
Title                                    : VFF 5.1

Audio #3
ID                                       : 4
Format                                   : AC-3
Channel(s)                               : 2 channels
Language                                 : German
Title                                    : Director's Commentary

Text #1
ID                                       : 5
Format                                   : PGS
Language                                 : French
Title                                    : VFF Forced
Forced                                   : Yes

Text #2
ID                                       : 6
Format                                   : PGS
Language                                 : French
Title                                    : VFF Full
Forced                                   : No

Text #3
ID                                       : 7
Format                                   : PGS
Language                                 : Spanish
Title                                    : Signs & Songs
Forced                                   : No

Text #4
ID                                       : 8
Format                                   : PGS
Language                                 : English
Title                                    : English SDH
Forced                                   : No

Menu
00:00:00.000                             : en:Italian Restaurant
00:12:34.000                             : en:Japanese Garden
[/code]`

	got := ParseMediaInfo(sample, true)
	if !slices.Equal(got.AudioLanguages, []string{"en", "fr"}) {
		t.Fatalf("AudioLanguages = %v, want [en fr] (commentary German and Menu must be excluded)", got.AudioLanguages)
	}
	if !slices.Equal(got.SubtitleLanguages, []string{"fr", "en"}) {
		t.Fatalf("SubtitleLanguages = %v, want [fr en] (forced/signs tracks must be excluded)", got.SubtitleLanguages)
	}
	if got.DurationSeconds != 2*3600+14*60 {
		t.Fatalf("DurationSeconds = %d, want 8040", got.DurationSeconds)
	}
	if got.BitrateBPS != 55_900_000 {
		t.Fatalf("BitrateBPS = %d, want 55900000", got.BitrateBPS)
	}
	if got.Resolution != "2160p" {
		t.Fatalf("Resolution = %q, want 2160p (CinemaScope 3840x1600)", got.Resolution)
	}
	if got.VideoCodec != "HEVC" {
		t.Fatalf("VideoCodec = %q, want HEVC", got.VideoCodec)
	}
	if got.HDR != "DV HDR10" {
		t.Fatalf("HDR = %q, want 'DV HDR10'", got.HDR)
	}
	if got.AudioCodec != "TRUEHD ATMOS" || got.AudioChannels != "7.1" {
		t.Fatalf("Audio = %q / %q, want TRUEHD ATMOS / 7.1", got.AudioCodec, got.AudioChannels)
	}
}

func TestParseMediaInfoLocalizedFrenchGermanRussian(t *testing.T) {
	french := `│ Général                                                                  │
│ Nom complet                              : Film.2024.1080p.mkv          │
│ Durée                                    : 1 h 48 min 30 s              │
│ Débit global moyen                       : 12 400 kb/s                  │
│ Vidéo                                                                    │
│ Format                                   : AVC                          │
│ Largeur                                  : 1 920 pixels                 │
│ Hauteur                                  : 800 pixels                   │
│ Audio #1                                                                 │
│ Format                                   : E-AC-3 JOC                   │
│ Canaux                                   : 6 canaux                     │
│ Langue                                   : Français                     │
│ Titre                                    : VFF                          │
│ Audio #2                                                                 │
│ Format                                   : E-AC-3                       │
│ Canaux                                   : 6 canaux                     │
│ Titre                                    : Japonais 5.1                 │
│ Texte #1                                                                 │
│ Langue                                   : Français                     │
│ Forcé                                    : Oui                          │
│ Texte #2                                                                 │
│ Langue                                   : Anglais                      │
│ Forcé                                    : Non                          │`

	gotFR := ParseMediaInfo(french, false)
	if !slices.Equal(gotFR.AudioLanguages, []string{"fr", "ja"}) {
		t.Fatalf("French MediaInfo AudioLanguages = %v, want [fr ja]", gotFR.AudioLanguages)
	}
	if !slices.Equal(gotFR.SubtitleLanguages, []string{"en"}) {
		t.Fatalf("French MediaInfo SubtitleLanguages = %v, want [en] (forced FR skipped)", gotFR.SubtitleLanguages)
	}
	if gotFR.DurationSeconds != 1*3600+48*60+30 || gotFR.BitrateBPS != 12_400_000 {
		t.Fatalf("French MediaInfo duration/bitrate = %d / %d", gotFR.DurationSeconds, gotFR.BitrateBPS)
	}
	if gotFR.Resolution != "1080p" || gotFR.VideoCodec != "H264" || gotFR.AudioCodec != "DDP ATMOS" || gotFR.AudioChannels != "5.1" {
		t.Fatalf("French MediaInfo tech = %+v", gotFR)
	}

	german := `Allgemein
Dauer                                    : 52 min 10 s
Audio #1
Format                                   : DTS XLL
Kanäle                                   : 6 Kanäle
Sprache                                  : Deutsch
Audio #2
Format                                   : AC-3
Kanäle                                   : 2 Kanäle
Sprache                                  : Englisch
Titel                                    : Audiokommentar
Text #1
Sprache                                  : Deutsch
Erzwungen                                : Ja
Text #2
Sprache                                  : Deutsch
Erzwungen                                : Nein`

	gotDE := ParseMediaInfo(german, true)
	if !slices.Equal(gotDE.AudioLanguages, []string{"de"}) {
		t.Fatalf("German MediaInfo AudioLanguages = %v, want [de]", gotDE.AudioLanguages)
	}
	if !slices.Equal(gotDE.SubtitleLanguages, []string{"de"}) {
		t.Fatalf("German MediaInfo SubtitleLanguages = %v, want [de]", gotDE.SubtitleLanguages)
	}
	if gotDE.AudioCodec != "DTS-HD MA" || gotDE.DurationSeconds != 52*60+10 {
		t.Fatalf("German MediaInfo tech = %+v", gotDE)
	}

	russian := `Общее
Продолжительность                        : 1 ч 30 мин
Видео
Формат                                   : HEVC
Ширина                                   : 1920 пикселей
Высота                                   : 1080 пикселей
Аудио #1
Формат                                   : AC-3
Каналы                                   : 6 каналов
Язык                                     : Русский
Аудио #2
Формат                                   : AC-3
Каналы                                   : 6 каналов
Язык                                     : Корейский
Текст #1
Язык                                     : Украинский`

	gotRU := ParseMediaInfo(russian, true)
	if !slices.Equal(gotRU.AudioLanguages, []string{"ru", "ko"}) || !slices.Equal(gotRU.SubtitleLanguages, []string{"uk"}) {
		t.Fatalf("Russian MediaInfo = %+v", gotRU)
	}
	if gotRU.DurationSeconds != 90*60 || gotRU.Resolution != "1080p" || gotRU.VideoCodec != "HEVC" || gotRU.AudioCodec != "AC3" || gotRU.AudioChannels != "5.1" {
		t.Fatalf("Russian MediaInfo tech = %+v", gotRU)
	}
}

func TestParseMediaInfoJSONFormat(t *testing.T) {
	payload := `{
  "media": {
    "track": [
      {"@type": "General", "Duration": "6540.2", "OverallBitRate": "18500000"},
      {"@type": "Video", "Format": "HEVC", "Width": "3840", "Height": "2160", "HDR_Format": "SMPTE ST 2094 App 4", "HDR_Format_Compatibility": "HDR10+"},
      {"@type": "Audio", "Language": "ja", "Format": "FLAC", "Channels": "6"},
      {"@type": "Audio", "Language": "en", "Format": "AC-3", "Channels": "2", "ServiceKind": "CM"},
      {"@type": "Text", "Language": "fr-FR", "Forced": "Yes"},
      {"@type": "Text", "Language": "fr-FR", "Forced": "No"},
      {"@type": "Text", "Language": "pt-BR", "Forced": "No"}
    ]
  }
}`
	fields := map[string]any{"mediainfo": payload}
	supplementMediaInfo(fields)
	if got, _ := fields["audiolangs"].([]string); !slices.Equal(got, []string{"ja"}) {
		t.Fatalf("JSON MediaInfo audiolangs = %v, want [ja]", fields["audiolangs"])
	}
	if got, _ := fields["sublangs"].([]string); !slices.Equal(got, []string{"fr", "pt"}) {
		t.Fatalf("JSON MediaInfo sublangs = %v, want [fr pt]", fields["sublangs"])
	}
	if fields["duration"] != int64(6540) || fields["bitrate"] != int64(18500000) {
		t.Fatalf("JSON MediaInfo duration/bitrate = %v / %v", fields["duration"], fields["bitrate"])
	}
	if fields["resolution"] != "2160p" || fields["video_codec"] != "HEVC" || fields["hdr"] != "HDR10+" || fields["audio_codec"] != "FLAC" || fields["audio_channels"] != "5.1" {
		t.Fatalf("JSON MediaInfo technical fields = %+v", fields)
	}
}

func TestParseBDInfoText(t *testing.T) {
	sample := `DISC INFO:

Disc Title:     EXAMPLE_UHD_DISC
Disc Size:      66,234,567,890 bytes
Length:         2:05:10.500 (h:m:s.ms)
Total Bitrate:  64.25 Mbps

VIDEO:

Codec                   Bitrate             Description
-----                   -------             -----------
MPEG-H HEVC Video       48500 kbps          2160p / 23.976 fps / 16:9 / Main 10 @ Level 5.1 @ High / 10 bits / HDR10 / BT.2020
* MPEG-H HEVC Video     5120 kbps           1080p / 23.976 fps / 16:9 / Main 10 @ Level 5.1 @ High / 10 bits / Dolby Vision / BT.2020

AUDIO:

Codec                           Language        Bitrate         Description
-----                           --------        -------         -----------
Dolby TrueHD/Atmos Audio        English         4920 kbps       7.1 / 48 kHz / 4280 kbps / 24-bit
DTS-HD Master Audio             French          3890 kbps       5.1 / 48 kHz / 3890 kbps / 24-bit
Dolby Digital Audio             English         192 kbps        2.0 / 48 kHz / 192 kbps / Director's Commentary

SUBTITLES:

Codec                           Language        Bitrate         Description
-----                           --------        -------         -----------
Presentation Graphics           French          0.42 kbps       Forced
Presentation Graphics           French          28.15 kbps
Presentation Graphics           English         31.20 kbps`

	got := ParseMediaInfo(sample, true)
	if !slices.Equal(got.AudioLanguages, []string{"en", "fr"}) {
		t.Fatalf("BDInfo AudioLanguages = %v, want [en fr]", got.AudioLanguages)
	}
	if !slices.Equal(got.SubtitleLanguages, []string{"fr", "en"}) {
		t.Fatalf("BDInfo SubtitleLanguages = %v, want [fr en]", got.SubtitleLanguages)
	}
	if got.DurationSeconds != 2*3600+5*60+10 || got.BitrateBPS != 64_250_000 {
		t.Fatalf("BDInfo duration/bitrate = %d / %d", got.DurationSeconds, got.BitrateBPS)
	}
	if got.Resolution != "2160p" || got.VideoCodec != "HEVC" || got.HDR != "HDR10 DV" || got.AudioCodec != "TRUEHD ATMOS" || got.AudioChannels != "7.1" {
		t.Fatalf("BDInfo tech = %+v", got)
	}

	// BDInfo Quick Summary inline format (common on UNIT3D trackers).
	quick := `QUICK SUMMARY:
Disc Title: EXAMPLE_DISC
Length: 1:48:20.000
Total Bitrate: 55.10 Mbps
Video: MPEG-H HEVC Video / 46200 kbps / 2160p / 23.976 fps / HDR10
Audio: Japanese / DTS-HD Master Audio / 5.1 / 48 kHz / 3200 kbps
Audio: English / Dolby Digital Audio / 5.1 / 48 kHz / 640 kbps
Audio: English (Commentary) / Dolby Digital Audio / 2.0 / 48 kHz / 192 kbps
Subtitle: French (Forced) / 0.85 kbps
Subtitle: French / 24.10 kbps
Subtitle: Spanish / 21.50 kbps`
	gotQuick := ParseMediaInfo(quick, true)
	if !slices.Equal(gotQuick.AudioLanguages, []string{"ja", "en"}) || !slices.Equal(gotQuick.SubtitleLanguages, []string{"fr", "es"}) {
		t.Fatalf("BDInfo Quick Summary langs = %+v", gotQuick)
	}
	if gotQuick.DurationSeconds != 1*3600+48*60+20 || gotQuick.Resolution != "2160p" || gotQuick.AudioCodec != "DTS-HD MA" || gotQuick.AudioChannels != "5.1" || gotQuick.HDR != "HDR10" {
		t.Fatalf("BDInfo Quick Summary tech = %+v", gotQuick)
	}
}

func TestParseStructuredNFOAndRejectFreeFormDescriptionProse(t *testing.T) {
	nfo := `[b]Release[/b]: Example.Movie.2024
[b]Langues[/b]     : [color=#00ff00]Multi (Français et Anglais)[/color]
[b]Sous-titres[/b] : Français (Forcés), Français, Espagnol
[b]Durée[/b]       : 1h 58min`

	got := ParseMediaInfo(nfo, false)
	if !slices.Equal(got.AudioLanguages, []string{"fr", "en"}) {
		t.Fatalf("NFO AudioLanguages = %v, want [fr en]", got.AudioLanguages)
	}
	if !slices.Equal(got.SubtitleLanguages, []string{"fr", "es"}) {
		t.Fatalf("NFO SubtitleLanguages = %v, want [fr es]", got.SubtitleLanguages)
	}
	if got.DurationSeconds != 1*3600+58*60 {
		t.Fatalf("NFO DurationSeconds = %d, want 7080", got.DurationSeconds)
	}

	// VOSTFR inside an NFO Language line must map to subtitles, not French audio.
	vostNFO := `Langue : VOSTFR
Durée  : 45 min`
	gotVost := ParseMediaInfo(vostNFO, false)
	if len(gotVost.AudioLanguages) != 0 || !slices.Equal(gotVost.SubtitleLanguages, []string{"fr"}) {
		t.Fatalf("VOSTFR NFO leaked audio: %+v", gotVost)
	}

	// Free-form plot prose in description must NEVER trigger extraction.
	proseFields := map[string]any{
		"title":       "Example.Movie.2024.1080p.WEB-DL",
		"description": "Synopsis:\nAudio: An emotional story set in French and German borders.\nDuration: 2 hours of suspense.\nLanguage: Spanish passion meets Italian cinema.",
	}
	supplementMediaInfo(proseFields)
	if proseFields["audiolangs"] != nil || proseFields["sublangs"] != nil || proseFields["duration"] != nil {
		t.Fatalf("description prose must never produce fields: %+v", proseFields)
	}
}

func TestOpportunisticExtractionThroughFieldsFromJSONAndCompactStorage(t *testing.T) {
	nfo := "\x00General\nDuration : 1 h 45 min\nOverall bit rate : 9 500 kb/s\n\nVideo\nFormat : HEVC\nWidth : 1 920 pixels\nHeight : 1 080 pixels\nHDR format : HDR10\n\nAudio #1\nFormat : E-AC-3\nChannel(s) : 6 channels\nLanguage : French\n\nAudio #2\nFormat : E-AC-3\nChannel(s) : 6 channels\nLanguage : English\n\nText #1\nLanguage : French\nForced : No\n"

	// 1. Metadata detail object with binary nfoContent (existing mapping without config changes).
	rawDetail, err := json.Marshal(map[string]any{
		"id":       "42",
		"name":     "Example.2024.MULTI.1080p.WEB-DL",
		"infoHash": strings.Repeat("a", 40),
		"metadata": map[string]any{"nfoContent": nfo},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, fields, err := FieldsFromJSON(rawDetail, model.Mapping{
		ID: "/id",
		Fields: map[string]string{
			"title":     "/name",
			"info_hash": "/infoHash",
			"metadata":  "/metadata",
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	compact := model.StoredTorrentFields(fields, model.StorageCompact)
	if got, _ := compact["audiolangs"].([]string); !slices.Equal(got, []string{"fr", "en"}) {
		t.Fatalf("compact audiolangs from metadata.nfoContent = %v, want [fr en]", compact["audiolangs"])
	}
	if got, _ := compact["sublangs"].([]string); !slices.Equal(got, []string{"fr"}) {
		t.Fatalf("compact sublangs from metadata.nfoContent = %v, want [fr]", compact["sublangs"])
	}
	if compact["duration"] != int64(1*3600+45*60) || compact["hdr"] != "HDR10" {
		t.Fatalf("compact technical fields missing: %+v", compact)
	}
	if meta, ok := compact["metadata"].(map[string]any); !ok || meta["nfoContent"] != nil {
		t.Fatalf("compact storage must drop raw nfoContent after extraction: %+v", compact["metadata"])
	}

	// 2. UNIT3D template payload with /attributes/media_info.
	var p model.Provider
	dec := json.NewDecoder(strings.NewReader(unit3dTemplate()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("unit3dTemplate invalid: %v", err)
	}
	rawUnit3D, err := json.Marshal(map[string]any{
		"id": "99",
		"attributes": map[string]any{
			"name":       "Example.2024.MULTI.1080p.WEB-DL",
			"info_hash":  strings.Repeat("b", 40),
			"size":       4_500_000_000,
			"resolution": "1080p",
			"media_info": nfo,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, uFields, err := FieldsFromJSON(rawUnit3D, p.Mapping, "")
	if err != nil {
		t.Fatal(err)
	}
	uCompact := model.StoredTorrentFields(uFields, model.StorageCompact)
	if got, _ := uCompact["audiolangs"].([]string); !slices.Equal(got, []string{"fr", "en"}) {
		t.Fatalf("UNIT3D compact audiolangs = %v, want [fr en]", uCompact["audiolangs"])
	}
	if uCompact["media_info"] != nil || uCompact["mediainfo"] != nil {
		t.Fatalf("UNIT3D compact storage must drop raw media_info: %+v", uCompact)
	}
}
