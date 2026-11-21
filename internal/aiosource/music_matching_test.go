package aiosource

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMusicIdentificationMatchesIdentifiersArtistAlbumAndRejectsConflicts(t *testing.T) {
	rgID := "11111111-2222-3333-4444-555555555555"
	work := model.AIOWork{
		ID:           10,
		Type:         "music",
		MusicBrainz:  rgID,
		DisplayTitle: "Neon Horizon",
		Titles:       []string{"Neon Horizon"},
		Years:        []int{2024},
		Music: model.MusicInformation{
			PrimaryType:    "Album",
			Artists:        []string{"Synthetic Duo"},
			ArtistAliases:  []string{"Synth Duo"},
			Labels:         []string{"Orbit Records"},
			CatalogNumbers: []string{"ORB-042"},
			Barcodes:       []string{"0724349691728"},
			DiscogsIDs:     []string{"master:26647", "release:99001"},
			WikidataID:     "Q43228",
			HasCover:       true,
		},
	}

	for _, tc := range []struct {
		name   string
		record model.AIORecord
		works  []model.AIOWork
		want   string
	}{
		{
			name:   "exact artist album and year",
			record: model.AIORecord{Title: "Synthetic Duo - Neon Horizon (2024) [FLAC]", MusicHints: model.MusicHints{Categories: []int{3040}}},
			works:  []model.AIOWork{work},
			want:   "matched",
		},
		{
			name:   "verified MusicBrainz ID",
			record: model.AIORecord{Title: "Unknown Release [FLAC]", MusicBrainz: rgID, MusicHints: model.MusicHints{Kind: "lossless", MBID: rgID}},
			works:  []model.AIOWork{work},
			want:   "matched",
		},
		{
			name:   "verified Discogs master ID",
			record: model.AIORecord{Title: "Neon Horizon [FLAC]", MusicHints: model.MusicHints{Kind: "lossless", DiscogsID: "m26647"}},
			works:  []model.AIOWork{work},
			want:   "matched",
		},
		{
			name:   "verified Wikidata ID",
			record: model.AIORecord{Title: "Neon Horizon [MP3]", MusicHints: model.MusicHints{Kind: "mp3", WikidataID: "Q43228"}},
			works:  []model.AIOWork{work},
			want:   "matched",
		},
		{
			name:   "verified barcode",
			record: model.AIORecord{Title: "Neon Horizon [FLAC]", MusicHints: model.MusicHints{Kind: "lossless", Barcode: "0724349691728"}},
			works:  []model.AIOWork{work},
			want:   "matched",
		},
		{
			name:   "verified catalog number with matching artist",
			record: model.AIORecord{Title: "Synthetic Duo - Neon Horizon [FLAC]", MusicHints: model.MusicHints{Kind: "lossless", CatalogNumber: "ORB 042"}},
			works:  []model.AIOWork{work},
			want:   "matched",
		},
		{
			name:   "catalog number contradicted by different artist",
			record: model.AIORecord{Title: "Other Band - Neon Horizon [FLAC]", MusicHints: model.MusicHints{Kind: "lossless", CatalogNumber: "ORB-042"}},
			works:  []model.AIOWork{work},
			want:   "unmatched",
		},
		{
			name:   "wrong artist rejected without strong ID",
			record: model.AIORecord{Title: "Different Artist - Neon Horizon (2024) [FLAC]", MusicHints: model.MusicHints{Categories: []int{3040}}},
			works:  []model.AIOWork{work},
			want:   "unmatched",
		},
		{
			name:   "missing artist without strong ID never guesses homonyms",
			record: model.AIORecord{Title: "Neon Horizon (2024) [FLAC]", MusicHints: model.MusicHints{Categories: []int{3040}}},
			works:  []model.AIOWork{work},
			want:   "unmatched",
		},
		{
			name:   "discography collection routed to review",
			record: model.AIORecord{Title: "Synthetic Duo - Complete Discography (2015-2024) [FLAC]", MusicHints: model.MusicHints{Categories: []int{3040}}},
			works:  []model.AIOWork{work},
			want:   "unmatched",
		},
		{
			name:   "homonymous albums by same artist flagged ambiguous",
			record: model.AIORecord{Title: "Synthetic Duo - Neon Horizon [FLAC]", MusicHints: model.MusicHints{Categories: []int{3040}}},
			works: []model.AIOWork{
				work,
				{ID: 11, Type: "music", MusicBrainz: "22222222-2222-3333-4444-555555555555", DisplayTitle: "Neon Horizon", Titles: []string{"Neon Horizon"}, Years: []int{2022}, Music: model.MusicInformation{PrimaryType: "EP", Artists: []string{"Synthetic Duo"}}},
			},
			want: "ambiguous",
		},
		{
			name:   "music information pending waits without guessing",
			record: model.AIORecord{Title: "Synthetic Duo - Neon Horizon (2024) [FLAC]", MusicInformationPending: true, MusicHints: model.MusicHints{Categories: []int{3040}}},
			works:  []model.AIOWork{work},
			want:   "unmatched",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed := ParseMusicRelease(tc.record)
			res, err := decideMusic(tc.record, parsed, func(filter model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
				if filter.Type != "music" {
					t.Fatalf("expected music filter type, got %q", filter.Type)
				}
				return tc.works, false, nil
			})
			if err != nil || res.Status != tc.want {
				t.Fatalf("status=%q want=%q reason=%q err=%v parsed=%+v", res.Status, tc.want, res.Reason, err, parsed)
			}
		})
	}
}

