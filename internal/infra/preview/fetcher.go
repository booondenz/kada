package preview

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/chun/kada-backend/internal/domain"
)

// blockedCIDRs lists private/reserved address ranges: blocks SSRF access to localhost, cloud metadata, and internal services
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

// isBlockedIP reports whether the IP falls in a private/reserved address range
func isBlockedIP(ip net.IP) bool {
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// validateHost validates a host: a literal IP is checked against the address ranges; a domain is resolved
// and every resulting address is checked (first line of defense against DNS rebinding)
func validateHost(ctx context.Context, host string) error {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("host %s is a private/reserved address, access blocked", host)
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("domain %s did not resolve to any address", host)
	}
	for _, a := range ips {
		if isBlockedIP(a.IP) {
			return fmt.Errorf("domain %s resolved to private address %s, access blocked", host, a.IP)
		}
	}
	return nil
}

// Fetcher fetches Open Graph / Twitter Card metadata from a target URL
type Fetcher struct {
	client  *http.Client
	timeout time.Duration
}

// NewFetcher creates a metadata fetcher (with SSRF protection: http/https only, ports 80/443 only,
// private-address blocklist, and re-validation at dial time)
func NewFetcher() *Fetcher {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// Validate again before dialing (final line of defense against DNS rebinding)
			if err := validateHost(ctx, host); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &Fetcher{
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				// Validate every redirect target the same way
				return validateTarget(req.Context(), req.URL.String())
			},
		},
		timeout: 5 * time.Second,
	}
}

// validateTarget validates a fetch target: scheme, port, and host address range
func validateTarget(ctx context.Context, rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch strings.ToLower(parsedURL.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported scheme %q, only http/https allowed", parsedURL.Scheme)
	}

	host := parsedURL.Hostname()
	if host == "" {
		return fmt.Errorf("URL is missing a hostname")
	}

	port := parsedURL.Port()
	if port == "" {
		if strings.EqualFold(parsedURL.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	if port != "80" && port != "443" {
		return fmt.Errorf("only ports 80/443 are allowed, got %s", port)
	}

	return validateHost(ctx, host)
}

// Fetch retrieves metadata for a URL
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*domain.LinkPreview, error) {
	rawURL = strings.TrimSpace(rawURL)

	// Prepend https when the scheme is missing (e.g. "example.com/path")
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Scheme == "" {
		rawURL = "https://" + rawURL
	}

	if err := validateTarget(ctx, rawURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Kada-LinkPreview/1.0 (compatible; +https://kada.click)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("URL returned status %d", resp.StatusCode)
	}

	// Parse only the first 1MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	meta := parseHTML(strings.NewReader(string(body)))

	// Fallback: use the host as the title
	parsedURL, _ := url.Parse(rawURL)
	if meta.Title == "" {
		meta.Title = parsedURL.Host
	}

	// Favicon
	meta.FaviconURL = fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=32", parsedURL.Host)

	return &meta, nil
}

// parseHTML parses OG / Twitter Card tags from HTML
func parseHTML(r io.Reader) domain.LinkPreview {
	var meta domain.LinkPreview
	z := html.NewTokenizer(r)

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}

		tagName, hasAttr := z.TagName()
		if string(tagName) != "meta" || !hasAttr {
			continue
		}

		var prop, content string
		for {
			key, val, more := z.TagAttr()
			k := string(key)
			v := string(val)
			if k == "property" || k == "name" {
				prop = strings.ToLower(v)
			}
			if k == "content" {
				content = v
			}
			if !more {
				break
			}
		}

		if prop == "" || content == "" {
			continue
		}

		switch prop {
		case "og:title", "twitter:title":
			if meta.Title == "" {
				meta.Title = content
			}
		case "og:description", "twitter:description":
			if meta.Description == "" {
				meta.Description = content
			}
		case "og:image", "twitter:image":
			if meta.ImageURL == "" {
				meta.ImageURL = content
			}
		}
	}

	// If there are no OG tags, the caller falls back to the host as the title
	return meta
}
