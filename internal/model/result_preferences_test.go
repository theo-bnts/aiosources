package model

import "testing"

func TestResultPreferenceBoundsAndDistinctPriorities(t *testing.T) {
	for _, p := range []ResultPreferences{
		{}, {Sorting: []ResultSort{{"cached", "desc"}, {"language", "desc"}, {"size", "asc"}}, MaxResults: 80, ResponseSeconds: 60},
	} {
		if !p.Valid(true) {
			t.Fatalf("rejected valid preferences: %+v", p)
		}
	}
	for _, p := range []ResultPreferences{
		{MaxResults: -1}, {MaxResults: 10001}, {ResponseSeconds: 601},
		{Sorting: []ResultSort{{"cached", "desc"}, {"cached", "asc"}}},
		{Sorting: []ResultSort{{"age", "asc"}, {"published", "desc"}}},
		{Sorting: []ResultSort{{"size", "DESC"}}}, {Sorting: []ResultSort{{"size; DROP TABLE torrents", "asc"}}},
		{Sorting: []ResultSort{{"cached", "desc"}, {"size", "asc"}, {"name", "asc"}, {"age", "asc"}}},
	} {
		if p.Valid(true) {
			t.Fatalf("accepted invalid preferences: %+v", p)
		}
	}
	if (ResultPreferences{Sorting: []ResultSort{{"language", "desc"}}}).Valid(false) {
		t.Fatal("Torznab accepted unsupported language ordering")
	}
	if ValidStreamPreferences(StreamPreferences{ResultPreferences: ResultPreferences{ResponseSeconds: 61}}) {
		t.Fatal("stream availability wait escaped its bound")
	}
	if q := (TorznabQuery{CachedOnly: true}).WithPreferences(TorznabPreferences{ResultPreferences: ResultPreferences{MaxResults: 80, MaxCached: 20}}); q.MaxResults != 20 {
		t.Fatal("Torznab cached-only did not use its cached limit", q)
	}
}
