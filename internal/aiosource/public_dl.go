package aiosource

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Read every inventory page before adding anything. A partial inventory cannot
// establish ownership: an add could return an existing item on a later page.
func (p *ProviderHTTP) dlInventory(ctx context.Context, key string) (map[string]bool, error) {
	prior := map[string]bool{}
	for page := 0; ; page++ {
		var items []dlTorrent
		if err := p.providerJSON(ctx, "debridlink", key, http.MethodGet, "seedbox/list?perPage=100&page="+strconv.Itoa(page), nil, &items); err != nil {
			return nil, err
		}
		if items == nil || len(items) > 100 || len(prior)+len(items) > 100000 {
			return nil, ErrInvalidResponse
		}
		for _, item := range items {
			if !providerObjectID.MatchString(item.ID) || prior[item.ID] {
				return nil, ErrInvalidResponse
			}
			prior[item.ID] = true
		}
		if len(items) < 100 {
			return prior, nil
		}
	}
}

func (p *ProviderHTTP) dlPublicCheck(ctx context.Context, key, hash string) (observed []model.AIOAvailability, checkErr error) {
	if err := validCheckHashes([]string{hash}); err != nil {
		return nil, err
	}
	prior, err := p.dlInventory(ctx, key)
	if err != nil {
		return nil, err
	}
	item, err := p.dlAdd(ctx, key, hash, hash)
	id := item.ID
	confirmed := strings.EqualFold(item.Hash, hash)
	defer func() {
		if !providerObjectID.MatchString(id) || prior[id] || !confirmed {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		var removed []string
		cleanupErr := p.providerJSON(cleanup, "debridlink", key, http.MethodDelete, "seedbox/"+url.PathEscape(id)+"/remove", nil, &removed)
		if cleanupErr == nil && !slices.Contains(removed, id) {
			cleanupErr = ErrInvalidResponse
		}
		if cleanupErr != nil && providerFailureCode(cleanupErr) != "not_found" && checkErr == nil {
			checkErr = cleanupErr
		}
	}()
	if err != nil {
		return nil, err
	}
	if item.Zip || item.Hash == "" {
		item, err = p.dlInfo(ctx, key, id, hash)
		confirmed = confirmed || strings.EqualFold(item.Hash, hash)
	}
	if err != nil {
		return nil, err
	}
	if item.Percent == nil || *item.Percent != 100 {
		return nil, nil
	}
	files, err := dlFiles(item, true)
	all, allErr := dlFiles(item, false)
	if err != nil || allErr != nil || len(files) != len(all) || len(files) == 0 {
		return nil, ErrInvalidResponse
	}
	return []model.AIOAvailability{{Hash: hash, Cached: true, CheckedAt: time.Now(), Files: files, Shareable: true}}, nil
}
