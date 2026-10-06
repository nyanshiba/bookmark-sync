package clef

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClassifyPicksMaxProbability verifies that Classify sends one choice
// question and returns the option with the highest probability.
func TestClassifyPicksMaxProbability(t *testing.T) {
	var gotReq runRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing Bearer auth: %q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Write([]byte(`{"success":true,"result":{"model":"clef-flash","answers":{"q0":{"type":"choice","choice":"tech","probabilities":{"tech":0.8,"life":0.2},"confidence":0.8}},"usage":{"input_tokens":10,"output_tokens":2}}}`))
	}))
	defer srv.Close()

	c := &Client{endpoint: srv.URL, apiToken: "token", model: "clef-flash", http: srv.Client()}
	res, err := c.Classify("Title: x\nURL: https://example.com/", []string{"tech", "life"}, "Pick one.")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Tag != "tech" || res.Probability != 0.8 {
		t.Errorf("got (%q, %v), want (tech, 0.8)", res.Tag, res.Probability)
	}
	if res.InputTokens != 10 {
		t.Errorf("input tokens = %d, want 10", res.InputTokens)
	}

	if gotReq.Model != "clef-flash" {
		t.Errorf("model = %q, want clef-flash", gotReq.Model)
	}
	q, ok := gotReq.Questions["q0"]
	if !ok {
		t.Fatalf("missing q0 in %+v", gotReq.Questions)
	}
	if q.Type != "choice" || len(q.Criteria) != 2 {
		t.Errorf("question = %+v, want choice with 2 options", q)
	}
}

// TestClassifyChunksOptions verifies that more than 255 options are split
// into multiple questions in a single request, with the global max winning.
func TestClassifyChunksOptions(t *testing.T) {
	var gotReq runRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotReq)
		w.Write([]byte(`{"success":true,"result":{"answers":{` +
			`"q0":{"type":"choice","choice":"a","probabilities":{"a":0.3},"confidence":0.3},` +
			`"q1":{"type":"choice","choice":"b","probabilities":{"b":0.9},"confidence":0.9}` +
			`}}}`))
	}))
	defer srv.Close()

	opts := make([]string, 300)
	for i := range opts {
		opts[i] = strings.Repeat("t", i+1)
	}
	c := &Client{endpoint: srv.URL, apiToken: "token", model: "clef-flash", http: srv.Client()}
	res, err := c.Classify("state", opts, "Pick one.")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Tag != "b" {
		t.Errorf("tag = %q, want b (global max)", res.Tag)
	}
	if len(gotReq.Questions) != 2 {
		t.Errorf("questions = %d, want 2", len(gotReq.Questions))
	}
}

// TestClassifyNoOptions verifies that empty options fail fast without
// any HTTP request.
func TestClassifyNoOptions(t *testing.T) {
	c := New("acct", "token", "clef-flash")
	if _, err := c.Classify("state", nil, "Pick one."); err == nil {
		t.Error("expected error for empty options, got nil")
	}
}
