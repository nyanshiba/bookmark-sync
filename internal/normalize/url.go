// Package normalize provides URL canonicalization for duplicate detection.
package normalize

import (
	"net/url"
	"sort"
	"strings"
)

// trackingParams are query parameters that add no content value and vary per visit.
var trackingParams = map[string]bool{
	"utm_source": true, "utm_medium": true, "utm_campaign": true,
	"utm_term": true, "utm_content": true, "utm_id": true,
	"utm_campaignid": true, "utm_adgroupid": true, "utm_adid": true,
	"utm_reader": true, "utm_viz_id": true, "utm_referrer": true,
	"fbclid": true, "gclid": true, "msclkid": true, "mc_cid": true,
	"mc_eid": true, "igshid": true, "ref_src": true, "ref_url": true,
	"source": true, "feature": true,
}

// URL normalizes a raw URL string for canonical identity comparison.
// It removes tracking parameters, sorts remaining query keys,
// strips the default port, lowercases scheme/host, removes trailing slash
// (except for root), and strips a leading "www.".
func URL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}

	// Normalize scheme and host.
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Reject URLs with an empty host or a corrupted hostname
	// (e.g. "rfc..editor.org" from Firefox Sync data corruption).
	host := u.Hostname()
	if host == "" || strings.Contains(host, "..") {
		return ""
	}

	// Remove default ports.
	if strings.HasPrefix(u.Host, host+":") {
		port := strings.TrimPrefix(u.Host[strings.Index(u.Host, ":"):], ":")
		if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
			u.Host = host
		}
	}

	// Strip leading www.
	u.Host = strings.TrimPrefix(u.Host, "www.")

	// Clean query parameters.
	q := u.Query()
	for k := range q {
		if trackingParams[strings.ToLower(k)] {
			q.Del(k)
		}
	}
	if len(q) == 0 {
		u.RawQuery = ""
	} else {
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			for _, v := range q[k] {
				if b.Len() > 0 {
					b.WriteString("&")
				}
				b.WriteString(url.QueryEscape(k))
				b.WriteString("=")
				b.WriteString(url.QueryEscape(v))
			}
		}
		u.RawQuery = b.String()
	}

	// Remove fragment (hash anchors are same-page).
	u.Fragment = ""

	// Remove trailing slash except for root.
	p := u.Path
	if p != "/" && strings.HasSuffix(p, "/") {
		u.Path = strings.TrimSuffix(p, "/")
	}

	return u.String()
}
