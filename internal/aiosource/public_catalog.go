package aiosource

import (
	"context"
	"errors"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *Service) PublicSearchAccess(ctx context.Context, visitor *PublicInstallation) (model.PublicAccess, error) {
	return s.PublicCatalogAccess(ctx, visitor, "search")
}

func (s *Service) PublicCatalogAccess(ctx context.Context, visitor *PublicInstallation, feature string) (model.PublicAccess, error) {
	if visitor != nil && visitor.Trusted != nil {
		return s.visitorAccess(ctx, visitor, feature)
	}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return access, err
	}
	if visitor == nil || !access.UseInstanceDebrid && !validPublicKeys(visitor.Services) || !access.Enabled || !access.Allows(feature) || access.Generation != visitor.Generation {
		return access, ErrAuth
	}
	return access, nil
}

// Only the identified metadata DTO crosses this boundary. Raw observations,
// source definitions, native identities and credentials never accompany titles.
func (s *Service) PublicCatalogSearch(ctx context.Context, visitor *PublicInstallation, opts model.ListOptions, filters model.CatalogSearchFilters) (model.List[model.CatalogTitle], error) {
	_, err := s.PublicCatalogAccess(ctx, visitor, "search")
	if err != nil {
		return model.List[model.CatalogTitle]{}, err
	}
	if filters.IncludeUnavailable {
		if visitor == nil || visitor.Trusted == nil {
			filters.IncludeUnavailable = false
		} else if _, err := s.PublicCatalogAccess(ctx, visitor, "alerts"); err != nil {
			filters.IncludeUnavailable = false
		}
	}
	result, err := s.CatalogSearch(ctx, opts, filters)
	return result, err
}

func (s *Service) PublicCatalogTrending(ctx context.Context, visitor *PublicInstallation, opts model.CatalogTrendingOptions) (model.CatalogTrending, error) {
	_, err := s.PublicCatalogAccess(ctx, visitor, "trending")
	if err != nil {
		return model.CatalogTrending{}, err
	}
	return s.CatalogTrending(ctx, opts)
}

func (s *Service) PublicCatalogTitle(ctx context.Context, visitor *PublicInstallation, workID int64, locales ...string) (model.CatalogTitle, error) {
	_, err := s.PublicCatalogAccess(ctx, visitor, "browse")
	if err != nil {
		return model.CatalogTitle{}, err
	}
	title, err := s.CatalogTitle(ctx, workID, locales...)
	return title, err
}

func (s *Service) PublicCatalogReleases(ctx context.Context, visitor *PublicInstallation, workID int64, selection model.CatalogSelection, opts model.ListOptions, filters model.CatalogReleaseFilters) (model.List[model.CatalogRelease], error) {
	return s.catalogReleases(ctx, workID, selection, opts, visitor, filters)
}

type publicCatalogSource struct {
	WorkID      int64  `json:"work"`
	Provider    string `json:"provider"`
	SourceID    string `json:"source"`
	Fingerprint string `json:"fingerprint"`
}

func (s *Service) sealPublicCatalogSource(workID int64, row store.CatalogRecord) (string, error) {
	return s.sealVaultJSON("public-catalog-source-v1", publicCatalogSource{WorkID: workID, Provider: row.Provider, SourceID: row.SourceID, Fingerprint: row.Fingerprint})
}

// Readiness is carried by the visitor, not by a saved credential or background
// job. Provider object IDs and the account binding stay inside this sealed blob.
type publicCatalogPreparation struct {
	TrustedID   string                 `json:"trusted_user,omitempty"`
	WorkID      int64                  `json:"work"`
	Source      publicCatalogSource    `json:"source"`
	Selection   model.CatalogSelection `json:"selection"`
	Service     string                 `json:"service"`
	OperationID string                 `json:"operation"`
	PreparedID  string                 `json:"prepared"`
	Account     string                 `json:"account"`
	Audience    string                 `json:"audience"`
	Generation  int64                  `json:"generation"`
	Expires     int64                  `json:"expires"`
}

