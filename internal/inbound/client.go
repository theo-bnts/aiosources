// Package inbound resolves visitor addresses from explicitly trusted proxies.
package inbound

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type Resolver struct {
	header   string
	chain    bool
	networks []netip.Prefix
}

func New(settings *model.IncomingNetwork) (Resolver, error) {
	var r Resolver
	if settings == nil || !settings.Enabled {
		return r, nil
	}
	if err := settings.Validate(); err != nil {
		return r, err
	}
	r.header, r.chain = settings.IPHeader, settings.Format == "chain"
	for _, value := range settings.TrustedProxies {
		if addr, err := netip.ParseAddr(value); err == nil {
			addr = addr.Unmap()
			r.networks = append(r.networks, netip.PrefixFrom(addr, addr.BitLen()))
		} else {
			prefix, _ := netip.ParsePrefix(value)
			r.networks = append(r.networks, prefix.Masked())
		}
	}
	return r, nil
}

func (r Resolver) trusted(addr netip.Addr) bool {
	for _, prefix := range r.networks {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func address(value string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func Direct(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if addr, ok := address(host); ok {
		return addr.String()
	}
	return "unknown"
}

func (r Resolver) ClientIP(request *http.Request) string {
	direct := Direct(request)
	peer, ok := address(direct)
	if !ok || r.header == "" || !r.trusted(peer) {
		return direct
	}
	values := request.Header.Values(r.header)
	if len(values) == 0 || len(values) > 64 || (!r.chain && len(values) != 1) {
		return direct
	}
	value := strings.Join(values, ",")
	if len(value) > 8192 {
		return direct
	}
	if !r.chain {
		if addr, ok := address(strings.TrimSpace(value)); ok {
			return addr.String()
		}
		return direct
	}
	parts := strings.Split(value, ",")
	if len(parts) > 64 {
		return direct
	}
	addresses := make([]netip.Addr, len(parts))
	for i, part := range parts {
		var valid bool
		addresses[i], valid = address(strings.TrimSpace(part))
		if !valid {
			return direct
		}
	}
	// Walk from the connected proxy towards the visitor, stopping at the first
	// untrusted hop. Never take a caller-controlled leftmost address blindly.
	for i := len(addresses) - 1; i >= 0 && r.trusted(peer); i-- {
		peer = addresses[i]
	}
	return peer.String()
}
