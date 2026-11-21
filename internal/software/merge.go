package software

import (
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

// MergedSoftware represents one deduplicated, projected Software catalog entry
// ready to be stored into sw_catalog, sw_aliases, sw_external_ids and aio_works.
type MergedSoftware struct {
	SoftwareKey string
	Name        string
	Normalized  string
	Part        string // "a" or "o"
	Info        model.SoftwareInformation
	Aliases     []string
	ExternalIDs []ExternalIDRef
}

// ExternalIDRef links a cross-source identifier to a merged Software work.
type ExternalIDRef struct {
	Kind  string // "nvd_cpe", "winget_id", "homebrew_cask"
	Value string
}

// MergeStats reports deterministic merge and filter outcomes.
type MergeStats struct {
	CatalogWorks int64
	Filtered     int64
	IconsMatched int64
}

type mergeCluster struct {
	part          string
	name          string
	vendor        string
	vendorNorm    string
	productNorm   string
	description   string
	license       string
	latestVersion string
	homepageKeys  []string
	platforms     []string
	tags          []string
	languages     []string
	aliases       []string
	nvdCPE        string
	wingetID      string
	homebrewCask  string
	hasWinGet     bool
	hasHomebrew   bool
	hasNVD        bool
	namePriority  int // 3=winget, 2=homebrew, 1=nvd
	descPriority  int
	vendPriority  int
	allPlugins    bool
	tokens        map[string]struct{}
}

// MergeCatalog deterministically deduplicates source entries across Microsoft
// WinGet, Homebrew Cask and NIST NVD CPE, applies ImportSelection filters, and
// attaches optional selfh.st/icons references.
func MergeCatalog(entries []SourceEntry, icons []IconEntry, sel ImportSelection) ([]MergedSoftware, MergeStats) {
	selectedPlatforms, ok := ImportPlatforms(sel.Platforms)
	if !ok {
		selectedPlatforms = slices.Clone(DefaultPlatforms)
		slices.Sort(selectedPlatforms)
	}
	allPlatformsSelected := len(selectedPlatforms) == len(ValidPlatforms)
	platformAllowed := make(map[string]struct{}, len(selectedPlatforms))
	for _, p := range selectedPlatforms {
		platformAllowed[p] = struct{}{}
	}

	// Deterministic source order: WinGet -> Homebrew Cask -> NVD CPE, then by ExternalID.
	ordered := make([]SourceEntry, 0, len(entries))
	var stats MergeStats
	for _, e := range entries {
		if e.Source == "nvd" {
			if !sel.IncludeNVD {
				stats.Filtered++
				continue
			}
			if e.Part == "o" && !sel.IncludeOS {
				stats.Filtered++
				continue
			}
			if e.IsPlugin && sel.ExcludePlugins {
				stats.Filtered++
				continue
			}
		}
		if strings.TrimSpace(e.Name) == "" || strings.TrimSpace(e.ExternalID) == "" {
			continue
		}
		ordered = append(ordered, e)
	}
	slices.SortFunc(ordered, func(a, b SourceEntry) int {
		pa, pb := sourceOrder(a.Source), sourceOrder(b.Source)
		if pa != pb {
			return pa - pb
		}
		return strings.Compare(a.ExternalID, b.ExternalID)
	})

	clusters := make([]*mergeCluster, 0, len(ordered))
	byHomepage := make(map[string][]*mergeCluster, len(ordered)/2)
	byVendorProduct := make(map[string]*mergeCluster, len(ordered))
	byCanonName := make(map[string][]*mergeCluster, len(ordered))

	registerCluster := func(c *mergeCluster, e SourceEntry) {
		if e.HomepageKey != "" {
			if !slices.Contains(c.homepageKeys, e.HomepageKey) {
				c.homepageKeys = append(c.homepageKeys, e.HomepageKey)
			}
			if !slices.Contains(byHomepage[e.HomepageKey], c) {
				byHomepage[e.HomepageKey] = append(byHomepage[e.HomepageKey], c)
			}
		}
		if e.VendorNorm != "" && e.ProductNorm != "" {
			vpKey := e.VendorNorm + "\x00" + e.ProductNorm
			if _, exists := byVendorProduct[vpKey]; !exists {
				byVendorProduct[vpKey] = c
			}
		}
		if c.vendorNorm != "" && c.productNorm != "" {
			vpKey := c.vendorNorm + "\x00" + c.productNorm
			if _, exists := byVendorProduct[vpKey]; !exists {
				byVendorProduct[vpKey] = c
			}
		}
		if normName := Normalize(e.Name); normName != "" {
			if !slices.Contains(byCanonName[normName], c) {
				byCanonName[normName] = append(byCanonName[normName], c)
			}
		}
		for _, alias := range e.Aliases {
			if normAlias := Normalize(alias); len([]rune(normAlias)) >= 3 {
				if !slices.Contains(byCanonName[normAlias], c) {
					byCanonName[normAlias] = append(byCanonName[normAlias], c)
				}
			}
		}
	}

	for _, e := range ordered {
		match := findMergeTarget(e, byHomepage, byVendorProduct, byCanonName)
		if match == nil {
			match = newMergeCluster(e)
			clusters = append(clusters, match)
		} else {
			match.absorb(e)
		}
		registerCluster(match, e)
	}

	// Build strict icon lookup indexes when IncludeIcons is enabled.
	iconByRef := make(map[string]string)
	iconByNorm := make(map[string]string)
	if sel.IncludeIcons && len(icons) > 0 {
		for _, ic := range icons {
			ref := ValidIconRef(ic.Reference)
			if ref == "" {
				continue
			}
			iconByRef[ref] = ref
			if ic.Normalized != "" {
				if _, exists := iconByNorm[ic.Normalized]; !exists {
					iconByNorm[ic.Normalized] = ref
				}
			}
		}
	}

	out := make([]MergedSoftware, 0, len(clusters))
	seenKeys := make(map[string]struct{}, len(clusters))

	for _, c := range clusters {
		// Platform filter check on the merged OS union.
		if !allPlatformsSelected {
			matchedPlatform := false
			for _, p := range c.platforms {
				if _, ok := platformAllowed[p]; ok {
					matchedPlatform = true
					break
				}
			}
			if !matchedPlatform {
				stats.Filtered++
				continue
			}
		}

		key := c.canonicalKey()
		if key == "" {
			continue
		}
		if _, dup := seenKeys[key]; dup {
			continue
		}
		seenKeys[key] = struct{}{}

		iconRef := ""
		if sel.IncludeIcons && len(iconByRef) > 0 {
			iconRef = matchIconRef(c, iconByRef, iconByNorm)
			if iconRef != "" {
				stats.IconsMatched++
			}
		}

		slices.Sort(c.platforms)
		slices.Sort(c.tags)
		slices.Sort(c.languages)

		var extIDs []ExternalIDRef
		if c.wingetID != "" {
			extIDs = append(extIDs, ExternalIDRef{Kind: "winget_id", Value: c.wingetID})
		}
		if c.homebrewCask != "" {
			extIDs = append(extIDs, ExternalIDRef{Kind: "homebrew_cask", Value: c.homebrewCask})
		}
		if c.nvdCPE != "" {
			extIDs = append(extIDs, ExternalIDRef{Kind: "nvd_cpe", Value: c.nvdCPE})
		}

		aliases := buildClusterAliases(c)
		info := model.SoftwareInformation{
			Vendor:        c.vendor,
			Description:   c.description,
			License:       c.license,
			LatestVersion: c.latestVersion,
			Part:          c.part,
			Platforms:     c.platforms,
			Tags:          c.tags,
			Languages:     c.languages,
			NVDCPE:        c.nvdCPE,
			WinGetID:      c.wingetID,
			HomebrewCask:  c.homebrewCask,
			IconRef:       iconRef,
			HasIcon:       iconRef != "",
		}
		out = append(out, MergedSoftware{
			SoftwareKey: key,
			Name:        c.name,
			Normalized:  Normalize(c.name),
			Part:        c.part,
			Info:        info,
			Aliases:     aliases,
			ExternalIDs: extIDs,
		})
	}

	slices.SortFunc(out, func(a, b MergedSoftware) int {
		return strings.Compare(a.SoftwareKey, b.SoftwareKey)
	})
	stats.CatalogWorks = int64(len(out))
	return out, stats
}

func sourceOrder(source string) int {
	switch source {
	case "winget":
		return 1
	case "homebrew":
		return 2
	case "nvd":
		return 3
	default:
		return 4
	}
}

func newMergeCluster(e SourceEntry) *mergeCluster {
	c := &mergeCluster{
		part:       defaultPart(e.Part),
		allPlugins: e.IsPlugin,
		tokens:     make(map[string]struct{}),
	}
	c.absorb(e)
	return c
}

func (c *mergeCluster) absorb(e SourceEntry) {
	if defaultPart(e.Part) == "a" {
		c.part = "a"
	}
	if !e.IsPlugin {
		c.allPlugins = false
	}

	np, dp, vp := sourcePriorities(e.Source)
	if strings.TrimSpace(e.Name) != "" && (c.name == "" || np > c.namePriority) {
		c.name = strings.TrimSpace(e.Name)
		c.namePriority = np
	}
	if strings.TrimSpace(e.Description) != "" && (c.description == "" || dp > c.descPriority) {
		c.description = strings.TrimSpace(e.Description)
		c.descPriority = dp
	}
	if strings.TrimSpace(e.Vendor) != "" && (c.vendor == "" || vp > c.vendPriority) {
		c.vendor = strings.TrimSpace(e.Vendor)
		c.vendorNorm = e.VendorNorm
		if c.vendorNorm == "" {
			c.vendorNorm = VendorNorm(c.vendor)
		}
		c.vendPriority = vp
	}
	if c.productNorm == "" && e.ProductNorm != "" {
		c.productNorm = e.ProductNorm
	}
	if c.license == "" && e.License != "" {
		c.license = e.License
	}
	if e.LatestVersion != "" && CompareVersions(e.LatestVersion, c.latestVersion) > 0 {
		c.latestVersion = e.LatestVersion
	}

	switch e.Source {
	case "winget":
		c.hasWinGet = true
		if c.wingetID == "" {
			c.wingetID = WinGetID(e.ExternalID)
		}
	case "homebrew":
		c.hasHomebrew = true
		if c.homebrewCask == "" {
			c.homebrewCask = HomebrewCask(e.ExternalID)
		}
	case "nvd":
		c.hasNVD = true
		if c.nvdCPE == "" {
			c.nvdCPE = NVDCPE(e.ExternalID)
		}
	}

	for _, p := range e.Platforms {
		if norm := NormalizePlatform(p); norm != "" && !slices.Contains(c.platforms, norm) {
			c.platforms = append(c.platforms, norm)
		}
	}
	for _, t := range e.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !slices.Contains(c.tags, t) && len(c.tags) < 16 {
			c.tags = append(c.tags, t)
		}
	}
	for _, l := range e.Languages {
		l = strings.ToLower(strings.TrimSpace(l))
		if l != "" && !slices.Contains(c.languages, l) && len(c.languages) < 16 {
			c.languages = append(c.languages, l)
		}
	}

	addAlias := func(a string) {
		a = strings.TrimSpace(a)
		if a == "" || len(a) > 200 {
			return
		}
		if !slices.Contains(c.aliases, a) && len(c.aliases) < 32 {
			c.aliases = append(c.aliases, a)
		}
		for _, tok := range strings.Fields(Normalize(a)) {
			if len(tok) >= 2 {
				c.tokens[tok] = struct{}{}
			}
		}
	}
	addAlias(e.Name)
	for _, a := range e.Aliases {
		addAlias(a)
	}
	if e.ProductNorm != "" {
		for _, tok := range strings.Fields(e.ProductNorm) {
			if len(tok) >= 2 {
				c.tokens[tok] = struct{}{}
			}
		}
	}
}

