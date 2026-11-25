package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/vault"
)

const (
	databaseExportPurpose       = "database-backup-export-v1"
	databaseExportTicketTTL     = 5 * time.Minute
	maxConcurrentDatabaseExport = 2
	databaseConfirmHeader       = "X-AIOSources-Confirm"
	databaseConfirmValue        = "replace-database"
	databaseFilenameHeader      = "X-AIOSources-Filename"
	databaseMinFreeMarginBytes  = 64 << 20
)

type databaseExportTicket struct {
	Session  string `json:"session"`
	Filename string `json:"filename"`
	Expires  int64  `json:"expires"`
}

type restoreAuthSnapshot struct {
	sessionID string
	tokenHash []byte
	csrf      string
	record    store.AdminSessionRecord
}

type databaseBackupController struct {
	mu            sync.Mutex
	exports       int
	uploading     bool
	restore       model.DatabaseRestoreState
	schemaSwapped bool
	activeSession *restoreAuthSnapshot
}

func (c *databaseBackupController) isSchemaSwapped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.schemaSwapped
}

func (c *databaseBackupController) setSchemaSwapped(swapped bool) {
	c.mu.Lock()
	c.schemaSwapped = swapped
	c.mu.Unlock()
}

func (c *databaseBackupController) isRestoreActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.uploading || c.restore.Status == "running"
}

func (c *databaseBackupController) matchRestoreSession(r *http.Request) (*restoreAuthSnapshot, bool) {
	cookie, err := requestSessionCookie(r)
	if err != nil || len(cookie.Value) != 43 {
		return nil, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	c.mu.Lock()
	snap := c.activeSession
	c.mu.Unlock()
	if snap == nil || len(snap.tokenHash) != 32 || subtle.ConstantTimeCompare(key[:], snap.tokenHash) != 1 {
		return nil, false
	}
	return snap, true
}

func (c *databaseBackupController) reserveExport() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uploading || c.restore.Status == "running" || c.exports >= maxConcurrentDatabaseExport {
		return false
	}
	c.exports++
	return true
}

func (c *databaseBackupController) releaseExport() {
	c.mu.Lock()
	if c.exports > 0 {
		c.exports--
	}
	c.mu.Unlock()
}

func (c *databaseBackupController) reserveUpload() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uploading || c.restore.Status == "running" || c.exports > 0 {
		return false
	}
	c.uploading = true
	return true
}

func (c *databaseBackupController) cancelUpload() {
	c.mu.Lock()
	c.uploading = false
	c.mu.Unlock()
}

func (c *databaseBackupController) startRestore(filename string, size int64, snap *restoreAuthSnapshot) model.DatabaseRestoreState {
	now := time.Now().UTC()
	c.mu.Lock()
	c.uploading = false
	c.activeSession = snap
	c.restore = model.DatabaseRestoreState{
		Status:    "running",
		Phase:     "stopping",
		Filename:  filename,
		Bytes:     size,
		StartedAt: &now,
	}
	state := c.restore
	c.mu.Unlock()
	return state
}

func (c *databaseBackupController) setPhase(phase string) {
	c.mu.Lock()
	if c.restore.Status == "running" {
		c.restore.Phase = phase
	}
	c.mu.Unlock()
}

func (c *databaseBackupController) finishRestore(err error) {
	now := time.Now().UTC()
	c.mu.Lock()
	c.uploading = false
	c.schemaSwapped = false
	c.activeSession = nil
	c.restore.Phase = ""
	c.restore.FinishedAt = &now
	if err == nil {
		c.restore.Status = "succeeded"
		c.restore.Error = ""
	} else {
		c.restore.Status = "failed"
		c.restore.Error = userFacingBackupError(err)
	}
	c.mu.Unlock()
}

func (c *databaseBackupController) snapshot() (exports int, restore model.DatabaseRestoreState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exports, c.restore
}

