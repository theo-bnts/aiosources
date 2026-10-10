package torznab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func paginationClient(t *testing.T, search http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("t") == "caps" {
			fmt.Fprint(w, `<caps><limits default="4" max="4"/><searching><search available="yes" supportedParams="q"/><movie-search available="no"/></searching></caps>`)
			return
		}
		search(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := Open(context.Background(), Config{URL: server.URL + "/api", RequestInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func releaseXML(id string) string {
	return `<item><guid>` + id + `</guid><title>Release ` + id + `</title><torznab:attr name="category" value="2000"/><torznab:attr name="size" value="12"/></item>`
}

func feedXML(metadata string, items ...string) string {
	return `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>` + metadata + strings.Join(items, "") + `</channel></rss>`
}

func TestSearchAcceptsEquivalentPaginationAliases(t *testing.T) {
	const guid = "urn:provider:release:alpha/suffix"
	const hash = "0123456789abcdef0123456789abcdef01234567"
	fragment := `<item><guid isPermaLink="false">` + guid + `</guid><title>A &amp; B</title><torznab:attr name="infohash" value="` + hash + `"/></item>`
	for _, test := range []struct {
		name    string
		headers string
		offset  int
		total   int
	}{
		{
			name:    "newznab then torznab",
			headers: `<newznab:response offset="0" total="1001"/><torznab:response offset="0" total="1001"/>`,
			total:   1001,
		},
		{
			name:    "reverse aliases with equivalent numeric values",
			headers: `<t:response xmlns:t="http://torznab.com/schemas/2015/feed" offset="04" total="09"/><n:response xmlns:n="http://www.newznab.com/DTD/2010/feeds/attributes/" offset="4" total="9"/>`,
			offset:  4,
			total:   9,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := feedXML(test.headers, "\n"+fragment+"\n")
			client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			})
			query := Query{Offset: test.offset}
			page, err := client.Search(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := client.SearchRaw(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range []Page{page, raw.Page} {
				if got.Offset != test.offset || got.Total == nil || *got.Total != test.total || len(got.Items) != 1 {
					t.Fatalf("pagination = %#v", got)
				}
				if got.Items[0].GUID != guid || got.Items[0].InfoHash != hash {
					t.Fatalf("native identity changed: %#v", got.Items[0])
				}
			}
			if string(raw.Body) != body || len(raw.Items) != 1 || string(raw.Items[0].Body) != fragment {
				t.Fatal("archival response or item bytes changed")
			}
			if raw.Items[0].Error != nil || raw.Items[0].Item.GUID != guid || raw.Items[0].Item.InfoHash != hash {
				t.Fatalf("valid archival identity rejected: %#v", raw.Items[0])
			}
		})
	}
}

func TestSearchRejectsInvalidPaginationAliases(t *testing.T) {
	const first = `<newznab:response offset="0" total="1001"/>`
	for name, headers := range map[string]string{
		"conflicting offset": first + `<torznab:response offset="1" total="1001"/>`,
		"conflicting total":  first + `<torznab:response offset="0" total="1002"/>`,
		"negative offset":    first + `<torznab:response offset="-1" total="1001"/>`,
		"overflow total":     first + `<torznab:response offset="0" total="9223372036854775808"/>`,
		"missing offset":     first + `<torznab:response total="1001"/>`,
		"missing total":      first + `<torznab:response offset="0"/>`,
		"same namespace":     first + `<n:response xmlns:n="http://www.newznab.com/DTD/2010/feeds/attributes/" offset="0" total="1001"/>`,
		"no namespace":       `<response/><response/>`,
		"unknown namespace":  first + `<other:response xmlns:other="urn:other" offset="0" total="1001"/>`,
		"third response":     first + `<torznab:response offset="0" total="1001"/>` + first,
	} {
		t.Run(name, func(t *testing.T) {
			fragment := releaseXML("native-guid")
			body := feedXML(headers, fragment)
			client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			})
			if _, err := client.Search(context.Background(), Query{}); !errors.Is(err, errProtocol) {
				t.Fatalf("Search accepted invalid pagination: %v", err)
			}
			raw, err := client.SearchRaw(context.Background(), Query{})
			if !errors.Is(err, errProtocol) {
				t.Fatalf("SearchRaw accepted invalid pagination: %v", err)
			}
			if string(raw.Body) != body || len(raw.Items) != 1 || string(raw.Items[0].Body) != fragment {
				t.Fatal("invalid pagination discarded archival bytes")
			}
		})
	}
}

func TestWalkContinuesShortPagesWithoutTotal(t *testing.T) {
	var offsets []int
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "4" {
			t.Errorf("negotiated limit = %q, want advertised maximum 4", q.Get("limit"))
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		offsets = append(offsets, offset)
		switch offset {
		case 0:
			fmt.Fprint(w, feedXML("", releaseXML("a"), releaseXML("b")))
		case 2:
			fmt.Fprint(w, feedXML("", `<item><guid>c</guid><title>Release c</title><torznab:attr name="imdbid" value="tt123"/></item>`))
		case 3:
			fmt.Fprint(w, feedXML(""))
		default:
			t.Errorf("unexpected offset %d", offset)
			http.Error(w, "wrong offset", http.StatusBadRequest)
		}
	})
	var ids []string
	err := client.Walk(context.Background(), Query{Limit: 500}, func(page Page) error {
		for _, item := range page.Items {
			ids = append(ids, item.GUID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b", "c"}) || !reflect.DeepEqual(offsets, []int{0, 2, 3}) {
		t.Fatalf("incomplete traversal: ids=%v offsets=%v", ids, offsets)
	}
	if got := client.Attributes(); !reflect.DeepEqual(got, []string{"category", "imdbid", "size"}) {
		t.Fatalf("observed attributes = %v", got)
	}
}

func TestWalkAdvisoryTotalsRequireEmptyPage(t *testing.T) {
	for _, advisory := range []bool{false, true} {
		t.Run(strconv.FormatBool(advisory), func(t *testing.T) {
			var offsets []int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				if r.URL.Query().Get("t") == "caps" {
					fmt.Fprint(w, `<caps><limits default="4" max="4"/><searching><search available="yes"/></searching></caps>`)
					return
				}
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				offsets = append(offsets, offset)
				total := 1
				if offset > 0 {
					total = 0
				}
				metadata := fmt.Sprintf(`<newznab:response offset="%d" total="%d"/>`, offset, total)
				switch offset {
				case 0:
					fmt.Fprint(w, feedXML(metadata, releaseXML("a"), releaseXML("b")))
				case 2:
					fmt.Fprint(w, feedXML(metadata, releaseXML("c")))
				case 3:
					fmt.Fprint(w, feedXML(metadata))
				default:
					t.Errorf("unexpected offset %d", offset)
					http.Error(w, "unexpected offset", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			client, err := Open(context.Background(), Config{URL: server.URL + "/api", RequestInterval: time.Nanosecond, DisableRetries: true, AdvisoryTotals: advisory})
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			err = client.Walk(context.Background(), Query{}, func(page Page) error {
				wantTotal := 1
				if page.Offset > 0 {
					wantTotal = 0
				}
				if page.Total == nil || *page.Total != wantTotal {
					t.Fatal("reported total was not retained as metadata")
				}
				for _, item := range page.Items {
					ids = append(ids, item.GUID)
				}
				return nil
			})
			if !advisory {
				if !errors.Is(err, ErrPaginationStalled) || len(ids) != 0 {
					t.Fatalf("strict traversal accepted excess items: ids=%v err=%v", ids, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(ids, []string{"a", "b", "c"}) || !reflect.DeepEqual(offsets, []int{0, 2, 3}) {
				t.Fatalf("advisory traversal incomplete: ids=%v offsets=%v err=%v", ids, offsets, err)
			}
		})
	}
}

func TestWalkResumesAndStopsAtAdvertisedTotal(t *testing.T) {
	var offsets []string
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		if offset != "8" {
			t.Errorf("unexpected request past completion: offset=%s", offset)
		}
		fmt.Fprint(w, feedXML(`<newznab:response offset="8" total="10"/>`, releaseXML("i"), releaseXML("j")))
	})
	var got []string
	err := client.Walk(context.Background(), Query{Offset: 8}, func(page Page) error {
		for _, item := range page.Items {
			got = append(got, item.GUID)
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(got, []string{"i", "j"}) || !reflect.DeepEqual(offsets, []string{"8"}) {
		t.Fatalf("resume: ids=%v offsets=%v error=%v", got, offsets, err)
	}
}

func TestWalkRejectsFalseCompletionAndCycles(t *testing.T) {
	cases := []struct {
		name  string
		pages []string
	}{
		{"early empty", []string{
			feedXML(`<newznab:response offset="0" total="4"/>`, releaseXML("a")),
			feedXML(""),
		}},
		{"ignored offset", []string{
			feedXML(`<newznab:response offset="0"/>`, releaseXML("a")),
			feedXML(`<newznab:response offset="0"/>`, releaseXML("b")),
		}},
		{"reordered page", []string{
			feedXML("", releaseXML("a"), releaseXML("b")),
			feedXML("", releaseXML("b"), releaseXML("a")),
		}},
		{"nonadjacent cycle", []string{
			feedXML("", releaseXML("a")),
			feedXML("", releaseXML("b")),
			feedXML("", releaseXML("a")),
		}},
		{"inconsistent total", []string{
			feedXML(`<newznab:response total="0"/>`, releaseXML("a")),
		}},
		{"decreasing total", []string{
			feedXML(`<newznab:response total="4"/>`, releaseXML("a")),
			feedXML(`<newznab:response total="2"/>`, releaseXML("b")),
		}},
		{"decreasing total on empty page", []string{
			feedXML(`<newznab:response total="4"/>`, releaseXML("a")),
			feedXML(`<newznab:response total="1"/>`),
		}},
		{"same magnet with different display names", []string{
			feedXML("", `<item><link>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=first</link></item>`),
			feedXML("", `<item><link>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=second</link></item>`),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if requests >= len(tc.pages) {
					t.Error("request after inconsistent pagination")
					fmt.Fprint(w, feedXML(""))
					return
				}
				fmt.Fprint(w, tc.pages[requests])
				requests++
			})
			delivered := 0
			err := client.Walk(context.Background(), Query{}, func(Page) error {
				delivered++
				return nil
			})
			if !errors.Is(err, ErrPaginationStalled) {
				t.Fatalf("error = %v, want pagination failure", err)
			}
			if delivered != len(tc.pages)-1 {
				t.Fatalf("invalid page delivered: %d callbacks", delivered)
			}
		})
	}
}

func TestWalkRetainsDistinctNativeLinksWithSameHash(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	var offsets []string
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		switch offset {
		case "0", "1":
			fmt.Fprint(w, feedXML("", `<item><link>https://indexer.example/get?id=`+offset+`</link><infohash>`+hash+`</infohash></item>`))
		default:
			fmt.Fprint(w, feedXML(""))
		}
	})
	var links []string
	err := client.Walk(context.Background(), Query{}, func(page Page) error {
		for _, item := range page.Items {
			links = append(links, item.Link)
		}
		return nil
	})
	wantLinks := []string{"https://indexer.example/get?id=0", "https://indexer.example/get?id=1"}
	if err != nil || !reflect.DeepEqual(links, wantLinks) || !reflect.DeepEqual(offsets, []string{"0", "1", "2"}) {
		t.Fatalf("native releases lost: links=%v offsets=%v error=%v", links, offsets, err)
	}
}

func TestWalkPropagatesSinkErrorWithoutRequestingAnotherPage(t *testing.T) {
	requests := 0
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, feedXML("", releaseXML("a")))
	})
	sinkError := errors.New("storage unavailable")
	err := client.Walk(context.Background(), Query{}, func(Page) error { return sinkError })
	if err != sinkError || requests != 1 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
}

func TestWalkHonorsCallbackCancellation(t *testing.T) {
	requests := 0
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, feedXML("", releaseXML("a")))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := client.Walk(ctx, Query{}, func(Page) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || requests != 1 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
}

func TestWalkHonorsBodyQuotaWithoutGuessingReset(t *testing.T) {
	requests := 0
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, feedXML(`<newznab:apilimits apiMax="1" apiCurrent="1"/>`, releaseXML("a")))
	})
	delivered := 0
	err := client.Walk(context.Background(), Query{}, func(Page) error {
		delivered++
		return nil
	})
	if !errors.Is(err, ErrRateLimited) || requests != 1 || delivered != 1 {
		t.Fatalf("error=%v requests=%d delivered=%d", err, requests, delivered)
	}
}

