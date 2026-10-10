package preview

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.0.0.5", "172.16.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0", "::1", "fe80::1", "fc00::1"}
	for _, s := range blocked {
		ip := parseIP(t, s)
		if !isBlockedIP(ip) {
			t.Errorf("expected %s to be blocked", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"}
	for _, s := range allowed {
		ip := parseIP(t, s)
		if isBlockedIP(ip) {
			t.Errorf("expected %s to be allowed", s)
		}
	}
}

func parseIP(t *testing.T, s string) (ip net.IP) {
	t.Helper()
	ip = net.ParseIP(s)
	if ip == nil {
		t.Fatalf("invalid test IP: %s", s)
	}
	return ip
}

func TestValidateTarget(t *testing.T) {
	ctx := context.Background()
	blocked := []string{
		"http://127.0.0.1/",
		"http://127.0.0.1:80/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://[::1]/",
		"javascript:alert(1)",
		"ftp://example.com/file",
		"file:///etc/passwd",
		"http://8.8.8.8:6379/", // port other than 80/443
		"https://8.8.8.8:8080/",
		"http:///path", // no host
	}
	for _, raw := range blocked {
		if err := validateTarget(ctx, raw); err == nil {
			t.Errorf("validateTarget(%q) expected error, got nil", raw)
		}
	}

	allowed := []string{
		"https://example.com/",
		"http://example.com/path?q=1",
		"https://8.8.8.8/",
		"http://8.8.8.8/",
	}
	for _, raw := range allowed {
		if err := validateTarget(ctx, raw); err != nil {
			t.Errorf("validateTarget(%q) unexpected error: %v", raw, err)
		}
	}
}

func TestFetchRejectsInternalTarget(t *testing.T) {
	f := NewFetcher()
	_, err := f.Fetch(context.Background(), "http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected SSRF target to be rejected")
	}
	if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "port") && !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}