func cleanupStaleDatabaseRestores(dataDir string) {
	if dataDir == "" {
		return
	}
	dir := filepath.Join(dataDir, "database-restore")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "restore-") && strings.HasSuffix(entry.Name(), ".dump") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func (s *server) databaseStagingDir() (string, error) {
	if s.options.DataDir != "" {
		dir := filepath.Join(s.options.DataDir, "database-restore")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", errors.New("could not prepare backup staging directory")
		}
		return dir, nil
	}
	return os.TempDir(), nil
}

func (s *server) registerDatabaseBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/storage/database", s.databaseBackupStatusHandler)
	mux.HandleFunc("POST /api/settings/storage/database/export", s.auth.protect(s.prepareDatabaseExport))
	mux.HandleFunc("GET /api/settings/storage/database/export/{ticket}", s.downloadDatabaseExport)
	mux.HandleFunc("POST /api/settings/storage/database/import", s.auth.protect(s.importDatabaseBackup))
}

func (s *server) databaseMaintenanceGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.backup.isSchemaSwapped() {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/settings/storage/database" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/session" {
			if snap, ok := s.backup.matchRestoreSession(r); ok {
				writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf_token": snap.csrf})
				return
			}
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, "Database restore is in progress. Try again shortly.")
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			if !noStorePath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "Database restore is in progress. Try again shortly.")
	})
}

func (s *server) databaseBackupStatusHandler(w http.ResponseWriter, r *http.Request) {
	if s.backup.isRestoreActive() {
		if _, ok := s.backup.matchRestoreSession(r); ok {
			exports, restore := s.backup.snapshot()
			writeJSON(w, http.StatusOK, model.DatabaseBackupStatus{
				Tools:         model.DatabaseBackupTools{ExportAvailable: true, ImportAvailable: true},
				ExportRunning: exports,
				Restore:       restore,
				Busy:          true,
			})
			return
		}
	}
	s.auth.protect(s.databaseBackupStatus)(w, r)
}

func (s *server) databaseBackupStatus(w http.ResponseWriter, r *http.Request) {
	exports, restore := s.backup.snapshot()
	status := model.DatabaseBackupStatus{
		Tools:         s.options.Store.DatabaseBackupTools(r.Context()),
		ExportRunning: exports,
		Restore:       restore,
	}
	if overview, err := s.options.Store.StorageOverview(r.Context()); err == nil {
		status.Running = overview.Running
		status.Indexing = overview.Indexing
	}
	if s.options.AIOSource != nil {
		status.Busy = s.options.AIOSource.DatabaseMaintenanceBusy()
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) prepareDatabaseExport(w http.ResponseWriter, r *http.Request) {
	if s.backup.isRestoreActive() {
		writeError(w, http.StatusConflict, "Wait for the active database restore to finish before exporting.")
		return
	}
	tools := s.options.Store.DatabaseBackupTools(r.Context())
	if !tools.ExportAvailable {
		msg := tools.Reason
		if msg == "" {
			msg = "Database export is unavailable on this server."
		}
		writeError(w, http.StatusServiceUnavailable, msg)
		return
	}
	exports, _ := s.backup.snapshot()
	if exports >= maxConcurrentDatabaseExport {
		writeError(w, http.StatusConflict, "Another database export is already in progress. Wait for it to finish.")
		return
	}
	if s.options.Vault == nil {
		writeError(w, http.StatusServiceUnavailable, "Backup download tickets are unavailable.")
		return
	}
	filename := fmt.Sprintf("aiosources-backup-%s.dump", time.Now().UTC().Format("2006-01-02T15-04-05Z"))
	expires := time.Now().Add(databaseExportTicketTTL).Unix()
	ticketURL, validSeconds, err := s.sealDownloadTicket(databaseExportPurpose, "/api/settings/storage/database/export/", expires, databaseExportTicket{
		Session:  currentSession(r).id,
		Filename: filename,
		Expires:  expires,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        ticketURL,
		"filename":   filename,
		"expires_in": validSeconds,
	})
}

func (s *server) downloadDatabaseExport(w http.ResponseWriter, r *http.Request) {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeError(w, http.StatusForbidden, "Cross-site backup download is not allowed")
		return
	}
	sess, err := s.auth.lookup(r)
	if errors.Is(err, store.ErrAuthentication) {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Authentication storage is unavailable")
		return
	}
	var ticket databaseExportTicket
	if s.options.Vault == nil || !s.openDownloadTicket(databaseExportPurpose, r.PathValue("ticket"), &ticket) {
		writeError(w, http.StatusForbidden, "Invalid or expired database export link. Start a new export.")
		return
	}
	if ticket.Session != sess.id || ticket.Expires <= time.Now().Unix() || !validBackupFilename(ticket.Filename) {
		writeError(w, http.StatusForbidden, "Invalid or expired database export link. Start a new export.")
		return
	}
	if !s.backup.reserveExport() {
		writeError(w, http.StatusConflict, "Another database backup or restore is already in progress.")
		return
	}
	defer s.backup.releaseExport()

	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})

	headersSent := false
	onHeader := func() error {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", ticket.Filename))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		headersSent = true
		return nil
	}
	dst := &streamingBackupWriter{w: w, controller: controller, chunkTimeout: 2 * time.Minute}
	err = s.options.Store.ExportDatabase(r.Context(), dst, onHeader)
	if err != nil {
		if !headersSent {
			respondBackupError(w, err)
			return
		}
		// Abort the chunked HTTP response cleanly so the browser marks the
		// download interrupted instead of saving a truncated archive.
		panic(http.ErrAbortHandler)
	}
	_ = controller.Flush()
	actCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.options.Store.RecordActivity(actCtx, "info", "storage.database_exported", "", "", "", nil)
}

