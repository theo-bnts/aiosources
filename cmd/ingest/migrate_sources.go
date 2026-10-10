package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func migrateSourcesCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("migrate-sources", flag.ContinueOnError)
	directory := flags.String("providers", environment("PROVIDERS_DIR", "providers"), "existing source directory; stop the application and all source writers first")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: ingest migrate-sources --providers DIRECTORY")
		fmt.Fprintln(flags.Output(), "Offline only: stop the application and all source writers before migration.")
		fmt.Fprintln(flags.Output(), "Preflights all YAML definitions, source IDs and JSON target collisions before source writes.")
		fmt.Fprintln(flags.Output(), "Retains exact originals and a private recovery journal in DIRECTORY/.source-migration/.")
		fmt.Fprintln(flags.Output(), "Never overwrites unrelated JSON. Interrupted runs resume only matching journaled output.")
		fmt.Fprintln(flags.Output(), "No database access, network requests, secret resolution or run-snapshot rewrites.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("migrate-sources does not accept positional arguments")
	}
	result, err := providers.MigrateSources(ctx, *directory, connectors.Validate)
	if err != nil {
		return err
	}
	return printJSON(result)
}