func TestWalkWaitsForBodyQuotaReset(t *testing.T) {
	requests := 0
	var reset time.Time
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			reset = time.Now().Add(60 * time.Millisecond)
			quota := fmt.Sprintf(`<newznab:apilimits apiMax="1" apiCurrent="1" apiNextAvailable="%s"/>`, reset.UTC().Format(time.RFC3339Nano))
			fmt.Fprint(w, feedXML(quota, releaseXML("a")))
			return
		}
		if time.Now().Before(reset) {
			t.Error("requested next page before body quota reset")
		}
		fmt.Fprint(w, feedXML(`<newznab:response total="2"/>`, releaseXML("b")))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var ids []string
	err := client.Walk(ctx, Query{}, func(page Page) error {
		for _, item := range page.Items {
			ids = append(ids, item.GUID)
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("error=%v ids=%v", err, ids)
	}
}

func TestSearchRejectsUnavailableModeAndReservedParameters(t *testing.T) {
	requests := 0
	client := paginationClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusBadRequest)
	})
	_, err := client.Search(context.Background(), Query{Mode: ModeMovie})
	if !errors.Is(err, ErrUnsupportedSearch) {
		t.Fatalf("unavailable mode error=%v", err)
	}
	_, err = client.Search(context.Background(), Query{Params: url.Values{"APIKEY": {"replacement"}}})
	if err == nil || requests != 0 {
		t.Fatalf("reserved parameter error=%v requests=%d", err, requests)
	}
}
