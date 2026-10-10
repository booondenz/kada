package service

import (
	"strings"
	"testing"
)

func TestVerificationCode(t *testing.T) {
	c1 := verificationCode(1, 42, "example.com")
	c2 := verificationCode(1, 42, "example.com")
	if c1 != c2 {
		t.Error("verificationCode should be deterministic")
	}
	if len(c1) != 16 {
		t.Errorf("expected 16 hex chars, got %q", c1)
	}
	// different domains/users should produce different verification codes
	if c1 == verificationCode(2, 42, "example.com") {
		t.Error("different user should produce different code")
	}
	if c1 == verificationCode(1, 43, "example.com") {
		t.Error("different domain id should produce different code")
	}
	if c1 == verificationCode(1, 42, "other.com") {
		t.Error("different name should produce different code")
	}
}

func TestExpectedTXT(t *testing.T) {
	got := expectedTXT(1, 42, "example.com")
	if !strings.HasPrefix(got, "kada-verify=") {
		t.Errorf("expected kada-verify= prefix, got %q", got)
	}
	if len(got) != len("kada-verify=")+16 {
		t.Errorf("unexpected length %d: %q", len(got), got)
	}
}
