package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"

	"github.com/theo-bnts/aiosources/internal/model"
)

func NormalizePublicCatalogOptions(value model.PublicCatalogOptions) model.PublicCatalogOptions {
	value.Sources = slices.Clone(value.Sources)
	sort.Strings(value.Sources)
	fields := []string{}
	for _, field := range CatalogFields() {
		if slices.Contains(value.Fields, field) {
			fields = append(fields, field)
		}
	}
	if len(value.Fields) > 0 {
		value.Fields = fields
	}
	return value
}

func ValidPublicCatalogOptions(value model.PublicCatalogOptions) bool {
	return validPublicCatalogOptions(value, 128, 4096)
}

func validPublicCatalogOptions(value model.PublicCatalogOptions, maxSources, maxBytes int) bool {
	if len(value.Sources) > maxSources {
		return false
	}
	fields := value.Fields
	if len(fields) == 0 {
		fields = CatalogFields()
	}
	scope := "all"
	if len(value.Sources) > 0 {
		scope = "selected"
	}
	err := ValidateShare(model.ShareInput{Name: "Public catalog", Scope: scope, SourceIDs: value.Sources, Fields: fields, CatalogEntities: value.CatalogEntities})
	data, marshalErr := json.Marshal(value)
	return err == nil && marshalErr == nil && len(data) <= maxBytes
}

func scanPublicCatalogPolicy(row scanner) (model.PublicCatalogPolicy, error) {
	var value model.PublicCatalogPolicy
	var entities []byte
	err := row.Scan(&value.Revision, &value.Sources, &value.Fields, &entities)
	if err == nil {
		err = json.Unmarshal(entities, &value.CatalogEntities)
	}
	return value, err
}

const publicCatalogPolicyColumns = "p.revision,p.source_ids,p.fields,p.catalog_entities"

