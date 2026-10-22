package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/booondenz/kada/internal/domain"
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

// The COUNT and the page are the same query up to projection and ordering, and the filters they share are
// written against the `l` alias. A count built on `Model(&entity.Link{})` renders `FROM "links"` with no
// alias, so PostgreSQL answers 42P01 and the list reports a total of zero forever - which it did.
func TestLinkListQueryCountCarriesTheAliasTheFiltersUse(t *testing.T) {
	svc := &LinkService{db: newDryRunDB(t)}

	var total int64
	stmt := svc.linkListQuery(context.Background(), "l.user_id = ?", []any{int64(7)}).
		Count(&total).Statement

	sql := stmt.SQL.String()
	if !strings.Contains(sql, "FROM links AS l") {
		t.Errorf("the count must carry the alias its filters use, got: %s", sql)
	}
	if !strings.Contains(sql, "l.user_id = $1") {
		t.Errorf("the count must keep the shared filter, got: %s", sql)
	}
	if len(stmt.Vars) != 1 || stmt.Vars[0] != int64(7) {
		t.Errorf("the filter value must be bound, got: %v", stmt.Vars)
	}
}

// fakeBlocker is the list as the service sees it: one category, or none.
type fakeBlocker struct{ category string }

func (f fakeBlocker) Blocked(string) (string, bool) { return f.category, f.category != "" }

// The refusal has to come before anything is written, and that is also what makes it testable without a
// database: this service has no db and no cache, so reaching either one would panic instead of failing the
// assertion.
func TestCreateRefusesABlockedTarget(t *testing.T) {
	svc := &LinkService{blocker: fakeBlocker{category: "gambling"}}

	_, err := svc.Create(context.Background(), 1, domain.CreateLinkRequest{OriginalURL: "https://casino.example/room"})

	var blocked *domain.BlockedTargetError
	if !errors.As(err, &blocked) {
		t.Fatalf("Create error = %v, want a BlockedTargetError", err)
	}
	if blocked.Category != "gambling" {
		t.Errorf("category = %q, want gambling", blocked.Category)
	}
}

// An existing link can be pointed somewhere new, so the edit is checked exactly like the creation.
func TestUpdateRefusesABlockedTarget(t *testing.T) {
	svc := &LinkService{blocker: fakeBlocker{category: "phishing"}}
	target := "https://phish.example/login"

	_, err := svc.Update(context.Background(), 1, 1, domain.UpdateLinkRequest{OriginalURL: &target})

	var blocked *domain.BlockedTargetError
	if !errors.As(err, &blocked) {
		t.Fatalf("Update error = %v, want a BlockedTargetError", err)
	}
	if blocked.Category != "phishing" {
		t.Errorf("category = %q, want phishing", blocked.Category)
	}
}

func TestAServiceWithoutABlockerBlocksNothing(t *testing.T) {
	svc := &LinkService{}

	if _, ok := svc.Blocked("https://evil.example/"); ok {
		t.Error("a nil list must block nothing")
	}
}
