// Package urlcheck provides target URL safety validation.
//
// A short link's target is persisted and then executed: a redirect sends the visitor there, and the guide
// page assigns it to window.location. It is therefore restricted to the http and https schemes, which is
// what prevents stored XSS through javascript:, data: and the like.
//
// This package is also the one place that decides a target points somewhere the visitor must not be sent.
// An address in a private or reserved range is refused here because a short link aimed at 192.168.1.1 asks
// the visitor's own browser to open their router, a printer or an internal panel, on behalf of whoever
// wrote the link - the visitor's network does the fetching, so the request never passes through us at all.
package urlcheck

import (
	"net"
	"net/url"
	"strings"
)

// blockedCIDRs lists the private and reserved address ranges that a target may not point into.
//
// It is deliberately wider than RFC 1918: the point is "not reachable on the public internet", which also
// covers carrier-grade NAT, the TEST-NET documentation ranges, benchmarking space, multicast, and the
// IPv6 equivalents. 169.254.169.254 is the cloud metadata endpoint and is why link-local is in the list.
var blockedCIDRs = []string{
	"0.0.0.0/8",       // this network
	"10.0.0.0/8",      // RFC1918 private
	"100.64.0.0/10",   // CGNAT
	"127.0.0.0/8",     // loopback
	"169.254.0.0/16",  // link-local (includes cloud metadata 169.254.169.254)
	"172.16.0.0/12",   // RFC1918 private
	"192.0.0.0/24",    // IETF reserved
	"192.0.2.0/24",    // TEST-NET-1
	"192.168.0.0/16",  // RFC1918 private
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved
	"::/128",          // unspecified
	"::1/128",         // loopback v6
	"fc00::/7",        // ULA
	"fe80::/10",       // link-local v6
	"ff00::/8",        // multicast v6
}

var blockedNets = func() []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(blockedCIDRs))
	for _, cidr := range blockedCIDRs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		nets = append(nets, n)
	}
	return nets
}()

// IsBlockedIP reports whether the address falls in a private or reserved range.
func IsBlockedIP(ip net.IP) bool {
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IsInternalHost reports whether a host names or addresses something inside the visitor's own network.
//
// Only the literal host is examined; a domain that merely resolves to a private address (a
// 127.0.0.1.nip.io and its many cousins) passes, because resolving every target at creation time would make
// the create path depend on a DNS lookup that can fail, be slow, or answer differently later. The preview
// fetcher, which fetches the address itself, does resolve and checks every answer.
func IsInternalHost(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if h == "" {
		return true
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return IsBlockedIP(ip)
	}
	return false
}

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
		return u.Host != "" && !IsInternalHost(u.Hostname())
	default:
		return false
	}
}
