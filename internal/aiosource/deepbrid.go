package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/theo-bnts/aiosources/internal/model"
)

type deepbridTorrent struct {
	ID       json.Number `json:"id"`
	Filename string      `json:"filename"`
	Status   string      `json:"status"`
	Links    []string    `json:"links"`
}

func (p *ProviderHTTP) dbPrepare(ctx context.Context, key, hash, magnet string, r Resource, cachedOnly bool, id string, saveID func(string) error) (result preparedPlayback, playErr error) {
	stage := "add"
	defer func() {
		if playErr != nil {
			playErr = &playbackError{stage: stage, cause: playErr}
		}
	}()
	if id == "" {
		// No documented global cache check or cached-only add operation exists.
		// A source without authorization to download must never be submitted.
		if cachedOnly {
			return result, newProviderError("not_cached", "")
		}
		var created struct {
			ID        json.Number `json:"id"`
			TorrentID json.Number `json:"torrent_id"`
		}
		if err := p.providerJSON(ctx, "deepbrid", key, http.MethodPost, "torrents/add", url.Values{"magnet": {magnet}}, &created); err != nil {
			return result, err
		}
		id = created.ID.String()
		if id == "" {
			id = created.TorrentID.String()
		}
		if created.ID != "" && created.TorrentID != "" && created.ID != created.TorrentID {
			return result, ErrInvalidResponse
		}
		if !validPositiveProviderID(id) {
			return result, ErrInvalidResponse
		}
		if err := saveID(id); err != nil {
			return result, err
		}
	}
	if !validPositiveProviderID(id) {
		return result, model.ErrInvalid
	}
	stage = "file_list"
	files, err := p.dbFiles(ctx, key, id)
	if err != nil {
		return result, err
	}
	if r.Type == "movie" || r.Episode == nil {
		candidates, unknown := 0, false
		for _, file := range files {
			if videoRE.MatchString(file.Path) && !sampleRE.MatchString(file.Path) {
				candidates++
				unknown = unknown || file.Size == 0
			}
		}
		if candidates > 1 && unknown {
			return result, errors.New("no unambiguous matching video")
		}
	}
	stage = "file_selection"
	file, ok := SelectFile(files, r)
	if !ok {
		return result, errors.New("no unambiguous matching video")
	}
	result.URL, result.Files = file.Link, files
	return result, nil
}

func (p *ProviderHTTP) dbFiles(ctx context.Context, key, id string) ([]model.AIOFile, error) {
	if !validPositiveProviderID(id) {
		return nil, model.ErrInvalid
	}
	var item deepbridTorrent
	if err := p.providerJSON(ctx, "deepbrid", key, http.MethodGet, "torrents/info?id="+url.QueryEscape(id), nil, &item); err != nil {
		return nil, err
	}
	if item.ID.String() != id {
		return nil, newProviderError("not_found", "")
	}
	switch item.Status {
	case "downloaded":
	case "queued", "magnet_conversion", "waiting_files_selection", "downloading", "compressing", "uploading":
		return nil, ErrPending
	case "error", "dead", "magnet_error", "virus":
		return nil, newProviderError("invalid_torrent", "")
	default:
		return nil, ErrInvalidResponse
	}
	if len(item.Links) == 0 || len(item.Links) > 100 {
		return nil, ErrInvalidResponse
	}
	files := []model.AIOFile{}
	for _, raw := range item.Links {
		link, err := HTTPSLink(raw)
		if err != nil {
			return nil, err
		}
		if len(item.Links) == 1 {
			// One named video can use the documented account download link.
			file := model.AIOFile{Path: item.Filename, Link: link}
			if _, ok := SelectFile([]model.AIOFile{file}, Resource{Type: "movie"}); ok {
				files = append(files, file)
				continue
			}
		}
		// Multi-file results must supply real file names before selection. Never
		// guess from an opaque short link or serve a ZIP as an episode.
		var generated struct {
			Filename string          `json:"filename"`
			Size     json.RawMessage `json:"size"`
			Link     string          `json:"link"`
		}
		if err := p.providerJSON(ctx, "deepbrid", key, http.MethodPost, "generate/link", url.Values{"link": {link}}, &generated); err != nil {
			return nil, err
		}
		link, err = HTTPSLink(generated.Link)
		if err != nil || generated.Filename == "" {
			return nil, ErrInvalidResponse
		}
		var sizeText string
		if json.Unmarshal(generated.Size, &sizeText) != nil {
			sizeText = string(generated.Size)
		}
		size, _ := model.ParseByteSize(sizeText)
		files = append(files, model.AIOFile{Path: generated.Filename, Link: link, Size: size})
	}
	if !validProviderFiles(files) {
		return nil, ErrInvalidResponse
	}
	return files, nil
}

func validPositiveProviderID(id string) bool {
	if !providerObjectID.MatchString(id) || len(id) > 19 {
		return false
	}
	value, err := json.Number(id).Int64()
	return err == nil && value > 0 && itoa64(value) == id
}
