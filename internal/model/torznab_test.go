package model

import (
	"reflect"
	"testing"
)

func TestTorznabQueryCannotWidenConnectionCacheFilters(t *testing.T) {
	for _, test := range []struct {
		name  string
		query TorznabQuery
		saved TorznabPreferences
		want  []string
		cache bool
	}{
		{"all", TorznabQuery{}, TorznabPreferences{}, nil, false},
		{"saved service", TorznabQuery{}, TorznabPreferences{Services: []string{"torbox"}}, []string{"torbox"}, false},
		{"query narrows all", TorznabQuery{Services: []string{"alldebrid"}}, TorznabPreferences{}, []string{"alldebrid"}, false},
		{"intersect", TorznabQuery{Services: []string{"alldebrid", "torbox"}}, TorznabPreferences{Services: []string{"torbox"}}, []string{"torbox"}, false},
		{"disjoint stays empty", TorznabQuery{Services: []string{"alldebrid"}}, TorznabPreferences{Services: []string{"torbox"}, CachedOnly: true}, []string{}, true},
		{"empty stays empty", TorznabQuery{Services: []string{}}, TorznabPreferences{Services: []string{"torbox"}}, []string{}, false},
		{"cannot disable saved cache", TorznabQuery{}, TorznabPreferences{CachedOnly: true}, nil, true},
		{"query may require cache", TorznabQuery{CachedOnly: true}, TorznabPreferences{}, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := test.query.WithPreferences(test.saved)
			if !reflect.DeepEqual(got.Services, test.want) || got.CachedOnly != test.cache {
				t.Fatalf("query widened its cache permissions: %+v", got)
			}
		})
	}
}

func TestTorznabCachePreferencesRejectUnsupportedAndDuplicateServices(t *testing.T) {
	for _, services := range [][]string{{"direct"}, {"other"}, {"torbox", "torbox"}, {"alldebrid", "torbox", "alldebrid"}} {
		if (TorznabPreferences{Services: services}).Valid() {
			t.Fatal("invalid cache service selection accepted")
		}
	}
	for _, services := range [][]string{nil, {}, {"alldebrid"}, {"torbox"}, {"alldebrid", "torbox"}} {
		if !(TorznabPreferences{Services: services, CachedOnly: true}).Valid() {
			t.Fatal("valid cache service selection rejected")
		}
	}
}
