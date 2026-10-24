package aiosource

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

var providerObjectID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type rdTorrent struct {
	ID     string `json:"id"`
	Hash   string `json:"hash"`
	Status string `json:"status"`
	Files  []struct {
		ID       int64  `json:"id"`
		Path     string `json:"path"`
		Bytes    int64  `json:"bytes"`
		Selected int    `json:"selected"`
	} `json:"files"`
	Links []string `json:"links"`
}

func (p *ProviderHTTP) rdInfo(ctx context.Context, key, id, hash string) (rdTorrent, error) {
	var item rdTorrent
	if !providerObjectID.MatchString(id) || !hashRE.MatchString(hash) {
		return item, model.ErrInvalid
	}
	if err := p.providerJSON(ctx, "realdebrid", key, http.MethodGet, "torrents/info/"+id, nil, &item); err != nil {
		return item, err
	}
	if item.ID != id || !strings.EqualFold(item.Hash, hash) {
		return item, newProviderError("not_found", "")
	}
	if item.Status == "" {
		return item, ErrInvalidResponse
	}
	return item, nil
}

func rdFiles(item rdTorrent, selectedOnly bool) ([]model.AIOFile, error) {
	if item.Files == nil {
		return nil, ErrInvalidResponse
	}
	files, selected, seen := []model.AIOFile{}, 0, map[int64]bool{}
	for _, raw := range item.Files {
		if raw.ID < 1 || raw.Path == "" || raw.Bytes < 0 || seen[raw.ID] || (raw.Selected != 0 && raw.Selected != 1) {
			return nil, ErrInvalidResponse
		}
		seen[raw.ID] = true
		if selectedOnly && raw.Selected == 0 {
			continue
		}
		file := model.AIOFile{ID: raw.ID, Path: strings.TrimPrefix(raw.Path, "/"), Size: raw.Bytes}
		if raw.Selected == 1 {
			if selected < len(item.Links) {
				file.Link = item.Links[selected]
			}
			selected++
		}
		files = append(files, file)
	}
	if selectedOnly && (selected != len(item.Links) || selected == 0) {
		return nil, ErrInvalidResponse
	}
	if !validProviderFiles(files) {
		return nil, ErrInvalidResponse
	}
	return files, nil
}

// The documented API no longer exposes global instant availability. Reading
// the caller's ready inventory is useful evidence, but a miss stays unknown and
// these account-specific observations never enter the shared cache.
func (p *ProviderHTTP) RDCheck(ctx context.Context, key string, hashes []string) ([]model.AIOAvailability, error) {
	if err := validCheckHashes(hashes); err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, hash := range hashes {
		wanted[hash] = true
	}
	out := []model.AIOAvailability{}
	if len(wanted) == 0 {
		return out, nil
	}
	for page := 1; page <= 20 && len(wanted) > 0; page++ {
		var items []rdTorrent
		if err := p.providerJSON(ctx, "realdebrid", key, http.MethodGet, "torrents?limit=5000&page="+itoa64(int64(page)), nil, &items); err != nil {
			return out, err
		}
		if items == nil {
			return out, ErrInvalidResponse
		}
		for _, item := range items {
			hash := strings.ToLower(item.Hash)
			if !wanted[hash] || item.Status != "downloaded" {
				continue
			}
			info, err := p.rdInfo(ctx, key, item.ID, hash)
			if err != nil {
				return out, err
			}
			if info.Status != "downloaded" {
				continue
			}
			files, err := rdFiles(info, true)
			if err != nil {
				return out, err
			}
			out = append(out, model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Files: files})
			delete(wanted, hash)
		}
		if len(items) < 5000 {
			break
		}
	}
	return out, nil
}

type preparedPlayback struct {
	URL   string
	Files []model.AIOFile
}

func (p *ProviderHTTP) rdPrepare(ctx context.Context, key, hash, magnet string, r Resource, cachedOnly bool, id string, saveID func(string) error) (result preparedPlayback, playErr error) {
	stage := "inventory"
	defer func() {
		if playErr != nil {
			playErr = &playbackError{stage: stage, cause: playErr}
		}
	}()
	if id == "" && cachedOnly {
		// Never submit a torrent to test an unavailable global cache endpoint.
		for page := 1; page <= 20; page++ {
			var items []rdTorrent
			if err := p.providerJSON(ctx, "realdebrid", key, http.MethodGet, "torrents?limit=5000&page="+itoa64(int64(page)), nil, &items); err != nil {
				return result, err
			}
			if items == nil {
				return result, ErrInvalidResponse
			}
			for _, item := range items {
				if strings.EqualFold(item.Hash, hash) && item.Status == "downloaded" {
					id = item.ID
					break
				}
			}
			if id != "" || len(items) < 5000 {
				break
			}
		}
		if id == "" {
			return result, newProviderError("not_cached", "")
		}
	}
	if id == "" {
		stage = "add"
		var created struct {
			ID string `json:"id"`
		}
		if err := p.providerJSON(ctx, "realdebrid", key, http.MethodPost, "torrents/addMagnet", url.Values{"magnet": {magnet}}, &created); err != nil {
			return result, err
		}
		if !providerObjectID.MatchString(created.ID) {
			return result, ErrInvalidResponse
		}
		id = created.ID
		if err := saveID(id); err != nil {
			return result, err
		}
	}
	stage = "file_list"
	item, err := p.rdInfo(ctx, key, id, hash)
	if err != nil {
		return result, err
	}
	if item.Status == "waiting_files_selection" {
		// A continuation is bound to an earlier explicit download request. It
		// can complete that selection, but never creates a replacement transfer.
		if cachedOnly {
			return result, ErrPending
		}
		files, err := rdFiles(item, false)
		if err != nil {
			return result, err
		}
		if _, ok := SelectFile(files, r); !ok {
			return result, errors.New("no unambiguous matching video")
		}
		stage = "add"
		if err := p.providerJSON(ctx, "realdebrid", key, http.MethodPost, "torrents/selectFiles/"+id, url.Values{"files": {"all"}}, nil); err != nil {
			return result, err
		}
		item, err = p.rdInfo(ctx, key, id, hash)
		if err != nil {
			return result, err
		}
	}
	switch item.Status {
	case "downloaded":
	case "error", "dead", "magnet_error", "virus":
		return result, newProviderError("invalid_torrent", "")
	case "queued", "magnet_conversion", "waiting_files_selection", "downloading", "compressing", "uploading":
		return result, ErrPending
	default:
		return result, ErrInvalidResponse
	}
	files, err := rdFiles(item, true)
	if err != nil {
		return result, err
	}
	stage = "file_selection"
	file, ok := SelectFile(files, r)
	if !ok {
		return result, errors.New("no unambiguous matching video")
	}
	if _, err := HTTPSLink(file.Link); err != nil {
		return result, err
	}
	stage = "unlock"
	var unlocked struct {
		Download string `json:"download"`
	}
	if err := p.providerJSON(ctx, "realdebrid", key, http.MethodPost, "unrestrict/link", url.Values{"link": {file.Link}}, &unlocked); err != nil {
		return result, err
	}
	result.URL, err = HTTPSLink(unlocked.Download)
	result.Files = files
	return result, err
}

func validCheckHashes(hashes []string) error {
	if len(hashes) > 100 {
		return model.ErrInvalid
	}
	for _, hash := range hashes {
		if !hashRE.MatchString(hash) {
			return model.ErrInvalid
		}
	}
	return nil
}
