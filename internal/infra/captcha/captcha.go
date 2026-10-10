// Package captcha generates the graphical challenge that gates SMS sending.
//
// It is deliberately dependency-free: the alternative (base64Captcha and friends) pulls in an image
// library and a font file, and this service only needs to tell a human from a script. The output is an
// SVG, which the browser renders natively - no PNG encoder, no font availability problem on a slim
// container, and the whole image is a few hundred bytes.
//
// The SVG carries no script and no external reference, so serving it as a data URI is safe: the markup
// is generated here from a fixed template, and only the per-character transform numbers and colors are
// randomized.
package captcha

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"
)

// CodeLength is the number of characters in a challenge.
const CodeLength = 4

// alphabet is chosen so that no two glyphs in it can be mistaken for each other once the image is
// rotated and speckled. At most one member of each commonly confused pair is present:
//
//	0/O  - both dropped       1/I/L - only L kept      2/Z - both dropped
//	5/S  - only 5 kept        6/G   - only 6 kept      8/B - only 8 kept
//
// A code the user reads correctly but types wrongly is a support ticket, and on a phone screen at 40px
// that is the common case, not the edge case. It leaves 26 symbols, so a 4-character code has ~457k
// combinations - far more than the per-IP and per-phone send limits allow an attacker to try.
const alphabet = "346789ACDEFHJKLMNPQRTUVWXY"

// ViewBox is the drawing area; the <svg> carries the same width/height so a plain <img> renders it at
// this size without CSS.
const (
	width  = 120
	height = 40
)

// Code returns a random challenge, drawn uniformly from the alphabet.
func Code() (string, error) {
	var b strings.Builder
	b.Grow(CodeLength)
	for i := 0; i < CodeLength; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("captcha: random source failed: %w", err)
		}
		b.WriteByte(alphabet[n.Int64()])
	}
	return b.String(), nil
}

// Equal reports whether two codes match, ignoring case and surrounding space.
//
// It compares in constant time: a byte-by-byte compare that returns early leaks how many leading
// characters a guess got right, which is exactly the signal a solver needs. Comparing the upper-cased
// forms is what makes the answer forgiving to type.
func Equal(a, b string) bool {
	x := strings.ToUpper(strings.TrimSpace(a))
	y := strings.ToUpper(strings.TrimSpace(b))
	return subtle.ConstantTimeCompare([]byte(x), []byte(y)) == 1
}

// Normalize is the canonical form that is hashed and stored, so " abcd " and "ABCD" are one answer.
func Normalize(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// DataURI renders the challenge as an SVG and returns it as a `data:` URI, ready for an <img src>.
//
// The distortion is what makes the code hard to read mechanically: per-character rotation and vertical
// offset, a wave of noise lines through the middle, and scattered dots. It is not meant to defeat a
// determined OCR pipeline - it is meant to make each guess cost a real request, which the send limits
// then bound.
func DataURI(code string) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(SVG(code)))
}

// SVG returns the raw markup, which is what the tests assert on.
func SVG(code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="verification code">`,
		width, height, width, height)
	// Opaque light background: a transparent SVG on a dark theme would put dark glyphs on a dark card.
	b.WriteString(`<rect width="100%" height="100%" fill="#f3f4f6"/>`)

	// Noise lines first, so the glyphs are drawn on top of them rather than hidden behind them.
	for i := 0; i < 4; i++ {
		fmt.Fprintf(&b, `<path d="M%d %d Q%d %d %d %d" stroke="%s" stroke-width="1.4" fill="none" opacity="0.7"/>`,
			randInt(0, 10), randInt(6, height-6),
			randInt(30, width-30), randInt(2, height-2),
			randInt(width-10, width), randInt(6, height-6),
			noiseColor())
	}

	for i := 0; i < 24; i++ {
		fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="1" fill="%s" opacity="0.55"/>`,
			randInt(2, width-2), randInt(2, height-2), noiseColor())
	}

	// Glyphs. A monospace family keeps the character cell predictable, so the rotation cannot push one
	// glyph into the next; the fallbacks cover a slim container without the font installed.
	step := width / (CodeLength + 1)
	for i, r := range code {
		x := step*(i+1) - 6
		y := height/2 + randInt(-4, 4) + 8
		fmt.Fprintf(&b,
			`<text x="%d" y="%d" font-family="ui-monospace,Menlo,Consolas,monospace" font-size="26" font-weight="700" fill="%s" transform="rotate(%d %d %d)">%s</text>`,
			x, y, glyphColor(), randInt(-22, 22), x, y, escape(r))
	}

	b.WriteString(`</svg>`)
	return b.String()
}

// escape keeps the markup well-formed even if a caller passes something the alphabet would not produce.
func escape(r rune) string {
	switch r {
	case '&':
		return "&amp;"
	case '<':
		return "&lt;"
	case '>':
		return "&gt;"
	case '"':
		return "&quot;"
	}
	return string(r)
}

// randInt returns a random integer in [min,max); a failure of the random source falls back to min, which
// only costs a little distortion - never correctness.
func randInt(min, max int) int {
	if max <= min {
		return min
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	if err != nil {
		return min
	}
	return min + int(n.Int64())
}

// glyphColor picks a dark, saturated ink so the code stays legible on the light background.
func glyphColor() string {
	palette := [...]string{"#1e3a8a", "#312e81", "#164e63", "#3b0764", "#7c2d12"}
	return palette[randInt(0, len(palette))]
}

// noiseColor picks a mid tone: visible enough to interfere, too light to be mistaken for a glyph.
func noiseColor() string {
	palette := [...]string{"#9ca3af", "#cbd5e1", "#a8a29e", "#b6c2d1"}
	return palette[randInt(0, len(palette))]
}