func (s *server) importDatabaseBackup(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(databaseConfirmHeader) != databaseConfirmValue {
		writeError(w, http.StatusBadRequest, "Confirm that importing a backup replaces the entire database.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "Upload the PostgreSQL custom archive (.dump) with Content-Type application/octet-stream.")
		return
	}
	tools := s.options.Store.DatabaseBackupTools(r.Context())
	if !tools.ImportAvailable {
		msg := tools.Reason
		if msg == "" {
			msg = "Database import is unavailable on this server."
		}
		writeError(w, http.StatusServiceUnavailable, msg)
		return
	}
	overview, err := s.options.Store.StorageOverview(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	if overview.Running > 0 {
		writeError(w, http.StatusConflict, "Pause running imports before importing a database backup.")
		return
	}
	if overview.Indexing {
		writeError(w, http.StatusConflict, "Wait for search index changes to finish before importing a database backup.")
		return
	}
	if s.options.AIOSource != nil && s.options.AIOSource.DatabaseMaintenanceBusy() {
		writeError(w, http.StatusConflict, "Wait for active metadata, identification or maintenance operations to finish before importing a database backup.")
		return
	}
	if !s.backup.reserveUpload() {
		writeError(w, http.StatusConflict, "Another database export or import is already in progress.")
		return
	}

	started := false
	stagedPath := ""
	defer func() {
		if !started {
			s.backup.cancelUpload()
			if stagedPath != "" {
				_ = os.Remove(stagedPath)
			}
		}
	}()

	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Minute))
	_ = controller.SetWriteDeadline(time.Time{})

	// Validate the PostgreSQL custom archive signature on the first 5 bytes
	// before creating any staging file on disk.
	var magic [5]byte
	if _, err := io.ReadFull(r.Body, magic[:]); err != nil {
		writeError(w, http.StatusBadRequest, "The uploaded file is empty or could not be read.")
		return
	}
	if !store.HasPGDumpMagic(magic[:]) {
		writeError(w, http.StatusBadRequest, "The selected file is not a PostgreSQL custom backup archive (.dump).")
		return
	}

	stagingDir, err := s.databaseStagingDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not prepare temporary backup storage.")
		return
	}
	if r.ContentLength > 0 {
		if avail, ok := availableDiskBytes(stagingDir); ok && avail < uint64(r.ContentLength)+databaseMinFreeMarginBytes {
			writeError(w, http.StatusInsufficientStorage, "Insufficient local disk space to stage the uploaded backup file.")
			return
		}
	}

	tempFile, err := os.CreateTemp(stagingDir, "restore-*.dump")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create temporary staging file for the backup.")
		return
	}
	stagedPath = tempFile.Name()
	_ = tempFile.Chmod(0o600)

	written, copyErr := copyBackupUpload(tempFile, magic[:], r.Body, controller)
	closeErr := tempFile.Close()
	if copyErr != nil {
		if errors.Is(copyErr, context.Canceled) || errors.Is(copyErr, context.DeadlineExceeded) {
			writeError(w, http.StatusRequestTimeout, "The backup upload was interrupted or timed out.")
			return
		}
		writeError(w, http.StatusBadRequest, "Could not receive the complete backup upload.")
		return
	}
	if closeErr != nil {
		writeError(w, http.StatusInternalServerError, "Could not save the uploaded backup file.")
		return
	}

	if err := s.options.Store.ValidateDatabaseArchive(r.Context(), stagedPath); err != nil {
		respondBackupError(w, err)
		return
	}

	sess := currentSession(r)
	record, err := s.options.Store.AdminSessionSnapshot(r.Context(), sess.id)
	if err != nil {
		respondError(w, err)
		return
	}
	snap := &restoreAuthSnapshot{
		sessionID: sess.id,
		tokenHash: slices.Clone(record.TokenHash),
		csrf:      sess.csrf,
		record:    record,
	}

	filename := sanitizeUploadedFilename(r.Header.Get(databaseFilenameHeader))
	restoreState := s.backup.startRestore(filename, written, snap)
	started = true
	go s.runDatabaseRestore(stagedPath, stagingDir, snap)

	exports, _ := s.backup.snapshot()
	writeJSON(w, http.StatusAccepted, model.DatabaseBackupStatus{
		Tools:         tools,
		ExportRunning: exports,
		Restore:       restoreState,
		Busy:          true,
	})
}

