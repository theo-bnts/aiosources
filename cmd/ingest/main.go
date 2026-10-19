package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/health"
	"github.com/theo-bnts/aiosources/internal/httpapi"
	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/vault"
	webui "github.com/theo-bnts/aiosources/web"
)

var version = "dev"

type config struct {
	dataDir      string
	providersDir string
	bind         string
	publicURL    string
	webDir       string
	workers      int
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := command(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ingest:", err)
		os.Exit(1)
	}
}

func command(ctx context.Context, args []string) error {
	name := "serve"
	if len(args) != 0 {
		name, args = args[0], args[1:]
	}
	if name == "help" || name == "--help" || name == "-h" {
		fmt.Print(`Usage: ingest <command> [options]

  serve          Serve the panel/API and durable collection workers (default)
  init           Create private local state and initial administrator password
  password       Print the configured administrator password to this terminal
  validate       Validate all provider JSON definitions, without any HTTP call
  migrate-sources  Convert legacy source YAML offline; retain private originals
  enqueue        Queue a provider for the running service (--provider ID|all)
  pause          Hold a collection after its in-flight page commits (--run ID)
  resume         Resume a saved collection from its checkpoint (--run ID)
  cancel         Interrupt a queued or running collection (--run ID)
  secret set     Read a named secret from stdin and store it encrypted
  import-legacy  Import public.torrents additively into the new ingest schema
  healthcheck    Probe the local service without database writes
  version        Print the build version

Common options: --data-dir, --providers
Database commands require DATABASE_URL. Run <command> --help for flags.
`)
		return nil
	}
	if name != "version" {
		if err := validateEnvironment(); err != nil {
			return err
		}
	}
	if name == "healthcheck" {
		return healthcheck(ctx)
	}
	if name == "version" {
		fmt.Println(version)
		return nil
	}
	if name == "migrate-sources" {
		return migrateSourcesCommand(ctx, args)
	}
	if name == "secret" {
		if len(args) == 0 || args[0] != "set" {
			return errors.New("usage: ingest secret set [options] NAME < secret-file")
		}
		args = args[1:]
	}
	switch name {
	case "serve", "init", "password", "validate", "enqueue", "pause", "resume", "cancel", "secret", "import-legacy":
	default:
		return errors.New("unknown command; run ingest help")
	}
	c := config{}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.StringVar(&c.dataDir, "data-dir", environment("DATA_DIR", ".ingest"), "private state directory (master key and initial password)")
	flags.StringVar(&c.providersDir, "providers", environment("PROVIDERS_DIR", "providers"), "authoritative JSON provider directory")
	var providerID, runMode, runID string
	var pageLimit, knownPageLimit int
	if name == "serve" {
		flags.StringVar(&c.bind, "bind", environment("BIND", "127.0.0.1:8080"), "HTTP listen address")
		flags.StringVar(&c.publicURL, "public-url", os.Getenv("PUBLIC_URL"), "external HTTP(S) origin, required behind HTTPS proxy")
		flags.StringVar(&c.webDir, "web-dir", "", "override compiled frontend assets directory")
		flags.IntVar(&c.workers, "workers", model.DefaultCollectionWorkers, "initial collection concurrency when no setting is saved (1-32)")
	}
	if name == "enqueue" {
		flags.StringVar(&providerID, "provider", "", "provider ID or all enabled providers")
		flags.StringVar(&runMode, "mode", "incremental", "preview, incremental, full or metadata")
		flags.IntVar(&pageLimit, "max-pages", 0, "per-attempt page budget; 0 is unlimited; preview defaults to 3 when omitted")
		flags.IntVar(&knownPageLimit, "known-pages", 0, "Incremental consecutive known-page boundary; omitted inherits the source, 0 disables early stopping")
	}
	if name == "pause" || name == "resume" || name == "cancel" {
		flags.StringVar(&runID, "run", "", "saved collection ID")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if name != "secret" && flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if name == "secret" && flags.NArg() != 1 {
		return errors.New("secret set requires exactly one secret name")
	}
	if (name == "pause" || name == "resume" || name == "cancel") && runID == "" {
		return errors.New("--run is required")
	}
	if name == "enqueue" && providerID == "" {
		return errors.New("--provider is required")
	}
	if name == "serve" && (c.workers < 1 || c.workers > model.MaxCollectionWorkers) {
		return errors.New("--workers must be between 1 and 32")
	}
	if name == "init" {
		if err := privateDirectory(c.dataDir); err != nil {
			return err
		}
		if _, err := masterKey(c.dataDir); err != nil {
			return err
		}
		if _, err := administratorPassword(c.dataDir); err != nil {
			return err
		}
		registry, err := providers.New(c.providersDir, connectors.Validate)
		if err != nil {
			return err
		}
		defer registry.Close()
		fmt.Printf("Private state ready in %s; providers in %s\n", c.dataDir, c.providersDir)
		if os.Getenv("ADMIN_PASSWORD") == "" {
			fmt.Printf("Initial password: %s\n", filepath.Join(c.dataDir, "admin-password"))
		}
		return nil
	}
	if name == "password" {
		value := os.Getenv("ADMIN_PASSWORD")
		if value == "" {
			data, err := readPrivateFile(filepath.Join(c.dataDir, "admin-password"))
			if err != nil {
				return err
			}
			value = strings.TrimSuffix(string(data), "\n")
		}
		fmt.Println(value)
		return nil
	}
	if name == "validate" {
		return validateDefinitions(c.providersDir)
	}
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL is required")
	}
	startup, stopStartup := context.WithTimeout(ctx, 30*time.Second)
	finishStage := startupStage(name == "serve", "database connection")
	db, err := store.Open(startup, os.Getenv("DATABASE_URL"))
	stopStartup()
	if err != nil {
		return err
	}
	defer db.Close()
	finishStage()
	finishStage = startupStage(name == "serve", "schema migrations")
	// Index builds on an existing catalogue can outlast the connection budget.
	// The command's context still cancels and rolls back an interrupted upgrade.
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	finishStage()
	// Pause and Cancel need no editable source or vault: a broken definition
	// must never prevent stopping durable work in another service process.
	if name == "pause" || name == "cancel" {
		manager := jobs.New(db, nil, nil, 1)
		var run model.Run
		if name == "pause" {
			run, err = manager.Pause(ctx, runID)
		} else {
			run, err = manager.Cancel(ctx, runID)
		}
		if err != nil {
			return err
		}
		return printJSON(run)
	}
	if name == "import-legacy" {
		count, err := db.ImportLegacy(ctx)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"imported": count, "source": "public.torrents", "destination": "ingest.torrents", "source_unchanged": true})
	}
	finishStage = startupStage(name == "serve", "local state and sources")
	if err := privateDirectory(c.dataDir); err != nil {
		return err
	}
	key, err := masterKey(c.dataDir)
	if err != nil {
		return err
	}
	defer clear(key)
	secretVault, err := vault.New(ctx, db, key)
	clear(key)
	if err != nil {
		return err
	}
	if name == "secret" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, (64<<10)+2))
		if err != nil {
			return errors.New("could not read the secret from stdin")
		}
		value := strings.TrimSuffix(string(data), "\n")
		clear(data)
		if err := secretVault.Put(ctx, flags.Arg(0), value); err != nil {
			return err
		}
		fmt.Printf("Secret %s stored; value is not displayed.\n", flags.Arg(0))
		return nil
	}
	registry, err := providers.New(c.providersDir, connectors.Validate)
	if err != nil {
		return err
	}
	defer registry.Close()
	if name == "serve" {
		if _, err := registry.UpgradeDefinitions(); err != nil {
			// The old adapter remains supported if the filesystem cannot be
			// updated. A cosmetic rename must not prevent collection recovery.
			fmt.Fprintln(os.Stderr, "ingest: Source adapter names could not be updated; existing definitions remain supported")
		}
	}
	manager := jobs.New(db, registry, secretVault.Resolve, c.workers)
	finishStage()
	if name == "resume" {
		run, err := manager.Resume(ctx, runID)
		if err != nil {
			return err
		}
		return printJSON(run)
	}
	if name == "enqueue" {
		var maxPages, knownPages *int
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "max-pages" {
				maxPages = &pageLimit
			}
			if f.Name == "known-pages" {
				knownPages = &knownPageLimit
			}
		})
		ids := []string{providerID}
		if providerID == "all" {
			items, err := registry.List()
			if err != nil {
				return err
			}
			ids = nil
			for _, item := range items {
				if item.Valid && item.Enabled {
					ids = append(ids, item.ID)
				}
			}
			if len(ids) == 0 {
				return errors.New("no valid enabled providers")
			}
		}
		var failures []error
		for _, id := range ids {
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: id, Mode: model.RunMode(runMode), MaxPages: maxPages, KnownPages: knownPages})
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", id, err))
				continue
			}
			if err := printJSON(run); err != nil {
				return err
			}
		}
		return errors.Join(failures...)
	}
	return serve(ctx, c, db, registry, secretVault, manager)
}

