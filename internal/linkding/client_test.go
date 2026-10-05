package linkding

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestBookmarkMarshalEmptyDates verifies that empty date fields are NOT
// serialized. Previously, empty date_added/date_modified strings were sent
// as "" and linkding rejected them with HTTP 400 ("Datetime has wrong
// format"). See the create-failed error reported against sync.
func TestBookmarkMarshalEmptyDates(t *testing.T) {
	b := Bookmark{
		URL:      "https://example.com/",
		Title:    "Example",
		TagNames: []string{"inbox"},
	}

	body, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := string(body)

	if strings.Contains(raw, "date_added") {
		t.Errorf("empty date_added must be omitted, got: %s", raw)
	}
	if strings.Contains(raw, "date_modified") {
		t.Errorf("empty date_modified must be omitted, got: %s", raw)
	}
}

// TestIsRetryableServerClosedIdle verifies that the "server closed idle
// connection" error — returned by Go's net/http when the server closes a
// keep-alive connection mid-run — is treated as transient so the same
// bookmark is retried on a fresh connection instead of failing.
func TestIsRetryableServerClosedIdle(t *testing.T) {
	err := errors.New(`Post "http://localhost:9090/api/bookmarks/?disable_scraping=": http: server closed idle connection`)
	if !isRetryable(err) {
		t.Errorf("expected retryable for server closed idle connection, got false")
	}
}

// TestCreateTruncatesOverlongTitle verifies that a title longer than 512
// code points is truncated to fit and marked with the title-truncated tag.
func TestCreateTruncatesOverlongTitle(t *testing.T) {
	var got struct {
		Title    string   `json:"title"`
		TagNames []string `json:"tag_names"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "token")
	longTitle := strings.Repeat("あ", 600) // 600 code points, well over the limit
	_, err := c.Create(Bookmark{URL: "https://example.com/", Title: longTitle})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if n := utf8.RuneCountInString(got.Title); n != maxTitleLen {
		t.Errorf("title rune count = %d, want %d", n, maxTitleLen)
	}
	if !containsTag(got.TagNames, tagTitleTruncated) {
		t.Errorf("expected tag %q, got %v", tagTitleTruncated, got.TagNames)
	}
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// TestCreateStripsNUL verifies that NUL bytes in the title and description
// are removed before sending to linkding, which rejects them with HTTP 400
// ("Null characters are not allowed.").
func TestCreateStripsNUL(t *testing.T) {
	var got struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "token")
	_, err := c.Create(Bookmark{
		URL:         "https://example.com/",
		Title:       "bad\x00 title",
		Description: "desc\x00 with nul",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if strings.Contains(got.Title, "\x00") {
		t.Errorf("title still contains NUL: %q", got.Title)
	}
	if strings.Contains(got.Description, "\x00") {
		t.Errorf("description still contains NUL: %q", got.Description)
	}
	if got.Title != "bad title" {
		t.Errorf("title = %q, want %q", got.Title, "bad title")
	}
}

// TestListBookmarks verifies that ListBookmarks returns id/url/title across
// both active and archived endpoints with pagination.
func TestListBookmarks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		if offset == "0" {
			w.Write([]byte(`{"results":[{"id":7,"url":"https://example.com/","title":"Example"}],"next":"` + r.URL.Path + `?limit=100&offset=100"}`))
		} else {
			w.Write([]byte(`{"results":[],"next":""}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "token")
	got, err := c.ListBookmarks()
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 bookmarks (active + archived), got %d", len(got))
	}
	if got[0].ID != 7 || got[0].URL != "https://example.com/" || got[0].Title != "Example" {
		t.Errorf("bookmark[0] = %+v", got[0])
	}
}

// TestSearchBookmarks verifies server-side q filtering: the query is
// forwarded to both endpoints, and overlapping results across queries are
// deduplicated by id.
func TestSearchBookmarks(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("q"))
		w.Write([]byte(`{"results":[{"id":7,"url":"https://example.com/","title":"t.co link"}],"next":""}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "token")
	got, err := c.SearchBookmarks([]string{"t.co", "bit.ly"})
	if err != nil {
		t.Fatalf("SearchBookmarks: %v", err)
	}

	if len(queries) != 4 {
		t.Fatalf("expected 4 requests (2 queries x 2 endpoints), got %v", queries)
	}
	for _, q := range queries {
		if q != "t.co" && q != "bit.ly" {
			t.Errorf("unexpected q param %q", q)
		}
	}
	if len(got) != 1 || got[0].ID != 7 {
		t.Errorf("expected 1 deduplicated bookmark, got %+v", got)
	}
}

// TestUpdateTitleSendsTitleOnly verifies that UpdateTitle PATCHes just the
// title field (so tags, notes, and dates survive) and truncates overlong
// titles to linkding's 512 code point limit.
func TestUpdateTitleSendsTitleOnly(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Write([]byte(`{"id":7,"title":"ok"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "token")
	longTitle := strings.Repeat("あ", 600)
	if _, err := c.UpdateTitle(7, longTitle); err != nil {
		t.Fatalf("UpdateTitle: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 field in PATCH body, got %v", got)
	}
	title, ok := got["title"].(string)
	if !ok {
		t.Fatalf("title field missing or not a string: %v", got)
	}
	if n := utf8.RuneCountInString(title); n != maxTitleLen {
		t.Errorf("title rune count = %d, want %d", n, maxTitleLen)
	}
}

// TestListBookmarkURLs verifies that ListBookmarkURLs paginates correctly
// across both active and archived endpoints without scraping URLs.
func TestListBookmarkURLs(t *testing.T) {
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		offset := r.URL.Query().Get("offset")
		// Return a single bookmark on the first page of each endpoint,
		// then an empty page to signal termination.
		// offset=0 is the first page (client always sends offset=0 initially).
		if offset == "0" {
			// First page: return one result and a next link.
			// The client only checks whether Next is non-empty; it computes
			// the next offset itself, so a relative path is fine.
			path := r.URL.Path
			u := "https://example.com/" // base URL doesn't matter
			if path == "/api/bookmarks/archived/" {
				u = "https://archived.example/"
			}
			w.Write([]byte(`{"results":[{"url":"` + u + `"}],"next":"` + path + `?limit=100&offset=100"}`))
		} else {
			// Second page: empty.
			w.Write([]byte(`{"results":[],"next":""}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "token")
	got, err := c.ListBookmarkURLs()
	if err != nil {
		t.Fatalf("ListBookmarkURLs: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("expected 2 URLs (active + archived), got %d", len(got))
	}
	if !got["https://example.com/"] {
		t.Error("missing active bookmark URL")
	}
	if !got["https://archived.example/"] {
		t.Error("missing archived bookmark URL")
	}
	if pages != 4 {
		// 2 endpoints × 2 pages each = 4
		t.Errorf("expected 4 HTTP requests, got %d", pages)
	}
}
// serialized in the RFC3339 format linkding accepts.
func TestBookmarkMarshalSetDates(t *testing.T) {
	b := Bookmark{
		URL:          "https://example.com/",
		Title:        "Example",
		DateAdded:    "2024-01-02T03:04:05Z",
		DateModified: "2024-01-02T03:04:05Z",
	}

	body, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := string(body)

	for _, want := range []string{`"date_added":"2024-01-02T03:04:05Z"`, `"date_modified":"2024-01-02T03:04:05Z"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
}
