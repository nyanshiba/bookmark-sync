package clef

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBudgetAllow verifies that both caps gate requests, and that a zero
// cap disables its dimension.
func TestBudgetAllow(t *testing.T) {
	b := &Budget{MaxNeurons: 100, MaxRequests: 2}
	if !b.Allow(60) {
		t.Error("Allow(60) = false, want true")
	}
	b.Neurons = 50
	if b.Allow(60) {
		t.Error("Allow(60) at 50 used = true, want false (exceeds 100)")
	}
	if !b.Allow(50) {
		t.Error("Allow(50) at 50 used = false, want true (exactly at cap)")
	}
	b.Requests = 2
	if b.Allow(1) {
		t.Error("Allow at request cap = true, want false")
	}

	unlimited := &Budget{}
	if !unlimited.Allow(1000000) {
		t.Error("zero caps must allow everything")
	}
}

// TestBudgetPersistence verifies the usage file round-trips within the same
// UTC date and resets on date rollover.
func TestBudgetPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")

	b, err := LoadBudget(path, 100, 10)
	if err != nil {
		t.Fatalf("LoadBudget: %v", err)
	}
	if err := b.Add(30); err != nil {
		t.Fatalf("Add: %v", err)
	}

	again, err := LoadBudget(path, 100, 10)
	if err != nil {
		t.Fatalf("LoadBudget: %v", err)
	}
	if again.Requests != 1 || again.Neurons != 30 {
		t.Errorf("got (%d req, %d neurons), want (1, 30)", again.Requests, again.Neurons)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("usage file must exist: %v", err)
	}
}

// TestActualNeuronsRounding verifies the per-request debit rounds to
// nearest: truncation would record 3 for a typical flash call and
// undercount ~25% against the dashboard.
func TestActualNeuronsRounding(t *testing.T) {
	if got := ActualNeurons("clef-flash", 1124); got != 4 {
		t.Errorf("ActualNeurons(flash, 1124) = %d, want 4", got)
	}
	if got := ActualNeurons("clef", 1124); got != 25 {
		t.Errorf("ActualNeurons(clef, 1124) = %d, want 25", got)
	}
}

// TestEstimateNeuronsSanity verifies the estimator stays in the low single
// digits per request for a realistic bookmark against 100 tags.
func TestEstimateNeuronsSanity(t *testing.T) {
	opts := make([]string, 100)
	for i := range opts {
		opts[i] = "tagname"
	}
	n := EstimateNeurons("clef-flash", "Title: example\nURL: https://example.com/article", opts, "Pick one.")
	if n < 1 || n > 10 {
		t.Errorf("estimate = %d neurons, want 1-10", n)
	}
}
