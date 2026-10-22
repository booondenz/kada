package urlcheck

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.0.0.5", "172.16.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0", "::1", "fe80::1", "fc00::1"}
	for _, s := range blocked {
		if !IsBlockedIP(net.ParseIP(s)) {
			t.Errorf("expected %s to be blocked", s)
		}
	}

	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"}
	for _, s := range allowed {
		if IsBlockedIP(net.ParseIP(s)) {
			t.Errorf("expected %s to be allowed", s)
		}
	}
}

func TestIsInternalHost(t *testing.T) {
	internal := []string{
		"localhost", "LOCALHOST", "app.localhost", "localhost.",
		"127.0.0.1", "10.1.2.3", "192.168.1.1", "169.254.169.254",
		"::1", "fe80::1", "fc00::1",
	}
	for _, h := range internal {
		if !IsInternalHost(h) {
			t.Errorf("expected %q to be internal", h)
		}
	}

	public := []string{"example.com", "www.baidu.com", "8.8.8.8", "kada.click", "notlocalhost.com"}
	for _, h := range public {
		if IsInternalHost(h) {
			t.Errorf("expected %q to be public", h)
		}
	}
}

// A short link aimed at the visitor's own network sends their browser at their router or an internal panel,
// so the address ranges are refused where the target is first accepted - and, through the same function, at
// redirect time for links that were created before the check existed.
func TestIsSafeTargetRefusesInternalAddresses(t *testing.T) {
	tests := []string{
		"http://localhost:8080/admin",
		"http://127.0.0.1/",
		"http://[::1]/",
		"http://192.168.1.1/",
		"http://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/",
	}
	for _, raw := range tests {
		if IsSafeTarget(raw) {
			t.Errorf("IsSafeTarget(%q) = true, want false", raw)
		}
	}
}

func TestIsSafeTarget(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"https://example.com", true},
		{"http://example.com/path?q=1#frag", true},
		{"HTTPS://EXAMPLE.COM", true},
		{"javascript:alert(1)", false},
		{"JAVASCRIPT:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"vbscript:msgbox(1)", false},
		{"file:///etc/passwd", false},
		{"//example.com/path", false}, // protocol-relative, no scheme
		{"ftp://example.com", false},
		{"", false},
		{"not a url", false},
		{"https://", false}, // no host
	}
	for _, tt := range tests {
		if got := IsSafeTarget(tt.raw); got != tt.want {
			t.Errorf("IsSafeTarget(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
