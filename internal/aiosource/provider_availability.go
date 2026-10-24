package aiosource

import (
	"context"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func validProviderFiles(files []model.AIOFile) bool {
	if len(files) > 10000 {
		return false
	}
	paths := make(map[string]bool, len(files))
	for _, file := range files {
		if file.Path == "" || len(file.Path) > 4096 || strings.ContainsAny(file.Path, "\x00\r\n") || file.Size < 0 || paths[file.Path] {
			return false
		}
		paths[file.Path] = true
	}
	return true
}

func (s *Service) checkProviderAvailability(ctx context.Context, service, key string, hashes []string, public bool) ([]model.AIOAvailability, error) {
	if service == "deepbrid" {
		ids := map[string]string{}
		if public {
			for _, hash := range hashes {
				if item := s.visitorPreparation(service, key, hash); item.ID != "" {
					ids[hash] = item.ID
				}
			}
		} else {
			var err error
			ids, err = s.DB.AIOPreparations(ctx, service, fingerprint(key), hashes)
			if err != nil {
				return nil, err
			}
		}
		items := []model.AIOAvailability{}
		for _, hash := range hashes {
			id := ids[hash]
			if id == "" {
				continue
			}
			files, err := s.Providers.dbFiles(ctx, key, id)
			if err != nil {
				if stopProviderChecks(err) {
					return items, err
				}
				continue
			}
			items = append(items, model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Files: files})
		}
		return items, nil
	}
	if service == "debridlink" {
		items := []model.AIOAvailability{}
		for _, hash := range hashes {
			err := s.withHash(ctx, service+":"+fingerprint(key), hash, func() error {
				observed, err := s.Providers.DLCheck(ctx, key, []string{hash})
				items = append(items, observed...)
				return err
			})
			if err != nil {
				return items, err
			}
		}
		return items, nil
	}
	return s.Providers.Check(ctx, service, key, hashes)
}

func (p *ProviderHTTP) Check(ctx context.Context, service, key string, hashes []string) ([]model.AIOAvailability, error) {
	switch service {
	case "torbox":
		return p.TBCheck(ctx, key, hashes)
	case "premiumize":
		return p.PMCheck(ctx, key, hashes)
	case "realdebrid":
		return p.RDCheck(ctx, key, hashes)
	case "debridlink":
		return p.DLCheck(ctx, key, hashes)
	case "deepbrid":
		return []model.AIOAvailability{}, nil
	default:
		return nil, model.ErrInvalid
	}
}

func (s *Service) checkPublicService(ctx context.Context, service, key, resource string, hashes []string) (observed []model.AIOAvailability, checkErr error) {
	if err := s.acquireCapacity(ctx, &s.publicProbe, "public_checks", true); err != nil {
		return nil, err
	}
	defer s.publicProbe.release()
	lock := "public-cache:" + service + ":" + resource
	if service == "realdebrid" {
		lock += ":" + fingerprint(key)
	}
	var release func()
	for {
		var locked bool
		var err error
		release, locked, err = s.DB.AIOLock(ctx, lock)
		if err != nil {
			return nil, err
		}
		if locked {
			break
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	defer release()
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	previous, err := s.DB.SharedAvailability(ctx, service, hashes, time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, item := range previous {
		known[item.Hash] = true
	}
	todo := []string{}
	for _, hash := range hashes {
		if !known[hash] {
			todo = append(todo, hash)
		}
	}
	if len(todo) == 0 {
		return nil, nil
	}
	h, err := s.beginOperation(ctx, "cache", service, "playback", resource, lock)
	if err != nil {
		return nil, err
	}
	defer func() { h.finish(ctx, checkErr) }()
	ctx = withProviderHistory(ctx, h)
	h.checkpoint(ctx, "availability")
	generation, err := s.DB.AIOCacheGeneration(ctx)
	if err != nil {
		return nil, err
	}
	for start := 0; start < len(todo); start += 100 {
		items, e := s.checkProviderAvailability(ctx, service, key, todo[start:min(len(todo), start+100)], true)
		// A provider can return verified items before a later file-list request
		// times out. Preserve that evidence without extending provider calls or
		// retaining any visitor account data. Cache resets still fence it out.
		save, cancel := context.WithCancel(ctx)
		if ctx.Err() != nil {
			cancel()
			save, cancel = context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		}
		current, err := s.DB.AIOCacheGeneration(save)
		if err != nil {
			cancel()
			return observed, err
		}
		if current != generation {
			cancel()
			return nil, e
		}
		for _, item := range items {
			item.Generation = generation
			// Inventory observations are scoped to this request/account. No
			// visitor fingerprint, object ID or credential is written to cache.
			if item.Shareable {
				if err := s.DB.SaveSharedAvailability(save, service, item); err != nil {
					cancel()
					return observed, err
				}
			}
			observed = append(observed, item)
			h.run.Processed++
			if item.Cached {
				h.run.Results["cached"]++
			} else {
				h.run.Results["uncached"]++
			}
		}
		cancel()
		if e != nil {
			h.run.Errors++
			return observed, e
		}
		h.checkpoint(ctx, "availability")
	}
	return observed, nil
}
