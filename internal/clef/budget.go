package clef

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unicode/utf8"
)

// inputNeuronsPerM maps Clef models to their Workers AI neuron rates
// (neurons per million input tokens). Unknown models fall back to the
// highest rate so estimates stay on the safe side.
var inputNeuronsPerM = map[string]int{
	"clef-flash": 8182,
	"clef":       21818,
}

// InputNeuronsPerM returns the neuron rate for the given model.
func InputNeuronsPerM(model string) int {
	if n, ok := inputNeuronsPerM[model]; ok {
		return n
	}
	return 21818
}

// EstimateInputTokens conservatively estimates Clef input tokens from the
// request strings. Japanese-heavy text runs near 1-2 characters per token,
// so runes/2 overestimates English text — deliberately, since overestimating
// stops earlier and never overshoots the free tier.
func EstimateInputTokens(state string, options []string, instructions string) int {
	runes := utf8.RuneCountInString(state) + utf8.RuneCountInString(instructions) + 64
	for _, opt := range options {
		runes += utf8.RuneCountInString(opt)
	}
	return runes / 2
}

// EstimateNeurons converts the token estimate to neurons for the model.
func EstimateNeurons(model, state string, options []string, instructions string) int {
	tokens := EstimateInputTokens(state, options, instructions)
	return tokens * InputNeuronsPerM(model) / 1000000
}

// ActualNeurons converts reported input tokens to neurons for the model.
func ActualNeurons(model string, inputTokens int) int {
	return inputTokens * InputNeuronsPerM(model) / 1000000
}

// Budget tracks daily Clef usage against configured caps shared by the sync
// pipeline and the backfill command. Limits reset at 00:00 UTC, matching
// Workers AI's daily reset. A zero cap disables that dimension.
type Budget struct {
	MaxNeurons  int
	MaxRequests int
	path        string

	Date     string `json:"date"` // YYYY-MM-DD UTC
	Neurons  int    `json:"neurons"`
	Requests int    `json:"requests"`
}

// LoadBudget reads usage from path, resetting it when the stored date is not
// today (UTC). A missing file starts at zero; that is not an error.
func LoadBudget(path string, maxNeurons, maxRequests int) (*Budget, error) {
	b := &Budget{
		MaxNeurons:  maxNeurons,
		MaxRequests: maxRequests,
		path:        path,
		Date:        time.Now().UTC().Format("2006-01-02"),
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, fmt.Errorf("reading budget: %w", err)
	}
	var stored Budget
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("parsing budget: %w", err)
	}
	if stored.Date == b.Date {
		b.Neurons = stored.Neurons
		b.Requests = stored.Requests
	}
	return b, nil
}

// Allow reports whether a request estimated at estNeurons fits the caps.
func (b *Budget) Allow(estNeurons int) bool {
	if b.MaxRequests > 0 && b.Requests >= b.MaxRequests {
		return false
	}
	if b.MaxNeurons > 0 && b.Neurons+estNeurons > b.MaxNeurons {
		return false
	}
	return true
}

// Add records one completed request and persists the usage file.
func (b *Budget) Add(neurons int) error {
	b.Requests++
	b.Neurons += neurons
	return b.Save()
}

// Save persists the usage file.
func (b *Budget) Save() error {
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal budget: %w", err)
	}
	if err := os.WriteFile(b.path, raw, 0600); err != nil {
		return fmt.Errorf("writing budget: %w", err)
	}
	return nil
}
