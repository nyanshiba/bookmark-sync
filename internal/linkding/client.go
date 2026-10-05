// Package linkding implements the subset of the linkding REST API
// needed by bookmark-sync: listing existing URLs for dedup, create, and update.
package linkding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Client is a minimal linkding API client.
type Client struct {
	baseURL   string
	token     string
	http      *http.Client
	userAgent string
}

// Bookmark mirrors the fields of a linkding bookmark we care about.
type Bookmark struct {
	ID           int64    `json:"id"`
	URL          string   `json:"url"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Notes        string   `json:"notes"`
	TagNames     []string `json:"tag_names"`
	DateAdded    string   `json:"date_added,omitempty"`
	DateModified string   `json:"date_modified,omitempty"`
	Shared       bool     `json:"shared"`
}

const (
	// maxTitleLen is linkding's hard limit for the title field (code points).
	maxTitleLen = 512
	// tagTitleTruncated marks a bookmark whose title was truncated to fit.
	tagTitleTruncated = "title-truncated"
)

// New creates a new Client.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL:   stringsTrimSlash(baseURL),
		token:     token,
		http: &http.Client{
			Timeout: 60 * time.Second,
			// linkding は uWSGI（--http ルータ）で稼働し、HTTP keep-alive を
			// 有効化していない（Connection: close を返す）。接続を使い回せる
			// 見込みがないため、Go 側でアイドル接続をプールに保持すると
			// EOF / server closed idle connection の再試行が毎回発生するだけ。
			// DisableKeepAlives でプールを使わず、IdleConnTimeout を短くして
			// 万一の残存接続も速やかに破棄する。
			Transport: &http.Transport{
				DisableKeepAlives: true,
				IdleConnTimeout:   3 * time.Second,
			},
		},
		userAgent: "bookmark-sync/1.0",
	}
}

// ListBookmarkURLs returns the set of all bookmark URLs currently stored in
// linkding (both active and archived), fetched via the paginated list API.
//
// IMPORTANT: this is the safe way to deduplicate. The /api/bookmarks/check/
// endpoint fetches each URL's website metadata via requests.get(), so calling
// it per-tab generates outbound traffic to every bookmarked site — unrelated
// to whether LLM is enabled. The list API only queries the database and never
// touches the external URLs.
func (c *Client) ListBookmarkURLs() (map[string]bool, error) {
	urls := map[string]bool{}
	for _, path := range []string{"/api/bookmarks/", "/api/bookmarks/archived/"} {
		offset := 0
		for {
			u := c.baseURL + path + "?" + url.Values{
				"limit":  {"100"},
				"offset": {strconv.Itoa(offset)},
			}.Encode()

			var page struct {
				Results []struct {
					URL string `json:"url"`
				} `json:"results"`
				Next string `json:"next"`
			}
			if err := c.get(u, &page); err != nil {
				return nil, err
			}
			for _, b := range page.Results {
				if b.URL != "" {
					urls[b.URL] = true
				}
			}
			if page.Next == "" || len(page.Results) == 0 {
				break
			}
			offset += len(page.Results)
		}
	}
	return urls, nil
}

// ListBookmarks returns all bookmarks (both active and archived) with the
// fields needed for title maintenance: id, url, title.
func (c *Client) ListBookmarks() ([]Bookmark, error) {
	var out []Bookmark
	for _, path := range []string{"/api/bookmarks/", "/api/bookmarks/archived/"} {
		page, err := c.fetchBookmarkPages(path, url.Values{})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
	}
	return out, nil
}

// SearchBookmarks returns bookmarks (both active and archived) matching any
// of the given queries, deduplicated by id. Each query is passed as the list
// API's q parameter, so filtering happens server-side — callers fetch only
// candidates instead of the whole collection.
func (c *Client) SearchBookmarks(queries []string) ([]Bookmark, error) {
	var out []Bookmark
	seen := map[int64]bool{}
	for _, q := range queries {
		if q == "" {
			continue
		}
		for _, path := range []string{"/api/bookmarks/", "/api/bookmarks/archived/"} {
			page, err := c.fetchBookmarkPages(path, url.Values{"q": {q}})
			if err != nil {
				return nil, err
			}
			for _, b := range page {
				if !seen[b.ID] {
					seen[b.ID] = true
					out = append(out, b)
				}
			}
		}
	}
	return out, nil
}

func (c *Client) fetchBookmarkPages(path string, query url.Values) ([]Bookmark, error) {
	var out []Bookmark
	offset := 0
	for {
		query.Set("limit", "100")
		query.Set("offset", strconv.Itoa(offset))
		u := c.baseURL + path + "?" + query.Encode()

		var page struct {
			Results []Bookmark `json:"results"`
			Next    string     `json:"next"`
		}
		if err := c.get(u, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Results...)
		if page.Next == "" || len(page.Results) == 0 {
			break
		}
		offset += len(page.Results)
	}
	return out, nil
}

// Create creates a new bookmark via POST /api/bookmarks/.
// The disable_scraping query parameter tells linkding not to fetch the
// target URL's metadata, so creating a bookmark never triggers an outbound
// request to the bookmarked site (we already supply the title ourselves).
func (c *Client) Create(b Bookmark) (*Bookmark, error) {
	// linkding rejects titles longer than maxTitleLen code points with a 400
	// ("Ensure this field has no more than 512 characters."). Truncate the
	// tail and tag the bookmark so the truncation is visible in linkding.
	truncated := utf8.RuneCountInString(b.Title) > maxTitleLen
	b.Title = fitTitle(b.Title)
	if truncated {
		b.TagNames = append(b.TagNames, tagTitleTruncated)
	}
	b.Description = strings.ReplaceAll(b.Description, "\x00", "")

	u := c.baseURL + "/api/bookmarks/?" + url.Values{"disable_scraping": {""}}.Encode()
	body, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("marshal bookmark: %w", err)
	}

	resp, err := c.doRetry(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.authorize(req)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("create bookmark: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("create bookmark: unexpected status %d: %s", resp.StatusCode, msg)
	}

	var created Bookmark
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decode created bookmark: %w", err)
	}
	return &created, nil
}

// UpdateTitle patches only the title of an existing bookmark via
// PATCH /api/bookmarks/{id}/. Sending just the title field (and nothing
// else) guarantees tags, notes, and dates are left untouched.
func (c *Client) UpdateTitle(id int64, title string) (*Bookmark, error) {
	body, err := json.Marshal(map[string]string{"title": fitTitle(title)})
	if err != nil {
		return nil, fmt.Errorf("marshal title: %w", err)
	}
	u := fmt.Sprintf("%s/api/bookmarks/%d/", c.baseURL, id)

	resp, err := c.doRetry(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPatch, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.authorize(req)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("update title: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("update title: unexpected status %d: %s", resp.StatusCode, msg)
	}

	var updated Bookmark
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("decode updated bookmark: %w", err)
	}
	return &updated, nil
}

// fitTitle strips NUL bytes (rejected by linkding with a 400) and truncates
// to linkding's 512 code point title limit.
func fitTitle(title string) string {
	title = strings.ReplaceAll(title, "\x00", "")
	if n := utf8.RuneCountInString(title); n > maxTitleLen {
		title = string([]rune(title)[:maxTitleLen])
	}
	return title
}

// Update patches an existing bookmark via PATCH /api/bookmarks/{id}/.
func (c *Client) Update(id int64, b Bookmark) (*Bookmark, error) {
	u := fmt.Sprintf("%s/api/bookmarks/%d/", c.baseURL, id)
	body, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("marshal bookmark: %w", err)
	}

	resp, err := c.doRetry(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPatch, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.authorize(req)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("update bookmark: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("update bookmark: unexpected status %d: %s", resp.StatusCode, msg)
	}

	var updated Bookmark
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("decode updated bookmark: %w", err)
	}
	return &updated, nil
}

func (c *Client) get(u string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: unexpected status %d: %s", u, resp.StatusCode, msg)
	}

	return json.NewDecoder(resp.Body).Decode(dst)
}

// doRetry runs fn and retries on transient connection errors
// (EOF, connection reset, timeout, server closed idle connection). Each
// attempt rebuilds the request so POST/PATCH bodies are sent fresh.
// Backoff starts at 0ms (connection reuse errors are typically resolved
// immediately by a fresh connection) and grows to 500ms.
func (c *Client) doRetry(fn func() (*http.Request, error)) (*http.Response, error) {
	const maxAttempts = 4
	delays := []time.Duration{0, 100 * time.Millisecond, 500 * time.Millisecond}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := fn()
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == maxAttempts-1 {
			return nil, err
		}
		log.Printf("  transient error on attempt %d/%d (%v) — retrying…", attempt+1, maxAttempts, err)
		if attempt < len(delays) {
			time.Sleep(delays[attempt])
		}
	}
	return nil, lastErr
}

// isRetryable reports whether a request error is transient.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"eof", "connection reset", "connection refused", "broken pipe", "unexpected eof", "server closed idle connection"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("User-Agent", c.userAgent)
}

func stringsTrimSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}
