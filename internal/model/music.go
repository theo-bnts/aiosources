package model

import (
	"strconv"
	"strings"
	"time"
)

// MusicInformation stores compact canonical MusicBrainz metadata for a release
// group alongside Cover Art Archive presence and extracted Discogs/Wikidata IDs.
type MusicInformation struct {
	YearVerified   bool     `json:"-"`
	ArtistCredit   string   `json:"artist_credit,omitempty"`
	Artists        []string `json:"artists,omitempty"`
	ArtistAliases  []string `json:"artist_aliases,omitempty"`
	PrimaryType    string   `json:"primary_type,omitempty"`
	SecondaryTypes []string `json:"secondary_types,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	CatalogNumbers []string `json:"catalog_numbers,omitempty"`
	Barcodes       []string `json:"barcodes,omitempty"`
	Formats        []string `json:"formats,omitempty"`
	TrackCounts    []int    `json:"track_counts,omitempty"`
	DiscogsIDs     []string `json:"discogs_ids,omitempty"`
	WikidataID     string   `json:"wikidata_id,omitempty"`
	Languages      []string `json:"languages,omitempty"`
	HasCover       bool     `json:"has_cover,omitempty"`
}

// MusicHints captures music-specific identifiers and source attributes from an
// imported torrent record without retaining raw source payloads.
type MusicHints struct {
	Kind          string   `json:"kind,omitempty"`
	MBID          string   `json:"mbid,omitempty"`
	ReleaseMBID   string   `json:"release_mbid,omitempty"`
	ArtistMBID    string   `json:"artist_mbid,omitempty"`
	DiscogsID     string   `json:"discogs_id,omitempty"`
	WikidataID    string   `json:"wikidata_id,omitempty"`
	Barcode       string   `json:"barcode,omitempty"`
	CatalogNumber string   `json:"catalog_number,omitempty"`
	Label         string   `json:"label,omitempty"`
	Artist        string   `json:"artist,omitempty"`
	Album         string   `json:"album,omitempty"`
	Format        string   `json:"format,omitempty"`
	Categories    []int    `json:"categories,omitempty"`
	CategoryNames []string `json:"category_names,omitempty"`
}

// MusicStatus reports local MusicBrainz + Cover Art Archive synchronization
// progress, Live Data Feed sequence cursors, and automatic recovery state.
type MusicStatus struct {
	Selection           string     `json:"-"`
	Generation          int64      `json:"-"`
	Running             bool       `json:"running"`
	Complete            bool       `json:"complete"`
	Works               int64      `json:"works"`
	Releases            int64      `json:"releases"`
	Artists             int64      `json:"artists"`
	Labels              int64      `json:"labels"`
	Covers              int64      `json:"covers"`
	Imported            int64      `json:"imported"`
	Position            int64      `json:"position"`
	Filtered            int64      `json:"filtered"`
	Skipped             int64      `json:"skipped"`
	Phase               string     `json:"phase"`
	Snapshot            string     `json:"snapshot,omitempty"`
	SnapshotAt          *time.Time `json:"snapshot_at"`
	LastImport          *time.Time `json:"last_import"`
	LastSync            *time.Time `json:"last_sync"`
	ChangesThrough      *time.Time `json:"changes_through"`
	LastPacketAt        *time.Time `json:"last_packet_at"`
	NextUpdate          *time.Time `json:"next_update"`
	RetryAt             *time.Time `json:"retry_at"`
	ReplicationSequence int64      `json:"replication_sequence"`
	SchemaSequence      int        `json:"schema_sequence"`
	LiveEnabled         bool       `json:"live_enabled"`
	NeedsExport         bool       `json:"needs_export"`
	RecoveryReason      string     `json:"recovery_reason,omitempty"`
	SchemaUnsupported   bool       `json:"schema_unsupported"`
	Error               string     `json:"error,omitempty"`
}

// MusicSourceHints reads only music metadata fields understood by the matcher
// and identification review UI.
func MusicSourceHints(fields map[string]any) MusicHints {
	b := BookSourceHints(fields)
	h := MusicHints{
		Categories:    b.Categories,
		CategoryNames: b.CategoryNames,
	}
	text := func(v any) string {
		switch value := v.(type) {
		case string:
			return strings.TrimSpace(value)
		case int:
			return strconv.Itoa(value)
		case int64:
			return strconv.FormatInt(value, 10)
		case float64:
			return strconv.FormatFloat(value, 'f', -1, 64)
		case []string:
			if len(value) > 0 {
				return strings.TrimSpace(value[0])
			}
		case []any:
			if len(value) > 0 {
				if s, ok := value[0].(string); ok {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}
	attrs := storageObject(fields["attributes"])
	ext := storageObject(fields["external_ids"])
	first := func(keys ...string) string {
		for _, key := range keys {
			if s := text(fields[key]); s != "" && len(s) <= 500 {
				return s
			}
		}
		if ext != nil {
			for _, key := range keys {
				if s := text(ext[key]); s != "" && len(s) <= 500 {
					return s
				}
			}
		}
		if attrs != nil {
			for _, key := range keys {
				if s := text(attrs[key]); s != "" && len(s) <= 500 {
					return s
				}
			}
		}
		return ""
	}
	h.MBID = first("musicbrainz_release_group_id", "musicbrainz_id", "musicbrainz", "mbid")
	h.ReleaseMBID = first("musicbrainz_release_id", "release_mbid")
	h.ArtistMBID = first("musicbrainz_artist_id", "artist_mbid")
	h.DiscogsID = first("discogs_master_id", "discogs_id", "discogs", "discogs_release_id")
	h.WikidataID = first("wikidata_id", "wikidata")
	h.Barcode = first("barcode", "upc", "ean")
	h.CatalogNumber = first("catalog_number", "catno")
	h.Label = first("label", "record_label")
	h.Artist = first("artist", "artists", "album_artist")
	h.Album = first("album", "album_title")
	h.Format = first("music_format", "audio_format", "format")

	var names func(any)
	names = func(value any) {
		if len(h.CategoryNames) >= 64 {
			return
		}
		switch v := value.(type) {
		case string:
			if _, err := strconv.Atoi(strings.TrimSpace(v)); err != nil && len(v) <= 200 {
				h.CategoryNames = append(h.CategoryNames, v)
			}
		case []any:
			for _, item := range v[:min(64, len(v))] {
				names(item)
			}
		case []string:
			for _, item := range v[:min(64, len(v))] {
				names(item)
			}
		}
	}
	names(fields["category"])
	names(fields["categories"])
	if attrs != nil {
		names(attrs["category"])
		names(attrs["categories"])
		extra := BookSourceHints(map[string]any{"category": attrs["category"], "categories": attrs["categories"]})
		h.Categories = append(h.Categories, extra.Categories...)
	}
	hasAudiobook := false
	for _, c := range h.Categories {
		switch c {
		case 3030:
			hasAudiobook = true
		case 3040:
			h.Kind = "lossless"
		case 3010:
			if h.Kind != "lossless" {
				h.Kind = "mp3"
			}
		case 3000, 3050:
			if h.Kind == "" {
				h.Kind = "music"
			}
		}
	}
	if !hasAudiobook && h.Kind == "" {
		switch strings.ToLower(first("type", "media_type")) {
		case "music", "audio", "album":
			h.Kind = "music"
		case "lossless", "flac":
			h.Kind = "lossless"
		case "mp3":
			h.Kind = "mp3"
		}
	}
	if hasAudiobook {
		h.Kind = ""
	}
	return h
}