func (s *server) runDatabaseRestore(stagedPath, stagingDir string, snap *restoreAuthSnapshot) {
	defer func() { _ = os.Remove(stagedPath) }()

	restoreCtx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	var resumeJobs func(context.Context, bool)
	var resumeAIO func(context.Context, bool)
	restored := false

	defer func() {
		resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer resumeCancel()
		if resumeAIO != nil {
			resumeAIO(resumeCtx, restored)
		}
		if resumeJobs != nil {
			resumeJobs(resumeCtx, restored)
		}
		if s.policies != nil {
			s.policies.invalidate()
		}
		if restored && s.options.Health != nil {
			s.options.Health.Refresh()
		}
	}()

	s.backup.setPhase("stopping")
	if s.options.Jobs != nil {
		var err error
		resumeJobs, err = s.options.Jobs.PauseForDatabaseMaintenance()
		if err != nil {
			s.backup.finishRestore(err)
			return
		}
	}
	if s.options.AIOSource != nil {
		pauseCtx, pauseCancel := context.WithTimeout(restoreCtx, 60*time.Second)
		var err error
		resumeAIO, err = s.options.AIOSource.PauseForDatabaseMaintenance(pauseCtx)
		pauseCancel()
		if err != nil {
			s.backup.finishRestore(err)
			return
		}
	}

	passwordHash := slices.Clone(s.auth.passwordHash)
	err := s.options.Store.RestoreDatabase(restoreCtx, stagedPath, stagingDir, store.RestoreCallbacks{
		OnPhase:         s.backup.setPhase,
		OnSchemaSwapped: s.backup.setSchemaSwapped,
		VerifyVault: func(ctx context.Context) error {
			if s.options.Vault == nil {
				return nil
			}
			return s.options.Vault.VerifyRestoredDatabase(ctx)
		},
		SyncAuth: func(ctx context.Context) error {
			return s.options.Store.SyncRestoredAdminAuth(ctx, passwordHash, &snap.record)
		},
	})
	if err != nil {
		s.backup.finishRestore(err)
		return
	}

	restored = true
	s.backup.finishRestore(nil)
}