func sourcePriorities(source string) (namePrio, descPrio, vendorPrio int) {
	switch source {
	case "winget":
		return 3, 3, 3
	case "homebrew":
		return 2, 2, 1
	case "nvd":
		return 1, 1, 2
	default:
		return 0, 0, 0
	}
}

func defaultPart(part string) string {
	if part == "o" {
		return "o"
	}
	return "a"
}

func findMergeTarget(
	e SourceEntry,
	byHomepage map[string][]*mergeCluster,
	byVendorProduct map[string]*mergeCluster,
	byCanonName map[string][]*mergeCluster,
) *mergeCluster {
	entryPart := defaultPart(e.Part)

	// 1. Official normalized homepage / repository match.
	if e.HomepageKey != "" {
		isRepo := strings.HasPrefix(e.HomepageKey, "repo:")
		for _, cand := range byHomepage[e.HomepageKey] {
			if cand.part != entryPart || hasSameSourceConflict(cand, e.Source) {
				continue
			}
			if isRepo || compatibleHomepageCandidate(cand, e) {
				return cand
			}
		}
	}

	// 2. Exact (vendor_norm, product_norm) match.
	if e.VendorNorm != "" && e.ProductNorm != "" {
		if cand, ok := byVendorProduct[e.VendorNorm+"\x00"+e.ProductNorm]; ok {
			if cand.part == entryPart && !hasSameSourceConflict(cand, e.Source) {
				return cand
			}
		}
	}

	// 2b. Entries without explicit vendor (e.g. Homebrew Cask "Adobe Photoshop")
	// matching an existing cluster's vendor + product.
	normName := Normalize(e.Name)
	if e.VendorNorm == "" && normName != "" {
		words := strings.Fields(normName)
		if len(words) >= 2 {
			// Try 1-word and 2-word vendor prefixes.
			for split := 1; split <= min(2, len(words)-1); split++ {
				vTry := VendorNorm(strings.Join(words[:split], " "))
				pTry := strings.Join(words[split:], " ")
				if cand, ok := byVendorProduct[vTry+"\x00"+pTry]; ok {
					if cand.part == entryPart && !hasSameSourceConflict(cand, e.Source) {
						return cand
					}
				}
			}
		}
	}

	// 3. Canonical normalized name matching corroborated by vendor (or
	// unattributed Homebrew Cask matching an unambiguous single-name product).
	if normName != "" {
		for _, cand := range byCanonName[normName] {
			if cand.part != entryPart || hasSameSourceConflict(cand, e.Source) {
				continue
			}
			if vendorsCompatible(cand.vendorNorm, e.VendorNorm, normName) {
				return cand
			}
		}
	}
	return nil
}

