package books

import "testing"

func TestISBNValidation(t *testing.T) {
	for _, raw := range []string{"9780306406157", "978-0-306-40615-7", "0306406152"} {
		if got := ISBN(raw); got != "9780306406157" {
			t.Fatalf("ISBN normalization: %q => %q", raw, got)
		}
	}
	for _, raw := range []string{"9780306406158", "1234567890123", "0306406153", "030640615X", "https://example.invalid/9780306406157", "0000000000000"} {
		if ISBN(raw) != "" {
			t.Errorf("invalid ISBN accepted: %q", raw)
		}
	}
}

func TestISBNBeforeTheFileFormatIsNotMistakenForABookCollection(t *testing.T) {
	for _, title := range []string{"Opaque ISBN 9781861972712 EPUB", "Opaque 978-1-86197-271-2 EPUB", "Opaque 1861972717 EPUB"} {
		release := Parse(title, Hints{})
		if release.ISBN != "9781861972712" || release.Collection {
			t.Fatal("one checksum-verified book was treated as a collection", title, release)
		}
	}
	if release := Parse("2 Books ISBN 9781861972712 EPUB", Hints{}); !release.Collection {
		t.Fatal("an explicit collection lost its review requirement", release)
	}
}

func TestBookReleaseEvidence(t *testing.T) {
	for _, tc := range []struct {
		raw                 string
		hints               Hints
		kind, title, author string
	}{
		{"Morgan Vale - The Amber Orchard.epub", Hints{}, "ebook", "The Amber Orchard", "Morgan Vale"},
		{"Morgan.Vale.-.The.Amber.Orchard.2024.EPUB", Hints{}, "ebook", "The Amber Orchard", "Morgan Vale"},
		{"The Amber Orchard - Morgan Vale [Audiobook] [MP3]", Hints{}, "audiobook", "Morgan Vale", "The Amber Orchard"},
		{"Morgan Vale - 1984 EPUB", Hints{}, "ebook", "1984", "Morgan Vale"},
		{"The Amber Orchard", Hints{Categories: []int{7020}, Author: "Morgan Vale"}, "ebook", "The Amber Orchard", "Morgan Vale"},
		{"Morgan Vale - The Amber Orchard.m4b", Hints{}, "audiobook", "The Amber Orchard", "Morgan Vale"},
		{"Example.Movie.2024.1080p.WEB.H264.mkv", Hints{}, "", "", ""},
		{"Example.Series.S01E02.1080p.WEB.mkv", Hints{Categories: []int{7000}}, "", "", ""},
		{"Example Album 2024 MP3", Hints{}, "", "", ""},
		{"Example Movie", Hints{}, "", "", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			p := Parse(tc.raw, tc.hints)
			if p.Kind != tc.kind || p.Title != tc.title || p.Author != tc.author {
				t.Fatalf("unexpected parse: %+v", p)
			}
		})
	}
}

func TestBookCollectionAndNumbers(t *testing.T) {
	if p := Parse("Morgan Vale - Amber Tales Tomes 1-4 EPUB", Hints{}); !p.Collection {
		t.Fatal("collection was treated as one book")
	}
	if p := Parse("Morgan Vale - The Amber Orchard Volume 2 EPUB", Hints{}); p.Title != "The Amber Orchard Volume 2" {
		t.Fatalf("volume evidence lost: %+v", p)
	}
}

func BenchmarkBookRelease(b *testing.B) {
	for b.Loop() {
		Parse("Morgan.Vale.-.The.Amber.Orchard.2024.FRENCH.EPUB", Hints{})
	}
}

func TestBookTitleWordsAndSoftwareAreNotDiscarded(t *testing.T) {
	for _, raw := range []string{"Morgan Vale - Complete Guide to English EPUB", "Morgan Vale - Learning French EPUB", "Morgan Vale - Understanding PDF EPUB", "Morgan Vale - Annual Review 2024 FR EPUB"} {
		p := Parse(raw, Hints{})
		if p.Kind != "ebook" || p.Collection || p.Title == "" {
			t.Fatalf("book title lost: %+v", p)
		}
	}
	for _, raw := range []string{"Example PDF Editor 2024 Windows x64", "Example.Movie.2024.1080p.WEB.mkv", "Example.Game.v1.2.34567890"} {
		if p := Parse(raw, Hints{}); p.Kind != "" {
			t.Fatalf("non-book classified as a book: %+v", p)
		}
	}
	if p := Parse("Example Software v1.234.567.890 PDF", Hints{}); p.ISBN != "" {
		t.Fatal("version number became an ISBN")
	}
}

func TestBookParsingDoesNotMutateSharedEvidence(t *testing.T) {
	backing := []string{"9780306406157", "unchanged"}
	Parse("The Amber Orchard EPUB", Hints{ISBNs: backing[:1], ISBN: "9780306406157"})
	if backing[1] != "unchanged" {
		t.Fatal("parser mutated the source evidence backing array")
	}
}
