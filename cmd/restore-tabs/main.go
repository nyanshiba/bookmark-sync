// Command restore-tabs imports the open tabs of Firefox sessionstore files
// (sessionstore.jsonlz4 and sessionstore-backups/*.jsonlz4) directly into
// linkding, bypassing Firefox Sync.
//
// It is a recovery tool for when the Sync tabs collection no longer contains
// the tabs — for example after cleaning up orphaned clients (SETUP.md ch. 13)
// — but the local session files still do. The per-tab lastAccessed timestamp
// is preserved as linkding's date_added, matching what the normal
// bookmark-sync-sync pipeline does with a tab's lastUsed.
//
// Usage:
//
//	bookmark-sync-restore-tabs [-config config.toml] sessionstore.jsonlz4 [...]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"bookmark-sync/internal/config"
	"bookmark-sync/internal/expand"
	"bookmark-sync/internal/filter"
	"bookmark-sync/internal/linkding"
	"bookmark-sync/internal/normalize"
	"bookmark-sync/internal/sessionstore"
)

func main() {
	var (
		configPath = flag.String("config", config.DefaultPath(), "path to TOML config")
		dryRun     = flag.Bool("dry-run", false, "print what would be done without writing")
	)
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bookmark-sync-restore-tabs [-config config.toml] sessionstore.jsonlz4 [...]")
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Parse every sessionstore file into a flat tab list.
	var tabs []sessionstore.Tab
	for _, f := range files {
		parsed, err := sessionstore.ParseFile(f)
		if err != nil {
			log.Fatalf("parse %s: %v", f, err)
		}
		log.Printf("parsed %d tabs from %s", len(parsed), f)
		tabs = append(tabs, parsed...)
	}
	log.Printf("total %d tab(s) from %d file(s)", len(tabs), len(files))

	client := linkding.New(cfg.Linkding.BaseURL, cfg.Linkding.APIToken)
	domainFilter := filter.New(cfg.Filter.BlockedDomains)
	expander := expand.New(cfg.Expand.Hosts, time.Duration(cfg.Expand.TimeoutSecs)*time.Second)

	// Dedup: load the set of existing bookmark URLs up front (list API only,
	// never /check — same reason as cmd/sync and cmd/import).
	log.Println("loading existing bookmark URLs…")
	rawURLs, err := client.ListBookmarkURLs()
	if err != nil {
		log.Fatalf("list bookmarks: %v", err)
	}
	existing := make(map[string]bool, len(rawURLs))
	for u := range rawURLs {
		if n := normalize.URL(u); n != "" {
			existing[n] = true
		}
	}
	log.Printf("loaded %d existing bookmark URL(s)", len(existing))

	created, skipped, failed := 0, 0, 0
	for i, tab := range tabs {
		norm := normalize.URL(tab.URL)
		if norm == "" {
			skipped++ // about:/blank:/invalid URLs normalize to empty
			continue
		}
		if domainFilter.IsBlocked(norm) {
			skipped++
			continue
		}
		if existing[norm] {
			skipped++
			continue
		}

		title := tab.Title
		if title == "" {
			title = norm
		}
		if cfg.Expand.Enabled {
			if expanded := expander.Title(title); expanded != title {
				log.Printf("  [%d] expanded short URL in title: %q -> %q", i, title, expanded)
				title = expanded
			}
		}
		b := linkding.Bookmark{
			URL:      norm,
			Title:    title,
			TagNames: []string{"inbox"},
		}
		// date_added = lastAccessed (approximation of when the tab was last
		// used), same semantics as the sync pipeline's tab lastUsed.
		if tab.LastUsedUnix > 0 {
			b.DateAdded = time.Unix(tab.LastUsedUnix, 0).UTC().Format("2006-01-02T15:04:05Z")
		}

		if *dryRun {
			fmt.Printf("  would create %q -> %s (%s)\n", title, norm, b.DateAdded)
			continue
		}

		if _, err := client.Create(b); err != nil {
			log.Printf("  [%d] create failed %q: %v", i, norm, err)
			failed++
			continue
		}
		created++
		existing[norm] = true // keep later tabs/files from duplicating
		if created%100 == 0 {
			log.Printf("  ... %d created so far", created)
		}
	}

	log.Printf("done: %d created, %d skipped, %d failed", created, skipped, failed)
}
