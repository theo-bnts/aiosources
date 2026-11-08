package model

import (
	"encoding/json"
	"testing"
)

func TestVideoTypeEvidenceUsesOnlyUnambiguousStandards(t *testing.T) {
	for _, tc := range []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"categories": []any{json.Number("5070")}}, "tv"},
		{map[string]any{"categories": []int{5070}}, "tv"},
		{map[string]any{"categories": []any{"2000", "2040"}}, "movie"},
		{map[string]any{"categories": []any{"2000", "5000"}}, "conflict"},
		{map[string]any{"categories": []any{"1", "100005"}}, ""},
		{map[string]any{"media_type": "anime"}, ""},
		{map[string]any{"media_type": "movie", "categories": []any{"5070"}}, "conflict"},
		{map[string]any{"attributes": map[string]any{"category": []any{"5000"}}}, "tv"},
		{map[string]any{"attributes": map[string][]string{"category": {"5000"}}}, "tv"},
	} {
		if got := VideoTypeEvidence(tc.fields); got != tc.want {
			t.Fatalf("type %q, wanted %q", got, tc.want)
		}
	}
}
