package service

import (
	"strings"
	"testing"
)

func TestGenerateShortCode(t *testing.T) {
	// generate 100 short codes and make sure they are all 12 hex digits and unique
	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code := generateShortCode()
		if len(code) != 12 {
			t.Errorf("expected short code length 12, got %d: %s", len(code), code)
		}
		// check that it contains only hex characters
		for _, c := range code {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Errorf("invalid character in short code: %c in %s", c, code)
			}
		}
		if codes[code] {
			t.Errorf("duplicate short code generated: %s", code)
		}
		codes[code] = true
	}
}

func TestShortCodePattern(t *testing.T) {
	tests := []struct {
		code    string
		isValid bool
		reason  string
	}{
		{"abc123", true, "alphanumeric"},
		{"my-link", true, "contains hyphen"},
		{"my_link", true, "contains underscore"},
		{"a1b2c3d4", true, "8 chars alphanumeric"},
		{"AbC123", true, "mixed case"},
		{"a1b2c3d4e5f6g7h8i9j0", true, "20 chars (upper bound)"},
		{"abc", false, "too short (3 chars)"},
		{"a", false, "too short (1 char)"},
		{"ab", false, "too short (2 chars)"},
		{"abcdefghijklmnopqrstu", false, "too long (21 chars)"},
		{"abc def", false, "contains space"},
		{"abc@def", false, "contains special character @"},
		{"abc.def", false, "contains period"},
		{"中文测试", false, "contains Chinese characters"},
		{"abc/def", false, "contains slash"},
		{"", false, "empty string"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			result := shortCodePattern.MatchString(tt.code)
			if result != tt.isValid {
				t.Errorf("shortCodePattern.MatchString(%q) = %v, want %v (%s)",
					tt.code, result, tt.isValid, tt.reason)
			}
		})
	}
}

func TestEscapeCSV(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello world", "hello world"},
		{"hello,world", `"hello,world"`},
		{`say "hello"`, `"say ""hello"""`},
		{"line1\nline2", "\"line1\nline2\""},
		{"normal text", "normal text"},
		{"", ""},
		// formula injection protection
		{"=1+1", "'=1+1"},
		{"+8613800000000", "'+8613800000000"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"-2+3", "'-2+3"},
		{"=HYPERLINK(\"http://evil\")", `"'=HYPERLINK(""http://evil"")"`},
		// the user-controlled domain field with commas/quotes is wrapped correctly
		{`evil.com,"x`, `"evil.com,""x"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeCSV(tt.input)
			if result != tt.expected {
				t.Errorf("escapeCSV(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildShortURL(t *testing.T) {
	svc := &LinkService{baseURL: "https://kada.click"}
	url := svc.BuildShortURL("kada.click", "abc123")
	expected := "https://kada.click/r/abc123"
	if url != expected {
		t.Errorf("BuildShortURL = %q, want %q", url, expected)
	}

	// custom domain
	url2 := svc.BuildShortURL("custom.domain.com", "xyz789")
	expected2 := "https://custom.domain.com/r/xyz789"
	if url2 != expected2 {
		t.Errorf("BuildShortURL = %q, want %q", url2, expected2)
	}
}

func TestHashPassword(t *testing.T) {
	pwd := "test-password-123"
	hash1 := hashPassword(pwd)
	hash2 := hashPassword(pwd)

	// bcrypt salt: the same password produces different hashes
	if hash1 == "" || hash2 == "" {
		t.Fatal("hashPassword returned empty hash")
	}
	if hash1 == hash2 {
		t.Error("bcrypt hashes should differ due to salt")
	}
	if !strings.HasPrefix(hash1, "$2") {
		t.Errorf("expected bcrypt hash, got %q", hash1)
	}

	// verify
	if !checkPasswordHash(pwd, hash1) {
		t.Error("checkPasswordHash should return true for correct password")
	}
	if checkPasswordHash("wrong-password", hash1) {
		t.Error("checkPasswordHash should return false for wrong password")
	}
}

func TestHashPassword_LongPassword(t *testing.T) {
	// bcrypt caps at 72 bytes; an over-long password should be truncated automatically and still verify
	pwd := strings.Repeat("a", 100)
	hash := hashPassword(pwd)
	if hash == "" {
		t.Fatal("hashPassword returned empty hash for long password")
	}
	if !checkPasswordHash(pwd, hash) {
		t.Error("checkPasswordHash should accept truncated long password")
	}
}

func TestCheckPasswordHash_LegacySHA256(t *testing.T) {
	// legacy data is an unsalted SHA-256 hex digest and should still verify after the upgrade
	legacy := sha256Hex("legacy-pass-123")
	if !checkPasswordHash("legacy-pass-123", legacy) {
		t.Error("legacy SHA-256 hash should still verify")
	}
	if checkPasswordHash("wrong", legacy) {
		t.Error("legacy SHA-256 hash should reject wrong password")
	}
	if checkPasswordHash("legacy-pass-123", "") {
		t.Error("empty hash should never verify")
	}
}
