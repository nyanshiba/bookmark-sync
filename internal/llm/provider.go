// Package llm provides an OpenAI-compatible chat completion client
// that works with llama.cpp, Ollama, OpenAI, Anthropic (via proxy), etc.
package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message represents a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the JSON body for POST /v1/chat/completions.
type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// chatResponse is the expected response shape.
type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Provider is an OpenAI-compatible chat completion provider.
type Provider struct {
	baseURL string // e.g. "http://127.0.0.1:8080/v1"
	apiKey  string
	model   string
	http    *http.Client
}

// NewProvider creates a new Provider.
// Leave apiKey empty for local servers (llama.cpp, Ollama).
func NewProvider(baseURL, apiKey, model string) *Provider {
	return &Provider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Chat sends a chat completion request and returns the response text.
func (p *Provider) Chat(messages []Message, temperature float64) (string, error) {
	reqBody := chatRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   1024,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat completion: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("chat completion: status %d: %s", resp.StatusCode, msg)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("chat completion: no choices returned")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}

// Summarize generates a one-line summary for the given title and URL.
func (p *Provider) Summarize(title, url, prompt string) (string, error) {
	tmpl := strings.ReplaceAll(prompt, "{{.Title}}", title)
	tmpl = strings.ReplaceAll(tmpl, "{{.URL}}", url)
	return p.Chat([]Message{
		{Role: "user", Content: tmpl},
	}, 0.3)
}

// GenerateTags generates tags for the given title, URL, and summary.
func (p *Provider) GenerateTags(title, url, summary, prompt string) ([]string, error) {
	tmpl := strings.ReplaceAll(prompt, "{{.Title}}", title)
	tmpl = strings.ReplaceAll(tmpl, "{{.URL}}", url)
	tmpl = strings.ReplaceAll(tmpl, "{{.Summary}}", summary)

	raw, err := p.Chat([]Message{
		{Role: "user", Content: tmpl},
	}, 0.4)
	if err != nil {
		return nil, err
	}

	// Split by comma, clean whitespace, filter empty.
	parts := strings.Split(raw, ",")
	var tags []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			tags = append(tags, t)
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}