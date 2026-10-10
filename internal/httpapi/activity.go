package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) registerActivityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/logs", s.auth.protect(s.activityLogs))
}

func activityOptions(w http.ResponseWriter, r *http.Request, defaultLimit int) (model.ListOptions, bool) {
	options, ok := listOptions(w, r)
	if !ok {
		return options, false
	}
	if r.URL.Query().Get("limit") == "" {
		options.Limit = defaultLimit
	}
	return options, true
}

func (s *server) activityLogs(w http.ResponseWriter, r *http.Request) {
	page, ok := activityOptions(w, r, 50)
	if !ok {
		return
	}
	options := model.LogOptions{ListOptions: page, Service: r.URL.Query().Get("service")}
	options.Levels = []string{"info", "warn", "error"}
	if raw := r.URL.Query().Get("level"); raw != "" {
		if len(raw) > 32 {
			writeError(w, http.StatusBadRequest, "Invalid log level filter")
			return
		}
		options.Levels = strings.Split(raw, ",")
		for _, level := range options.Levels {
			if !model.IsActivityLevel(level) {
				writeError(w, http.StatusBadRequest, "Log levels must be debug, info, warn or error")
				return
			}
		}
	}
	for key, target := range map[string]**time.Time{"from": &options.From, "to": &options.To} {
		if raw := r.URL.Query().Get(key); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "Time filters must be RFC3339 timestamps")
				return
			}
			*target = &value
		}
	}
	if options.From != nil && options.To != nil && options.From.After(*options.To) {
		writeError(w, http.StatusBadRequest, "from must not be later than to")
		return
	}
	result, err := s.options.Store.Logs(r.Context(), options)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
