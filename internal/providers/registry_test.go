package providers_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func definition(id string) string {
	return "{\n  \"version\": 1,\n  \"id\": \"" + id + "\",\n  \"name\": \"Fictional archive\",\n  \"adapter\": \"http_json\",\n  \"url\": \"https://archive.invalid/api\",\n  \"enabled\": false\n}\n"
}

func withFields(raw, fields string) string {
	return strings.TrimSuffix(strings.TrimSpace(raw), "}") + ",\n" + fields + "\n}\n"
}

func TestRemoteCatalogIntervalValidation(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	remote := withFields(definition("remote"), `"http":{"catalog":true},"auth":{"type":"bearer","secret_ref":"catalog_password"}`)
	for _, test := range []struct {
		name  string
		block string
		valid bool
	}{
		{"enabled interval", `{"enabled":true,"interval_minutes":60}`, true},
		{"disabled interval", `{"enabled":false,"interval_minutes":45}`, true},
		{"manual interval", `{"interval_minutes":1}`, true},
		{"maximum", `{"interval_minutes":525600}`, true},
		{"missing interval", `{"enabled":true}`, false},
		{"zero", `{"interval_minutes":0}`, false},
		{"negative", `{"interval_minutes":-1}`, false},
		{"over maximum", `{"interval_minutes":525601}`, false},
		{"fraction", `{"interval_minutes":1.5}`, false},
		{"quoted interval", `{"interval_minutes":"60"}`, false},
		{"null interval", `{"interval_minutes":null}`, false},
		{"quoted enabled", `{"enabled":"true","interval_minutes":60}`, false},
		{"null enabled", `{"enabled":null,"interval_minutes":60}`, false},
		{"null block", `null`, false},
		{"advanced source timing", `{"interval_minutes":60,"cron":"* * * * *"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := r.Validate(withFields(remote, `"catalog_sync":`+test.block))
			if result.Valid != test.valid {
				t.Fatalf("catalog interval validity = %v, want %v: %v", result.Valid, test.valid, result.Issues)
			}
		})
	}
	if result := r.Validate(withFields(definition("native"), `"catalog_sync":{"enabled":true,"interval_minutes":60}`)); result.Valid {
		t.Fatal("remote catalog synchronization accepted for a native source")
	}
}

func TestOutputAttributeSelectors(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	for _, test := range []struct {
		name   string
		fields string
		valid  bool
	}{
		{"selected attributes", "title, attributes.imdbid, attributes.vendor-ID", true},
		{"whole attributes and selected attribute", "attributes, attributes.imdbid", true},
		{"missing attribute name", "attributes.", false},
		{"unsupported nested object", "metadata.imdbid", false},
		{"unsupported deeper path", "attributes.imdbid.value", false},
		{"duplicate selector", "attributes.imdbid, attributes.imdbid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := r.Validate(withFields(definition("fixture"), `"output": {"fields": ["`+strings.ReplaceAll(test.fields, ", ", `", "`)+`"]}`))
			if result.Valid != test.valid {
				t.Fatalf("selector validity = %v, want %v: %v", result.Valid, test.valid, result.Issues)
			}
		})
	}
}

func TestTraversalRejectsLiteralCredentialsBeforeSaving(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	for _, section := range []string{
		`"scopes": [{"id": "scope", "query": {"access_token": "literal-secret"}, "match": {"category_id": 1}}]`,
		`"query_variants": [{"api_key": "literal-secret"}]`,
		`"id_recovery": {"discovery_query": {"passkey": "literal-secret"}}`,
	} {
		raw := withFields(definition("traversal-credentials"), `"traversal": {`+section+`}`)
		if result := r.Validate(raw); result.Valid {
			t.Fatal("traversal accepted a literal credential")
		}
		if _, err := r.Save("traversal-credentials", raw, ""); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("credential-bearing source was saved: %v", err)
		}
	}
}

func TestIncrementalQueryRejectsCredentialsBeforeSaving(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	for _, config := range []string{
		`"http": {"incremental_query": {"access_token": "literal-secret"}}`,
		`"http": {"incremental_query": {"filter": {"password": "literal-secret"}}}`,
		`"auth": {"type": "query", "name": "gate", "secret_ref": "key"}, "http": {"incremental_query": {"GATE": "literal-secret"}}`,
	} {
		raw := withFields(definition("incremental-credentials"), config)
		if result := r.Validate(raw); result.Valid {
			t.Fatal("incremental query accepted a literal credential")
		}
		if _, err := r.Save("incremental-credentials", raw, ""); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("credential-bearing incremental source was saved: %v", err)
		}
	}
}

func TestRequestLimitsValidation(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	for _, test := range []struct {
		name   string
		limits string
		valid  bool
	}{
		{"omitted", "", true},
		{"all windows", `{"per_minute":30,"per_hour":600,"per_day":5000}`, true},
		{"minute only", `{"per_minute":1}`, true},
		{"hour only", `{"per_hour":1}`, true},
		{"day only", `{"per_day":1}`, true},
		{"disabled window", `{"per_minute":0,"per_hour":600}`, true},
		{"maximum", `{"per_minute":1000000,"per_hour":1000000,"per_day":1000000}`, true},
		{"empty", `{}`, false},
		{"all zero", `{"per_minute":0,"per_hour":0,"per_day":0}`, false},
		{"negative minute", `{"per_minute":-1,"per_hour":600}`, false},
		{"negative hour", `{"per_minute":30,"per_hour":-1}`, false},
		{"negative day", `{"per_minute":30,"per_day":-1}`, false},
		{"minute overflow", `{"per_minute":1000001}`, false},
		{"hour overflow", `{"per_hour":1000001}`, false},
		{"day overflow", `{"per_day":1000001}`, false},
		{"integer overflow", `{"per_day":9223372036854775808}`, false},
		{"unknown field", `{"per_minute":30,"per_week":10000}`, false},
		{"null object", `null`, false},
		{"null count", `{"per_minute":null}`, false},
		{"fraction", `{"per_minute":1.5}`, false},
		{"string count", `{"per_minute":"30"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := definition("request-limits")
			if test.limits != "" {
				raw = withFields(raw, `"request_limits":`+test.limits)
			}
			result := r.Validate(raw)
			if result.Valid != test.valid {
				t.Fatalf("validity = %v, want %v: %v", result.Valid, test.valid, result.Issues)
			}
			if result.Provider != nil {
				err := connectors.Validate(*result.Provider)
				if (err == nil) != test.valid {
					t.Fatalf("adapter validation disagrees with document validation: %v", err)
				}
			}
			if !test.valid {
				if _, err := r.Save("request-limits", raw, ""); !errors.Is(err, model.ErrInvalid) {
					t.Fatalf("invalid request limits were saved: %v", err)
				}
			}
		})
	}
}

