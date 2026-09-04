// Package filter applies domain-based exclusion rules.
package filter

import (
	"net/url"
	"strings"
)

// DomainFilter excludes URLs whose host is on a blocklist.
type DomainFilter struct {
	blocked map[string]bool
}

// New creates a DomainFilter from a list of blocked domains.
// Domain matching is suffix-based ("example.com" also matches "sub.example.com").
func New(blockedDomains []string) *DomainFilter {
	f := &DomainFilter{blocked: make(map[string]bool)}
	for _, d := range blockedDomains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		f.blocked[strings.ToLower(d)] = true
	}
	return f
}

// IsBlocked reports whether the given URL's host is on the blocklist.
func (f *DomainFilter) IsBlocked(rawURL string) bool {
	if len(f.blocked) == 0 {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for d := range f.blocked {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}
