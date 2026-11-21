package software

import (
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/theo-bnts/aiosources/internal/model"
)

var (
	softwareKeyRE  = regexp.MustCompile(`^(winget|brew|cpe):[a-z0-9][a-z0-9._:-]{0,190}$`)
	iconRefRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
	wingetIDRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}\.[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	homebrewCaskRE = regexp.MustCompile(`^[a-z0-9][a-z0-9@+._-]{0,127}$`)
	cpeTokenRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,127}$`)
	cpeTitleTailRE = regexp.MustCompile(`(?i)\s+(?:v?\d+(?:\.\d+){1,4}(?:[._-][a-z0-9]+)?|for\s+(?:windows|macos|mac\s+os\s+x?|os\s+x|linux|android|ios|iphone\s+os|ipados|ubuntu|debian|fedora|freebsd|unix)(?:\s+\S+)?)\s*$`)
	cpeVersionRE   = regexp.MustCompile(`(?i)\s+v?\d+(?:\.\d+){1,4}(?:[._-](?:alpha|beta|rc|sp|update|patch|build)\d*)?$`)
	verSegmentRE   = regexp.MustCompile(`[0-9]+|[A-Za-z]+`)
)

// DefaultPlatforms lists the default OS platforms included by Software catalog
// filters.
var DefaultPlatforms = []string{"windows", "macos", "linux", "android", "ios"}

// ValidPlatforms lists all supported canonical OS platform identifiers.
var ValidPlatforms = []string{"windows", "macos", "linux", "android", "ios"}

// ImportSelection configures which Software catalog sources, categories and OS
// platforms are projected into the local searchable catalog.
type ImportSelection struct {
	IncludeNVD     bool
	ExcludePlugins bool
	IncludeOS      bool
	IncludeIcons   bool
	Platforms      []string
}

// DefaultImportSelection returns the default Software catalog filter values.
func DefaultImportSelection() ImportSelection {
	return ImportSelection{
		IncludeNVD:     true,
		ExcludePlugins: true,
		IncludeOS:      false,
		IncludeIcons:   true,
		Platforms:      slices.Clone(DefaultPlatforms),
	}
}

// Key builds a deterministic fingerprint of the active Software catalog filter
// configuration so changes trigger automatic re-projection or re-import.
func (s ImportSelection) Key() string {
	platforms, ok := ImportPlatforms(s.Platforms)
	if !ok {
		platforms = slices.Clone(DefaultPlatforms)
		slices.Sort(platforms)
	}
	return "v1:nvd=" + strconv.FormatBool(s.IncludeNVD) +
		";noplugins=" + strconv.FormatBool(s.ExcludePlugins) +
		";os=" + strconv.FormatBool(s.IncludeOS) +
		";icons=" + strconv.FormatBool(s.IncludeIcons) +
		";platforms=" + strings.Join(platforms, ",")
}

// Normalize delegates to the shared catalog title normalizer.
func Normalize(s string) string {
	return model.NormalizeTitle(s)
}

// TitleScore computes normalized title similarity for software matching.
func TitleScore(a, b string) float64 {
	return model.TitleSimilarity(Normalize(a), Normalize(b))
}

// ValidSoftwareKey reports whether raw is a valid canonical software_key.
func ValidSoftwareKey(raw string) bool {
	return softwareKeyRE.MatchString(strings.ToLower(strings.TrimSpace(raw)))
}

// CanonicalSoftwareKey validates and lowercases a canonical software_key.
func CanonicalSoftwareKey(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if softwareKeyRE.MatchString(s) {
		return s
	}
	return ""
}

// ValidIconRef validates a selfh.st/icons PNG reference slug.
func ValidIconRef(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if iconRefRE.MatchString(s) {
		return s
	}
	return ""
}

// WinGetID validates a Microsoft WinGet PackageIdentifier (e.g. "VideoLAN.VLC").
func WinGetID(raw string) string {
	s := strings.TrimSpace(raw)
	if !wingetIDRE.MatchString(s) || strings.Contains(s, "..") {
		return ""
	}
	return s
}

// HomebrewCask validates and lowercases a Homebrew Cask token (e.g. "vlc").
func HomebrewCask(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if !homebrewCaskRE.MatchString(s) || strings.Contains(s, "..") {
		return ""
	}
	return s
}

// CPE23 holds parsed components of an NVD CPE 2.3 formatted string.
type CPE23 struct {
	Part     string // "a" (application), "o" (operating system), "h" (hardware)
	Vendor   string
	Product  string
	Version  string
	TargetSW string
	TargetHW string
	Language string
}

// CanonicalID returns the aggregated product-level CPE 2.3 identifier
// ("cpe:2.3:<part>:<vendor>:<product>").
func (c CPE23) CanonicalID() string {
	if (c.Part != "a" && c.Part != "o") || c.Vendor == "" || c.Product == "" {
		return ""
	}
	return "cpe:2.3:" + c.Part + ":" + c.Vendor + ":" + c.Product
}

// ParseCPE23 parses a full or aggregated CPE 2.3 URI while respecting CPE 2.3
// backslash escapes (e.g. `\:` inside tokens).
func ParseCPE23(raw string) (CPE23, bool) {
	s := strings.TrimSpace(raw)
	if len(s) < 12 || len(s) > 512 || !strings.HasPrefix(strings.ToLower(s), "cpe:2.3:") {
		return CPE23{}, false
	}
	fields := splitCPE23Fields(s)
	if len(fields) < 5 {
		return CPE23{}, false
	}
	part := strings.ToLower(strings.TrimSpace(fields[2]))
	if part != "a" && part != "o" && part != "h" {
		return CPE23{}, false
	}
	vendor := sanitizeCPEToken(fields[3])
	product := sanitizeCPEToken(fields[4])
	if vendor == "" || product == "" {
		return CPE23{}, false
	}
	out := CPE23{
		Part:    part,
		Vendor:  vendor,
		Product: product,
	}
	if len(fields) > 5 {
		out.Version = cpeOptionalField(fields[5])
	}
	if len(fields) > 9 {
		out.Language = cpeOptionalField(fields[9])
	}
	if len(fields) > 10 {
		out.TargetSW = cpeOptionalField(fields[10])
	}
	if len(fields) > 11 {
		out.TargetHW = cpeOptionalField(fields[11])
	}
	return out, true
}

// NVDCPE validates and canonicalizes an NVD CPE 2.3 product identifier
// ("cpe:2.3:<a|o>:<vendor>:<product>").
func NVDCPE(raw string) string {
	c, ok := ParseCPE23(raw)
	if !ok {
		return ""
	}
	return c.CanonicalID()
}

func splitCPE23Fields(s string) []string {
	var fields []string
	var cur strings.Builder
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			cur.WriteByte('\\')
			cur.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == ':' {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if escaped {
		cur.WriteByte('\\')
	}
	fields = append(fields, cur.String())
	return fields
}

func unescapeCPE23(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func sanitizeCPEToken(raw string) string {
	s := strings.ToLower(strings.TrimSpace(unescapeCPE23(raw)))
	if s == "" || s == "*" || s == "-" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '+':
			b.WriteRune(r)
		case r == ' ' || r == '/':
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._-+")
	if !cpeTokenRE.MatchString(out) {
		return ""
	}
	return out
}

func cpeOptionalField(raw string) string {
	s := strings.ToLower(strings.TrimSpace(unescapeCPE23(raw)))
	if s == "" || s == "*" || s == "-" {
		return ""
	}
	return s
}

// HumanizeCPEToken turns a snake_case CPE vendor or product token into a
// readable display label.
func HumanizeCPEToken(token string) string {
	s := strings.TrimSpace(unescapeCPE23(token))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		low := strings.ToLower(w)
		switch low {
		case "vlc", "obs", "gimp", "kde", "gnu", "ssh", "sql", "ftp", "vpn", "ide", "sdk", "cli", "gui", "pdf", "cad", "daw", "vst", "os", "ios", "macos", "ipados", "tvos", "watchos", "amd", "ibm", "sap", "hp", "dell", "cisco", "nvidia", "intel":
			switch low {
			case "ios":
				words[i] = "iOS"
			case "macos":
				words[i] = "macOS"
			case "ipados":
				words[i] = "iPadOS"
			case "tvos":
				words[i] = "tvOS"
			case "watchos":
				words[i] = "watchOS"
			case "dell":
				words[i] = "Dell"
			case "cisco":
				words[i] = "Cisco"
			case "nvidia":
				words[i] = "NVIDIA"
			case "intel":
				words[i] = "Intel"
			default:
				words[i] = strings.ToUpper(low)
			}
		default:
			runes := []rune(w)
			if len(runes) > 0 {
				runes[0] = unicode.ToUpper(runes[0])
			}
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

// CleanCPETitle extracts a clean product display name from an NVD CPE title by
// stripping version and target-OS suffixes while keeping product identity intact.
func CleanCPETitle(rawTitle, vendor, product string) string {
	title := strings.TrimSpace(rawTitle)
	if title != "" {
		for i := 0; i < 3; i++ {
			next := strings.TrimSpace(cpeTitleTailRE.ReplaceAllString(title, ""))
			next = strings.TrimSpace(cpeVersionRE.ReplaceAllString(next, ""))
			if next == title || len([]rune(next)) < 2 {
				break
			}
			title = next
		}
		title = strings.Trim(title, " -–—:,")
		if len([]rune(title)) >= 2 {
			return title
		}
	}
	v := HumanizeCPEToken(vendor)
	p := HumanizeCPEToken(product)
	if p == "" {
		return v
	}
	if v == "" || strings.HasPrefix(strings.ToLower(p), strings.ToLower(v)) {
		return p
	}
	return v + " " + p
}

// NormalizePlatform maps a raw OS or platform token onto one of the 5 canonical
// Software platforms ("windows", "macos", "linux", "android", "ios").
func NormalizePlatform(raw string) string {
	s := strings.ToLower(strings.TrimSpace(unescapeCPE23(raw)))
	s = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	switch s {
	case "windows", "win", "win32", "win64", "win x64", "win x86", "win arm64", "ms windows", "microsoft windows", "windows nt", "windows 10", "windows 11", "windows server":
		return "windows"
	case "macos", "mac os", "mac os x", "osx", "os x", "mac", "darwin", "apple macos", "apple mac os x":
		return "macos"
	case "linux", "gnu linux", "ubuntu", "debian", "fedora", "rhel", "red hat", "centos", "arch", "arch linux", "archlinux", "suse", "opensuse", "alpine", "mint", "linux mint", "manjaro", "gentoo", "kali", "nixos", "appimage", "deb", "rpm", "flatpak", "snap", "freebsd", "openbsd", "netbsd", "unix":
		return "linux"
	case "android", "apk", "google android":
		return "android"
	case "ios", "iphone os", "iphoneos", "ipados", "ipa", "apple ios", "apple iphone os":
		return "ios"
	}
	switch {
	case strings.HasPrefix(s, "windows ") || strings.HasPrefix(s, "win32") || strings.HasPrefix(s, "win64"):
		return "windows"
	case strings.HasPrefix(s, "macos ") || strings.HasPrefix(s, "mac os") || strings.HasPrefix(s, "osx ") || strings.HasPrefix(s, "os x "):
		return "macos"
	case strings.Contains(s, "linux") || strings.HasPrefix(s, "ubuntu ") || strings.HasPrefix(s, "debian ") || strings.HasPrefix(s, "fedora "):
		return "linux"
	case strings.HasPrefix(s, "android "):
		return "android"
	case strings.HasPrefix(s, "iphone os") || strings.HasPrefix(s, "ios ") || strings.HasPrefix(s, "ipados"):
		return "ios"
	}
	return ""
}

// ImportPlatforms validates and canonicalizes selected OS platforms.
func ImportPlatforms(values []string) ([]string, bool) {
	if len(values) == 0 || len(values) > len(ValidPlatforms) {
		return nil, false
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		p := NormalizePlatform(value)
		if p == "" {
			return nil, false
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out, true
}

// PlatformsFromCPE infers canonical OS platforms from a CPE entry's part,
// vendor, product, target_sw and title.
func PlatformsFromCPE(c CPE23, title string) []string {
	var out []string
	add := func(raw string) {
		if p := NormalizePlatform(raw); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	if c.TargetSW != "" {
		add(c.TargetSW)
	}
	if c.Part == "o" {
		add(c.Product)
		add(c.Vendor + " " + c.Product)
		switch c.Vendor {
		case "canonical", "debian", "fedoraproject", "redhat", "suse", "opensuse", "archlinux", "alpinelinux", "linux", "linuxmint", "gentoo", "kali", "nixos", "ubuntu", "freebsd", "openbsd", "netbsd":
			add("linux")
		case "microsoft":
			if strings.Contains(c.Product, "windows") {
				add("windows")
			}
		case "apple":
			switch {
			case strings.Contains(c.Product, "mac") || strings.Contains(c.Product, "os_x"):
				add("macos")
			case strings.Contains(c.Product, "iphone") || strings.Contains(c.Product, "ipad") || c.Product == "ios":
				add("ios")
			}
		case "google":
			if strings.Contains(c.Product, "android") {
				add("android")
			}
		}
	}
	lowTitle := strings.ToLower(title)
	if strings.Contains(lowTitle, " for windows") {
		add("windows")
	}
	if strings.Contains(lowTitle, " for macos") || strings.Contains(lowTitle, " for mac os") || strings.Contains(lowTitle, " for os x") {
		add("macos")
	}
	if strings.Contains(lowTitle, " for linux") {
		add("linux")
	}
	if strings.Contains(lowTitle, " for android") {
		add("android")
	}
	if strings.Contains(lowTitle, " for ios") || strings.Contains(lowTitle, " for iphone") || strings.Contains(lowTitle, " for ipados") {
		add("ios")
	}
	slices.Sort(out)
	return out
}

var pluginTargetSW = map[string]struct{}{
	"wordpress":          {},
	"drupal":             {},
	"joomla":             {},
	"joomla!":            {},
	"magento":            {},
	"prestashop":         {},
	"shopify":            {},
	"typo3":              {},
	"moodle":             {},
	"mediawiki":          {},
	"jenkins":            {},
	"confluence":         {},
	"jira":               {},
	"bitbucket":          {},
	"chrome":             {},
	"firefox":            {},
	"safari":             {},
	"edge":               {},
	"visual_studio_code": {},
	"vscode":             {},
	"eclipse":            {},
	"intellij":           {},
	"grafana":            {},
	"kibana":             {},
	"roundcube":          {},
	"nextcloud":          {},
	"owncloud":           {},
	"woocommerce":        {},
	"elementor":          {},
	"glpi":               {},
	"opencart":           {},
	"oscommerce":         {},
	"phpbb":              {},
	"vbulletin":          {},
	"mybb":               {},
	"discourse":          {},
	"craft_cms":          {},
	"node.js":            {},
	"nodejs":             {},
	"npm":                {},
	"python":             {},
	"pypi":               {},
	"ruby":               {},
	"rubygems":           {},
	"java":               {},
	"maven":              {},
	"gradle":             {},
	"php":                {},
	"composer":           {},
	"packagist":          {},
	"perl":               {},
	"cpan":               {},
	"rust":               {},
	"cargo":              {},
	"crates.io":          {},
	"go":                 {},
	"golang":             {},
	".net":               {},
	"nuget":              {},
	"dart":               {},
	"elixir":             {},
	"haskell":            {},
	"r":                  {},
	"swift":              {},
	"cocoapods":          {},
}

var pluginRefSnippets = []string{
	"wordpress.org/plugins",
	"wordpress.org/themes",
	"plugins.trac.wordpress.org",
	"themes.trac.wordpress.org",
	"drupal.org/project/",
	"extensions.joomla.org",
	"plugins.jenkins.io",
	"npmjs.com/package",
	"registry.npmjs.org",
	"pypi.org/project",
	"rubygems.org/gems",
	"mvnrepository.com",
	"search.maven.org",
	"packagist.org/packages",
	"crates.io/crates",
	"nuget.org/packages",
	"addons.mozilla.org",
	"chrome.google.com/webstore",
	"chromewebstore.google.com",
	"marketplace.visualstudio.com",
	"plugins.jetbrains.com",
	"pkg.go.dev",
	"metacpan.org",
}

var pluginTitleRE = regexp.MustCompile(`(?i)\b(?:plugin|theme|extension|module|add-?on|widget)\s+for\s+(?:wordpress|drupal|joomla|magento|prestashop|typo3|moodle|mediawiki|jenkins|confluence|jira|woocommerce|chrome|firefox|edge|safari|vscode|visual\s+studio\s+code|nextcloud|owncloud)\b`)

// IsPluginOrLibrary classifies whether an NVD CPE entry represents a web
// plugin, CMS extension/theme, developer library package, or hardware firmware
// that should be excluded when ExcludePlugins is enabled.
func IsPluginOrLibrary(c CPE23, title string, refs []string) bool {
	if c.Part == "h" {
		return true
	}
	if strings.HasSuffix(c.Product, "_firmware") || c.Product == "firmware" || strings.HasSuffix(c.Product, "_bios") || strings.HasSuffix(c.Product, "_uefi") {
		return true
	}
	lowTitle := strings.ToLower(title)
	if c.Part == "o" && (strings.Contains(lowTitle, " firmware") || strings.HasSuffix(lowTitle, "firmware")) {
		return true
	}
	if c.TargetSW != "" {
		if _, isHost := pluginTargetSW[c.TargetSW]; isHost {
			// Do not filter a host platform itself (e.g. wordpress:wordpress or python:python).
			if c.Product != c.TargetSW && c.Vendor != c.TargetSW {
				return true
			}
		}
	}
	if pluginTitleRE.MatchString(title) {
		return true
	}
	if strings.HasSuffix(c.Product, "_plugin") || strings.HasSuffix(c.Product, "_theme") || strings.HasSuffix(c.Product, "_extension") || strings.HasSuffix(c.Product, "_addon") {
		return true
	}
	for _, ref := range refs {
		lowRef := strings.ToLower(ref)
		for _, snippet := range pluginRefSnippets {
			if strings.Contains(lowRef, snippet) {
				// Keep standalone Drupal/WordPress core itself.
				if (c.Vendor == "wordpress" && c.Product == "wordpress") || (c.Vendor == "drupal" && c.Product == "drupal") {
					continue
				}
				return true
			}
		}
	}
	return false
}

var vendorSuffixWords = map[string]struct{}{
	"inc":          {},
	"incorporated": {},
	"llc":          {},
	"ltd":          {},
	"limited":      {},
	"corp":         {},
	"corporation":  {},
	"gmbh":         {},
	"ag":           {},
	"sa":           {},
	"sas":          {},
	"sarl":         {},
	"bv":           {},
	"nv":           {},
	"plc":          {},
	"co":           {},
	"company":      {},
	"foundation":   {},
	"project":      {},
	"technologies": {},
	"technology":   {},
	"software":     {},
	"systems":      {},
	"labs":         {},
	"studios":      {},
	"studio":       {},
	"group":        {},
	"developers":   {},
	"team":         {},
	"community":    {},
	"org":          {},
	"open":         {},
	"source":       {},
	"opensource":   {},
}

// VendorNorm normalizes a vendor or publisher name by stripping corporate and
// organizational suffixes so cross-source vendors match deterministically.
func VendorNorm(raw string) string {
	words := strings.Fields(Normalize(unescapeCPE23(raw)))
	if len(words) == 0 {
		return ""
	}
	kept := make([]string, 0, len(words))
	for _, w := range words {
		if _, stop := vendorSuffixWords[w]; stop {
			continue
		}
		kept = append(kept, w)
	}
	if len(kept) == 0 {
		return strings.Join(words, " ")
	}
	return strings.Join(kept, " ")
}

// ProductNorm normalizes a software product name and strips a redundant leading
// vendor name when present (e.g. ("Adobe Photoshop", "Adobe") -> "photoshop").
func ProductNorm(name, vendor string) string {
	norm := Normalize(unescapeCPE23(name))
	if norm == "" {
		return ""
	}
	v := VendorNorm(vendor)
	if v != "" && strings.HasPrefix(norm, v+" ") {
		rest := strings.TrimSpace(strings.TrimPrefix(norm, v+" "))
		if len([]rune(rest)) >= 2 {
			return rest
		}
	}
	return norm
}

var repoHosts = map[string]struct{}{
	"github.com":    {},
	"gitlab.com":    {},
	"codeberg.org":  {},
	"bitbucket.org": {},
	"gitea.com":     {},
}

var genericMultiProductHosts = map[string]struct{}{
	"microsoft.com":                {},
	"apps.microsoft.com":           {},
	"learn.microsoft.com":          {},
	"azure.microsoft.com":          {},
	"apple.com":                    {},
	"apps.apple.com":               {},
	"google.com":                   {},
	"play.google.com":              {},
	"cloud.google.com":             {},
	"chrome.google.com":            {},
	"chromewebstore.google.com":    {},
	"adobe.com":                    {},
	"autodesk.com":                 {},
	"jetbrains.com":                {},
	"oracle.com":                   {},
	"ibm.com":                      {},
	"sap.com":                      {},
	"cisco.com":                    {},
	"intel.com":                    {},
	"nvidia.com":                   {},
	"amd.com":                      {},
	"dell.com":                     {},
	"hp.com":                       {},
	"lenovo.com":                   {},
	"samsung.com":                  {},
	"sony.com":                     {},
	"amazon.com":                   {},
	"aws.amazon.com":               {},
	"store.steampowered.com":       {},
	"github.io":                    {},
	"gitlab.io":                    {},
	"sourceforge.io":               {},
	"netlify.app":                  {},
	"vercel.app":                   {},
	"pages.dev":                    {},
	"wordpress.org":                {},
	"npmjs.com":                    {},
	"pypi.org":                     {},
	"apache.org":                   {},
	"gnu.org":                      {},
	"kde.org":                      {},
	"gnome.org":                    {},
	"freedesktop.org":              {},
	"mozilla.org":                  {},
	"launchpad.net":                {},
	"savannah.gnu.org":             {},
	"chocolatey.org":               {},
	"flathub.org":                  {},
	"snapcraft.io":                 {},
	"winget.run":                   {},
	"formulae.brew.sh":             {},
	"brew.sh":                      {},
	"nvd.nist.gov":                 {},
	"cve.mitre.org":                {},
	"wikipedia.org":                {},
	"en.wikipedia.org":             {},
	"medium.com":                   {},
	"blogspot.com":                 {},
	"wordpress.com":                {},
	"tumblr.com":                   {},
	"itch.io":                      {},
	"gumroad.com":                  {},
	"patreon.com":                  {},
	"discord.gg":                   {},
	"twitter.com":                  {},
	"x.com":                        {},
	"youtube.com":                  {},
	"facebook.com":                 {},
	"linkedin.com":                 {},
	"reddit.com":                   {},
	"sourceforge.net":              {},
	"github.com":                   {},
	"gitlab.com":                   {},
	"codeberg.org":                 {},
	"bitbucket.org":                {},
	"gitea.com":                    {},
	"raw.githubusercontent.com":    {},
	"gist.github.com":              {},
	"addons.mozilla.org":           {},
	"marketplace.visualstudio.com": {},
}

// CanonicalHomepageKey extracts a deterministic deduplication key from an
// official product URL:
//   - "repo:<host>/<owner>/<repo>" for code-hosting repositories (GitHub,
//     GitLab, Codeberg, Bitbucket, SourceForge projects).
//   - "host:<domain>" for dedicated official product domains (excluding
//     multi-product corporate portals and generic hosting providers).
//
// The URL itself is never stored or served to clients.
func CanonicalHomepageKey(rawURL string) string {
	s := strings.TrimSpace(rawURL)
	if s == "" || len(s) > 1024 {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	host = strings.TrimPrefix(host, "www.")
	if host == "" || !strings.Contains(host, ".") || host == "localhost" || net.ParseIP(host) != nil {
		return ""
	}
	segments := nonEmptyPathSegments(u.Path)
	if _, isRepo := repoHosts[host]; isRepo {
		if len(segments) < 2 {
			return ""
		}
		owner := strings.ToLower(segments[0])
		repo := strings.TrimSuffix(strings.ToLower(segments[1]), ".git")
		if owner == "" || repo == "" || owner == "orgs" || owner == "topics" || owner == "marketplace" || owner == "explore" {
			return ""
		}
		return "repo:" + host + "/" + owner + "/" + repo
	}
	if host == "sourceforge.net" {
		if len(segments) >= 2 && (strings.EqualFold(segments[0], "projects") || strings.EqualFold(segments[0], "p")) {
			proj := strings.ToLower(segments[1])
			if proj != "" {
				return "repo:sourceforge.net/" + proj
			}
		}
		return ""
	}
	if _, generic := genericMultiProductHosts[host]; generic {
		return ""
	}
	for suffix := range genericMultiProductHosts {
		if strings.HasSuffix(host, "."+suffix) {
			// Allow distinct dedicated subdomains of multi-product platforms only
			// when they are not generic free-hosting suffixes.
			switch suffix {
			case "github.io", "gitlab.io", "sourceforge.io", "netlify.app", "vercel.app", "pages.dev", "blogspot.com", "wordpress.com", "tumblr.com", "itch.io":
				return ""
			}
		}
	}
	return "host:" + host
}

func nonEmptyPathSegments(path string) []string {
	raw := strings.Split(path, "/")
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// CompareVersions compares two software version strings using natural
// numeric/alphanumeric ordering (e.g. "1.10.0" > "1.9.9" and "2.0.0" > "2.0.0-beta.1").
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	aBase, aPre := splitPrerelease(a)
	bBase, bPre := splitPrerelease(b)
	if cmp := compareVersionTokens(aBase, bBase); cmp != 0 {
		return cmp
	}
	if aPre == "" && bPre != "" {
		return 1
	}
	if aPre != "" && bPre == "" {
		return -1
	}
	return compareVersionTokens(aPre, bPre)
}

func splitPrerelease(v string) (base, pre string) {
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

func compareVersionTokens(a, b string) int {
	ta := verSegmentRE.FindAllString(a, -1)
	tb := verSegmentRE.FindAllString(b, -1)
	n := max(len(ta), len(tb))
	for i := 0; i < n; i++ {
		if i >= len(ta) {
			if isAllZero(tb[i]) {
				continue
			}
			return -1
		}
		if i >= len(tb) {
			if isAllZero(ta[i]) {
				continue
			}
			return 1
		}
		sa, sb := ta[i], tb[i]
		na, errA := strconv.ParseUint(sa, 10, 64)
		nb, errB := strconv.ParseUint(sb, 10, 64)
		switch {
		case errA == nil && errB == nil:
			if na < nb {
				return -1
			}
			if na > nb {
				return 1
			}
		case errA == nil:
			return 1
		case errB == nil:
			return -1
		default:
			la, lb := strings.ToLower(sa), strings.ToLower(sb)
			if la < lb {
				return -1
			}
			if la > lb {
				return 1
			}
		}
	}
	return 0
}

func isAllZero(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}
