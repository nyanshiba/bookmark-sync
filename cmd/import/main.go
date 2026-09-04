// Command import ingests an existing Netscape bookmark HTML file into
// linkding. It is a one-time migration tool: folder paths are converted
// to tags and existing URLs are skipped (original ADD_DATE preserved).
//
// Usage:
//
//	bookmark-sync-import -config config.toml -input bookmarks.html
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"bookmark-sync/internal/bookmark"
	"bookmark-sync/internal/config"
	"bookmark-sync/internal/linkding"
	"bookmark-sync/internal/normalize"
)

func main() {
	var (
		configPath = flag.String("config", config.DefaultPath(), "path to TOML config")
		inputPath  = flag.String("input", "", "path to Netscape bookmark HTML file (required)")
		dryRun     = flag.Bool("dry-run", false, "print what would be done without writing")
	)
	flag.Parse()

	if *inputPath == "" {
		fmt.Fprintln(os.Stderr, "error: -input is required")
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	entries, err := bookmark.ParseFile(*inputPath)
	if err != nil {
		log.Fatalf("parse bookmarks: %v", err)
	}
	log.Printf("parsed %d bookmarks from %s", len(entries), *inputPath)

	client := linkding.New(cfg.Linkding.BaseURL, cfg.Linkding.APIToken)

	// Dedup: load the set of existing bookmark URLs up front.
	// We deliberately do NOT use the linkding /check API here: it scrapes the
	// target website (requests.get) for every URL, which for a large import
	// means thousands of outbound requests. The list API only reads the DB.
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
	for i, e := range entries {
		norm := normalize.URL(e.URL)
		if norm == "" {
			log.Printf("  [%d] skip: empty URL (%q)", i, e.Title)
			skipped++
			continue
		}

		// Build tags from the folder path plus any existing TAGS.
		tags := appendFolderTags(e)
		tags = mergeTags(tags, e.Tags)

		if *dryRun {
			fmt.Printf("  would create %q -> %s [%s]\n", e.Title, norm, strings.Join(tags, ", "))
			continue
		}

		if existing[norm] {
			log.Printf("  [%d] skip: already exists (%q)", i, norm)
			skipped++
			continue
		}

		b := linkding.Bookmark{
			URL:      norm,
			Title:    e.Title,
			TagNames: tags,
		}
		// Preserve the original ADD_DATE as date_added.
		if e.DateAdded != "" {
			b.DateAdded = unixSeconds(e.DateAdded)
		}
		if e.LastModified != "" {
			b.DateModified = unixSeconds(e.LastModified)
		}

		if _, err := client.Create(b); err != nil {
			log.Printf("  [%d] create failed %q: %v", i, norm, err)
			failed++
			continue
		}
		created++
		if created%100 == 0 {
			log.Printf("  ... %d created so far", created)
		}
	}

	log.Printf("done: %d created, %d skipped, %d failed", created, skipped, failed)
}

// appendFolderTags converts the deepest folder into a single tag.
// Folder hierarchy is not preserved: "Mozilla Firefox > etc. > Coffee"
// becomes just "Coffee". Spaces in the folder name are replaced with
// hyphens ("Mozilla Firefox" -> "Mozilla-Firefox") so linkding does
// not split the tag on whitespace.
func appendFolderTags(e bookmark.Entry) []string {
	if len(e.Folder) == 0 {
		return nil
	}
	leaf := e.Folder[len(e.Folder)-1]
	leaf = strings.ReplaceAll(leaf, " ", "-")
	if leaf == "" {
		return nil
	}
	return []string{leaf}
}

// mergeTags appends tags not already present.
func mergeTags(base, extra []string) []string {
	seen := map[string]bool{}
	for _, t := range base {
		seen[t] = true
	}
	for _, t := range extra {
		if !seen[t] {
			base = append(base, t)
			seen[t] = true
		}
	}
	return base
}

// unixSeconds converts a Unix timestamp string (seconds) to an ISO8601
// timestamp that linkding expects for date_added/date_modified.
func unixSeconds(s string) string {
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return ""
	}
	// linkding accepts RFC3339; seconds-level precision is enough.
	return time.Unix(sec, 0).UTC().Format("2006-01-02T15:04:05Z")
}
