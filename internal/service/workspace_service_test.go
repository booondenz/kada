package service

import (
	"testing"
)

func TestSlugPattern(t *testing.T) {
	tests := []struct {
		slug    string
		isValid bool
		reason  string
	}{
		{"my-project", true, "standard slug"},
		{"test123", true, "alphanumeric"},
		{"a-b-c", true, "contains multiple hyphens"},
		{"abc", true, "3 chars (lower bound)"},
		{"a1b", true, "3 chars mixed"},
		{"a", false, "too short (1 char)"},
		{"ab", false, "too short (2 chars)"},
		{"-abc", false, "starts with a hyphen"},
		{"abc-", false, "ends with a hyphen"},
		{"ABC-DEF", false, "contains uppercase letters"},
		{"abc_def", false, "contains underscore"},
		{"abc def", false, "contains space"},
		{"abc.def", false, "contains period"},
		{"", false, "empty string"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			result := slugPattern.MatchString(tt.slug)
			if result != tt.isValid {
				t.Errorf("slugPattern.MatchString(%q) = %v, want %v (%s)",
					tt.slug, result, tt.isValid, tt.reason)
			}
		})
	}
}

func TestSlugValidExamples(t *testing.T) {
	validSlugs := []string{
		"my-project",
		"personal",
		"team-alpha",
		"project-2024",
		"client-work",
		"a1b2c3",
		"dev",
	}

	for _, slug := range validSlugs {
		if !slugPattern.MatchString(slug) {
			t.Errorf("expected %q to be valid slug", slug)
		}
	}
}

func TestSlugInvalidExamples(t *testing.T) {
	invalidSlugs := []string{
		"",
		"ab",
		"-start",
		"end-",
		"UPPERCASE",
		"has space",
		"special!char",
	}

	for _, slug := range invalidSlugs {
		if slugPattern.MatchString(slug) {
			t.Errorf("expected %q to be invalid slug", slug)
		}
	}
}
