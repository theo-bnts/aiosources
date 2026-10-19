package aiosource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

var ErrPending = errors.New("torrent is not ready")
var ErrAuth = errors.New("provider refused credentials")
var ErrInvalidResponse = errors.New("unusable provider response")

type ProviderHTTP struct {
	Client         *http.Client
	ADBase, TBBase string
	mu             sync.Mutex
	next           map[string]time.Time
}

func NewProviderHTTP() *ProviderHTTP {
	return &ProviderHTTP{Client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, ADBase: "https://api.alldebrid.com", TBBase: "https://api.torbox.app/v1/api", next: map[string]time.Time{}}
}

// Documented demonstration keys authenticate static fixtures, not an account.
// Check them locally too so previously issued public links fail closed.
func allDebridDemoKey(key string) bool {
	switch key {
	case "staticDemoApikeyPrem", "staticDemoApikeyTria", "staticDemoApikeyFree":
		return true
	default:
		return false
	}
}

func (p *ProviderHTTP) pace(ctx context.Context, service string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	gap := 200 * time.Millisecond
	if service == "alldebrid" {
		gap = 110 * time.Millisecond
	}
	now := time.Now()
	at := maxTime(now, p.next[service])
	p.next[service] = at.Add(gap)
	p.mu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func (p *ProviderHTTP) request(ctx context.Context, service, key, method, path string, body io.Reader, contentType string) (payload json.RawMessage, requestErr error) {
	started := time.Now()
	status := 0
	defer func() { recordProviderCall(ctx, strings.Split(path, "?")[0], status, started, requestErr) }()
	if strings.TrimSpace(key) == "" || (service == "alldebrid" && allDebridDemoKey(key)) {
		return nil, ErrAuth
	}
	if err := p.pace(ctx, service); err != nil {
		return nil, err
	}
	base := p.TBBase
	if service == "alldebrid" {
		base = p.ADBase
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/"+path, body)
	if err != nil {
		return nil, ErrInvalidResponse
	}
	req = req.WithContext(outbound.WithTarget(req.Context(), "debrid:"+service))
	req.Header.Set("Authorization", "Bearer "+key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := p.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var timed net.Error
		if errors.As(err, &timed) && timed.Timeout() {
			return nil, newProviderError("timeout", "")
		}
		return nil, newProviderError("provider_unavailable", "")
	}
	defer res.Body.Close()
	status = res.StatusCode
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20+1))
	if err != nil || len(b) > 8<<20 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if res.StatusCode != http.StatusOK {
			return nil, providerHTTPError(res.StatusCode, res.Header.Get("Retry-After"), nil)
		}
		return nil, ErrInvalidResponse
	}
	var envelope struct {
		Status  string          `json:"status"`
		Success *bool           `json:"success"`
		Demo    json.RawMessage `json:"demo"`
		Data    json.RawMessage `json:"data"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(b, &envelope) != nil {
		if res.StatusCode != http.StatusOK {
			return nil, providerHTTPError(res.StatusCode, res.Header.Get("Retry-After"), nil)
		}
		return nil, ErrInvalidResponse
	}
	var rejected *ProviderError
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		rejected = providerResponseError(envelope.Error)
	}
	if res.StatusCode != http.StatusOK {
		return nil, providerHTTPError(res.StatusCode, res.Header.Get("Retry-After"), rejected)
	}
	if service == "alldebrid" && len(envelope.Demo) > 0 {
		switch string(envelope.Demo) {
		case `true`, `"true"`:
			return nil, ErrAuth
		case `false`, `"false"`, `null`:
		default:
			return nil, ErrInvalidResponse
		}
	}
	success := envelope.Success != nil && *envelope.Success
	if service == "alldebrid" {
		success = envelope.Status == "success"
	}
	if rejected != nil {
		return nil, newProviderError(rejected.Code, res.Header.Get("Retry-After"))
	}
	if !success {
		return nil, newProviderError("provider_error", "")
	}
	if service == "alldebrid" {
		var data struct {
			Error   json.RawMessage `json:"error"`
			Magnets []struct {
				Error json.RawMessage `json:"error"`
			} `json:"magnets"`
		}
		if json.Unmarshal(envelope.Data, &data) == nil {
			if len(data.Error) > 0 && string(data.Error) != "null" {
				return nil, newProviderError(providerResponseError(data.Error).Code, res.Header.Get("Retry-After"))
			}
			for _, magnet := range data.Magnets {
				if len(magnet.Error) > 0 && string(magnet.Error) != "null" {
					return nil, newProviderError(providerResponseError(magnet.Error).Code, res.Header.Get("Retry-After"))
				}
			}
		}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, ErrInvalidResponse
	}
	return envelope.Data, nil
}
func (p *ProviderHTTP) ad(ctx context.Context, key, path string, form url.Values, out any) error {
	b, err := p.request(ctx, "alldebrid", key, "POST", path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	if json.Unmarshal(b, out) != nil {
		return ErrInvalidResponse
	}
	return nil
}

type adMagnet struct {
	ID     int64           `json:"id"`
	Hash   string          `json:"hash"`
	Status *int            `json:"statusCode"`
	Ready  *bool           `json:"ready"`
	Error  json.RawMessage `json:"error"`
}

func validAD(m adMagnet) bool {
	return m.ID > 0 && hashRE.MatchString(m.Hash) && (len(m.Error) == 0 || string(m.Error) == "null")
}
func (p *ProviderHTTP) ADInventory(ctx context.Context, key string) ([]adMagnet, error) {
	var out struct {
		Magnets []adMagnet `json:"magnets"`
	}
	if err := p.ad(ctx, key, "v4.1/magnet/status", url.Values{}, &out); err != nil {
		return nil, err
	}
	if out.Magnets == nil {
		return nil, ErrInvalidResponse
	}
	for _, m := range out.Magnets {
		if !validAD(m) || m.Status == nil {
			return nil, ErrInvalidResponse
		}
	}
	return out.Magnets, nil
}
func (p *ProviderHTTP) ADAdd(ctx context.Context, key, hash, magnet string) (adMagnet, error) {
	var out struct {
		Magnets []adMagnet `json:"magnets"`
	}
	if err := p.ad(ctx, key, "v4/magnet/upload", url.Values{"magnets[]": {magnet}}, &out); err != nil {
		return adMagnet{}, err
	}
	if len(out.Magnets) != 1 || !validAD(out.Magnets[0]) || !strings.EqualFold(out.Magnets[0].Hash, hash) {
		return adMagnet{}, ErrInvalidResponse
	}
	m := out.Magnets[0]
	if m.Ready == nil {
		return m, ErrInvalidResponse
	}
	return m, nil
}
func existingAD(items []adMagnet, hash string) *adMagnet {
	var found *adMagnet
	for i := range items {
		m := &items[i]
		if strings.EqualFold(m.Hash, hash) {
			if found == nil || *m.Status == 4 {
				found = m
			}
		}
	}
	return found
}

type adNode struct {
	Name     string          `json:"n"`
	Size     *int64          `json:"s"`
	Link     *string         `json:"l"`
	Children json.RawMessage `json:"e"`
}

func flattenAD(raw json.RawMessage, parent string, depth int) ([]model.AIOFile, error) {
	if depth > 32 {
		return nil, ErrInvalidResponse
	}
	var nodes []adNode
	if json.Unmarshal(raw, &nodes) != nil || nodes == nil {
		return nil, ErrInvalidResponse
	}
	out := []model.AIOFile{}
	for _, n := range nodes {
		if n.Name == "" {
			return nil, ErrInvalidResponse
		}
		path := n.Name
		if parent != "" {
			path = parent + "/" + path
		}
		if len(n.Children) > 0 {
			children, err := flattenAD(n.Children, path, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
		} else {
			if n.Size == nil || *n.Size < 0 || n.Link == nil {
				return nil, ErrInvalidResponse
			}
			out = append(out, model.AIOFile{Path: path, Size: *n.Size, Link: *n.Link})
		}
	}
	return out, nil
}
func (p *ProviderHTTP) ADFiles(ctx context.Context, key string, id int64) ([]model.AIOFile, error) {
	var out struct {
		Magnets []struct {
			// The file endpoint returns quoted IDs in its documented examples;
			// status/upload use numbers. Preserve exact integer identity for both.
			ID    json.Number     `json:"id"`
			Files json.RawMessage `json:"files"`
			Error json.RawMessage `json:"error"`
		} `json:"magnets"`
	}
	if err := p.ad(ctx, key, "v4/magnet/files", url.Values{"id[]": {itoa64(id)}}, &out); err != nil {
		return nil, err
	}
	for _, m := range out.Magnets {
		if m.ID.String() == itoa64(id) && (len(m.Error) == 0 || string(m.Error) == "null") {
			return flattenAD(m.Files, "", 0)
		}
	}
	return nil, ErrInvalidResponse
}
func (p *ProviderHTTP) ADDelete(ctx context.Context, key string, id int64) error {
	var out struct {
		Message string `json:"message"`
	}
	if err := p.ad(ctx, key, "v4/magnet/delete", url.Values{"id": {itoa64(id)}}, &out); err != nil {
		return err
	}
	if out.Message == "" {
		return ErrInvalidResponse
	}
	return nil
}
func (p *ProviderHTTP) ADUnlock(ctx context.Context, key, link string) (string, error) {
	var out struct {
		Link string `json:"link"`
	}
	if err := p.ad(ctx, key, "v4/link/unlock", url.Values{"link": {link}}, &out); err != nil {
		return "", err
	}
	return HTTPSLink(out.Link)
}
func HTTPSLink(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", ErrInvalidResponse
	}
	return u.String(), nil
}

type tbFile struct {
	ID   *int64 `json:"id"`
	Name string `json:"name"`
	Size *int64 `json:"size"`
}

func tbFiles(b json.RawMessage) ([]model.AIOFile, bool) {
	var raw []tbFile
	if json.Unmarshal(b, &raw) != nil || raw == nil {
		return nil, false
	}
	out := []model.AIOFile{}
	for _, f := range raw {
		if f.ID == nil || *f.ID < 0 || f.Size == nil || *f.Size < 0 || f.Name == "" {
			return nil, false
		}
		out = append(out, model.AIOFile{ID: *f.ID, Path: f.Name, Size: *f.Size})
	}
	return out, true
}
func (p *ProviderHTTP) TBCheck(ctx context.Context, key string, hashes []string) ([]model.AIOAvailability, error) {
	if len(hashes) == 0 {
		return []model.AIOAvailability{}, nil
	}
	if len(hashes) > 100 {
		return nil, errors.New("TorBox batch exceeds 100 hashes")
	}
	for _, h := range hashes {
		if !hashRE.MatchString(h) {
			return nil, errors.New("invalid hash")
		}
	}
	q := url.Values{"hash": {strings.Join(hashes, ",")}, "format": {"object"}, "list_files": {"true"}}
	raw, err := p.request(ctx, "torbox", key, "GET", "torrents/checkcached?"+q.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	var items map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, ErrInvalidResponse
	}
	out := []model.AIOAvailability{}
	complete := true
	byHash := map[string]model.AIOAvailability{}
	for h, b := range items {
		var item struct {
			Hash, Name string
			Size       *int64
			Files      json.RawMessage
		}
		if json.Unmarshal(b, &item) != nil || !hashRE.MatchString(h) || !strings.EqualFold(h, item.Hash) || item.Name == "" || item.Size == nil || *item.Size < 0 {
			complete = false
			continue
		}
		v := model.AIOAvailability{Hash: strings.ToLower(h), Cached: true, CheckedAt: time.Now()}
		v.Shareable = true
		if files, ok := tbFiles(item.Files); ok {
			v.Files = files
		}
		byHash[v.Hash] = v
	}
	for _, h := range hashes {
		h = strings.ToLower(h)
		if v, ok := byHash[h]; ok {
			out = append(out, v)
		} else if complete {
			out = append(out, model.AIOAvailability{Hash: h, Cached: false, CheckedAt: time.Now(), Shareable: true})
		}
	}
	return out, nil
}
func (p *ProviderHTTP) TBPlay(ctx context.Context, key, hash, magnet string, r Resource) (link string, playErr error) {
	link, _, playErr = p.tbPrepare(ctx, key, hash, magnet, r, false, "")
	return
}

// Preparation IDs stay in the administrator's open view. Readiness checks
// read that account item without recreating a deleted or unfinished download.
// Verify its hash again before selecting or unlocking any file.
func (p *ProviderHTTP) tbPrepare(ctx context.Context, key, hash, magnet string, r Resource, cachedOnly bool, preparationID string) (link, prepared string, playErr error) {
	stage := "add"
	resuming := preparationID != ""
	defer func() {
		if playErr != nil {
			playErr = &playbackError{stage: stage, cause: playErr}
		}
	}()
	var b json.RawMessage
	var err error
	if preparationID == "" {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if cachedOnly {
			if err := form.WriteField("add_only_if_cached", "true"); err != nil {
				return "", prepared, err
			}
		}
		if err := form.WriteField("magnet", magnet); err != nil {
			return "", prepared, err
		}
		if err := form.Close(); err != nil {
			return "", prepared, err
		}
		b, err = p.request(ctx, "torbox", key, "POST", "torrents/createtorrent", &body, form.FormDataContentType())
		if err != nil {
			return "", prepared, err
		}
		var created struct {
			ID   *int64 `json:"torrent_id"`
			Hash string `json:"hash"`
		}
		if json.Unmarshal(b, &created) != nil || created.ID == nil || *created.ID < 0 || !strings.EqualFold(created.Hash, hash) {
			return "", prepared, ErrInvalidResponse
		}
		preparationID = itoa64(*created.ID)
	}
	prepared = preparationID
	q := url.Values{"bypass_cache": {"true"}, "id": {prepared}}
	stage = "file_list"
	b, err = p.request(ctx, "torbox", key, "GET", "torrents/mylist?"+q.Encode(), nil, "")
	if err != nil {
		return "", prepared, err
	}
	var item struct {
		Hash     string
		Finished bool `json:"download_finished"`
		Present  bool `json:"download_present"`
		Files    json.RawMessage
	}
	if json.Unmarshal(b, &item) != nil {
		return "", prepared, ErrInvalidResponse
	}
	if !strings.EqualFold(item.Hash, hash) {
		if resuming && item.Hash != "" {
			return "", prepared, newProviderError("not_found", "")
		}
		return "", prepared, ErrInvalidResponse
	}
	if !item.Finished || !item.Present {
		return "", prepared, ErrPending
	}
	files, ok := tbFiles(item.Files)
	if !ok {
		return "", prepared, ErrInvalidResponse
	}
	stage = "file_selection"
	file, ok := SelectFile(files, r)
	if !ok {
		return "", prepared, errors.New("no unambiguous matching video")
	}
	q = url.Values{"token": {key}, "torrent_id": {prepared}, "file_id": {itoa64(file.ID)}, "redirect": {"false"}}
	stage = "unlock"
	b, err = p.request(ctx, "torbox", key, "GET", "torrents/requestdl?"+q.Encode(), nil, "")
	if err != nil {
		return "", prepared, err
	}
	if json.Unmarshal(b, &link) != nil {
		return "", prepared, ErrInvalidResponse
	}
	link, err = HTTPSLink(link)
	return link, prepared, err
}
