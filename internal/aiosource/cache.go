package aiosource

import (
	"context"
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"sort"
	"strings"
	"time"
)

func (s *Service) cacheFlight(service, key, resource string, hashes []string, c model.AIOConfig) (*flight, error) {
	return s.cacheFlightTriggered(service, key, resource, hashes, c, "playback", false)
}
func (s *Service) cacheFlightTriggered(service, key, resource string, hashes []string, c model.AIOConfig, trigger string, warm bool) (*flight, error) {
	account := fingerprint(key)
	identity := service + ":" + account + ":" + resource + ":" + fingerprint(strings.Join(hashes, ","))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, model.ErrConflict
	}
	previous := s.flights[identity]
	if previous != nil && (warm || !previous.warm) {
		return previous, nil
	}
	if previous == nil && len(s.flights) >= 64 {
		return nil, model.ErrBusy
	}
	ctx, cancel := context.WithCancel(s.ctx)
	f := &flight{done: make(chan struct{}), cancel: cancel, warm: warm}
	s.flights[identity] = f
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() {
			s.mu.Lock()
			if s.flights[identity] == f {
				delete(s.flights, identity)
			}
			close(f.done)
			s.mu.Unlock()
		}()
		if previous != nil {
			// An expired resource joining a warm retry needs a full check,
			// including previously positive hashes. Queue one owned follow-up
			// rather than treating the older retry as that full check.
			stopPrevious := context.AfterFunc(ctx, previous.cancel)
			defer stopPrevious()
			select {
			case <-previous.done:
			case <-ctx.Done():
				f.err = ctx.Err()
				return
			}
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		case <-ctx.Done():
			f.err = ctx.Err()
			return
		}
		f.err = s.checkResourceTriggered(ctx, service, key, resource, hashes, c, trigger)
	}()
	return f, nil
}
func RetryDelay(attempt int) time.Duration {
	return min(time.Duration(15)*time.Minute*time.Duration(1<<min(max(attempt, 0), 12)), 48*time.Hour)
}
func sameHashes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func freshResource(v model.AIOResource, hashes []string, now time.Time, ttl time.Duration) bool {
	return sameHashes(v.Hashes, hashes) && !now.Before(v.CheckedAt) && now.Sub(v.CheckedAt) < ttl
}
func (s *Service) checkResource(ctx context.Context, service, key, resource string, hashes []string, c model.AIOConfig) error {
	return s.checkResourceTriggered(ctx, service, key, resource, hashes, c, "playback")
}
func (s *Service) checkResourceTriggered(ctx context.Context, service, key, resource string, hashes []string, c model.AIOConfig, trigger string) (checkResult error) {
	account := fingerprint(key)
	release, locked, err := s.DB.AIOLock(ctx, "resource:"+service+":"+account+":"+resource)
	if err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer release()
	generation, err := s.DB.AIOCacheGeneration(ctx)
	if err != nil {
		return err
	}
	ttl := time.Duration(c.CacheMinutes) * time.Minute
	now := time.Now()
	v, found, err := s.DB.AIOResource(ctx, service, account, resource)
	if err != nil {
		return err
	}
	full := !found || !freshResource(v, hashes, now, ttl)
	finalAt := v.CheckedAt.Add(ttl - 2*time.Minute)
	finalDue := !now.Before(finalAt) && v.LastAttempt.Before(finalAt)
	if !full && now.Sub(v.LastAttempt) < RetryDelay(v.Retry-1) && !finalDue {
		return nil
	}
	todo := append([]string{}, hashes...)
	if !full {
		previous, err := s.DB.AIOAvailability(ctx, service, account, hashes, now.Add(-ttl))
		if err != nil {
			return err
		}
		positive := map[string]bool{}
		for _, a := range previous {
			if a.Cached {
				positive[a.Hash] = true
			}
		}
		todo = todo[:0]
		for _, h := range hashes {
			if !positive[h] {
				todo = append(todo, h)
			}
		}
		if len(todo) == 0 {
			return nil
		}
	}
	history, err := s.beginOperation(ctx, "cache", service, trigger, resource, "resource:"+service+":"+account+":"+resource)
	if err != nil {
		return err
	}
	defer func() { history.finish(ctx, checkResult) }()
	ctx = withProviderHistory(ctx, history)
	history.checkpoint(ctx, "availability")
	if full {
		v = model.AIOResource{Key: resource, Service: service, Account: account, Hashes: hashes, CheckedAt: now}
	}
	v.Generation = generation
	v.LastAttempt = now
	v.Retry++
	if err := s.DB.SaveAIOResource(ctx, v); err != nil {
		return err
	}
	var checkErr error
	if service == "torbox" {
		for start := 0; start < len(todo); start += 100 {
			results, e := s.Providers.TBCheck(ctx, key, todo[start:min(len(todo), start+100)])
			if e != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				history.run.Errors += int64(min(100, len(todo)-start))
				checkErr = e
				break
			}
			for _, a := range results {
				a.Generation = generation
				if e = s.DB.SaveAIOAvailability(ctx, service, account, a); e != nil {
					return e
				}
				history.run.Processed++
				if a.Cached {
					history.run.Results["cached"]++
				} else {
					history.run.Results["uncached"]++
				}
			}
			history.checkpoint(ctx, "availability")
		}
	} else {
		checkErr = s.checkADResource(ctx, key, todo, generation, history)
	}
	s.observe(service, checkErr)
	return checkErr
}
func (s *Service) withHash(ctx context.Context, account, hash string, work func() error) error {
	for {
		release, locked, err := s.DB.AIOLock(ctx, "magnet:"+account+":"+hash)
		if err != nil {
			return err
		}
		if locked {
			defer release()
			return work()
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type adProbe struct {
	done   chan struct{}
	result model.AIOAvailability
	err    error
}

func (s *Service) adCheck(ctx context.Context, key, hash string) (model.AIOAvailability, error) {
	identity := fingerprint(key) + ":" + hash
	s.mu.Lock()
	if p := s.probes[identity]; p != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return model.AIOAvailability{}, ctx.Err()
		case <-p.done:
			return p.result, p.err
		}
	}
	p := &adProbe{done: make(chan struct{})}
	s.probes[identity] = p
	s.mu.Unlock()
	p.result, p.err = s.adCheckOwned(ctx, key, hash)
	s.mu.Lock()
	delete(s.probes, identity)
	close(p.done)
	s.mu.Unlock()
	return p.result, p.err
}
func (s *Service) adCheckOwned(ctx context.Context, key, hash string) (model.AIOAvailability, error) {
	account := fingerprint(key)
	result := model.AIOAvailability{Hash: hash}
	err := s.withHash(ctx, account, hash, func() error {
		if err := s.cleanupHash(ctx, key, hash, false); err != nil {
			return err
		}
		inventory, err := s.Providers.ADInventory(ctx, key)
		if err != nil {
			return err
		}
		existing := existingAD(inventory, hash)
		var id int64
		owned := false
		if existing != nil {
			if *existing.Status < 0 || *existing.Status > 4 {
				return ErrInvalidResponse
			}
			id = existing.ID
			result.Cached = *existing.Status == 4
		} else {
			added, e := s.Providers.ADAdd(ctx, key, hash, hash)
			id = added.ID
			if id > 0 {
				owned = true
				for _, m := range inventory {
					if m.ID == id {
						owned = false
					}
				}
			}
			if owned {
				persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				err = s.DB.AddAIOCleanup(persistCtx, model.AIOCleanup{Account: account, Hash: hash, ID: id})
				cancel()
				if err != nil {
					// Ownership is already confirmed. If durable storage fails, compensate
					// directly rather than abandoning a provider-side download.
					cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
					deletion := s.Providers.ADDelete(cleanupCtx, key, id)
					stop()
					if deletion != nil {
						s.observe("cleanup", deletion)
					}
					return err
				}
			}
			if owned {
				defer s.enqueueCleanup(ctx, key, hash)
			}
			if e != nil {
				return e
			}
			result.Cached = *added.Ready
		}
		result.CheckedAt = time.Now()
		if result.Cached {
			if files, e := s.Providers.ADFiles(ctx, key, id); e == nil {
				result.Files = files
				result.Shareable = owned && len(files) > 0
			}
		}
		return nil
	})
	return result, err
}
func (s *Service) enqueueCleanup(parent context.Context, key, hash string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	// Ownership is already durable. Leave excess jobs for recovery instead of
	// opening an unbounded number of database locks or provider requests.
	select {
	case s.cleanupSlots <- struct{}{}:
	default:
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() { <-s.cleanupSlots }()
		ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
		defer cancel()
		// Keep the safe operation reference, without inheriting the caller's
		// cancellation into cleanup whose ownership is already durable.
		if h, ok := parent.Value(providerHistoryKey{}).(providerCallHistory); ok {
			ctx = context.WithValue(ctx, providerHistoryKey{}, h)
		}
		_ = s.withHash(ctx, fingerprint(key), hash, func() error { return s.cleanupHash(ctx, key, hash, false) })
	}()
}
func (s *Service) cleanupHash(ctx context.Context, key, hash string, retry bool) error {
	jobs, err := s.DB.AIOCleanup(ctx, fingerprint(key))
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Hash != hash {
			continue
		}
		if j.Failed && !retry {
			return errors.New("temporary magnet cleanup requires attention")
		}
		err = s.Providers.ADDelete(ctx, key, j.ID)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		saved := s.DB.FinishAIOCleanup(persist, j, err == nil)
		cancel()
		if saved != nil {
			return saved
		}
		if err != nil {
			s.observe("cleanup", err)
			return err
		}
	}
	return nil
}
func (s *Service) RecoverCleanup(ctx context.Context, key string, retry bool) (cleanupResult error) {
	jobs, err := s.DB.AIOCleanup(ctx, fingerprint(key))
	if err != nil || len(jobs) == 0 {
		return err
	}
	lock := "cleanup:" + fingerprint(key)
	release, locked, err := s.DB.AIOLock(ctx, lock)
	if err != nil || !locked {
		return err
	}
	defer release()
	// An overlapping cleanup may have completed while this caller waited.
	jobs, err = s.DB.AIOCleanup(ctx, fingerprint(key))
	if err != nil || len(jobs) == 0 {
		return err
	}
	trigger := "automatic"
	if retry {
		trigger = "manual"
	}
	history, err := s.beginOperation(ctx, "cleanup", "alldebrid", trigger, "", lock)
	if err != nil {
		return err
	}
	defer func() { history.finish(ctx, cleanupResult) }()
	ctx = withProviderHistory(ctx, history)
	history.checkpoint(ctx, "cleanup")
	inventory, err := s.Providers.ADInventory(ctx, key)
	if err != nil {
		return err
	}
	existing := map[int64]bool{}
	for _, m := range inventory {
		existing[m.ID] = true
	}
	for _, j := range jobs {
		if !existing[j.ID] {
			if err = s.DB.FinishAIOCleanup(ctx, j, true); err != nil {
				return err
			}
			history.run.Processed++
			history.run.Results["cleaned"]++
			continue
		}
		if j.Failed && !retry {
			continue
		}
		err = s.withHash(ctx, j.Account, j.Hash, func() error { return s.cleanupHash(ctx, key, j.Hash, retry) })
		if err != nil {
			return err
		}
		history.run.Processed++
		history.run.Results["cleaned"]++
		history.checkpoint(ctx, "cleanup")
	}
	remaining, err := s.DB.AIOCleanup(ctx, fingerprint(key))
	if err == nil && len(remaining) == 0 {
		s.observe("cleanup", nil)
	}
	return err
}
func (s *Service) Availability(ctx context.Context, c model.AIOConfig, resource string, hashes []string) (map[string]map[string]model.AIOAvailability, error) {
	sort.Strings(hashes)
	hashes = uniqueStrings(hashes)
	out := map[string]map[string]model.AIOAvailability{}
	keys := map[string]string{}
	flights := []*flight{}
	for service, name := range map[string]string{"alldebrid": c.AllDebridSecret, "torbox": c.TorBoxSecret} {
		if name == "" {
			continue
		}
		key, err := s.key(ctx, name)
		if err != nil {
			return nil, err
		}
		keys[service] = key
		// Decide from the resource that existed before starting this check.
		// A first check writes its retry state before any observation arrives;
		// reading after launch could incorrectly treat a cold check as warm.
		shared, err := s.DB.SharedAvailability(ctx, service, hashes, time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
		if err != nil {
			return nil, err
		}
		out[service] = map[string]model.AIOAvailability{}
		for _, a := range shared {
			out[service][a.Hash] = a
		}
		missing := []string{}
		for _, h := range hashes {
			if _, ok := out[service][h]; !ok {
				missing = append(missing, h)
			}
		}
		if len(missing) == 0 {
			continue
		}
		resourceState, found, err := s.DB.AIOResource(ctx, service, fingerprint(key), resource)
		if err != nil {
			return nil, err
		}
		warm := found && freshResource(resourceState, missing, time.Now(), time.Duration(c.CacheMinutes)*time.Minute)
		f, err := s.cacheFlightTriggered(service, key, resource, missing, c, "playback", warm)
		if err == nil && (!warm || !f.warm) {
			flights = append(flights, f)
		} else if err != nil && !errors.Is(err, model.ErrBusy) {
			return nil, err
		}
	}
	wait, cancel := context.WithTimeout(ctx, time.Duration(c.ResponseSeconds)*time.Second)
	defer cancel()
	for _, f := range flights {
		select {
		case <-f.done:
		case <-wait.Done():
		}
	}
	for service, key := range keys {
		items, err := s.DB.AIOAvailability(ctx, service, fingerprint(key), hashes, time.Now().Add(-time.Duration(c.CacheMinutes)*time.Minute))
		if err != nil {
			return nil, err
		}
		for _, a := range items {
			if current, ok := out[service][a.Hash]; !ok || a.CheckedAt.After(current.CheckedAt) {
				out[service][a.Hash] = a
			}
		}
	}
	return out, nil
}
func uniqueStrings(in []string) []string {
	out := []string{}
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