func (s *Service) openPublicCatalogValue(purpose, token string, result any) error {
	if len(token) > 16384 || s.openVaultJSON(purpose, token, result) != nil {
		return model.ErrNotFound
	}
	return nil
}

func (s *Service) PublicCatalogDownload(ctx context.Context, visitor *PublicInstallation, workID int64, input model.CatalogDownloadInput) (result model.CatalogDownload, downloadErr error) {
	feature := "browse"
	if workID == 0 {
		feature = "search"
	}
	if _, err := s.PublicCatalogAccess(ctx, visitor, feature); err != nil {
		return model.CatalogDownload{}, err
	}
	var source publicCatalogSource
	operation, account := "", ""
	if input.PreparationID != "" {
		var preparation publicCatalogPreparation
		if err := s.openPublicCatalogValue("public-catalog-preparation-v1", input.PreparationID, &preparation); err != nil {
			return model.CatalogDownload{}, err
		}
		now := time.Now().Unix()
		if preparation.TrustedID != trustedVisitorID(visitor) || preparation.WorkID != workID || preparation.Audience != visitor.Audience || preparation.Generation != visitor.Generation || preparation.Expires <= now || preparation.Expires > now+7*24*3600 || preparation.OperationID == "" || visitor.Services[preparation.Service] == "" || fingerprint(visitor.Services[preparation.Service]) != preparation.Account {
			return model.CatalogDownload{}, model.ErrConflict
		}
		source = preparation.Source
		operation, account = preparation.OperationID, preparation.Account
		chosenFile := input.File
		input = model.CatalogDownloadInput{CatalogSelection: preparation.Selection, Service: preparation.Service, PreparationID: preparation.PreparedID}
		if chosenFile != "" {
			input.File = chosenFile
		}
	} else if err := s.openPublicCatalogValue("public-catalog-source-v1", input.DownloadToken, &source); err != nil {
		return model.CatalogDownload{}, err
	}
	if source.WorkID != workID || source.Provider == "" || source.SourceID == "" || source.Fingerprint == "" || visitor.Services[input.Service] == "" {
		return model.CatalogDownload{}, model.ErrNotFound
	}
	defer func() {
		s.retireUnavailableOperationDownload(ctx, operation, downloadErr)
	}()
	var rows []store.CatalogRecord
	var err error
	if workID == 0 {
		row, e := s.DB.CatalogSourceTorrent(ctx, source.Provider, source.SourceID)
		rows, err = []store.CatalogRecord{row}, e
	} else {
		rows, err = s.DB.CatalogRecords(ctx, workID)
	}
	if err != nil {
		return model.CatalogDownload{}, err
	}
	input.OccurrenceID = ""
	for _, row := range rows {
		if row.Provider == source.Provider && row.SourceID == source.SourceID {
			input.OccurrenceID = row.OccurrenceID
			break
		}
	}
	if input.OccurrenceID == "" {
		return model.CatalogDownload{}, model.ErrNotFound
	}
	input.DownloadToken = ""
	input.Fingerprint = source.Fingerprint
	input.AllowUncached = input.AllowUncached && !visitor.Preferences.CachedOnly
	result, downloadErr = s.catalogDownload(ctx, workID, input, operation, account, visitor)
	if errors.Is(downloadErr, ErrPending) {
		result.PreparationID, err = s.sealVaultJSON("public-catalog-preparation-v1", publicCatalogPreparation{TrustedID: trustedVisitorID(visitor), WorkID: workID, Source: source, Selection: input.CatalogSelection, Service: input.Service, OperationID: result.OperationID, PreparedID: result.PreparationID, Account: fingerprint(visitor.Services[input.Service]), Audience: visitor.Audience, Generation: visitor.Generation, Expires: time.Now().Add(7 * 24 * time.Hour).Unix()})
		if err != nil {
			return model.CatalogDownload{}, err
		}
	}
	return result, downloadErr
}
