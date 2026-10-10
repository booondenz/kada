package config

import "testing"

func TestIsWeakJWTSecret(t *testing.T) {
	weak := []string{"", "kada-dev-secret-change-in-production", "changeme", "secret", "jwt-secret"}
	for _, s := range weak {
		if !IsWeakJWTSecret(s) {
			t.Errorf("expected %q to be weak", s)
		}
	}
	strong := []string{"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6"}
	for _, s := range strong {
		if IsWeakJWTSecret(s) {
			t.Errorf("expected %q to be strong", s)
		}
	}
}
