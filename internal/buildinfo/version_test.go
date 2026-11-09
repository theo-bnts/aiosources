package buildinfo

import "testing"

func TestBuildDisplayIdentifiesTheProductAndPublishedRelease(t *testing.T) {
	for _, tc := range []struct{ input, expected string }{
		{"977dd1727684afebe3686df08af69102fc7661ed", "2.3.0.977dd1727684"},
		{"977dd1727684", "2.3.0.977dd1727684"},
		{"2.3.0", "2.3.0"},
		{"2.3.0-custom", "2.3.0-custom"},
		{"", "2.3.0.dev"},
		{"dev", "2.3.0.dev"},
		{"not-a-revision", "not-a-revision"},
	} {
		if got := Display(tc.input); got != tc.expected {
			t.Errorf("display %q = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
