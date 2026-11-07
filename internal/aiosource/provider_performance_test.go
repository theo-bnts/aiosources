package aiosource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func BenchmarkProviderAccountValidation(b *testing.B) {
	for _, service := range model.DebridServices() {
		b.Run(service, func(b *testing.B) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-performance-key" {
					b.Error("Provider authentication changed")
				}
				switch service {
				case "alldebrid":
					fmt.Fprint(w, `{"status":"success","data":{"user":{"isPremium":true}}}`)
				case "torbox":
					fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
				case "realdebrid":
					fmt.Fprint(w, `{"type":"premium","premium":3600}`)
				case "premiumize":
					fmt.Fprintf(w, `{"status":"success","customer_id":"1","premium_until":%d}`, time.Now().Add(time.Hour).Unix())
				case "debridlink":
					fmt.Fprint(w, `{"success":true,"value":{"accountType":1,"premiumLeft":3600}}`)
				case "deepbrid":
					fmt.Fprint(w, `{"type":"premium"}`)
				}
			}))
			b.Cleanup(server.Close)
			p := NewProviderHTTP()
			p.Client = server.Client()
			p.ADBase, p.TBBase, p.RDBase, p.PMBase, p.DLBase, p.DBBase = server.URL, server.URL, server.URL, server.URL, server.URL, server.URL
			b.ResetTimer()
			for b.Loop() {
				// This loopback fixture measures validation work, not deliberate
				// provider pacing. Each call has an immediately available slot.
				p.mu.Lock()
				p.next[service] = time.Time{}
				p.mu.Unlock()
				if err := p.ValidateKey(b.Context(), service, "synthetic-performance-key"); err != nil {
					b.Fatal("Synthetic account validation failed", err)
				}
			}
		})
	}
}
