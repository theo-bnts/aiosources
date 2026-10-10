// Command crawl discovers a Torznab endpoint and streams release metadata as
// JSON Lines. It does not download torrents or publish data to another service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/moodiness/ingest/torznab"
)

var errPageLimit = errors.New("requested page limit reached")

type collectionConfig struct {
	client     torznab.Config
	categories []int
	projection *torznab.Projection
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	site := flag.String("url", "", "Website URL or complete Torznab endpoint (or TORZNAB_URL)")
	providerPath := flag.String("provider", "", "Custom provider JSON file; credentials are resolved from its env references")
	publishedAtUnit := flag.String("published-at-unit", "", "Unix publication date unit: seconds or milliseconds (overrides provider setting)")
	query := flag.String("query", "", "Optional search text; empty requests the accessible feed")
	offset := flag.Int("offset", 0, "Initial result offset (ordering may change between runs)")
	pageLimit := flag.Int("pages", 0, "Maximum nonempty pages to emit in full (0 = all accessible pages)")
	flag.Parse()
	if *offset < 0 {
		return fmt.Errorf("-offset must not be negative")
	}
	if *pageLimit < 0 {
		return fmt.Errorf("-pages must not be negative")
	}
	config, err := configuration(*site, *providerPath)
	if err != nil {
		return err
	}
	if *publishedAtUnit != "" {
		config.client.PublishedAtUnit = *publishedAtUnit
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := torznab.Open(ctx, config.client)
	if err != nil {
		return err
	}
	caps := client.Capabilities()
	fmt.Fprintf(os.Stderr, "Torznab discovered: %d top-level categories, maximum page size %d (0 = unknown)\n", len(caps.Categories), caps.Limits.Max)
	encoder := json.NewEncoder(os.Stdout)
	count := 0
	pages := 0
	err = client.Walk(ctx, torznab.Query{Text: *query, Offset: *offset, Categories: config.categories}, func(page torznab.Page) error {
		for index := range page.Items {
			item := &page.Items[index]
			var record any = item
			if config.projection != nil {
				record = config.projection.Project(item)
			}
			if err := encoder.Encode(record); err != nil {
				return err
			}
			count++
		}
		pages++
		fmt.Fprintf(os.Stderr, "Page %d: offset=%d, items=%d\n", pages, page.Offset, len(page.Items))
		if *pageLimit > 0 && pages >= *pageLimit {
			return errPageLimit
		}
		return nil
	})
	summary := "Accessible feed traversed"
	if errors.Is(err, errPageLimit) {
		summary = "Page limit reached"
	} else if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s: %d pages, %d items; observed attributes: %v\n", summary, pages, count, client.Attributes())
	return nil
}

func configuration(site, providerPath string) (collectionConfig, error) {
	if providerPath != "" {
		if site != "" {
			return collectionConfig{}, fmt.Errorf("choose -provider or -url, not both")
		}
		file, err := os.Open(providerPath)
		if err != nil {
			return collectionConfig{}, err
		}
		defer file.Close()
		provider, err := torznab.LoadProvider(file)
		if err != nil {
			return collectionConfig{}, err
		}
		clientConfig, err := provider.Config(nil)
		if err != nil {
			return collectionConfig{}, err
		}
		result := collectionConfig{client: clientConfig, categories: provider.Search.Categories}
		if provider.Output.Fields != nil {
			result.projection, err = torznab.NewProjection(provider.Output.Fields)
			if err != nil {
				return collectionConfig{}, err
			}
		}
		return result, nil
	}
	if site == "" {
		site = os.Getenv("TORZNAB_URL")
	}
	if site == "" {
		return collectionConfig{}, fmt.Errorf("provide -provider, -url or TORZNAB_URL")
	}
	return collectionConfig{client: torznab.Config{
		URL:      site,
		APIKey:   os.Getenv("TORZNAB_API_KEY"),
		Cookie:   os.Getenv("TORZNAB_COOKIE"),
		Username: os.Getenv("TORZNAB_USERNAME"),
		Password: os.Getenv("TORZNAB_PASSWORD"),
	}}, nil
}
