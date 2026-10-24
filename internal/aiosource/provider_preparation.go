package aiosource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Some services expose string IDs or omit the hash on subsequent responses.
// Seal the binding instead of trusting caller-supplied provider object IDs.
// Tickets contain no API key, announce URL or download URL.
type providerPreparation struct {
	Service string `json:"service"`
	Account string `json:"account"`
	Hash    string `json:"hash"`
	ID      string `json:"id"`
	Expires int64  `json:"expires"`
}

type visitorPreparation struct {
	ID         string
	Expires    time.Time
	Files      []model.AIOFile
	Generation int64
}

// Bounded, short-lived deduplication has no keys or announce URLs. Nothing in
// this map is written to PostgreSQL or restored after a process restart.
func (s *Service) visitorPreparation(service, key, hash string) visitorPreparation {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity := service + ":" + fingerprint(key) + ":" + hash
	item := s.visitorPreparations[identity]
	if time.Now().After(item.Expires) {
		delete(s.visitorPreparations, identity)
		return visitorPreparation{}
	}
	return item
}

func (s *Service) rememberVisitorPreparation(service, key, hash, id string, files []model.AIOFile, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.visitorPreparations == nil {
		s.visitorPreparations = map[string]visitorPreparation{}
	}
	identity := service + ":" + fingerprint(key) + ":" + hash
	for saved, item := range s.visitorPreparations {
		if time.Now().After(item.Expires) {
			delete(s.visitorPreparations, saved)
		}
	}
	if _, exists := s.visitorPreparations[identity]; !exists && len(s.visitorPreparations) >= 512 {
		oldest := ""
		for saved, item := range s.visitorPreparations {
			if oldest == "" || item.Expires.Before(s.visitorPreparations[oldest].Expires) {
				oldest = saved
			}
		}
		delete(s.visitorPreparations, oldest)
	}
	safe := append([]model.AIOFile{}, files...)
	bytes := 0
	for _, file := range safe {
		bytes += len(file.Path) + 32
	}
	if len(safe) > 128 || bytes > 64<<10 {
		safe = nil
	}
	for i := range safe {
		safe[i].Link = ""
		safe[i].ID = 0
	}
	s.visitorPreparations[identity] = visitorPreparation{ID: id, Expires: time.Now().Add(30 * time.Minute), Files: safe, Generation: generation}
}

func (s *Service) sealPreparation(service, key, hash, id string) (string, error) {
	if !model.IsDebridService(service) || !providerObjectID.MatchString(id) || !hashRE.MatchString(hash) {
		return "", model.ErrInvalid
	}
	data, err := json.Marshal(providerPreparation{service, fingerprint(key), hash, id, time.Now().Add(7 * 24 * time.Hour).Unix()})
	if err != nil {
		return "", err
	}
	defer clear(data)
	sealed, err := s.Vault.Seal("provider-preparation-v1", data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) openPreparation(service, key, hash, token string) (string, error) {
	if len(token) < 32 || len(token) > 1024 {
		return "", model.ErrInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", model.ErrInvalid
	}
	data, err := s.Vault.Open("provider-preparation-v1", sealed)
	if err != nil {
		return "", model.ErrInvalid
	}
	defer clear(data)
	var saved providerPreparation
	if json.Unmarshal(data, &saved) != nil || saved.Service != service || saved.Account != fingerprint(key) || saved.Hash != hash || !providerObjectID.MatchString(saved.ID) || saved.Expires <= time.Now().Unix() {
		return "", model.ErrConflict
	}
	return saved.ID, nil
}

func (s *Service) prepareProvider(ctx context.Context, service, key string, rec model.AIORecord, magnet string, r Resource, cachedOnly bool, policy playbackPolicy, generation int64, public bool) (link string, prepareErr error) {
	err := s.withHash(ctx, service+":"+fingerprint(key), rec.Hash, func() error {
		link, prepareErr = s.prepareProviderOwned(ctx, service, key, rec, magnet, r, cachedOnly, policy, generation, public)
		return prepareErr
	})
	return link, err
}

func (s *Service) prepareProviderOwned(ctx context.Context, service, key string, rec model.AIORecord, magnet string, r Resource, cachedOnly bool, policy playbackPolicy, generation int64, public bool) (string, error) {
	started := time.Now()
	id := ""
	if policy.PreparationID != "" {
		var err error
		id, err = s.openPreparation(service, key, rec.Hash, policy.PreparationID)
		if err != nil {
			return "", err
		}
	} else if public {
		id = s.visitorPreparation(service, key, rec.Hash).ID
	} else {
		var err error
		id, err = s.DB.AIOPreparation(ctx, service, fingerprint(key), rec.Hash)
		if err != nil {
			return "", err
		}
	}
	saveID := func(id string) error {
		if policy.PreparedID != nil {
			token, err := s.sealPreparation(service, key, rec.Hash, id)
			if err != nil {
				return err
			}
			*policy.PreparedID = token
		}
		if public {
			s.rememberVisitorPreparation(service, key, rec.Hash, id, nil, 0)
			return nil
		}
		return s.DB.SaveAIOPreparation(ctx, service, fingerprint(key), rec.Hash, id)
	}
	if id != "" {
		if err := saveID(id); err != nil {
			return "", err
		}
	}
	var result preparedPlayback
	var err error
	switch service {
	case "realdebrid":
		result, err = s.Providers.rdPrepare(ctx, key, rec.Hash, magnet, r, cachedOnly && id == "", id, saveID)
	case "premiumize":
		result, err = s.Providers.pmPrepare(ctx, key, rec.Hash, magnet, r, cachedOnly, id, saveID)
	case "debridlink":
		result, err = s.Providers.dlPrepare(ctx, key, rec.Hash, magnet, r, cachedOnly, id, saveID)
	case "deepbrid":
		result, err = s.Providers.dbPrepare(ctx, key, rec.Hash, magnet, r, cachedOnly, id, saveID)
	default:
		return "", model.ErrInvalid
	}
	if err == nil {
		availability := model.AIOAvailability{Hash: rec.Hash, Cached: true, CheckedAt: time.Now(), Files: result.Files, Generation: generation, Shareable: service == "premiumize" || service == "debridlink"}
		if !public {
			_ = s.DB.SaveAIOAvailability(ctx, service, fingerprint(key), availability)
		} else {
			if availability.Shareable {
				_ = s.DB.SaveSharedAvailability(ctx, service, availability)
			}
			if item := s.visitorPreparation(service, key, rec.Hash); item.ID != "" {
				s.rememberVisitorPreparation(service, key, rec.Hash, item.ID, availability.Files, generation)
			}
		}
	} else if code := providerFailureCode(err); code == "not_found" || code == "not_cached" && (service == "realdebrid" || service == "deepbrid") {
		if public && id != "" {
			s.mu.Lock()
			identity := service + ":" + fingerprint(key) + ":" + rec.Hash
			if current := s.visitorPreparations[identity]; current.ID == id {
				delete(s.visitorPreparations, identity)
			}
			s.mu.Unlock()
		} else if !public {
			if id != "" {
				_ = s.DB.ForgetAIOPreparation(ctx, service, fingerprint(key), rec.Hash, id)
			}
			if service == "realdebrid" || service == "deepbrid" {
				// A deleted account item does not disprove provider-wide cache
				// availability. Only invalidate this account's older evidence.
				_ = s.DB.ForgetAIOAvailability(ctx, service, fingerprint(key), rec.Hash, started)
			}
		}
	}
	return result.URL, err
}