func openRegistry(t *testing.T, dir string) *providers.Registry {
	t.Helper()
	r, err := providers.New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func writeDefinition(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("file %q changed unexpectedly: got %q, want %q", path, got, want)
	}
}

func assertPrivate(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("%q permissions = %o, want 600", path, info.Mode().Perm())
	}
}

func TestRepairPreservesOriginalFilenameAndSubmittedBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator notes.JSON")
	broken := "{\n  \"version\": [\n"
	writeDefinition(t, path, broken)
	writeDefinition(t, filepath.Join(dir, "ignored.example.json"), "not valid JSON: [")
	r := openRegistry(t, dir)
	items, err := r.List()
	if err != nil || len(items) != 1 || items[0].Valid {
		t.Fatalf("invalid definition must remain listed for repair: items=%+v, err=%v", items, err)
	}
	doc, err := r.Get(items[0].ID)
	if err != nil || doc.JSON != broken || doc.Revision == "" || len(doc.Issues) == 0 {
		t.Fatalf("repair route lost original JSON/revision/issues: doc=%+v, err=%v", doc, err)
	}
	repaired := definition("archive")
	saved, err := r.Save(items[0].ID, repaired, doc.Revision)
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, repaired)
	assertPrivate(t, path)
	got, err := r.Get("archive")
	if err != nil || got.JSON != repaired || got.Revision != saved.Revision || len(got.Issues) != 0 {
		t.Fatalf("repaired definition unavailable by its new ID: doc=%+v, err=%v", got, err)
	}
	updated := strings.Replace(repaired, `"enabled": false`, `"enabled": true`, 1)
	if _, err := r.Save("archive", updated, saved.Revision); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, updated)
	if _, err := os.Stat(filepath.Join(dir, "archive.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save must not relocate an arbitrary filename: %v", err)
	}
}

