package model

import (
	"errors"
	"net/netip"
	"regexp"
)

// IncomingNetwork only grants address-header trust to explicitly named peers.
// It never changes authentication, request origins or outgoing routing.
type IncomingNetwork struct {
	Enabled        bool     `json:"enabled"`
	IPHeader       string   `json:"ip_header"`
	Format         string   `json:"format"`
	TrustedProxies []string `json:"trusted_proxies"`
}

var incomingHeader = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")

func (s IncomingNetwork) Validate() error {
	if !incomingHeader.MatchString(s.IPHeader) || (s.Format != "single" && s.Format != "chain") || len(s.TrustedProxies) > 256 {
		return errors.New("Choose an IP header, its address format and up to 256 trusted proxy addresses")
	}
	if s.Enabled && len(s.TrustedProxies) == 0 {
		return errors.New("Add at least one trusted proxy IP address or network before enabling header trust")
	}
	for _, value := range s.TrustedProxies {
		if addr, err := netip.ParseAddr(value); err == nil && addr.Zone() == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
			return errors.New("Trusted proxies must be IP addresses or CIDR networks; trusting every address is not allowed")
		}
	}
	return nil
}
