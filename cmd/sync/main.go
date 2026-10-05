// Command sync runs the bookmark-sync pipeline: it fetches open tabs from
// Firefox Sync via ffsclient, deduplicates against linkding, runs LLM
// summarization/tagging, and pushes new items to the linkding Inbox.
//
// Configuration is read from the TOML config file
// (default ~/.config/bookmark-sync/config.toml).
//
// Usage:
//
//	bookmark-sync-sync [-config config.toml]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"bookmark-sync/internal/config"
	"bookmark-sync/internal/expand"
	"bookmark-sync/internal/filter"
	"bookmark-sync/internal/linkding"
	"bookmark-sync/internal/llm"
	"bookmark-sync/internal/normalize"
)

// syncTab is one tab as output by `ffsclient tabs list --format json`.
// ffsclient flattens tabs across all devices into a single list.
type syncTab struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	Index        int      `json:"index"`
	Title        string   `json:"title"`
	URLHistory   []string `json:"urlHistory"`
	Icon         string   `json:"icon"`
	LastUsed     string   `json:"lastUsed"`      // formatted; unused by us
	LastUsedUnix int64    `json:"lastUsed_unix"` // Unix seconds
}

func main() {
	configPath := flag.String("config", config.DefaultPath(), "path to TOML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	tabs, err := fetchTabs(cfg.Sync.FirefoxSyncCLI)
	if err != nil {
		log.Fatalf("fetch tabs: %v", err)
	}
	log.Printf("fetched %d tab(s)", len(tabs))

	client := linkding.New(cfg.Linkding.BaseURL, cfg.Linkding.APIToken)
	domainFilter := filter.New(cfg.Filter.BlockedDomains)
	expander := expand.New(cfg.Expand.Hosts, time.Duration(cfg.Expand.TimeoutSecs)*time.Second)

	// Dedup: load the set of existing bookmark URLs up front.
	// IMPORTANT: we deliberately do NOT use the linkding /check API, because it
	// fetches each URL's website metadata via requests.get(). That would make
	// outbound requests to every open tab's site even when LLM is disabled.
	// The list API only reads the database and never touches external URLs.
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

	// LLM は既定で無効（config の [llm] enabled が false）。
	// 無効時は要約・タグ生成を一切呼ばず、タイトル+URL+inbox タグだけで保存する。
	llmEnabled := cfg.LLM.Enabled
	var summarizer, tagger *llm.Provider
	if llmEnabled {
		summarizer = llm.NewProvider(
			cfg.LLM.Summarize.BaseURL,
			cfg.LLM.Summarize.APIKey,
			cfg.LLM.Summarize.Model,
		)
		tagger = llm.NewProvider(
			cfg.LLM.Tag.BaseURL,
			cfg.LLM.Tag.APIKey,
			cfg.LLM.Tag.Model,
		)
	}

	created, skipped, failed := 0, 0, 0
	for _, tab := range tabs {
		url := pickURL(tab.URLHistory)
		if url == "" {
			continue
		}
		norm := normalize.URL(url)
		if norm == "" {
			continue
		}

		// Domain filter.
		if domainFilter.IsBlocked(norm) {
			log.Printf("  skip blocked: %s", norm)
			skipped++
			continue
		}

		// Dedup against linkding (local URL set, loaded once up front).
		if existing[norm] {
			log.Printf("  skip exists: %s", norm)
			skipped++
			continue
		}

		// Build the bookmark.
		title := tab.Title
		if title == "" {
			title = norm
		}
		if cfg.Expand.Enabled {
			if expanded := expander.Title(title); expanded != title {
				log.Printf("  expanded short URL in title: %q -> %q", title, expanded)
				title = expanded
			}
		}
		b := linkding.Bookmark{
			URL:      norm,
			Title:    title,
			TagNames: []string{"inbox"},
		}

		// date_added = lastUsed (approximation of when the tab was used/opened).
		if tab.LastUsedUnix > 0 {
			b.DateAdded = time.Unix(tab.LastUsedUnix, 0).UTC().Format("2006-01-02T15:04:05Z")
		}

		// LLM: summarization (skipped when [llm] enabled=false).
		summary := ""
		if llmEnabled {
			var err error
			summary, err = summarizer.Summarize(title, norm, cfg.LLM.SummarizePrompt)
			if err != nil {
				log.Printf("  summary failed for %q: %v", norm, err)
				summary = "" // acceptable; continue with empty summary
			}
		}
		b.Description = summary

		// LLM: tag generation (skipped when [llm] enabled=false).
		llmTags := []string{}
		if llmEnabled {
			var err error
			llmTags, err = tagger.GenerateTags(title, norm, summary, cfg.LLM.TagPrompt)
			if err != nil {
				log.Printf("  tag generation failed for %q: %v", norm, err)
				llmTags = nil
			}
		}
		for _, t := range llmTags {
			t = strings.TrimSpace(t)
			if t != "" && t != "inbox" {
				b.TagNames = append(b.TagNames, t)
			}
		}

		// Create in linkding.
		if _, err := client.Create(b); err != nil {
			log.Printf("  create failed %q: %v", norm, err)
			failed++
			continue
		}
		created++
	}

	log.Printf("done: %d created, %d skipped (existing/blocked), %d failed", created, skipped, failed)
}

// fetchTabs runs `ffsclient tabs list --format json` and parses the output.
// It is the sole input source of bookmark-sync-sync: we never read
// ffsclient bookmarks list or any other Firefox Sync collection.
// It expects ffsclient to be installed and authenticated.
func fetchTabs(cliPath string) ([]syncTab, error) {
	cmd := exec.Command(cliPath, "tabs", "list", "--format", "json")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run %s tabs list: %w", cliPath, err)
	}

	var tabs []syncTab
	if err := json.Unmarshal(out, &tabs); err != nil {
		return nil, fmt.Errorf("parse ffsclient output: %w", err)
	}
	return tabs, nil
}

// pickURL returns the current URL (first entry in urlHistory) or empty.
func pickURL(history []string) string {
	if len(history) == 0 {
		return ""
	}
	return history[0]
}
