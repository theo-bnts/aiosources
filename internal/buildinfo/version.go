// Package buildinfo identifies the product release and its exact published build.
package buildinfo

const Release = "2.4.0"

// Display preserves explicit local version overrides. Actions supplies the full
// Git revision; the same twelve characters identify its GitHub pre-release.
func Display(build string) string {
	if len(build) == 40 || len(build) == 12 {
		hex := true
		for _, c := range build {
			hex = hex && (c >= '0' && c <= '9' || c >= 'a' && c <= 'f')
		}
		if hex {
			return Release + "." + build[:12]
		}
	}
	if build == "" || build == "dev" {
		return Release + ".dev"
	}
	return build
}
