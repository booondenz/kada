// Package urlcheck provides target URL safety validation.
// Short-link target URLs are persisted and executed in redirect/interstitial pages (window.location.href),
// so they must be restricted to the http/https schemes to prevent stored XSS via javascript:/data: and similar schemes.
package urlcheck

import (
	"net/url"
	"strings"
)

// IsSafeTarget reports whether the target URL is an absolute http/https URL that is safe to redirect to.
func IsSafeTarget(raw string) bool {
	if raw == "" || len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	default:
		return false
	}
}
