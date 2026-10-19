package scheduling

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotEqualityKeepsExactNumbersAndStructuralDifferences(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{`{"ratio":1.2300e+2,"id":9007199254740993}`, `{"id":9007199254740993,"ratio":123.00}`, true},
		{`[-0,1E-3,1000000]`, `[0,0.001000,1e6]`, true},
		{`{"id":9007199254740993}`, `{"id":9007199254740992}`, false},
		{`{"id":1}`, `{"id":"1"}`, false},
		{`{"values":[1,2]}`, `{"values":[2,1]}`, false},
		{`{"body":{}}`, `{}`, false},
		{`{"body":null}`, `{"body":{}}`, false},
		{`1e99999999999999999999999999`, `1`, false},
	} {
		decode := func(raw string) any {
			t.Helper()
			var value any
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		if got := equalSnapshotJSON(decode(tc.a), decode(tc.b)); got != tc.same {
			t.Fatalf("snapshot equality = %v, want %v for %s and %s", got, tc.same, tc.a, tc.b)
		}
	}
}
