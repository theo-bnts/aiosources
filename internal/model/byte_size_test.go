package model

import (
	"math"
	"strings"
	"testing"
)

func TestByteSizesStayExactAcrossProviderAndSourceFormats(t *testing.T) {
	for _, tc := range []struct {
		value string
		bytes int64
	}{
		{"1.5 GB", 1500000000},
		{"1,5 GiB", 1610612736},
		{"1.5G", 1610612736},
		{"1 234 567", 1234567},
		{"1\u202f234\u202f567", 1234567},
		{"9223372036854775807", math.MaxInt64},
		{"0 B", 0},
	} {
		got, err := ParseByteSize(tc.value)
		if err != nil || got != tc.bytes {
			t.Fatalf("%q: got %d (%v), want %d", tc.value, got, err, tc.bytes)
		}
	}
	for _, value := range []string{"-1", "NaN GB", "9223372036854775808", "10 EB", strings.Repeat("1", 257) + " GB"} {
		if _, err := ParseByteSize(value); err == nil {
			t.Fatalf("invalid or excessive size accepted: %q", value)
		}
	}
}
