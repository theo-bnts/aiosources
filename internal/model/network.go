package model

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const NetworkSystem = "system"
const NetworkDirect = "direct"

var networkID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var networkSecret = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var networkTarget = regexp.MustCompile(`^(source|metadata|debrid|download):[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type NetworkProxy struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	UsernameRef string `json:"username_ref,omitempty"`
	PasswordRef string `json:"password_ref,omitempty"`
}

type NetworkSettings struct {
	Revision  int64             `json:"revision"`
	Default   string            `json:"default"`
	Proxies   []NetworkProxy    `json:"proxies"`
	Overrides map[string]string `json:"overrides"`
	Incoming  *IncomingNetwork  `json:"incoming,omitempty"`
}

func (s NetworkSettings) Validate() error {
	if s.Incoming != nil {
		if err := s.Incoming.Validate(); err != nil {
			return err
		}
	}
	if s.Revision < 1 || len(s.Proxies) > 32 || len(s.Overrides) > 4096 {
		return errors.New("Network settings exceed their limits or have an invalid revision")
	}
	known := map[string]bool{NetworkSystem: true, NetworkDirect: true}
	for _, proxy := range s.Proxies {
		if !networkID.MatchString(proxy.ID) || known[proxy.ID] || strings.TrimSpace(proxy.Name) == "" || len(proxy.Name) > 160 || strings.ContainsAny(proxy.Name, "\r\n\x00") {
			return errors.New("Each proxy needs a unique ID and a display name")
		}
		u, err := url.Parse(proxy.URL)
		if err != nil || len(proxy.URL) > 2048 || strings.TrimSpace(proxy.URL) != proxy.URL || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || strings.ContainsAny(u.Host, "\\%\r\n\x00") {
			return errors.New("Proxy URL must contain only its protocol, hostname and port; use Secrets for credentials")
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return errors.New("Proxy protocol must be HTTP, HTTPS or SOCKS5")
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return errors.New("Proxy port must be between 1 and 65535")
			}
		}
		if (proxy.UsernameRef == "") != (proxy.PasswordRef == "") || (proxy.UsernameRef != "" && (!networkSecret.MatchString(proxy.UsernameRef) || !networkSecret.MatchString(proxy.PasswordRef))) {
			return errors.New("Proxy authentication requires both username and password secret references")
		}
		known[proxy.ID] = true
	}
	if !known[s.Default] {
		return errors.New("Select an existing default connection")
	}
	for target, route := range s.Overrides {
		if (!networkTarget.MatchString(target) && target != "downloads") || !known[route] {
			return errors.New("Each connection override needs a valid target and an existing proxy or direct connection")
		}
	}
	return nil
}

func (s NetworkSettings) Route(target string) string {
	if route, ok := s.Overrides[target]; ok {
		return route
	}
	if service, ok := strings.CutPrefix(target, "download:"); ok {
		if route, exists := s.Overrides["downloads"]; exists {
			return route
		}
		if route, exists := s.Overrides["debrid:"+service]; exists {
			return route
		}
	}
	return s.Default
}