type streamingBackupWriter struct {
	w            http.ResponseWriter
	controller   *http.ResponseController
	chunkTimeout time.Duration
}

func (s *streamingBackupWriter) Write(p []byte) (int, error) {
	_ = s.controller.SetWriteDeadline(time.Now().Add(s.chunkTimeout))
	n, err := s.w.Write(p)
	if err == nil {
		_ = s.controller.Flush()
	}
	return n, err
}

func copyBackupUpload(dst *os.File, magic []byte, src io.Reader, controller *http.ResponseController) (int64, error) {
	if _, err := dst.Write(magic); err != nil {
		return 0, err
	}
	total := int64(len(magic))
	buf := make([]byte, 64<<10)
	for {
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Minute))
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func availableDiskBytes(dir string) (uint64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil || stat.Bsize <= 0 {
		return 0, false
	}
	return stat.Bavail * uint64(stat.Bsize), true
}

func validBackupFilename(name string) bool {
	if name == "" || len(name) > 128 || strings.ContainsAny(name, `/\`) || path.Base(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '"' || r == ';' {
			return false
		}
	}
	return strings.HasSuffix(name, ".dump")
}

func sanitizeUploadedFilename(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "database-backup.dump"
	}
	raw = strings.ReplaceAll(raw, `\`, "/")
	base := path.Base(raw)
	var out strings.Builder
	for _, r := range base {
		if unicode.IsControl(r) || r == '"' || r == ';' {
			continue
		}
		out.WriteRune(r)
		if out.Len() >= 120 {
			break
		}
	}
	result := strings.TrimSpace(out.String())
	if result == "" || result == "." || result == ".." {
		return "database-backup.dump"
	}
	return result
}

func userFacingBackupError(err error) string {
	if err == nil {
		return ""
	}
	var backupErr *store.BackupError
	if errors.As(err, &backupErr) {
		return backupErr.Message
	}
	if errors.Is(err, vault.ErrMasterKeyMismatch) {
		return "This backup contains encrypted secrets created with a different master key. Restore the matching master key (DATA_DIR/master-key or MASTER_KEY) first; the previous database has been restored."
	}
	if errors.Is(err, model.ErrBusy) {
		return "Another import, metadata or maintenance operation is active. Pause running operations and try again."
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "The database restore was interrupted or timed out. The previous database has been restored."
	}
	return "The database restore could not be completed. The previous database has been restored."
}

func respondBackupError(w http.ResponseWriter, err error) {
	var backupErr *store.BackupError
	if errors.As(err, &backupErr) {
		switch {
		case errors.Is(err, store.ErrBackupToolUnavailable):
			writeError(w, http.StatusServiceUnavailable, backupErr.Message)
		case errors.Is(err, store.ErrInvalidDatabaseArchive):
			writeError(w, http.StatusUnprocessableEntity, backupErr.Message)
		default:
			writeError(w, http.StatusUnprocessableEntity, backupErr.Message)
		}
		return
	}
	if errors.Is(err, store.ErrBackupToolUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "PostgreSQL backup tools are unavailable on this server.")
		return
	}
	if errors.Is(err, store.ErrInvalidDatabaseArchive) {
		writeError(w, http.StatusUnprocessableEntity, "The uploaded file is not a valid AIOSources database backup archive.")
		return
	}
	respondError(w, err)
}
