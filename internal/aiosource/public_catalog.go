package aiosource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *Service) PublicSearchAccess(ctx context.Context, visitor *PublicInstallation) (model.PublicAccess, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return access, err
	}
	if visitor == nil || !validPublicKeys(visitor.Services) || !access.Enabled || !access.SearchEnabled || access.Generation != visitor.Generation {
		return access, ErrAuth
	}
	return access, nil
}

// Only the identified metadata DTO crosses this boundary. Raw observations,
// source definitions, native identities and credentials never accompany titles.
func (s *Service) PublicCatalogSearch(ctx context.Context, visitor *PublicInstallation, opts model.ListOptions, filters model.CatalogSearchFilters) (model.List[model.CatalogTitle], error) {
	access, err := s.PublicSearchAccess(ctx, visitor)
	if err != nil {
		return model.List[model.CatalogTitle]{}, err
	}
	result, err := s.CatalogSearch(ctx, opts, filters)
	if !access.ExposeSources {
		for i := range result.Items {
			result.Items[i].Providers = []string{}
		}
	}
	return result, err
}

func (s *Service) PublicCatalogTitle(ctx context.Context, visitor *PublicInstallation, workID int64, locales ...string) (model.CatalogTitle, error) {
	access, err := s.PublicSearchAccess(ctx, visitor)
	if err != nil {
		return model.CatalogTitle{}, err
	}
	title, err := s.CatalogTitle(ctx, workID, locales...)
	if !access.ExposeSources {
		title.Providers = []string{}
	}
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
	data, err := json.Marshal(publicCatalogSource{WorkID: workID, Provider: row.Provider, SourceID: row.SourceID, Fingerprint: row.Fingerprint})
	if err != nil {
		return "", err
	}
	defer clear(data)
	sealed, err := s.Vault.Seal("public-catalog-source-v1", data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Readiness is carried by the visitor, not by a saved credential or background
// job. Provider object IDs and the account binding stay inside this sealed blob.
type publicCatalogPreparation struct {
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
	if len(token) < 32 || len(token) > 16384 {
		return model.ErrNotFound
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return model.ErrNotFound
	}
	data, err := s.Vault.Open(purpose, sealed)
	if err != nil {
		return model.ErrNotFound
	}
	defer clear(data)
	if json.Unmarshal(data, result) != nil {
		return model.ErrNotFound
	}
	return nil
}

func (s *Service) PublicCatalogDownload(ctx context.Context, visitor *PublicInstallation, workID int64, input model.CatalogDownloadInput) (result model.CatalogDownload, downloadErr error) {
	if _, err := s.PublicSearchAccess(ctx, visitor); err != nil {
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
		if preparation.WorkID != workID || preparation.Audience != visitor.Audience || preparation.Generation != visitor.Generation || preparation.Expires <= now || preparation.Expires > now+7*24*3600 || preparation.OperationID == "" || visitor.Services[preparation.Service] == "" || fingerprint(visitor.Services[preparation.Service]) != preparation.Account {
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
		if operation != "" && (errors.Is(downloadErr, model.ErrNotFound) || errors.Is(downloadErr, model.ErrConflict)) {
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.DB.UnavailableOperationDownload(final, operation)
		}
	}()
	rows, err := s.DB.CatalogRecords(ctx, workID)
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
		data, err := json.Marshal(publicCatalogPreparation{WorkID: workID, Source: source, Selection: input.CatalogSelection, Service: input.Service, OperationID: result.OperationID, PreparedID: result.PreparationID, Account: fingerprint(visitor.Services[input.Service]), Audience: visitor.Audience, Generation: visitor.Generation, Expires: time.Now().Add(7 * 24 * time.Hour).Unix()})
		if err != nil {
			return model.CatalogDownload{}, err
		}
		defer clear(data)
		sealed, err := s.Vault.Seal("public-catalog-preparation-v1", data)
		if err != nil {
			return model.CatalogDownload{}, err
		}
		result.PreparationID = base64.RawURLEncoding.EncodeToString(sealed)
	}
	return result, downloadErr
}
