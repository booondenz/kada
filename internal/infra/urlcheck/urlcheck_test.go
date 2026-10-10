package urlcheck

import "testing"

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
