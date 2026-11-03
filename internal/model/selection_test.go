package model

import (
	"strings"
	"testing"
)

func TestSelectionsKeepSingleURLsAndRejectMalformedAlternatives(t *testing.T) {
	allowed := func(value string) bool { return value == "one" || value == "two" }
	for _, value := range []string{"", "one", "one,two", "one,one"} {
		if !ValidSelection(value, allowed) {
			t.Fatalf("rejected valid selection %q", value)
		}
	}
	for _, value := range []string{",one", "one,", "one,,two", "one,unknown", strings.Repeat("one,", 101) + "one"} {
		if ValidSelection(value, allowed) {
			t.Fatal("malformed selection accepted")
		}
	}
}
