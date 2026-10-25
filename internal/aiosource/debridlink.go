package aiosource

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type dlTorrent struct {
	ID      string   `json:"id"`
	Hash    string   `json:"hashString"`
	Zip     bool     `json:"isZip"`
	Percent *float64 `json:"downloadPercent"`
	Files   []struct {
		Name    string   `json:"name"`
		Size    *int64   `json:"size"`
		URL     string   `json:"downloadUrl"`
		Percent *float64 `json:"downloadPercent"`
	} `json:"files"`
}

func (p *ProviderHTTP) dlInfo(ctx context.Context, key, id, hash string) (dlTorrent, error) {
	var out []dlTorrent
	if !providerObjectID.MatchString(id) {
		return dlTorrent{}, model.ErrInvalid
	}
	if err := p.providerJSON(ctx, "debridlink", key, http.MethodGet, "seedbox/list?ids="+url.QueryEscape(id), nil, &out); err != nil {
		return dlTorrent{}, err
	}
	for _, item := range out {
		if item.ID == id {
			if item.Hash == "" {
				return item, ErrPending
			}
			if strings.EqualFold(item.Hash, hash) {
				return item, nil
			}
		}
	}
	return dlTorrent{}, newProviderError("not_found", "")
}

func dlFiles(item dlTorrent, readyOnly bool) ([]model.AIOFile, error) {
	if item.Zip || item.Files == nil || len(item.Files) > 10000 {
		return nil, ErrInvalidResponse
	}
	out := []model.AIOFile{}
	for _, file := range item.Files {
		if file.Name == "" || file.Size == nil || *file.Size < 0 || file.Percent == nil || *file.Percent < 0 || *file.Percent > 100 {
			return nil, ErrInvalidResponse
		}
		if readyOnly && *file.Percent != 100 {
			continue
		}
		if *file.Percent == 100 {
			if _, err := HTTPSLink(file.URL); err != nil {
				return nil, err
			}
		}
		out = append(out, model.AIOFile{Path: file.Name, Size: *file.Size, Link: file.URL})
	}
	if !validProviderFiles(out) {
		return nil, ErrInvalidResponse
	}
	return out, nil
}

func (p *ProviderHTTP) dlAdd(ctx context.Context, key, hash, source string) (dlTorrent, error) {
	var item dlTorrent
	if err := p.providerJSON(ctx, "debridlink", key, http.MethodPost, "seedbox/add", url.Values{"url": {source}, "structureType": {"list"}}, &item); err != nil {
		return item, err
	}
	if !providerObjectID.MatchString(item.ID) || (item.Hash != "" && !strings.EqualFold(item.Hash, hash)) {
		return item, ErrInvalidResponse
	}
	return item, nil
}

// The official API accepts a bare hash only if it is already cached. Unlike
// submitting a magnet, this cannot start a new uncached transfer or send trackers.
func (p *ProviderHTTP) DLCheck(ctx context.Context, key string, hashes []string) ([]model.AIOAvailability, error) {
	if err := validCheckHashes(hashes); err != nil {
		return nil, err
	}
	out := []model.AIOAvailability{}
	for _, hash := range hashes {
		item, err := p.dlAdd(ctx, key, hash, hash)
		if err != nil {
			// Generic add failures do not prove a negative cache observation.
			if stopProviderChecks(err) {
				return out, err
			}
			continue
		}
		if item.Zip || item.Hash == "" {
			item, err = p.dlInfo(ctx, key, item.ID, hash)
		}
		if err != nil {
			if stopProviderChecks(err) {
				return out, err
			}
			continue
		}
		if item.Percent == nil || *item.Percent != 100 {
			continue
		}
		files, err := dlFiles(item, true)
		all, allErr := dlFiles(item, false)
		if err != nil || allErr != nil || len(files) != len(all) || len(files) == 0 {
			continue
		}
		out = append(out, model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Files: files, Shareable: true})
	}
	return out, nil
}

func (p *ProviderHTTP) dlPrepare(ctx context.Context, key, hash, magnet string, r Resource, cachedOnly bool, id string, saveID func(string) error) (result preparedPlayback, playErr error) {
	stage := "add"
	defer func() {
		if playErr != nil {
			playErr = &playbackError{stage: stage, cause: playErr}
		}
	}()
	var item dlTorrent
	var err error
	if id == "" {
		source := magnet
		if cachedOnly {
			source = hash
		}
		item, err = p.dlAdd(ctx, key, hash, source)
		if err != nil {
			return result, err
		}
		id = item.ID
		if err := saveID(id); err != nil {
			return result, err
		}
	} else {
		stage = "file_list"
		item, err = p.dlInfo(ctx, key, id, hash)
		if err != nil {
			return result, err
		}
	}
	if item.Zip || item.Hash == "" {
		item, err = p.dlInfo(ctx, key, id, hash)
		if err != nil {
			return result, err
		}
	}
	if item.Percent == nil || *item.Percent != 100 || item.Files == nil || len(item.Files) == 0 {
		return result, ErrPending
	}
	stage = "file_selection"
	all, err := dlFiles(item, false)
	if err != nil {
		return result, err
	}
	selected, ok := SelectFile(all, r)
	if !ok {
		return result, errors.New("no unambiguous matching video")
	}
	ready, err := dlFiles(item, true)
	if err != nil {
		return result, err
	}
	if len(ready) != len(all) {
		return result, ErrPending
	}
	for _, file := range ready {
		if file.Path == selected.Path && file.Size == selected.Size {
			result.URL, result.Files = file.Link, ready
			return result, nil
		}
	}
	return result, ErrPending
}