func TestSaveDoesNotOverwriteFilenameOwnedByAnotherID(t *testing.T) {
	dir := t.TempDir()
	original := definition("beta")
	path := filepath.Join(dir, "alpha.json")
	writeDefinition(t, path, original)
	r := openRegistry(t, dir)
	created := definition("alpha")
	if _, err := r.Save("alpha", created, ""); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, original)
	for _, id := range []string{"alpha", "beta"} {
		doc, err := r.Get(id)
		if err != nil || doc.Provider.ID != id || doc.JSON != definition(id) {
			t.Fatalf("provider %q was lost after filename collision: doc=%+v, err=%v", id, doc, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		assertPrivate(t, filepath.Join(dir, entry.Name()))
	}
}

func TestConcurrentSaveRejectsLostUpdateAcrossRegistries(t *testing.T) {
	dir := t.TempDir()
	first := openRegistry(t, dir)
	second := openRegistry(t, dir)
	initial, err := first.Save("archive", definition("archive"), "")
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		raw string
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i, r := range []*providers.Registry{first, second} {
		raw := initial.JSON + []string{"  \n", "\t\n"}[i]
		go func() {
			<-start
			_, err := r.Save("archive", raw, initial.Revision)
			results <- result{raw: raw, err: err}
		}()
	}
	close(start)
	winner := ""
	conflicts := 0
	for range 2 {
		res := <-results
		switch {
		case res.err == nil:
			if winner != "" {
				t.Fatal("both stale-revision writes succeeded")
			}
			winner = res.raw
		case errors.Is(res.err, model.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected save failure: %v", res.err)
		}
	}
	if winner == "" || conflicts != 1 {
		t.Fatalf("expected one winner and one conflict: winner=%q, conflicts=%d", winner, conflicts)
	}
	doc, err := second.Get("archive")
	if err != nil || doc.JSON != winner || doc.Revision == initial.Revision {
		t.Fatalf("winning edit was not retained: doc=%+v, err=%v", doc, err)
	}
	if err := first.Delete("archive", initial.Revision, nil); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale deletion must not remove the winner: %v", err)
	}
	assertFile(t, filepath.Join(dir, "archive.json"), winner)
}

func TestDeleteGuardPreservesBlockedDefinition(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	saved, err := r.Save("archive", definition("archive"), "")
	if err != nil {
		t.Fatal(err)
	}
	blocked := errors.New("collection admission prevents deletion")
	if err := r.Delete("archive", saved.Revision, func() error { return blocked }); !errors.Is(err, blocked) {
		t.Fatalf("guarded deletion did not retain the admission error: %v", err)
	}
	document, err := r.Get("archive")
	if err != nil || document.JSON != saved.JSON || document.Revision != saved.Revision {
		t.Fatalf("guarded deletion changed the source: %v", err)
	}
	if err := r.Delete("archive", saved.Revision, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("archive"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("admitted deletion left the source accessible: %v", err)
	}
}

func TestDuplicateIDsRemainIndividuallyRepairable(t *testing.T) {
	dir := t.TempDir()
	left := definition("duplicate") + "  \n"
	right := definition("duplicate") + "\t\n"
	writeDefinition(t, filepath.Join(dir, "left.json"), left)
	writeDefinition(t, filepath.Join(dir, "right.json"), right)
	r := openRegistry(t, dir)
	items, err := r.List()
	if err != nil || len(items) != 2 || items[0].ID == items[1].ID || items[0].Valid || items[1].Valid {
		t.Fatalf("duplicate definitions need distinct invalid repair routes: items=%+v, err=%v", items, err)
	}
	var route string
	var revision string
	for _, item := range items {
		doc, err := r.Get(item.ID)
		if err != nil || len(doc.Issues) == 0 || (doc.JSON != left && doc.JSON != right) {
			t.Fatalf("duplicate original unavailable: doc=%+v, err=%v", doc, err)
		}
		if doc.JSON == left {
			route, revision = item.ID, doc.Revision
		}
	}
	if route == "" {
		t.Fatal("left duplicate has no repair route")
	}
	if _, err := r.Save(route, left, revision); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("repair must not retain the duplicate ID: %v", err)
	}
	repaired := strings.Replace(left, `"id": "duplicate"`, `"id": "unique"`, 1)
	if _, err := r.Save(route, repaired, revision); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dir, "left.json"), repaired)
	assertFile(t, filepath.Join(dir, "right.json"), right)
	for id, want := range map[string]string{"unique": repaired, "duplicate": right} {
		doc, err := r.Get(id)
		if err != nil || len(doc.Issues) != 0 || doc.JSON != want {
			t.Fatalf("resolved duplicate %q is not valid and intact: doc=%+v, err=%v", id, doc, err)
		}
	}
}