func hasSameSourceConflict(c *mergeCluster, source string) bool {
	switch source {
	case "winget":
		return c.hasWinGet
	case "homebrew":
		return c.hasHomebrew
	case "nvd":
		return c.hasNVD
	default:
		return false
	}
}

func compatibleHomepageCandidate(c *mergeCluster, e SourceEntry) bool {
	if c.vendorNorm != "" && e.VendorNorm != "" && c.vendorNorm != e.VendorNorm {
		// Different explicit vendors can still share a single-product domain when
		// one is an alias or organization rename, provided product names overlap.
		if !namesOverlap(c, e) {
			return false
		}
	}
	return namesOverlap(c, e)
}

func namesOverlap(c *mergeCluster, e SourceEntry) bool {
	en := Normalize(e.Name)
	cn := Normalize(c.name)
	if en != "" && cn != "" && (en == cn || strings.Contains(cn, en) || strings.Contains(en, cn)) {
		return true
	}
	if c.productNorm != "" && e.ProductNorm != "" && (c.productNorm == e.ProductNorm || strings.Contains(c.productNorm, e.ProductNorm) || strings.Contains(e.ProductNorm, c.productNorm)) {
		return true
	}
	for _, tok := range strings.Fields(en) {
		if len(tok) < 3 {
			continue
		}
		if _, ok := c.tokens[tok]; ok {
			return true
		}
	}
	for _, alias := range e.Aliases {
		for _, tok := range strings.Fields(Normalize(alias)) {
			if len(tok) < 3 {
				continue
			}
			if _, ok := c.tokens[tok]; ok {
				return true
			}
		}
	}
	return false
}

