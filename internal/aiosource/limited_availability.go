package aiosource

import (
	"context"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type availabilityPlan struct {
	states map[string]map[string]model.AIOAvailability
	keys   map[string]string
	public bool
}

// Loading known observations never starts provider work. All selected hashes
// participate, so a result limit can prioritize already-known cache hits.
func (s *Service) planAvailability(ctx context.Context, c model.AIOConfig, hashes []string, visitor *PublicInstallation) (*availabilityPlan, error) {
	p := &availabilityPlan{states: map[string]map[string]model.AIOAvailability{}, keys: map[string]string{}, public: visitor != nil}
	for _, service := range model.DebridServices() {
		key := ""
		if visitor != nil {
			key = visitor.Services[service]
		} else if ref := c.DebridSecret(service); ref != "" {
			var err error
			key, err = s.key(ctx, ref)
			if err != nil {
				return nil, err
			}
		}
		if key == "" {
			continue
		}
		p.keys[service] = key
		p.states[service] = map[string]model.AIOAvailability{}
		if err := s.readPlannedAvailability(ctx, c, p, service, hashes); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (s *Service) readPlannedAvailability(ctx context.Context, c model.AIOConfig, p *availabilityPlan, service string, hashes []string) error {
	after := time.Now().Add(-time.Duration(c.CacheMinutes) * time.Minute)
	items, err := s.DB.SharedAvailability(ctx, service, hashes, after)
	if err != nil {
		return err
	}
	for _, a := range items {
		p.states[service][a.Hash] = a
	}
	if !p.public {
		items, err = s.DB.AIOAvailability(ctx, service, fingerprint(p.keys[service]), hashes, after)
		if err != nil {
			return err
		}
		for _, a := range items {
			current, ok := p.states[service][a.Hash]
			if ok && service == "alldebrid" && current.Cached && !a.Cached {
				continue
			}
			if !ok || a.CheckedAt.After(current.CheckedAt) {
				p.states[service][a.Hash] = a
			}
		}
	} else if service == "deepbrid" {
		generation, err := s.DB.AIOCacheGeneration(ctx)
		if err != nil {
			return err
		}
		for _, hash := range hashes {
			if item := s.visitorPreparation(service, p.keys[service], hash); item.Generation == generation && len(item.Files) > 0 {
				p.states[service][hash] = model.AIOAvailability{Hash: hash, Cached: true, Files: item.Files, CheckedAt: time.Now()}
			}
		}
	}
	return nil
}

// A cache target limits new work, not the truth of previously saved evidence.
// Batches never contain more hashes than the remaining target. They are joined
// before another starts; no speculative job continues checking the rest later.
func (s *Service) limitedAvailability(ctx context.Context, c model.AIOConfig, resource string, hashes []string, p *availabilityPlan, target int, r Resource, allowNoFiles map[string]bool) (map[string]map[string]model.AIOAvailability, error) {
	unique := []string{}
	seen := map[string]bool{}
	for _, hash := range hashes {
		if !seen[hash] {
			unique = append(unique, hash)
			seen[hash] = true
		}
	}
	count := func() int {
		n := 0
		for _, states := range p.states {
			for _, hash := range unique {
				if a, ok := states[hash]; ok && a.Cached {
					if _, file := SelectFile(a.Files, r); file || (r.Type == "book" && len(bookFiles(a.Files)) > 0) || (r.Episode == nil && len(a.Files) == 0 && allowNoFiles[hash]) {
						n++
					}
				}
			}
		}
		return n
	}
	check, cancel := context.WithTimeout(ctx, time.Duration(c.ResponseSeconds)*time.Second)
	defer cancel()
	var negativeRetryBefore time.Time
	if !p.public && p.keys["alldebrid"] != "" {
		delay, err := s.DB.RuntimeValue(ctx, "debrid_retries", "delay_minutes")
		if err != nil {
			return nil, err
		}
		negativeRetryBefore = time.Now().Add(-time.Duration(delay) * time.Minute)
	}
	stopped := map[string]bool{}
	for start := 0; start < len(unique) && count() < target && check.Err() == nil; {
		end := min(len(unique), start+min(100, target-count()))
		for _, service := range model.DebridServices() {
			if p.keys[service] == "" || stopped[service] || count() >= target || check.Err() != nil {
				continue
			}
			todo := []string{}
			for _, hash := range unique[start:end] {
				observed, known := p.states[service][hash]
				// Account-only AD misses use the existing retry clock, rather
				// than staying negative for the entire positive-cache lifetime.
				// checkResourceTriggered still enforces its saved backoff.
				retry := service == "alldebrid" && !p.public && !observed.Cached && !observed.CheckedAt.After(negativeRetryBefore)
				if !known || retry {
					todo = append(todo, hash)
				}
			}
			if len(todo) == 0 {
				continue
			}
			todo = todo[:min(len(todo), target-count())]
			key := p.keys[service]
			var err error
			if p.public && service != "alldebrid" {
				var items []model.AIOAvailability
				items, err = s.checkPublicService(check, service, key, resource, todo)
				for _, item := range items {
					p.states[service][item.Hash] = item
				}
			} else {
				// Public AllDebrid checks retain only the administrator's own
				// recoverable probes, never a visitor's key or account objects.
				if p.public {
					if c.AllDebridSecret == "" {
						stopped[service] = true
						continue
					}
					key, err = s.key(check, c.AllDebridSecret)
					if err != nil {
						stopped[service] = true
						continue
					}
				}
				if err = s.acquireCapacity(check, &s.slots, "cache_jobs", true); err == nil {
					batchResource := resource + ":limited:" + fingerprint(strings.Join(todo, ","))
					err = s.checkResourceTriggered(check, service, key, batchResource, todo, c, "playback")
					s.slots.release()
				}
			}
			if err != nil {
				stopped[service] = true
				s.observe(service, err)
			}
			// A provider timeout must not turn successful local results into
			// an HTTP error. Read them with the caller's remaining delivery time.
			if e := s.readPlannedAvailability(ctx, c, p, service, todo); e != nil {
				return nil, e
			}
		}
		start = end
	}
	return p.states, nil
}