func TestInvalidJSONNeverReplacesSavedDefinition(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	raw := definition("archive")
	saved, err := r.Save("archive", raw, "")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"missing version":             strings.Replace(raw, "  \"version\": 1,\n", "", 1),
		"unsupported version":         strings.Replace(raw, `"version": 1`, `"version": 2`, 1),
		"missing enabled":             strings.Replace(raw, ",\n  \"enabled\": false", "", 1),
		"quoted boolean":              strings.Replace(raw, `"enabled": false`, `"enabled": "false"`, 1),
		"null boolean":                strings.Replace(raw, `"enabled": false`, `"enabled": null`, 1),
		"quoted schedule boolean":     withFields(raw, `"schedule": {"enabled": "false", "every": "1h"}`),
		"null schedule boolean":       withFields(raw, `"schedule": {"enabled": null, "every": "1h"}`),
		"conflicting schedule timing": withFields(raw, `"schedule": {"every": "1h", "cron": "* * * * *"}`),
		"null Full interval":          withFields(raw, `"schedule": {"every": "1h", "full_every": null}`),
		"numeric Full interval":       withFields(raw, `"schedule": {"every": "1h", "full_every": 86400}`),
		"Full without base timing":    withFields(raw, `"schedule": {"full_every": "24h"}`),
		"Full in incompatible mode":   withFields(raw, `"schedule": {"every": "1h", "mode": "full", "full_every": "24h"}`),
		"coerced string":              strings.Replace(raw, `"name": "Fictional archive"`, `"name": 123`, 1),
		"NUL scalar value":            withFields(raw, `"search": {"query": "synthetic\u0000value"}`),
		"NUL mapping key":             withFields(raw, `"options": {"synthetic\u0000name": "value"}`),
		"duplicate key":               withFields(raw, `"enabled": true`),
		"nested duplicate key":        withFields(raw, `"options": {"filter": {"limit": 1, "limit": 2}}`),
		"escaped duplicate key":       withFields(raw, `"enabl\u0065d": true`),
		"unknown field":               withFields(raw, `"unexpected": true`),
		"case-folded schema field":    withFields(raw, `"HTTP": {}`),
		"multiple documents":          raw + raw,
		"oversized whitespace":        raw + strings.Repeat(" ", providers.MaxDocumentBytes),
		"comments":                    "// operator comment\n" + raw,
		"trailing comma":              strings.Replace(raw, `"enabled": false`, `"enabled": false,`, 1),
		"explicit zero page size":     withFields(raw, `"page_size": 0`),
		"null defaulted object":       withFields(raw, `"http": null`),
		"null optional object":        withFields(raw, `"traversal": null`),
		"null typed map value":        withFields(raw, `"mapping": {"fields": {"title": null}}`),
		"null typed array item":       withFields(raw, `"search": {"categories": [null]}`),
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			if validation := r.Validate(invalid); validation.Valid {
				t.Fatal("invalid JSON accepted")
			}
			if _, err := r.Save("archive", invalid, saved.Revision); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("save must reject invalid JSON: %v", err)
			}
			doc, err := r.Get("archive")
			if err != nil || doc.JSON != raw || doc.Revision != saved.Revision {
				t.Fatalf("rejected edit changed saved definition: doc=%+v, err=%v", doc, err)
			}
		})
	}
}

func TestLinkedDefinitionsCannotBeReadReplacedOrDeleted(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			r := openRegistry(t, dir)
			original, err := r.Save("archive", definition("archive"), "")
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "external.json")
			foreign := definition("archive") + "\t\n"
			writeDefinition(t, outside, foreign)
			path := filepath.Join(dir, "archive.json")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			link := os.Symlink
			if kind == "hardlink" {
				link = os.Link
			}
			if err := link(outside, path); err != nil {
				t.Fatal(err)
			}
			items, err := r.List()
			if err != nil || len(items) != 1 || items[0].Valid {
				t.Fatalf("unsafe file must be visible but invalid: items=%+v, err=%v", items, err)
			}
			if doc, err := r.Get(items[0].ID); !errors.Is(err, model.ErrInvalid) || doc.JSON != "" {
				t.Fatalf("linked file content was exposed: doc=%+v, err=%v", doc, err)
			}
			if _, err := r.Save(items[0].ID, original.JSON, original.Revision); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("linked definition was replaceable: %v", err)
			}
			if err := r.Delete(items[0].ID, original.Revision, nil); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("linked definition was deletable: %v", err)
			}
			assertFile(t, outside, foreign)
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("link itself was removed: %v", err)
			}
		})
	}
}