func vendorsCompatible(vA, vB, normName string) bool {
	if vA != "" && vB != "" {
		return vA == vB || strings.Contains(vA, vB) || strings.Contains(vB, vA)
	}
	// If one side has no explicit vendor (common for Homebrew Cask), allow
	// merging on exact canonical name only when the name is reasonably specific.
	return len([]rune(normName)) >= 4
}

func (c *mergeCluster) canonicalKey() string {
	var raw string
	switch {
	case c.wingetID != "":
		raw = "winget:" + strings.ToLower(c.wingetID)
	case c.homebrewCask != "":
		raw = "brew:" + strings.ToLower(c.homebrewCask)
	case c.nvdCPE != "":
		raw = "cpe:" + strings.TrimPrefix(strings.ToLower(c.nvdCPE), "cpe:2.3:")
	default:
		return ""
	}
	if len(raw) > 195 {
		raw = strings.TrimRight(raw[:195], "._:-")
	}
	return CanonicalSoftwareKey(raw)
}

func buildClusterAliases(c *mergeCluster) []string {
	out := make([]string, 0, len(c.aliases)+4)
	seenNorm := make(map[string]struct{}, len(c.aliases)+4)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || len(raw) > 200 || len(out) >= 32 {
			return
		}
		norm := Normalize(raw)
		if norm == "" {
			return
		}
		if _, exists := seenNorm[norm]; exists {
			return
		}
		seenNorm[norm] = struct{}{}
		out = append(out, raw)
	}
	add(c.name)
	if c.vendor != "" && c.name != "" && !strings.HasPrefix(strings.ToLower(c.name), strings.ToLower(c.vendor)) {
		add(c.vendor + " " + c.name)
	}
	if c.productNorm != "" && len([]rune(c.productNorm)) >= 3 {
		add(c.productNorm)
	}
	for _, a := range c.aliases {
		add(a)
	}
	if c.homebrewCask != "" {
		add(strings.ReplaceAll(c.homebrewCask, "-", " "))
	}
	if c.wingetID != "" {
		if _, tail, ok := strings.Cut(c.wingetID, "."); ok && len(tail) >= 3 {
			add(strings.ReplaceAll(tail, ".", " "))
		}
	}
	return out
}

