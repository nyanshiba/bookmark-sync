// Command expand-titles retroactively expands short URLs (t.co by default)
// embedded in the titles of existing linkding bookmarks.
//
// The sync and restore-tabs pipelines expand short URLs in titles at intake,
// but bookmarks registered before that feature existed keep their raw short
// links. This one-shot command finds those titles and patches them in place.
// Only the title field is sent (PATCH), so tags, notes, and dates are left
// untouched.
//
// Usage:
//
//	bookmark-sync-expand-titles [-config config.toml] [-dry-run]
package main

import (
	"flag"
	"log"
	"time"

	"bookmark-sync/internal/config"
	"bookmark-sync/internal/expand"
	"bookmark-sync/internal/linkding"
)

func main() {
	var (
		configPath = flag.String("config", config.DefaultPath(), "path to TOML config")
		dryRun     = flag.Bool("dry-run", false, "print what would be done without writing")
	)
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !cfg.Expand.Enabled {
		log.Println("expand disabled in config, nothing to do")
		return
	}
	expander := expand.New(cfg.Expand.Hosts, time.Duration(cfg.Expand.TimeoutSecs)*time.Second)

	client := linkding.New(cfg.Linkding.BaseURL, cfg.Linkding.APIToken)

	// Server-side filtering: fetch only bookmarks mentioning a shortener
	// host instead of the whole collection.
	log.Printf("searching bookmarks mentioning %v…", cfg.Expand.Hosts)
	bookmarks, err := client.SearchBookmarks(cfg.Expand.Hosts)
	if err != nil {
		log.Fatalf("search bookmarks: %v", err)
	}
	log.Printf("found %d candidate(s)", len(bookmarks))

	updated, skipped, failed := 0, 0, 0
	for _, b := range bookmarks {
		if len(expander.Candidates(b.Title)) == 0 {
			skipped++
			continue
		}
		expanded := expander.Title(b.Title)
		if expanded == b.Title {
			skipped++
			continue
		}
		if *dryRun {
			log.Printf("  would update [%d] %q -> %q", b.ID, b.Title, expanded)
			continue
		}
		if _, err := client.UpdateTitle(b.ID, expanded); err != nil {
			log.Printf("  [%d] update failed %q: %v", b.ID, b.URL, err)
			failed++
			continue
		}
		log.Printf("  [%d] updated %q -> %q", b.ID, b.Title, expanded)
		updated++
		if updated%100 == 0 {
			log.Printf("  ... %d updated so far", updated)
		}
	}

	log.Printf("done: %d updated, %d skipped, %d failed", updated, skipped, failed)
}