func TestRegistryRejectsLinkedRootAndLock(t *testing.T) {
	dir := t.TempDir()
	rootLink := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink(dir, rootLink); err != nil {
		t.Fatal(err)
	}
	if r, err := providers.New(rootLink, nil); err == nil {
		r.Close()
		t.Fatal("symlinked provider root was accepted")
	}
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "external-lock")
			writeDefinition(t, outside, "external lock bytes")
			if err := os.Chmod(outside, 0640); err != nil {
				t.Fatal(err)
			}
			link := os.Symlink
			if kind == "hardlink" {
				link = os.Link
			}
			if err := link(outside, filepath.Join(dir, ".registry.lock")); err != nil {
				t.Fatal(err)
			}
			if r, err := providers.New(dir, nil); err == nil {
				r.Close()
				t.Fatal("linked registry lock was accepted")
			}
			assertFile(t, outside, "external lock bytes")
			info, err := os.Stat(outside)
			if err != nil || info.Mode().Perm() != 0640 {
				t.Fatalf("opening the registry changed external lock permissions: info=%v, err=%v", info, err)
			}
		})
	}
}

func TestNullableCollectionsPreserveDefaultsAndExplicitFalse(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	raw := withFields(definition("null-collections"), `"options": null, "http": {"headers": null, "query": null, "body": null}, "search": {"categories": null}, "mapping": {"fields": null}, "output": {"fields": null}, "schedule": {"enabled": false}`)
	result := r.Validate(raw)
	if !result.Valid {
		t.Fatalf("nullable collections were rejected: %v", result.Issues)
	}
	p := result.Provider
	if p.Enabled || p.Schedule.Enabled == nil || *p.Schedule.Enabled || p.PageSize != 100 || p.Auth.Type != "none" || p.HTTP.Method != "GET" || p.Schedule.Mode != model.ModeIncremental || p.RequestInterval != "1s" {
		t.Fatalf("absent defaults or explicit disabled state changed: %+v", p)
	}
}

func TestExactNumbersAndRawFormattingSurviveRegistryRoundTrip(t *testing.T) {
	r := openRegistry(t, t.TempDir())
	raw := withFields(definition("precision"), `"http": {"query": {"id": 9007199254740993}, "incremental_query": {"id": 184467440737095516170}, "body": {"ratio": 1.2300e+2}}, "options": {"numbers": [9007199254740993, 0.123456789012345678901234567890]}, "schedule": {"every": "1h", "full_every": "168h"}`)
	document, err := r.Save("precision", raw, "")
	if err != nil {
		t.Fatal(err)
	}
	document, err = r.Get("precision")
	if err != nil || document.JSON != raw {
		t.Fatalf("raw numeric lexemes or whitespace changed: %v", err)
	}
	p := document.Provider
	for name, value := range map[string]struct {
		actual any
		want   string
	}{
		"query":             {p.HTTP.Query["id"], "9007199254740993"},
		"incremental query": {p.HTTP.IncrementalQuery["id"], "184467440737095516170"},
		"body":              {p.HTTP.Body.(map[string]any)["ratio"], "1.2300e+2"},
		"options":           {p.Options["numbers"].([]any)[1], "0.123456789012345678901234567890"},
	} {
		if number, ok := value.actual.(json.Number); !ok || string(number) != value.want {
			t.Fatalf("%s was rounded or lost its numeric type: %#v", name, value.actual)
		}
	}
}

func TestRuntimeIgnoresLegacyFilesAndRejectsLegacyDocuments(t *testing.T) {
	dir := t.TempDir()
	writeDefinition(t, filepath.Join(dir, "old.yaml"), "version: 1\nid: old\n")
	writeDefinition(t, filepath.Join(dir, "old.yml"), "version: 1\nid: other\n")
	r := openRegistry(t, dir)
	if items, err := r.List(); err != nil || len(items) != 0 {
		t.Fatalf("runtime loaded legacy source files: %+v, %v", items, err)
	}
	if result := r.Validate("version: 1\nid: old\n"); result.Valid {
		t.Fatal("runtime validation accepted YAML")
	}
}
