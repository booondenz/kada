package captcha

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCodeShape(t *testing.T) {
	for i := 0; i < 200; i++ {
		code, err := Code()
		if err != nil {
			t.Fatalf("Code() failed: %v", err)
		}
		if len([]rune(code)) != CodeLength {
			t.Fatalf("code %q has %d characters, want %d", code, len([]rune(code)), CodeLength)
		}
		for _, r := range code {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("code %q contains %q, which is not in the alphabet", code, r)
			}
		}
	}
}

// confusablePairs are glyph pairs that a rotated, speckled 26px rendering makes hard to tell apart. The
// alphabet may contain at most one member of each pair: if both were present, a user who read the code
// correctly would still be rejected for typing the other one, which looks like a broken captcha.
var confusablePairs = [][2]rune{
	{'0', 'O'}, {'1', 'I'}, {'1', 'L'}, {'2', 'Z'}, {'5', 'S'}, {'6', 'G'}, {'8', 'B'},
}

func TestAlphabetHasAtMostOneOfEachConfusablePair(t *testing.T) {
	for _, pair := range confusablePairs {
		first := strings.ContainsRune(alphabet, pair[0])
		second := strings.ContainsRune(alphabet, pair[1])
		if first && second {
			t.Errorf("the alphabet contains both %q and %q, which a distorted glyph makes indistinguishable",
				pair[0], pair[1])
		}
	}
}

// Guards the other direction: an alphabet that accidentally lost most of its characters would still pass
// the pair test, and a 3-symbol alphabet is not a challenge.
func TestAlphabetIsLargeEnough(t *testing.T) {
	if n := len([]rune(alphabet)); n < 20 {
		t.Errorf("the alphabet has %d symbols; a challenge needs enough entropy to be worth solving", n)
	}
}

func TestAlphabetHasNoDuplicates(t *testing.T) {
	seen := make(map[rune]bool)
	for _, r := range alphabet {
		if seen[r] {
			t.Errorf("the alphabet contains %q twice", r)
		}
		seen[r] = true
	}
}

func TestCodeIsNotConstant(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		code, err := Code()
		if err != nil {
			t.Fatalf("Code() failed: %v", err)
		}
		seen[code] = true
	}
	if len(seen) < 40 {
		t.Errorf("50 codes produced only %d distinct values; the source does not look random", len(seen))
	}
}

func TestEqualIgnoresCaseAndSpace(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"ABCD", "ABCD", true},
		{"abcd", "ABCD", true},
		{" ABCD ", "abcd", true},
		{"ABCD", "ABCE", false},
		{"ABCD", "ABC", false},
		{"", "", true},
		{"ABCD", "", false},
	}
	for _, c := range cases {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("Equal(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize(" ab3d "); got != "AB3D" {
		t.Errorf("Normalize(\" ab3d \") = %q, want %q", got, "AB3D")
	}
}

func TestSVGIsWellFormedAndCarriesEveryGlyph(t *testing.T) {
	const code = "A3B7"
	svg := SVG(code)

	if !strings.HasPrefix(svg, "<svg ") {
		t.Errorf("SVG does not start with an <svg> element: %.60s", svg)
	}
	if !strings.HasSuffix(svg, "</svg>") {
		t.Error("SVG does not end with </svg>")
	}
	// Exactly one <text> per character: a glyph silently dropped would make the challenge unanswerable.
	if n := strings.Count(svg, "<text "); n != CodeLength {
		t.Errorf("SVG contains %d <text> elements, want %d", n, CodeLength)
	}
	for _, r := range code {
		if !strings.Contains(svg, ">"+string(r)+"</text>") {
			t.Errorf("SVG does not render the glyph %q", r)
		}
	}
	// An opaque background matters: the sign-in card is dark, and a transparent image would put dark
	// glyphs on a dark surface.
	if !strings.Contains(svg, `fill="#f3f4f6"`) {
		t.Error("SVG has no opaque light background")
	}
	// No script, no external reference: a data URI is only safe to serve because the markup is inert.
	for _, forbidden := range []string{"<script", "javascript:", "<image", "xlink:href"} {
		if strings.Contains(strings.ToLower(svg), forbidden) {
			t.Errorf("SVG contains %q, which must never appear", forbidden)
		}
	}
}

func TestSVGDistortsEachGlyph(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		seen[SVG("A3B7")] = true
	}
	if len(seen) < 2 {
		t.Error("20 renderings of the same code are identical; the distortion is not applied")
	}
}

func TestDataURIIsDecodableBase64(t *testing.T) {
	uri := DataURI("A3B7")
	const prefix = "data:image/svg+xml;base64,"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("DataURI does not start with %q", prefix)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		t.Fatalf("the payload is not valid base64: %v", err)
	}
	if !strings.Contains(string(raw), "<svg") {
		t.Error("the decoded payload is not an SVG document")
	}
}