func matchIconRef(c *mergeCluster, iconByRef, iconByNorm map[string]string) string {
	// 1. Exact Homebrew Cask token match (highly reliable).
	if c.homebrewCask != "" {
		if ref, ok := iconByRef[c.homebrewCask]; ok {
			return ref
		}
	}
	// 2. Exact slugified canonical name match.
	normName := Normalize(c.name)
	if normName != "" {
		slug := strings.ReplaceAll(normName, " ", "-")
		if ref, ok := iconByRef[slug]; ok {
			return ref
		}
		if ref, ok := iconByNorm[normName]; ok {
			return ref
		}
	}
	// 3. Exact product name match (stripped of vendor prefix, e.g. "Adobe Photoshop" -> "photoshop" or "adobe-photoshop").
	if c.vendorNorm != "" && c.productNorm != "" {
		vpSlug := strings.ReplaceAll(c.vendorNorm+" "+c.productNorm, " ", "-")
		if ref, ok := iconByRef[vpSlug]; ok {
			return ref
		}
	}
	if len([]rune(c.productNorm)) >= 3 {
		pSlug := strings.ReplaceAll(c.productNorm, " ", "-")
		if ref, ok := iconByRef[pSlug]; ok {
			return ref
		}
		if ref, ok := iconByNorm[c.productNorm]; ok {
			return ref
		}
	}
	// 4. Exact alias / moniker match (length >= 3 to avoid short collisions).
	for _, a := range c.aliases {
		na := Normalize(a)
		if len([]rune(na)) < 3 {
			continue
		}
		aSlug := strings.ReplaceAll(na, " ", "-")
		if ref, ok := iconByRef[aSlug]; ok {
			return ref
		}
		if ref, ok := iconByNorm[na]; ok {
			return ref
		}
	}
	return ""
}
