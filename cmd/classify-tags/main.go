// Command classify-tags retroactively classifies existing linkding
// bookmarks with Clef, picking the nearest existing tag for each.
//
// It processes bookmarks tagged inbox that have no content tag yet, so the
// ~10k backlog never grows new tags: only the closest existing one is added
// alongside the tags already present. The daily budget from [clef]
// (daily_neuron_budget / daily_request_budget) is shared with the sync
// pipeline via clef-usage.json next to the config file; the run stops when
// either cap is reached and resumes where it left off on the next run.
//
// Usage:
//
//	bookmark-sync-classify-tags [-config config.toml] [-dry-run] [-limit N]
package main

import (
	"flag"
	"log"
	"path/filepath"
	"sort"
	"time"

	"bookmark-sync/internal/clef"
	"bookmark-sync/internal/config"
	"bookmark-sync/internal/linkding"
)

// systemTags are never classification candidates and never count as
// "already classified".
var systemTags = map[string]bool{"inbox": true, "title-truncated": true}

func hasContentTag(tags []string) bool {
	for _, t := range tags {
		if !systemTags[t] {
			return true
		}
	}
	return false
}

// hasTag reports whether tags contains name.
func hasTag(tags []string, name string) bool {
	for _, t := range tags {
		if t == name {
			return true
		}
	}
	return false
}

// contentTags returns the non-system tags for display.
func contentTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if !systemTags[t] {
			out = append(out, t)
		}
	}
	return out
}

// sortByOldest orders bookmarks oldest-first (unparseable dates sort last)
// so repeated capped runs drain the backlog in order.
func sortByOldest(bs []linkding.Bookmark) {
	sort.SliceStable(bs, func(i, j int) bool {
		ti, erri := time.Parse(time.RFC3339, bs[i].DateAdded)
		tj, errj := time.Parse(time.RFC3339, bs[j].DateAdded)
		if erri != nil || errj != nil {
			return erri == nil
		}
		return ti.Before(tj)
	})
}

