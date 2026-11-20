package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) aioCorrespondences(w http.ResponseWriter, r *http.Request) {
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	rule := strings.TrimSpace(r.URL.Query().Get("rule"))
	if !model.ValidSelection(rule, model.ValidCorrespondenceRule) {
		writeError(w, http.StatusBadRequest, "Unknown correspondence rule")
		return
	}
	enrichedRaw := strings.TrimSpace(r.URL.Query().Get("enriched"))
	enrichedOnly := false
	if enrichedRaw != "" {
		parsed, err := strconv.ParseBool(enrichedRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid enriched filter")
			return
		}
		enrichedOnly = parsed
	}
	language := strings.TrimSpace(r.URL.Query().Get("language"))
	if language != "" {
		language = model.NormalizeLanguage(language)
	}
	result, err := s.options.Store.ListReleaseCorrespondences(r.Context(), model.ReleaseCorrespondenceOptions{
		ListOptions:  opts,
		Rule:         rule,
		EnrichedOnly: enrichedOnly,
		Language:     language,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) aioCorrespondenceDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "Correspondence not found")
		return
	}
	language := strings.TrimSpace(r.URL.Query().Get("language"))
	if language != "" {
		language = model.NormalizeLanguage(language)
	}
	detail, err := s.options.Store.ReleaseCorrespondenceDetail(r.Context(), id, language)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *server) aioCorrespondenceUnlink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "Correspondence not found")
		return
	}
	occurrence := r.PathValue("occurrence")
	nextGroupID, err := s.options.AIOSource.UnlinkCorrespondenceMember(r.Context(), id, occurrence)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": nextGroupID})
}

func (s *server) aioCorrespondenceRestore(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "Correspondence not found")
		return
	}
	occurrence := r.PathValue("occurrence")
	nextGroupID, err := s.options.AIOSource.RestoreCorrespondenceExclusion(r.Context(), id, occurrence)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": nextGroupID})
}
