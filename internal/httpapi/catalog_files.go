package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

const catalogFilePurpose = "catalog-browser-download-v1"

type catalogFileTicket struct {
	URL     string `json:"url"`
	Session string `json:"session"`
	Expires int64  `json:"expires"`
}

func (s *server) writeCatalogDownload(w http.ResponseWriter, r *http.Request, item model.CatalogDownload, err error) {
	if errors.Is(err, aiosource.ErrPending) {
		w.Header().Set("Retry-After", "15")
		writeJSON(w, http.StatusAccepted, item)
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	// The browser receives only a short-lived, session-bound attachment URL.
	// The provider URL and file contents are never saved to the database or disk.
	if item.URL != "" {
		if !validCatalogFileURL(item.URL) {
			respondError(w, aiosource.ErrInvalidResponse)
			return
		}
		data, e := json.Marshal(catalogFileTicket{URL: item.URL, Session: r.Context().Value(sessionContextKey{}).(session).id, Expires: time.Now().Add(10 * time.Minute).Unix()})
		if e != nil {
			respondError(w, e)
			return
		}
		sealed, e := s.options.Vault.Seal(catalogFilePurpose, data)
		clear(data)
		if e != nil {
			respondError(w, e)
			return
		}
		item.URL = "/api/catalog-downloads/" + base64.RawURLEncoding.EncodeToString(sealed)
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *server) operationDownload(w http.ResponseWriter, r *http.Request) {
	item, err := s.options.AIOSource.CheckOperationDownload(r.Context(), r.PathValue("id"))
	s.writeCatalogDownload(w, r, item, err)
}

func validCatalogFileURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 8192 && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && !strings.ContainsAny(u.Host, "\\%")
}

// Pin public addresses at connection time, including redirects. Never send
// administrator cookies, API keys or proxy credentials to a file host.
func catalogFileAddressAllowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}
func newCatalogFileClient() *http.Client {
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid file destination")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("file destination unavailable")
		}
		for _, ip := range ips {
			if !catalogFileAddressAllowed(ip) {
				return nil, errors.New("file destination not permitted")
			}
		}
		d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New("file destination unavailable")
	}
	transport := &http.Transport{DialContext: dial, ForceAttemptHTTP2: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, MaxResponseHeaderBytes: 64 << 10, DisableCompression: true}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validCatalogFileURL(req.URL.String()) {
			return errors.New("file redirect not permitted")
		}
		return nil
	}}
}

func (s *server) catalogFile(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if len(token) > 14000 {
		respondError(w, model.ErrNotFound)
		return
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		respondError(w, model.ErrNotFound)
		return
	}
	data, err := s.options.Vault.Open(catalogFilePurpose, ciphertext)
	if err != nil {
		respondError(w, model.ErrNotFound)
		return
	}
	defer clear(data)
	var ticket catalogFileTicket
	now := time.Now().Unix()
	if json.Unmarshal(data, &ticket) != nil || ticket.Session != r.Context().Value(sessionContextKey{}).(session).id || ticket.Expires <= now || ticket.Expires > now+600 || !validCatalogFileURL(ticket.URL) {
		respondError(w, model.ErrNotFound)
		return
	}
	select {
	case s.files <- struct{}{}:
		defer func() { <-s.files }()
	default:
		respondError(w, model.ErrBusy)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Hour)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, r.Method, ticket.URL, nil)
	if err != nil {
		respondError(w, aiosource.ErrInvalidResponse)
		return
	}
	request.Header.Set("Accept-Encoding", "identity")
	// Range support lets the browser resume without buffering the file in RAM.
	if value := r.Header.Get("Range"); value != "" {
		request.Header.Set("Range", value)
	}
	if value := r.Header.Get("If-Range"); value != "" {
		request.Header.Set("If-Range", value)
	}
	response, err := s.fileClient.Do(request)
	if err != nil {
		respondError(w, aiosource.ErrInvalidResponse)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			w.Header().Set("Content-Range", response.Header.Get("Content-Range"))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
			respondError(w, model.ErrNotFound)
			return
		}
		respondError(w, aiosource.ErrInvalidResponse)
		return
	}
	if err = http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		respondError(w, err)
		return
	}
	filename := "download"
	if _, values, e := mime.ParseMediaType(response.Header.Get("Content-Disposition")); e == nil && values["filename"] != "" {
		filename = values["filename"]
	} else if u, e := url.Parse(ticket.URL); e == nil {
		filename = path.Base(u.Path)
	}
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if filename == "." || filename == "/" || filename == "" || len(filename) > 255 {
		filename = "download"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	// Safari rejects an attachment framed under DENY. Only this authenticated
	// download can be framed by this application; scripts and foreign frames
	// remain forbidden. The application's own pages retain their DENY policy.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "application/octet-stream")
	if kind, _, e := mime.ParseMediaType(response.Header.Get("Content-Type")); e == nil && kind == "multipart/byteranges" {
		w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	}
	if n, e := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64); e == nil && n >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	}
	for _, key := range []string{"Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Content-Encoding"} {
		if value := response.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.CopyBuffer(w, response.Body, make([]byte, 32<<10))
	}
}