func (s *Store) PublicCatalogPolicy(ctx context.Context) (model.PublicCatalogPolicy, error) {
	value, err := scanPublicCatalogPolicy(s.pool.QueryRow(ctx, `SELECT `+publicCatalogPolicyColumns+` FROM aiosources.public_catalog_policy p WHERE singleton`))
	return value, databaseError("read public catalog policy", err)
}
func (s *Store) SetPublicCatalogPolicy(ctx context.Context, value model.PublicCatalogPolicy) error {
	if value.Revision < 1 || !validPublicCatalogOptions(value.PublicCatalogOptions, 1000, 128<<10) {
		return model.ErrInvalid
	}
	value.PublicCatalogOptions = NormalizePublicCatalogOptions(value.PublicCatalogOptions)
	if len(value.Fields) == 0 {
		value.Fields = CatalogFields()
	}
	if value.Sources == nil {
		value.Sources = []string{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin public catalog policy", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3); SELECT pg_advisory_xact_lock(1768843109,9)`); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE aiosources.public_catalog_policy SET revision=revision+1,source_ids=$1,fields=$2,catalog_entities=$3 WHERE singleton AND revision=$4`, value.Sources, value.Fields, catalogEntitiesJSON(value.CatalogEntities), value.Revision)
	if err != nil {
		return databaseError("save public catalog policy", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	if err = recordAccessActivityTx(ctx, tx, "security.public_catalog_permissions_changed", "public"); err != nil {
		return err
	}
	return databaseError("commit public catalog policy", tx.Commit(ctx))
}

// Selection can only narrow the current administrator ceiling. An empty
// intersection is denied rather than silently falling back to every source.
func ResolvePublicCatalogShare(policy model.PublicCatalogPolicy, options model.PublicCatalogOptions) (model.Share, error) {
	if !ValidPublicCatalogOptions(options) {
		return model.Share{}, model.ErrInvalid
	}
	share := model.Share{Format: "catalog", Enabled: true, Scope: "all", Revision: policy.Revision, Fields: slices.Clone(policy.Fields), CatalogEntities: options.CatalogEntities.Intersect(policy.CatalogEntities)}
	if len(share.Fields) == 0 {
		return model.Share{}, ErrCatalogAuthentication
	}
	if len(options.Fields) > 0 {
		share.Fields = nil
		for _, field := range policy.Fields {
			if slices.Contains(options.Fields, field) {
				share.Fields = append(share.Fields, field)
			}
		}
		if len(share.Fields) == 0 {
			return model.Share{}, ErrCatalogAuthentication
		}
	}
	if len(policy.Sources) > 0 {
		share.Scope = "selected"
		share.SourceIDs = slices.Clone(policy.Sources)
	}
	if len(options.Sources) > 0 {
		share.Scope = "selected"
		share.SourceIDs = nil
		for _, source := range options.Sources {
			if len(policy.Sources) == 0 || slices.Contains(policy.Sources, source) {
				share.SourceIDs = append(share.SourceIDs, source)
			}
		}
		if len(share.SourceIDs) == 0 {
			return model.Share{}, ErrCatalogAuthentication
		}
	}
	share.CatalogEntities.Availability = share.CatalogEntities.Availability && slices.Contains(share.Fields, "info_hash")
	return share, nil
}

func publicCatalogScopeID(options model.PublicCatalogOptions) string {
	data, _ := json.Marshal(NormalizePublicCatalogOptions(options))
	sum := sha256.Sum256(data)
	return "public-catalog:" + hex.EncodeToString(sum[:])
}

func (s *Store) PublicCatalogPage(ctx context.Context, grant model.PublicCatalogGrant, request CatalogPageRequest) (model.CatalogEnvelope, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.CatalogEnvelope{}, 0, databaseError("begin public catalog export", err)
	}
	defer rollback(tx)
	// Lock the access gate before its policy, matching revocation. One joined
	// locking SELECT does not guarantee this order when both rows change.
	var enabled, exposed, catalogEnabled bool
	var generation, catalogGeneration int64
	if grant.Trusted != nil {
		var permissions []byte
		err = tx.QueryRow(ctx, `SELECT enabled,generation,permissions FROM aiosources.trusted_users WHERE id=$1 FOR SHARE`, grant.Trusted.UserID).Scan(&enabled, &generation, &permissions)
		var p model.TrustedPermissions
		if err == nil {
			err = json.Unmarshal(permissions, &p)
		}
		exposed, catalogEnabled = p.ExposeSources, p.CatalogEnabled
		if generation != grant.Trusted.Generation {
			enabled = false
		}
	} else {
		err = tx.QueryRow(ctx, `SELECT enabled,expose_sources,catalog_enabled,generation,catalog_generation FROM aiosources.aio_public_access WHERE singleton FOR SHARE`).Scan(&enabled, &exposed, &catalogEnabled, &generation, &catalogGeneration)
	}
	if err != nil {
		return model.CatalogEnvelope{}, 0, databaseError("authorize public catalog export", err)
	}
	if !enabled || !exposed || !catalogEnabled || (grant.Trusted == nil && (grant.Generation != generation || grant.CatalogGeneration != catalogGeneration)) {
		return model.CatalogEnvelope{}, 0, ErrCatalogAuthentication
	}
	policy, err := scanPublicCatalogPolicy(tx.QueryRow(ctx, `SELECT `+publicCatalogPolicyColumns+` FROM aiosources.public_catalog_policy p WHERE singleton FOR SHARE`))
	if err != nil {
		return model.CatalogEnvelope{}, 0, databaseError("read public catalog export policy", err)
	}
	share, err := ResolvePublicCatalogShare(policy, grant.PublicCatalogOptions)
	if err != nil {
		return model.CatalogEnvelope{}, 0, err
	}
	result, err := catalogExportPage(ctx, tx, share, publicCatalogScopeID(grant.PublicCatalogOptions), request)
	if err != nil {
		return model.CatalogEnvelope{}, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.CatalogEnvelope{}, 0, databaseError("finish public catalog export", err)
	}
	if err = s.ValidatePublicCatalog(ctx, grant, policy.Revision); err != nil {
		return model.CatalogEnvelope{}, 0, err
	}
	return result, policy.Revision, nil
}
func (s *Store) ValidatePublicCatalog(ctx context.Context, grant model.PublicCatalogGrant, revision int64) error {
	var valid bool
	var err error
	if grant.Trusted != nil {
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.trusted_users u CROSS JOIN aiosources.public_catalog_policy p WHERE u.id=$1 AND u.generation=$2 AND u.enabled AND u.permissions->>'expose_sources'='true' AND u.permissions->>'catalog_enabled'='true' AND p.singleton AND p.revision=$3)`, grant.Trusted.UserID, grant.Trusted.Generation, revision).Scan(&valid)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.aio_public_access a CROSS JOIN aiosources.public_catalog_policy p WHERE a.singleton AND p.singleton AND a.enabled AND a.expose_sources AND a.catalog_enabled AND a.generation=$1 AND a.catalog_generation=$2 AND p.revision=$3)`, grant.Generation, grant.CatalogGeneration, revision).Scan(&valid)
	}
	if err != nil {
		return databaseError("validate public catalog delivery", err)
	}
	if !valid {
		return ErrCatalogAuthentication
	}
	return nil
}
