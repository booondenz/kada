package preview

import (
	"context"
	"strings"
	"testing"
)

// The address ranges themselves are urlcheck.IsBlockedIP's business, and are tested there. What is left to
// this package is that it asks before it dials.
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
