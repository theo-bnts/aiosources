package store

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/software"
)

func matchedSoftwareKind(record model.AIORecord, work *model.AIOWork) string {
	if work != nil && work.Type != "software" {
		return ""
	}
	kind := software.ParseRecord(record).Kind
	if kind == "" && work != nil && work.Type == "software" {
		return "pc"
	}
	return kind
}

// Software candidate discovery checks canonical software_key and cross-source
// external identifiers (WinGet ID, Homebrew Cask, NVD CPE) before exact/trigram
// title and alias matches. Never prefix % or similarity() with public. so
// isolated search_path tests remain compatible.
const softwareCandidateIDsSQL = `WITH candidate_ids AS (
 SELECT id FROM aiosources.aio_works WHERE $2<>'' AND media_type='software' AND software_key<>'' AND software_key=aiosources.sw_resolve_key($2)
 UNION SELECT w.id FROM aiosources.sw_external_ids e JOIN aiosources.aio_works w ON w.media_type='software' AND w.software_key<>'' AND w.software_key=e.software_key
  WHERE $3<>'' AND e.kind='winget_id' AND e.external_lower=lower($3)
 UNION SELECT w.id FROM aiosources.sw_external_ids e JOIN aiosources.aio_works w ON w.media_type='software' AND w.software_key<>'' AND w.software_key=e.software_key
  WHERE $4<>'' AND e.kind='homebrew_cask' AND e.external_lower=lower($4)
 UNION SELECT w.id FROM aiosources.sw_external_ids e JOIN aiosources.aio_works w ON w.media_type='software' AND w.software_key<>'' AND w.software_key=e.software_key
  WHERE $5<>'' AND e.kind='nvd_cpe' AND e.external_lower=lower($5)
 UNION SELECT id FROM aiosources.aio_works WHERE $2='' AND $3='' AND $4='' AND $5='' AND media_type='software' AND software_normalized<>'' AND (
  software_normalized=ANY($6::text[]) OR (NOT $7::boolean AND $1<>'' AND software_normalized % $1)
 )
 UNION SELECT t.work_id FROM aiosources.aio_titles t JOIN aiosources.aio_works w ON w.id=t.work_id
  WHERE $2='' AND $3='' AND $4='' AND $5='' AND w.media_type='software' AND (
   t.normalized=ANY($6::text[]) OR (NOT $7::boolean AND $1<>'' AND t.normalized % $1)
  )
 UNION SELECT w.id FROM aiosources.sw_aliases a JOIN aiosources.aio_works w ON w.media_type='software' AND w.software_key<>'' AND w.software_key=a.software_key
  WHERE $2='' AND $3='' AND $4='' AND $5='' AND a.normalized<>'' AND (
   a.normalized=ANY($6::text[]) OR (NOT $7::boolean AND $1<>'' AND a.normalized % $1)
  )
)
SELECT ` + aioWorkColumns + ` FROM candidate_ids c
CROSS JOIN LATERAL (SELECT * FROM aiosources.aio_works WHERE id=c.id OFFSET 0) w
WHERE w.media_type='software' AND NOT w.adult
ORDER BY w.id LIMIT 101`

func searchSoftwareCandidates(ctx context.Context, tx pgx.Tx, f model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
	titles := []string{}
	for _, t := range append(slices.Clone(f.Titles), f.Title) {
		n := model.NormalizeTitle(t)
		if n != "" && !slices.Contains(titles, n) {
			titles = append(titles, n)
		}
	}
	rows, err := tx.Query(ctx, softwareCandidateIDsSQL, f.Title, f.SoftwareKey, f.WinGetID, f.HomebrewCask, f.NVDCPE, titles, f.ExactOnly)
	if err != nil {
		return nil, false, databaseError("find software candidates", err)
	}
	defer rows.Close()
	out := []model.AIOWork{}
	for rows.Next() {
		w, e := scanAIOWork(rows)
		if e != nil {
			return nil, false, databaseError("read software candidate", e)
		}
		out = append(out, w)
		if len(out) > aioCandidateLimit {
			break
		}
	}
	return out, len(out) > aioCandidateLimit, databaseError("find software candidates", rows.Err())
}

