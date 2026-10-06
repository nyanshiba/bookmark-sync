// Package clef classifies bookmarks with Cloudflare's Clef decision models
// (clef-flash, clef) via the Workers AI REST API.
//
// Unlike chat generation, Clef answers closed questions: given the bookmark
// (state) and the existing linkding tags (choice criteria), it returns the
// nearest tag with per-option probabilities. No new tags are ever created.
package clef

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxOptionsPerQuestion is Clef's hard limit for choice criteria (2-255).
const maxOptionsPerQuestion = 255

// maxQuestionsPerRequest is Clef's hard limit for the questions map (1-64).
const maxQuestionsPerRequest = 64

// Client calls the Workers AI run endpoint for a Clef model.
type Client struct {
	endpoint string
	apiToken string
	model    string // e.g. "clef-flash"
	http     *http.Client
}

// New creates a new Client. accountID is the Cloudflare account ID,
// apiToken a token with Workers AI access, model "clef-flash" or "clef".
func New(accountID, apiToken, model string) *Client {
	return &Client{
		endpoint: fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/@cf/cloudflare/%s", accountID, model),
		apiToken: apiToken,
		model:    model,
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

type choiceQuestion struct {
	Type         string         `json:"type"`
	Instructions string         `json:"instructions"`
	Criteria     map[string]any `json:"criteria"`
}

type runRequest struct {
	Model     string                    `json:"model"`
	State     string                    `json:"state"`
	Questions map[string]choiceQuestion `json:"questions"`
}

type choiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

type runResult struct {
	Answers map[string]choiceAnswer `json:"answers"`
	Usage   struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

// Classification is one Clef decision: the winning tag, its probability
// (0-1), and the reported input tokens for budget accounting.
type Classification struct {
	Tag         string
	Probability float64
	InputTokens int
}

// Classify picks the nearest tag for the given bookmark state (title + URL)
// from options (existing linkding tags). Options are split into chunks of
// maxOptionsPerQuestion, each asked as one choice question in a single
// request; the option with the highest probability across all answers wins.
func (c *Client) Classify(state string, options []string, instructions string) (Classification, error) {
	if len(options) == 0 {
		return Classification{}, fmt.Errorf("classify: no options")
	}
	if n := (len(options) + maxOptionsPerQuestion - 1) / maxOptionsPerQuestion; n > maxQuestionsPerRequest {
		return Classification{}, fmt.Errorf("classify: %d options exceed %d-question limit", len(options), maxQuestionsPerRequest)
	}

	req := runRequest{
		Model:     c.model,
		State:     state,
		Questions: map[string]choiceQuestion{},
	}
	for i := 0; i < len(options); i += maxOptionsPerQuestion {
		end := i + maxOptionsPerQuestion
		if end > len(options) {
			end = len(options)
		}
		criteria := make(map[string]any, end-i)
		for _, opt := range options[i:end] {
			criteria[opt] = nil
		}
		qid := fmt.Sprintf("q%d", len(req.Questions))
		req.Questions[qid] = choiceQuestion{
			Type:         "choice",
			Instructions: instructions,
			Criteria:     criteria,
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Classification{}, fmt.Errorf("marshal request: %w", err)
	}

	u := c.endpoint
	httpReq, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return Classification{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Classification{}, fmt.Errorf("clef request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Classification{}, fmt.Errorf("clef request: status %d: %s", resp.StatusCode, msg)
	}

	var envelope struct {
		Result  *runResult `json:"result"`
		Success bool       `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return Classification{}, fmt.Errorf("decode response: %w", err)
	}
	if envelope.Result == nil {
		return Classification{}, fmt.Errorf("clef request: empty result")
	}

	best, bestProb := "", 0.0
	for _, ans := range envelope.Result.Answers {
		for opt, p := range ans.Probabilities {
			if p > bestProb {
				best, bestProb = opt, p
			}
		}
	}
	if best == "" {
		return Classification{}, fmt.Errorf("clef request: no choice returned")
	}
	return Classification{Tag: best, Probability: bestProb, InputTokens: envelope.Result.Usage.InputTokens}, nil
}
