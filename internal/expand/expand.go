package expand

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var DefaultHosts = []string{"t.co"}

var urlRe = regexp.MustCompile(`https?://[^\s<>"'\]）」』】〉》！？。、，；：…・’”]+`)

var tcoRe = regexp.MustCompile(`(?i)https?://(?:www\.)?t\.co/(?-i:[A-Za-z0-9]+)`)

const trimCutset = ".,!?;:'\")]}…。、，．！？；：）」』】〉》’”・"

const maxExpansions = 5

type Expander struct {
	client *http.Client
	hosts  map[string]bool
}

func New(hosts []string, timeout time.Duration) *Expander {
	if hosts == nil {
		hosts = DefaultHosts
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		h = strings.TrimPrefix(h, "www.")
		if h != "" {
			m[h] = true
		}
	}
	return &Expander{
		client: &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		},
		hosts: m,
	}
}

func (e *Expander) Title(title string) string {
	if e == nil || title == "" || !strings.Contains(strings.ToLower(title), "http") {
		return title
	}
	done := 0
	for _, short := range e.Candidates(title) {
		if done >= maxExpansions {
			break
		}
		done++
		if dest := e.expand(short); dest != "" && dest != short {
			title = strings.ReplaceAll(title, short, dest)
		}
	}
	return title
}

func (e *Expander) Candidates(title string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if e.hosts["t.co"] {
		for _, m := range tcoRe.FindAllString(title, -1) {
			add(m)
		}
	}
	for _, m := range urlRe.FindAllString(title, -1) {
		short := strings.TrimRight(m, trimCutset)
		if short == "" || seen[short] {
			continue
		}
		u, err := url.Parse(short)
		if err != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		host = strings.TrimPrefix(host, "www.")
		if host == "" {
			continue
		}
		if host == "t.co" || strings.HasSuffix(host, ".t.co") {
			continue
		}
		if !e.isShortHost(host) {
			continue
		}
		add(short)
	}
	return out
}

func (e *Expander) isShortHost(host string) bool {
	if e.hosts[host] {
		return true
	}
	for h := range e.hosts {
		if strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func (e *Expander) expand(raw string) string {
	resp, err := e.client.Get(raw)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	loc := resp.Header.Get("Location")
	if loc == "" {
		return ""
	}
	u, err := url.Parse(loc)
	if err != nil {
		return ""
	}
	if !u.IsAbs() && resp.Request != nil && resp.Request.URL != nil {
		u = resp.Request.URL.ResolveReference(u)
	}
	return u.String()
}
