package model

// DebridProvider is the shared, ordered service catalog. Endpoint selection,
// credential references and capabilities never come from a visitor's URL.
type DebridProvider struct {
	ID, Name, Marker string
}

func DebridProviders() []DebridProvider {
	return []DebridProvider{
		{"alldebrid", "AllDebrid", "AD"},
		{"debridlink", "Debrid-Link", "DL"},
		// DB is already an alias for Debrider in stream aggregators.
		{"deepbrid", "Deepbrid", "Deepbrid"},
		{"premiumize", "Premiumize", "PM"},
		{"realdebrid", "Real-Debrid", "RD"},
		{"torbox", "TorBox", "TB"},
	}
}

func DebridServices() []string {
	services := make([]string, 0, 6)
	for _, provider := range DebridProviders() {
		services = append(services, provider.ID)
	}
	return services
}

func IsDebridService(service string) bool {
	for _, provider := range DebridProviders() {
		if provider.ID == service {
			return true
		}
	}
	return false
}

func (c AIOConfig) DebridSecret(service string) string {
	switch service {
	case "alldebrid":
		return c.AllDebridSecret
	case "debridlink":
		return c.DebridLinkSecret
	case "deepbrid":
		return c.DeepbridSecret
	case "premiumize":
		return c.PremiumizeSecret
	case "realdebrid":
		return c.RealDebridSecret
	case "torbox":
		return c.TorBoxSecret
	default:
		return ""
	}
}

func (c AIOConfig) DebridSecrets() map[string]string {
	refs := make(map[string]string, 6)
	for _, provider := range DebridProviders() {
		refs[provider.ID] = c.DebridSecret(provider.ID)
	}
	return refs
}

func (c AIOConfig) SecretReferences() []string {
	refs := []string{c.TMDBSecret}
	for _, provider := range DebridProviders() {
		refs = append(refs, c.DebridSecret(provider.ID))
	}
	return refs
}

// RestrictDebrid removes unselected credentials from a copy used for a request.
// Empty selection means all; a direct-only selection contacts no debrid service.
func (c AIOConfig) RestrictDebrid(services []string) AIOConfig {
	if len(services) == 0 {
		return c
	}
	allowed := func(id string) bool {
		for _, service := range services {
			if id == service {
				return true
			}
		}
		return false
	}
	if !allowed("alldebrid") {
		c.AllDebridSecret = ""
	}
	if !allowed("debridlink") {
		c.DebridLinkSecret = ""
	}
	if !allowed("deepbrid") {
		c.DeepbridSecret = ""
	}
	if !allowed("premiumize") {
		c.PremiumizeSecret = ""
	}
	if !allowed("realdebrid") {
		c.RealDebridSecret = ""
	}
	if !allowed("torbox") {
		c.TorBoxSecret = ""
	}
	return c
}
