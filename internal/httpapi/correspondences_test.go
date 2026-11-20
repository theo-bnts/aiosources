package httpapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCorrespondencesHTTPAPI(t *testing.T) {
	f, _, conn := addonAPI(t)

	for _, tc := range []struct {
		method, path string
	}{
		{"GET", "/api/aiosource/correspondences"},
		{"GET", "/api/aiosource/correspondences/1"},
		{"DELETE", "/api/aiosource/correspondences/1/members/occ"},
		{"POST", "/api/aiosource/correspondences/1/exclusions/occ/restore"},
		{"POST", "/api/aiosource/recompute-correspondences"},
	} {
		if res := f.request(tc.method, tc.path, "", false, ""); res.Code != 401 {
			t.Fatalf("%s %s without auth: got %d, want 401", tc.method, tc.path, res.Code)
		}
	}

	if res := f.request("GET", "/api/aiosource/correspondences?rule=bogus", "", true, ""); res.Code != 400 {
		t.Fatalf("invalid rule filter: got %d, want 400", res.Code)
	}
	if res := f.request("GET", "/api/aiosource/correspondences?enriched=maybe", "", true, ""); res.Code != 400 {
		t.Fatalf("invalid enriched filter: got %d, want 400", res.Code)
	}
	if res := f.request("GET", "/api/aiosource/correspondences/not-a-number", "", true, ""); res.Code != 404 {
		t.Fatalf("invalid correspondence id: got %d, want 404", res.Code)
	}

	var workID int64
	if err := conn.QueryRow(f.ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,imdb_id,display_title,years) VALUES('movie','7001','tt7001001','Orbit Example',ARRAY[2024]) RETURNING id`).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	const exactSize = int64(5_321_987_654)
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES
('src-a','a1',jsonb_build_object('title','Orbit.Example.2024.1080p.BluRay.x264-GRP','size',$1::bigint,'info_hash',repeat('a',40))),
('src-b','b1',jsonb_build_object('title','Orbit.Example.2024.FRENCH.1080p.BluRay.x264-GRP','size',$1::bigint,'info_hash',repeat('b',40))),
('src-c','c1',jsonb_build_object('title','Orbit.Example.2024.MULTI.1080p.BluRay.x264-GRP','size',$1::bigint,'info_hash',repeat('c',40)))`, exactSize); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','test'
FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id=$1`, workID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `DELETE FROM aiosources.aio_match_queue`); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueueAllReleaseCorrespondences(f.ctx); err != nil {
		t.Fatal(err)
	}
	scopes, err := f.db.ClaimCorrespondenceScopes(f.ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.db.ProcessCorrespondenceBatch(f.ctx, scopes, aiosource.BuildReleaseCorrespondences); err != nil {
		t.Fatal(err)
	}

	listRes := f.request("GET", "/api/aiosource/correspondences?enriched=true&rule=work_exact_size", "", true, "")
	if listRes.Code != 200 {
		t.Fatalf("list correspondences: %d %s", listRes.Code, listRes.Body.String())
	}
	var page struct {
		Items []model.ReleaseCorrespondenceGroup `json:"items"`
		Total int                                `json:"total"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("expected 1 correspondence group, got total=%d len=%d", page.Total, len(page.Items))
	}
	groupID := page.Items[0].ID

	detailRes := f.request("GET", fmt.Sprintf("/api/aiosource/correspondences/%d", groupID), "", true, "")
	if detailRes.Code != 200 {
		t.Fatalf("detail correspondence: %d %s", detailRes.Code, detailRes.Body.String())
	}
	var detail model.ReleaseCorrespondenceDetail
	if err := json.Unmarshal(detailRes.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(detail.Members))
	}

	var occA string
	for _, member := range detail.Members {
		if member.ProviderID == "src-a" {
			occA = member.OccurrenceID
		}
	}
	if occA == "" {
		t.Fatal("missing src-a occurrence id")
	}

	unlinkRes := f.request("DELETE", fmt.Sprintf("/api/aiosource/correspondences/%d/members/%s", groupID, url.PathEscape(occA)), "", true, "")
	if unlinkRes.Code != 200 {
		t.Fatalf("unlink member: %d %s", unlinkRes.Code, unlinkRes.Body.String())
	}
	var updated struct {
		GroupID int64 `json:"group_id"`
	}
	if err := json.Unmarshal(unlinkRes.Body.Bytes(), &updated); err != nil || updated.GroupID <= 0 {
		t.Fatalf("expected replacement group_id after unlinking 1 of 3 members, got %s (%v)", unlinkRes.Body.String(), err)
	}

	afterUnlink := f.request("GET", fmt.Sprintf("/api/aiosource/correspondences/%d", updated.GroupID), "", true, "")
	if afterUnlink.Code != 200 {
		t.Fatalf("detail after unlink: %d %s", afterUnlink.Code, afterUnlink.Body.String())
	}
	var detail2 model.ReleaseCorrespondenceDetail
	if err := json.Unmarshal(afterUnlink.Body.Bytes(), &detail2); err != nil {
		t.Fatal(err)
	}
	if len(detail2.Members) != 2 || len(detail2.Exclusions) != 1 || detail2.Exclusions[0].OccurrenceID != occA {
		t.Fatalf("expected 2 members and 1 exclusion for src-a, got members=%d exclusions=%d", len(detail2.Members), len(detail2.Exclusions))
	}

	restoreRes := f.request("POST", fmt.Sprintf("/api/aiosource/correspondences/%d/exclusions/%s/restore", updated.GroupID, url.PathEscape(occA)), "", true, "")
	if restoreRes.Code != 200 {
		t.Fatalf("restore exclusion: %d %s", restoreRes.Code, restoreRes.Body.String())
	}
	if err := json.Unmarshal(restoreRes.Body.Bytes(), &updated); err != nil || updated.GroupID <= 0 {
		t.Fatalf("expected restored group_id, got %s (%v)", restoreRes.Body.String(), err)
	}
	afterRestore := f.request("GET", fmt.Sprintf("/api/aiosource/correspondences/%d", updated.GroupID), "", true, "")
	if afterRestore.Code != 200 {
		t.Fatalf("detail after restore: %d %s", afterRestore.Code, afterRestore.Body.String())
	}
	var detail3 model.ReleaseCorrespondenceDetail
	if err := json.Unmarshal(afterRestore.Body.Bytes(), &detail3); err != nil {
		t.Fatal(err)
	}
	if len(detail3.Members) != 3 || len(detail3.Exclusions) != 0 {
		t.Fatalf("expected 3 members and 0 exclusions after restore, got members=%d exclusions=%d", len(detail3.Members), len(detail3.Exclusions))
	}
}
