package expand

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/short", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final?x=1", http.StatusFound)
	})
	mux.HandleFunc("/chain", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mid", http.StatusFound)
	})
	mux.HandleFunc("/mid", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final?x=1", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(mux)
	host := strings.TrimPrefix(srv.URL, "http://")
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return srv, host
}

func TestTitleExpandsShortLink(t *testing.T) {
	srv, host := testServer(t)
	defer srv.Close()
	e := New([]string{host}, 5*time.Second)
	got := e.Title("read this " + srv.URL + "/short plz")
	want := "read this " + srv.URL + "/final?x=1 plz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTitleFollowsSingleHopOnly(t *testing.T) {
	srv, host := testServer(t)
	defer srv.Close()
	e := New([]string{host}, 5*time.Second)
	got := e.Title("read this " + srv.URL + "/chain plz")
	want := "read this " + srv.URL + "/mid plz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultHostsIsTcoOnly(t *testing.T) {
	if len(DefaultHosts) != 1 || DefaultHosts[0] != "t.co" {
		t.Errorf("DefaultHosts = %v, want [t.co]", DefaultHosts)
	}
}

func TestTitleKeepsTrailingPunctuation(t *testing.T) {
	srv, host := testServer(t)
	defer srv.Close()
	e := New([]string{host}, 5*time.Second)
	got := e.Title("see " + srv.URL + "/short.")
	want := "see " + srv.URL + "/final?x=1."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTitleSkipsUnlistedHost(t *testing.T) {
	e := New([]string{"t.co"}, 5*time.Second)
	in := "visit https://example.com/page?a=1"
	if got := e.Title(in); got != in {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestTitleNoChangeWithoutRedirect(t *testing.T) {
	srv, host := testServer(t)
	defer srv.Close()
	e := New([]string{host}, 5*time.Second)
	in := "open " + srv.URL + "/final"
	if got := e.Title(in); got != in {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestCandidatesTcoStrictCode(t *testing.T) {
	e := New([]string{"t.co"}, 5*time.Second)
	got := e.Candidates("見る https://t.co/abc123が話題、次 https://t.co/xyz。")
	want := []string{"https://t.co/abc123", "https://t.co/xyz"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCandidatesGenericFullWidthPunctuation(t *testing.T) {
	e := New([]string{"bit.ly"}, 5*time.Second)
	got := e.Candidates("参照 https://bit.ly/xyz。")
	if len(got) != 1 || got[0] != "https://bit.ly/xyz" {
		t.Errorf("got %v, want [https://bit.ly/xyz]", got)
	}
}

func TestTitleHandlesBadInput(t *testing.T) {
	e := New([]string{"t.co"}, 5*time.Second)
	for _, in := range []string{"", "no urls here", "visit https://example.com/a"} {
		if got := e.Title(in); got != in {
			t.Errorf("Title(%q) = %q, want unchanged", in, got)
		}
	}
	var nilExpander *Expander
	if got := nilExpander.Title("x https://example.com/a"); got != "x https://example.com/a" {
		t.Errorf("nil expander changed title: %q", got)
	}
}