func serve(ctx context.Context, c config, db *store.Store, registry *providers.Registry, secretVault *vault.Vault, manager *jobs.Manager) (serveError error) {
	password, err := administratorPassword(c.dataDir)
	if err != nil {
		return err
	}
	var frontend fs.FS
	if c.webDir != "" {
		frontend = os.DirFS(c.webDir)
	} else {
		frontend, err = webui.Assets()
		if err != nil {
			return err
		}
	}
	monitor, err := health.New(db, c.dataDir)
	if err != nil {
		return err
	}
	var addon *aiosource.Service
	operationsClosed := false
	closeOperations := func(shutdown context.Context) error {
		operationsClosed = true
		var addonError error
		if addon != nil {
			addonError = addon.Close(shutdown)
		}
		return errors.Join(addonError, monitor.Close(shutdown))
	}
	defer func() {
		if !operationsClosed {
			shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			serveError = errors.Join(serveError, closeOperations(shutdown))
		}
	}()
	finishStage := startupStage(true, "application services")
	addon, err = aiosource.New(ctx, db, secretVault, registry)
	if err != nil {
		return err
	}
	finishStage()
	finishStage = startupStage(true, "sign-in protection")
	handler, err := httpapi.New(httpapi.Options{
		Store: db, Providers: registry, Jobs: manager, Vault: secretVault, AIOSource: addon,
		Health: monitor, AdminPassword: password,
		PublicURL: c.publicURL, Frontend: frontend, Version: version,
	})
	if err != nil {
		return err
	}
	finishStage()
	listener, err := net.Listen("tcp", c.bind)
	if err != nil {
		return errors.New("could not bind the requested HTTP address")
	}
	defer listener.Close()
	workerContext, stopServices := context.WithCancel(ctx)
	defer stopServices()
	finishStage = startupStage(true, "collection recovery")
	if err := manager.Start(workerContext); err != nil {
		return err
	}
	finishStage()
	finishStage = startupStage(true, "storage monitor and startup activity")
	startupError := monitor.Start(workerContext)
	if startupError == nil {
		var collections model.CollectionOverview
		collections, startupError = db.CollectionOverview(ctx)
		if startupError == nil {
			startupError = db.RecordActivity(ctx, "info", "service.started", "", "", "", map[string]any{
				"version": version, "workers": collections.Settings.Workers,
			})
		}
	}
	if startupError != nil {
		stopServices()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return errors.Join(startupError, manager.Close(shutdown), closeOperations(shutdown))
	}
	finishStage()
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	result := make(chan error, 1)
	go func() { result <- httpServer.Serve(listener) }()
	fmt.Printf("AIOSources ready at http://%s (version %s)\n", listener.Addr(), version)
	if os.Getenv("ADMIN_PASSWORD") == "" {
		fmt.Printf("Initial administrator password file: %s\n", filepath.Join(c.dataDir, "admin-password"))
	}
	var servingError error
	select {
	case <-ctx.Done():
	case <-manager.Done():
	case <-monitor.Done():
		servingError = monitor.Err()
	case servingError = <-result:
		if errors.Is(servingError, http.ErrServerClosed) {
			servingError = nil
		}
	}
	stopServices()
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Cancellation stops every producer before the HTTP server and database close.
	managerError := manager.Close(shutdown)
	operationsError := closeOperations(shutdown)
	backgroundFailure := monitor.Err()
	httpError := httpServer.Shutdown(shutdown)
	if httpError != nil {
		_ = httpServer.Close()
	}
	level, kind := "info", "service.stopped"
	if servingError != nil || managerError != nil || operationsError != nil || backgroundFailure != nil || httpError != nil {
		level, kind = "error", "service.failed"
	}
	activityError := db.RecordActivity(shutdown, level, kind, "", "", "", nil)
	return errors.Join(servingError, managerError, operationsError, backgroundFailure, activityError, httpError)
}

func validateDefinitions(directory string) error {
	registry, err := providers.New(directory, connectors.Validate)
	if err != nil {
		return err
	}
	defer registry.Close()
	items, err := registry.List()
	if err != nil {
		return err
	}
	if items == nil {
		items = []model.ProviderSummary{}
	}
	if err := printJSON(map[string]any{"items": items}); err != nil {
		return err
	}
	for _, item := range items {
		if !item.Valid {
			return errors.New("one or more provider definitions are invalid")
		}
	}
	return nil
}

func printJSON(value any) error { return json.NewEncoder(os.Stdout).Encode(value) }
func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