func main() {
	var (
		configPath = flag.String("config", config.DefaultPath(), "path to TOML config")
		dryRun     = flag.Bool("dry-run", false, "print what would be done without writing")
		limit      = flag.Int("limit", 0, "max bookmarks to process (0 = unlimited)")
		model      = flag.String("model", "", "override config model (clef-flash or clef)")
		evalN      = flag.Int("eval", 0, "evaluate agreement on N already-tagged bookmarks without writing")
	)
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !cfg.Clef.Enabled {
		log.Println("clef disabled in config, nothing to do")
		return
	}
	useModel := cfg.Clef.Model
	if *model != "" {
		useModel = *model
	}

	client := linkding.New(cfg.Linkding.BaseURL, cfg.Linkding.APIToken)
	classifier := clef.New(cfg.Clef.AccountID, cfg.Clef.APIToken, useModel)

	allTags, err := client.ListTags()
	if err != nil {
		log.Fatalf("list tags: %v", err)
	}
	var options []string
	for _, t := range allTags {
		if !systemTags[t] {
			options = append(options, t)
		}
	}
	if len(options) < 2 {
		log.Printf("only %d classifiable tag(s), nothing to do", len(options))
		return
	}
	log.Printf("loaded %d existing tag(s) for classification", len(options))

	budget, err := clef.LoadBudget(filepath.Join(filepath.Dir(*configPath), "clef-usage.json"), cfg.Clef.DailyNeuronBudget, cfg.Clef.DailyRequestBudget)
	if err != nil {
		log.Fatalf("load budget: %v", err)
	}

	// Eval mode: no writes. Classifies already-tagged bookmarks and reports
	// how often the model agrees with the user's own tags. The pool is the
	// newest tagged bookmarks excluding inbox (inbox+tagged is empty in
	// practice since reviewed bookmarks have inbox removed). The list API
	// orders by -date_added, so ScanRecent stops after N matches instead of
	// scanning the whole collection.
	if *evalN > 0 {
		log.Println("sampling recent tagged bookmarks for eval ground truth…")
		truth, err := client.ScanRecent(*evalN, func(b linkding.Bookmark) bool {
			return hasContentTag(b.TagNames) && !hasTag(b.TagNames, "inbox")
		})
		if err != nil {
			log.Fatalf("scan bookmarks: %v", err)
		}
		if len(truth) == 0 {
			log.Println("no tagged bookmark found: nothing to evaluate against")
			return
		}
		log.Printf("found %d tagged bookmark(s) as ground truth", len(truth))
		hits, tested := 0, 0
		for _, b := range truth {
			state := "Title: " + b.Title + "\nURL: " + b.URL
			est := clef.EstimateNeurons(useModel, state, options, cfg.Clef.Instructions)
			if !budget.Allow(est) {
				log.Printf("budget exhausted, stopping eval")
				break
			}
			res, err := classifier.Classify(state, options, cfg.Clef.Instructions)
			if err != nil {
				log.Printf("  [%d] classify failed %q: %v", b.ID, b.URL, err)
				continue
			}
			if err := budget.Add(clef.ActualNeurons(useModel, res.InputTokens)); err != nil {
				log.Printf("  budget save failed: %v", err)
			}
			tested++
			match := ""
			for _, t := range b.TagNames {
				if t == res.Tag {
					match = " HIT"
					hits++
					break
				}
			}
			log.Printf("  [%d] %q -> %q (p=%.2f, mine=%v)%s", b.ID, b.Title, res.Tag, res.Probability, contentTags(b.TagNames), match)
		}
		log.Printf("eval (%s): agreement %d/%d", useModel, hits, tested)
		return
	}

	log.Println("searching inbox bookmarks…")
	candidates, err := client.SearchBookmarks([]string{"#inbox"})
	if err != nil {
		log.Fatalf("search bookmarks: %v", err)
	}
	var pending []linkding.Bookmark
	for _, b := range candidates {
		if !hasContentTag(b.TagNames) {
			pending = append(pending, b)
		}
	}
	// Oldest first so repeated capped runs drain the backlog in order.
	sortByOldest(pending)
	if *limit > 0 && len(pending) > *limit {
		pending = pending[:*limit]
	}
	log.Printf("found %d unclassified inbox bookmark(s)", len(pending))

	if *dryRun {
		for i, b := range pending {
			if i >= 10 {
				log.Printf("  … and %d more", len(pending)-10)
				break
			}
			log.Printf("  would classify [%d] %q", b.ID, b.Title)
		}
		return
	}

	updated, failed := 0, 0
	for _, b := range pending {
		state := "Title: " + b.Title + "\nURL: " + b.URL
		est := clef.EstimateNeurons(useModel, state, options, cfg.Clef.Instructions)
		if !budget.Allow(est) {
			log.Printf("budget exhausted (%d/%d neurons, %d/%d requests), stopping",
				budget.Neurons, cfg.Clef.DailyNeuronBudget, budget.Requests, cfg.Clef.DailyRequestBudget)
			break
		}
		res, err := classifier.Classify(state, options, cfg.Clef.Instructions)
		if err != nil {
			log.Printf("  [%d] classify failed %q: %v", b.ID, b.URL, err)
			failed++
			continue
		}
		tags := append(append([]string{}, b.TagNames...), res.Tag)
		if _, err := client.UpdateTags(b.ID, tags); err != nil {
			log.Printf("  [%d] update failed %q: %v", b.ID, b.URL, err)
			failed++
			continue
		}
		if err := budget.Add(clef.ActualNeurons(useModel, res.InputTokens)); err != nil {
			log.Printf("  budget save failed: %v", err)
		}
		log.Printf("  [%d] %q -> %q (p=%.2f)", b.ID, b.Title, res.Tag, res.Probability)
		updated++
		if updated%100 == 0 {
			log.Printf("  ... %d updated so far", updated)
		}
	}

	log.Printf("done: %d updated, %d failed (budget: %d/%d neurons, %d/%d requests)",
		updated, failed, budget.Neurons, cfg.Clef.DailyNeuronBudget, budget.Requests, cfg.Clef.DailyRequestBudget)
}
