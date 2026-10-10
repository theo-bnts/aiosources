package torznab

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const providerHeader = `{"version":1,"id":"sample","url":"https://indexer.example/"`

func TestProviderRejectsAmbiguousAndUnsafeProfiles(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":              providerHeader + `,"private-secret":4.1}`,
		"case insensitive field":     providerHeader + `,"NAME":"private-secret"}`,
		"duplicate secret reference": providerHeader + `,"auth":{"api_key_env":"FIRST","api_key_env":"SECOND"}}`,
		"escaped duplicate key":      providerHeader + `,"auth":{"api_key_env":"FIRST","api_key_\u0065nv":"SECOND"}}`,
		"duplicate root field":       providerHeader + `,"id":"other"}`,
		"multiple values":            providerHeader + `} {"private-secret":true}`,
		"trailing scalar":            providerHeader + `} "private-secret"`,
		"legacy YAML":                "version: 1\nid: sample\nurl: https://indexer.example/\n",
		"trailing comma":             providerHeader + `,}`,
		"comment":                    providerHeader + `/*private-secret*/}`,
		"invalid UTF-8":              providerHeader + ",\"name\":\"\xff\"}",
		"root array":                 `[]`,
		"root null":                  `null`,
		"unsupported version":        `{"version":2,"id":"sample","url":"https://indexer.example/"}`,
		"cross origin API":           providerHeader + `,"api_path":"https://other.example/api"}`,
		"network relative API":       providerHeader + `,"api_path":"//other.example/api"}`,
		"ambiguous relative API":     providerHeader + `,"api_path":"api/torznab"}`,
		"API fragment":               providerHeader + `,"api_path":"/api#fragment"}`,
		"unitless interval":          providerHeader + `,"request_interval":4100000000}`,
		"empty interval":             providerHeader + `,"request_interval":""}`,
		"negative interval":          providerHeader + `,"request_interval":"-1s"}`,
		"invalid interval":           providerHeader + `,"request_interval":"private-secret"}`,
		"null interval":              providerHeader + `,"request_interval":null}`,
		"null string":                providerHeader + `,"name":null}`,
		"boolean string":             providerHeader + `,"name":true}`,
		"null integer":               providerHeader + `,"page_size":null}`,
		"string integer":             providerHeader + `,"page_size":"private-secret"}`,
		"fractional integer":         providerHeader + `,"page_size":1.5}`,
		"overflow integer":           providerHeader + `,"page_size":9223372036854775808}`,
		"null object":                providerHeader + `,"auth":null}`,
		"null credential reference":  providerHeader + `,"auth":{"api_key_env":null}}`,
		"null category element":      providerHeader + `,"search":{"categories":[null]}}`,
		"null output element":        providerHeader + `,"output":{"fields":[null]}}`,
		"unsupported date unit":      providerHeader + `,"published_at_unit":"microseconds"}`,
		"embedded password":          providerHeader + `,"auth":{"password":"private-secret"}}`,
		"invalid reference":          providerHeader + `,"auth":{"api_key_env":"${SECRET}"}}`,
		"password without username":  providerHeader + `,"auth":{"password_env":"PASSWORD"}}`,
		"oversized document":         providerHeader + `,"name":"` + strings.Repeat("x", 128<<10) + `"}`,
		"negative category filter":   providerHeader + `,"search":{"categories":[-1]}}`,
		"unknown search setting":     providerHeader + `,"search":{"category":[2000]}}`,
		"empty output selection":     providerHeader + `,"output":{"fields":[]}}`,
		"unknown output field":       providerHeader + `,"output":{"fields":["private-secret"]}}`,
		"empty attribute name":       providerHeader + `,"output":{"fields":["attributes."]}}`,
		"unknown output setting":     providerHeader + `,"output":{"field":["title"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadProvider(strings.NewReader(content))
			if err == nil {
				t.Fatal("unsafe or ambiguous profile accepted")
			}
			if strings.Contains(err.Error(), "private-secret") {
				t.Fatal("JSON validation error exposed a credential")
			}
		})
	}
}

func TestProviderDurationRoundTripAndCredentialResolution(t *testing.T) {
	provider, err := LoadProvider(strings.NewReader(providerHeader + `,"api_path":"/api/torznab","auth":{"api_key_env":"API_KEY"},"request_interval":"1m2.5s","search":{"categories":[2030,5070]},"output":{"fields":["guid","attributes.imdbid"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["request_interval"]) != `"1m2.5s"` {
		t.Fatalf("duration is not a human-readable string: %s", document["request_interval"])
	}
	roundTrip, err := LoadProvider(strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	config, err := roundTrip.Config(func(name string) (string, bool) {
		if name != "API_KEY" {
			t.Fatalf("unexpected credential reference: %s", name)
		}
		return "resolved-key", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.RequestInterval != 62500*time.Millisecond || config.URL != "https://indexer.example/api/torznab" || config.APIKey != "resolved-key" {
		t.Fatal("round trip changed pacing, endpoint resolution or credentials")
	}
	if len(roundTrip.Search.Categories) != 2 || roundTrip.Search.Categories[0] != 2030 || roundTrip.Search.Categories[1] != 5070 {
		t.Fatal("round trip changed category filters")
	}
	projection, err := NewProjection(roundTrip.Output.Fields)
	if err != nil {
		t.Fatal(err)
	}
	item := projection.Project(&Item{GUID: "release", Title: "excluded"})
	if item["guid"] != "release" {
		t.Fatal("round trip lost selected identity")
	}
	if _, ok := item["title"]; ok {
		t.Fatal("round trip broadened the output selection")
	}
}

func TestProviderNullCollectionsPreserveUnfilteredFullOutput(t *testing.T) {
	provider, err := LoadProvider(strings.NewReader(providerHeader + `,"search":{"categories":null},"output":{"fields":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	if provider.Search.Categories != nil || provider.Output.Fields != nil {
		t.Fatal("null collection settings must retain their omitted semantics")
	}
}

func TestProviderMissingCredentialNeverFallsBackToAnonymous(t *testing.T) {
	provider, err := LoadProvider(strings.NewReader(providerHeader + `,"auth":{"api_key_env":"REQUIRED_KEY"}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, lookup := range []func(string) (string, bool){
		func(string) (string, bool) { return "", false },
		func(string) (string, bool) { return "", true },
	} {
		if _, err := provider.Config(lookup); err == nil {
			t.Fatal("missing configured API key permitted anonymous access")
		}
	}
}

func TestProviderRejectsResolvedInvalidCookieHeader(t *testing.T) {
	provider, err := LoadProvider(strings.NewReader(providerHeader + `,"auth":{"cookie_env":"SESSION"}}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"header injection": "session=secret\r\nX-Injected: true",
		"invalid name":     "session id=secret",
		"missing equals":   "session=secret; malformed",
		"invalid value":    "session=secret\tinvalid",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := provider.Config(func(string) (string, bool) { return value, true })
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("invalid cookie was accepted or disclosed: %v", err)
			}
		})
	}
}

func TestProviderRejectsResolvedBasicUsernameColon(t *testing.T) {
	provider, err := LoadProvider(strings.NewReader(providerHeader + `,"auth":{"username_env":"USERNAME","password_env":"PASSWORD"}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Config(func(name string) (string, bool) {
		if name == "USERNAME" {
			return "reader:private-secret", true
		}
		return "password", true
	})
	if err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("invalid Basic username was accepted or disclosed: %v", err)
	}
}