// Scope catalog software search to works present in the local source catalog
// so alias and vendor substring filters never scan unrelated software entries.
const catalogSoftwareScopeCTE = `catalog_software_works AS MATERIALIZED(
 SELECT id,software_key FROM catalog_matched_works WHERE media_type='software' AND software_key<>''),
catalog_software_aliases AS MATERIALIZED(
 SELECT w.id AS work_id,a.normalized FROM catalog_software_works w JOIN aiosources.sw_aliases a ON a.software_key=w.software_key WHERE a.normalized<>''),
catalog_software_vendors AS MATERIALIZED(
 SELECT w.id AS work_id,c.vendor_norm AS normalized FROM catalog_software_works w JOIN aiosources.sw_catalog c ON c.software_key=w.software_key WHERE c.vendor_norm<>'')`

func catalogSoftwareCandidates(queryArg, normalizedArg, matching string) string {
	resolved := `(SELECT aiosources.sw_resolve_key(` + queryArg + `))`
	return `
 UNION ALL SELECT id,5,1::real FROM catalog_software_works WHERE software_key=` + resolved + `
 UNION ALL SELECT w.id,5,1::real FROM aiosources.sw_external_ids e JOIN catalog_software_works w ON w.software_key=e.software_key WHERE e.kind IN ('nvd_cpe','winget_id','homebrew_cask') AND e.external_lower=lower(` + queryArg + `)
 UNION ALL SELECT work_id,CASE WHEN normalized=` + normalizedArg + ` THEN 4 ELSE 2 END,public.similarity(` + normalizedArg + `,normalized) FROM catalog_software_aliases WHERE ` + matching + `
 UNION ALL SELECT work_id,CASE WHEN normalized=` + normalizedArg + ` THEN 3 ELSE 1 END,public.similarity(` + normalizedArg + `,normalized) FROM catalog_software_vendors WHERE ` + matching
}

// softwareCatalogCandidates returns SQL UNION clauses allowing AIOReviewCandidates
// to find software works by software_key, WinGet ID, Homebrew Cask token, NVD CPE,
// alias or vendor in addition to product titles.
func softwareCatalogCandidates(queryArg, normalizedArg, matching string) string {
	aliasMatch := strings.ReplaceAll(matching, "normalized", "a.normalized")
	vendorMatch := strings.ReplaceAll(matching, "normalized", "c.vendor_norm")
	resolved := `(SELECT aiosources.sw_resolve_key(` + queryArg + `))`
	return `
 UNION ALL SELECT id,5,1::real FROM aiosources.aio_works WHERE media_type='software' AND software_key<>'' AND software_key=` + resolved + `
 UNION ALL SELECT w.id,5,1::real FROM aiosources.sw_external_ids e JOIN aiosources.aio_works w ON w.media_type='software' AND w.software_key<>'' AND w.software_key=e.software_key WHERE e.kind IN ('nvd_cpe','winget_id','homebrew_cask') AND e.external_lower=lower(` + queryArg + `)
 UNION ALL SELECT w.id,CASE WHEN a.normalized=` + normalizedArg + ` THEN 4 ELSE 2 END,similarity(` + normalizedArg + `,a.normalized) FROM aiosources.sw_aliases a JOIN aiosources.aio_works w ON w.media_type='software' AND w.software_key<>'' AND w.software_key=a.software_key WHERE a.normalized<>'' AND ` + aliasMatch + `
 UNION ALL SELECT w.id,CASE WHEN c.vendor_norm=` + normalizedArg + ` THEN 3 ELSE 1 END,similarity(` + normalizedArg + `,c.vendor_norm) FROM aiosources.sw_catalog c JOIN aiosources.aio_works w ON w.media_type='software' AND w.software_key<>'' AND w.software_key=c.software_key WHERE c.vendor_norm<>'' AND ` + vendorMatch
}
