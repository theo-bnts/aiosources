package httpapi

import (
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"net/http"
)

func (s *server) operationHistory(w http.ResponseWriter, r *http.Request) {
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	filter := model.OperationFilter{ListOptions: opts, Kind: r.URL.Query().Get("kind"), Service: r.URL.Query().Get("service"), Group: r.URL.Query().Get("group")}
	if (filter.Group != "" && filter.Group != "metadata" && filter.Group != "debrid") || (filter.Kind != "" && filter.Kind != "metadata" && filter.Kind != "matching" && filter.Kind != "cache" && filter.Kind != "cleanup" && filter.Kind != "download") || (filter.Service != "" && filter.Service != "tmdb" && filter.Service != "local" && !model.IsDebridService(filter.Service)) || (opts.Status != "" && opts.Status != "running" && opts.Status != "succeeded" && opts.Status != "failed" && opts.Status != "cancelled" && opts.Status != "interrupted") {
		writeError(w, http.StatusBadRequest, "Unknown operation history filter")
		return
	}
	if err := s.options.Store.RecoverOperations(r.Context()); err != nil {
		respondError(w, err)
		return
	}
	result, err := s.options.Store.ListOperations(r.Context(), filter)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) operationDetail(w http.ResponseWriter, r *http.Request) {
	if err := s.options.Store.RecoverOperations(r.Context()); err != nil {
		respondError(w, err)
		return
	}
	result, err := s.options.Store.Operation(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if result.Kind == "download" {
		continuation, e := s.options.Store.OperationDownload(r.Context(), result.ID)
		if e == nil {
			result.Download = &continuation
		} else if !errors.Is(e, model.ErrNotFound) {
			respondError(w, e)
			return
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) operationEvents(w http.ResponseWriter, r *http.Request) {
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	result, err := s.options.Store.OperationEvents(r.Context(), r.PathValue("id"), opts)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