func TestMusicKeepsAudiobooksVideosAndAudioFileSelectionSeparate(t *testing.T) {
	// Audiobooks (3030) and videos must never be parsed as music.
	for _, rec := range []model.AIORecord{
		{Title: "Morgan Vale - The Amber Orchard [MP3 Audiobook]", MusicHints: model.MusicHints{Categories: []int{3030}}},
		{Title: "Example.Concert.2024.1080p.BluRay.x264.FLAC", VideoType: "movie"},
		{Title: "Synthetic.Odyssey.PC.ISO", GameHints: model.GameHints{Kind: "game"}},
	} {
		if parsed := ParseMusicRelease(rec); parsed.Kind != "" {
			t.Fatalf("non-music release parsed as music: %q -> %+v", rec.Title, parsed)
		}
	}

	// Music resource ID parsing & multi-track selection.
	res, err := ParseID("musicbrainz:11111111-2222-3333-4444-555555555555", "music")
	if err != nil || res.Kind != "musicbrainz" || res.ID != "11111111-2222-3333-4444-555555555555" || res.Type != "music" {
		t.Fatalf("ParseID music failed: %+v %v", res, err)
	}
	if _, err = ParseID("musicbrainz:11111111-2222-3333-4444-555555555555:1:2", "music"); err == nil {
		t.Fatal("music resource accepted episode numbering")
	}
	if _, err = ParseID("musicbrainz:not-a-uuid", "music"); err == nil {
		t.Fatal("music resource accepted invalid UUID")
	}

	singleTrack := []model.AIOFile{
		{Path: "01 - Opening.flac", Size: 40_000_000},
		{Path: "cover.jpg", Size: 200_000},
		{Path: "album.log", Size: 2_000},
	}
	chosen, ok := SelectFile(singleTrack, res)
	if !ok || chosen.Path != "01 - Opening.flac" {
		t.Fatalf("single audio track was not selected: %+v %v", chosen, ok)
	}
	multiTrack := append(singleTrack, model.AIOFile{Path: "02 - Horizon.flac", Size: 42_000_000})
	if _, ok = SelectFile(multiTrack, res); ok {
		t.Fatal("multi-track album silently chose one track instead of prompting")
	}
}
