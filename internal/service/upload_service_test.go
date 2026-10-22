package service

import "testing"

// The extension decides which type a file claims to be; the first bytes decide whether the claim holds.
func TestContentMatches(t *testing.T) {
	tests := []struct {
		want, sniffed string
		ok            bool
	}{
		{"image/png", "image/png", true},
		{"image/png", "image/jpeg", false},
		{"application/pdf", "application/pdf", true},
		{"application/pdf", "text/html", false},
		// The Office formats are zip containers: the bytes only have to be a zip, the extension says which.
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/zip", true},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "image/png", false},
		{"image/png", "application/octet-stream", false},
	}

	for _, tt := range tests {
		if got := contentMatches(tt.want, tt.sniffed); got != tt.ok {
			t.Errorf("contentMatches(%q, %q) = %v, want %v", tt.want, tt.sniffed, got, tt.ok)
		}
	}
}

// A client-supplied filename is shown to the owner and put in a response header, so it is stripped of the
// things that could end a header value or a path early - but it is not collapsed to an extension.
func TestSafeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"photo.png", "photo.png"},
		{`C:\Users\me\photo.png`, "photo.png"},
		{"../../etc/passwd", "passwd"},
		{"a\r\nb.png", "ab.png"},
		{"   ", "file"},
		{"", "file"},
		{"..", "file"},
	}

	for _, tt := range tests {
		if got := safeName(tt.in); got != tt.want {
			t.Errorf("safeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{512, "512 B"},
		{2048, "2 KB"},
		{20 << 20, "20 MB"},
		{1 << 30, "1 GB"},
	}

	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
