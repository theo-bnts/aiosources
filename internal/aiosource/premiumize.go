package aiosource

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (p *ProviderHTTP) pmCached(ctx context.Context, key string, hashes []string) ([]bool, error) {
	if err := validCheckHashes(hashes); err != nil {
		return nil, err
	}
	if len(hashes) == 0 {
		return []bool{}, nil
	}
	var out struct {
		Response []*bool `json:"response"`
	}
	if err := p.providerJSON(ctx, "premiumize", key, http.MethodPost, "cache/check", url.Values{"items[]": hashes}, &out); err != nil {
		return nil, err
	}
	if len(out.Response) != len(hashes) {
		return nil, ErrInvalidResponse
	}
	results := make([]bool, len(hashes))
	for i, state := range out.Response {
		if state == nil {
			return nil, ErrInvalidResponse
		}
		results[i] = *state
	}
	return results, nil
}

func (p *ProviderHTTP) pmDirect(ctx context.Context, key, source string) ([]model.AIOFile, error) {
	var out struct {
		Content []struct {
			Path string `json:"path"`
			Size *int64 `json:"size"`
			Link string `json:"link"`
		} `json:"content"`
	}
	if err := p.providerJSON(ctx, "premiumize", key, http.MethodPost, "transfer/directdl", url.Values{"src": {source}}, &out); err != nil {
		return nil, err
	}
	if len(out.Content) == 0 || len(out.Content) > 10000 {
		return nil, ErrInvalidResponse
	}
	files := []model.AIOFile{}
	for _, raw := range out.Content {
		if raw.Path == "" || raw.Size == nil || *raw.Size < 0 {
			return nil, ErrInvalidResponse
		}
		link, err := HTTPSLink(raw.Link)
		if err != nil {
			return nil, err
		}
		files = append(files, model.AIOFile{Path: raw.Path, Size: *raw.Size, Link: link})
	}
	if !validProviderFiles(files) {
		return nil, ErrInvalidResponse
	}
	return files, nil
}

func (p *ProviderHTTP) PMCheck(ctx context.Context, key string, hashes []string) ([]model.AIOAvailability, error) {
	states, err := p.pmCached(ctx, key, hashes)
	if err != nil {
		return nil, err
	}
	out := []model.AIOAvailability{}
	for i, cached := range states {
		item := model.AIOAvailability{Hash: hashes[i], Cached: cached, Shareable: true, CheckedAt: time.Now()}
		if cached {
			// Listing cached files is read-only: no cloud transfer is created.
			// Failed listings stay unknown and stop this batch. Publishing an
			// incomplete hit would prevent the requested episode from being
			// rechecked until cache expiry, and ignoring quota errors would keep
			// calling an already rate-limited account.
			item.Files, err = p.pmDirect(ctx, key, "magnet:?xt=urn:btih:"+hashes[i])
			if err != nil {
				return out, err
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (p *ProviderHTTP) pmPrepare(ctx context.Context, key, hash, magnet string, r Resource, cachedOnly bool, id string, saveID func(string) error) (result preparedPlayback, playErr error) {
	stage := "inventory"
	defer func() {
		if playErr != nil {
			playErr = &playbackError{stage: stage, cause: playErr}
		}
	}()
	if id != "" {
		var transfers struct {
			Transfers []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"transfers"`
		}
		if !providerObjectID.MatchString(id) {
			return result, model.ErrInvalid
		}
		if err := p.providerJSON(ctx, "premiumize", key, http.MethodGet, "transfer/list", nil, &transfers); err != nil {
			return result, err
		}
		found := false
		for _, transfer := range transfers.Transfers {
			if transfer.ID != id {
				continue
			}
			found = true
			switch transfer.Status {
			case "finished", "seeding":
			case "queued", "running", "waiting", "downloading":
				return result, ErrPending
			case "error", "deleted":
				return result, newProviderError("not_found", "")
			default:
				return result, ErrInvalidResponse
			}
			break
		}
		if !found {
			return result, newProviderError("not_found", "")
		}
	}
	states, err := p.pmCached(ctx, key, []string{hash})
	if err != nil {
		return result, err
	}
	if !states[0] {
		if cachedOnly || id != "" {
			return result, newProviderError("not_cached", "")
		}
		stage = "add"
		var created struct {
			ID string `json:"id"`
		}
		if err := p.providerJSON(ctx, "premiumize", key, http.MethodPost, "transfer/create", url.Values{"src": {magnet}}, &created); err != nil {
			return result, err
		}
		if !providerObjectID.MatchString(created.ID) {
			return result, ErrInvalidResponse
		}
		if err := saveID(created.ID); err != nil {
			return result, err
		}
		return result, ErrPending
	}
	stage = "file_list"
	files, err := p.pmDirect(ctx, key, "magnet:?xt=urn:btih:"+hash)
	if err != nil {
		return result, err
	}
	stage = "file_selection"
	file, ok := SelectFile(files, r)
	if !ok {
		return result, fileSelectionError(files, r)
	}
	result.URL, result.Files = file.Link, files
	return result, nil
}
