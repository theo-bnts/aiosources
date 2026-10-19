package model

import "testing"

func TestCredentialNamesUseTheSamePunctuationAndSuffixRules(t *testing.T) {
	for _, name := range []string{"API-Key", "session", "session_id", "sid", "query[access_token]", "client-secret", "X-Authentication", "my_private_key", "Cookie"} {
		if !CredentialParameter(name) {
			t.Errorf("credential name %q was not recognized", name)
		}
	}
	for _, name := range []string{"id", "page", "limit", "offset", "cursor", "categories[]", "sort", "year"} {
		if CredentialParameter(name) {
			t.Errorf("ordinary parameter %q was treated as a credential", name)
		}
	}
}
