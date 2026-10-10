package scheduling

import (
	"errors"
	"fmt"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// CatalogSynchronization reads the dedicated interval settings, or converts an
// older definition without rewriting its JSON or changing saved run snapshots.
func CatalogSynchronization(provider model.Provider) (model.CatalogSync, error) {
	settings := model.CatalogSync{IntervalMinutes: model.DefaultCatalogSyncMinutes}
	if !provider.IsRemoteCatalog() {
		return settings, errors.New("catalog_sync is only supported for remote catalogs")
	}
	if provider.CatalogSync != nil {
		settings = *provider.CatalogSync
		if settings.IntervalMinutes < 1 || settings.IntervalMinutes > model.MaxCatalogSyncMinutes {
			return settings, errors.New("catalog_sync.interval_minutes must be between 1 and 525600")
		}
		return settings, nil
	}
	legacy, err := Parse(provider.Schedule)
	if err != nil {
		return settings, err
	}
	settings.Enabled = legacy.Enabled
	if !legacy.Configured {
		return settings, nil
	}
	interval := legacy.every
	if interval == 0 {
		// A fixed reference keeps conversion independent of when a process
		// restarts. Daily and weekly Cron definitions retain their frequency.
		first := legacy.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
		if first.IsZero() {
			return settings, errors.New("legacy catalog schedule has no future occurrence")
		}
		second := legacy.Next(first)
		if second.IsZero() {
			return settings, errors.New("legacy catalog schedule has no future occurrence")
		}
		interval = second.Sub(first)
	}
	minutes := int(interval / time.Minute)
	if interval%time.Minute != 0 {
		minutes++
	}
	if minutes > model.MaxCatalogSyncMinutes {
		minutes = model.MaxCatalogSyncMinutes
	}
	settings.IntervalMinutes = minutes
	return settings, nil
}

// Settings uses the same durable queue and clocks for both kinds of work, while
// catalog imports have only an interval and incremental checkpoint semantics.
func Settings(provider model.Provider) (model.Schedule, error) {
	if !provider.IsRemoteCatalog() {
		if provider.CatalogSync != nil {
			return model.Schedule{}, errors.New("catalog_sync is only supported for remote catalogs")
		}
		return provider.Schedule, nil
	}
	sync, err := CatalogSynchronization(provider)
	return model.Schedule{Enabled: &sync.Enabled, Every: fmt.Sprintf("%dm", sync.IntervalMinutes), Mode: model.ModeIncremental}, err
}
