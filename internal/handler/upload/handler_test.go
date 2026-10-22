package upload

import (
	"strings"
	"testing"
)

// An image has to be shown to be saved by hand, a PDF is what the browser's own viewer is for, and an
// Office file cannot be rendered at all - offering it inline would be a blank page.
func TestDisposition(t *testing.T) {
	tests := []struct {
		contentType string
		filename    string
		download    bool
		want        string
	}{
		{"image/png", "photo.png", false, `inline; filename="photo.png"`},
		{"application/pdf", "deck.pdf", false, `inline; filename="deck.pdf"`},
		{"application/pdf", "deck.pdf", true, `attachment; filename="deck.pdf"`},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "book.xlsx", false,
			`attachment; filename="book.xlsx"`},
		// A quote or a newline in a filename would end the header value early.
		{"image/png", `a"b.png`, false, `inline; filename="ab.png"`},
	}

	for _, tt := range tests {
		if got := disposition(tt.contentType, tt.filename, tt.download); got != tt.want {
			t.Errorf("disposition(%q, %q, %v) = %q, want %q", tt.contentType, tt.filename, tt.download, got, tt.want)
		}
		if strings.ContainsAny(disposition(tt.contentType, tt.filename, tt.download), "\r\n") {
			t.Error("the disposition header carries a line break")
		}
	}
}
