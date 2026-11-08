package aiosource

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *Service) CatalogTorrents(ctx context.Context, opts model.ListOptions, filters model.CatalogReleaseFilters, visitor *PublicInstallation) (model.List[model.CatalogRelease], error) {
	empty := model.List[model.CatalogRelease]{Items: []model.CatalogRelease{}, Limit: opts.Limit, Offset: opts.Offset}
	var access model.PublicAccess
	var err error
	if visitor != nil {
		access, err = s.PublicSearchAccess(ctx, visitor)
		if err != nil {
			return empty, err
		}
		if !access.ExposeSources && (opts.ProviderID != "" || len(opts.Providers) > 0 || slices.ContainsFunc(filters.StreamPreferences.Sorting, func(r model.ResultSort) bool { return r.Field == "source" })) {
			return empty, model.ErrInvalid
		}
	}
	if !ValidStreamPreferences(filters.StreamPreferences) || filters.StreamPreferences.Mode != "" || slices.ContainsFunc(filters.StreamPreferences.Sorting, func(r model.ResultSort) bool { return r.Field == "language" }) {
		return empty, model.ErrInvalid
	}
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return empty, err
	}
	policy := store.CatalogTorrentPolicy{Public: visitor != nil, CachedOnly: filters.CachedOnly, CacheAfter: time.Now().Add(-time.Duration(config.CacheMinutes) * time.Minute), SourceNames: s.sourceNames(), Services: []string{}, UncachedSources: []string{}}
	if visitor == nil {
		for _, service := range model.DebridServices() {
			if config.DebridSecret(service) != "" {
				policy.Services = append(policy.Services, service)
			}
		}
	} else {
		policy.CachedOnly = policy.CachedOnly || visitor.Preferences.CachedOnly
		for _, service := range model.DebridServices() {
			if visitor.Services[service] != "" && AllowsInstallation(visitor.Preferences, "", service, "") {
				policy.Services = append(policy.Services, service)
			}
		}
		if len(policy.Services) == 0 {
			return empty, nil
		}
		announces, e := s.visitorAnnounces(ctx, config, visitor)
		if e != nil {
			return empty, e
		}
		defer clear(announces)
		if !policy.CachedOnly {
			for id, tracker := range config.Trackers {
				if !tracker.Private || announces[id] != "" {
					policy.UncachedSources = append(policy.UncachedSources, id)
				}
			}
		}
		if len(visitor.Preferences.Sources) > 0 {
			if opts.ProviderID != "" {
				for _, id := range model.SelectionValues(opts.ProviderID) {
					if !slices.Contains(visitor.Preferences.Sources, id) {
						return empty, model.ErrInvalid
					}
				}
			} else {
				opts.Providers = slices.Clone(visitor.Preferences.Sources)
			}
		}
	}
	page, err := s.DB.CatalogTorrentRecords(ctx, opts, filters.StreamPreferences.Sorting, policy)
	if err != nil {
		return empty, err
	}
	pageOpts := opts
	pageOpts.Limit = page.Limit
	pageOpts.Offset = 0
	result, err := s.catalogReleasePage(ctx, 0, Resource{Type: "torrent", Kind: "torrent", ID: Normalize(opts.Query)}, page.Items, pageOpts, visitor, access, filters)
	result.Limit, result.Offset, result.Total = page.Limit, page.Offset, page.Total
	return result, err
}

func (s *Service) catalogDownloadContext(ctx context.Context, workID int64, input model.CatalogDownloadInput) (model.CatalogTitle, Resource, string, []store.CatalogRecord, error) {
	if workID == 0 {
		if input.Season != nil || input.Episode != nil || len(input.File) > 4096 {
			return model.CatalogTitle{}, Resource{}, "", nil, model.ErrInvalid
		}
		row, err := s.DB.CatalogTorrent(ctx, input.OccurrenceID)
		return model.CatalogTitle{Type: "torrent"}, Resource{Type: "torrent", Kind: "torrent", ID: input.OccurrenceID, File: input.File}, input.OccurrenceID, []store.CatalogRecord{row}, err
	}
	title, err := s.DB.CatalogTitle(ctx, workID)
	if err != nil {
		return title, Resource{}, "", nil, err
	}
	r, id, err := catalogResource(title, input.CatalogSelection)
	if err != nil {
		return title, r, id, nil, err
	}
	rows, err := s.DB.CatalogRecords(ctx, workID)
	return title, r, id, rows, err
}

func directCatalogResource(r Resource) bool {
	return r.Type == "torrent" && r.Kind == "torrent" && strings.TrimSpace(r.ID) != ""
}
