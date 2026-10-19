package model

import (
	"strings"
	"unicode"
)

// CredentialParameter keeps definition validation and persisted source links on
// the same policy. Values for these names belong in secret references, not
// literal configuration, published fields or continuation URLs.
func CredentialParameter(name string) bool {
	name = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' || r == '[' || r == ']' {
			return -1
		}
		return unicode.ToLower(r)
	}, name)
	switch name {
	case "key", "auth", "authorization", "proxyauthorization", "cookie", "setcookie", "credentials", "credential", "username", "password", "passwd", "pass", "passkey", "secret", "token", "session", "sessionid", "sid", "apikey", "accesskey", "privatekey", "clientsecret", "signature", "sig":
		return true
	}
	for _, suffix := range []string{"token", "password", "passwd", "passkey", "secret", "apikey", "authkey", "auth", "authorization", "authentication", "credential", "credentials", "cookie", "sessionid", "accesskey", "privatekey"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
