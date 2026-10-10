package service

import (
	"testing"

	"github.com/chun/kada-backend/internal/domain/entity"
)

// The redirect path used to read the password column again on every request. It now reads this flag
// off the entry it already loaded, so the mapping is what decides whether a visitor sees the password
// page: a nil or empty hash must map to false, and a stored hash to true.
func TestLinkInfoFromEntityReportsPasswordPresence(t *testing.T) {
	empty := ""
	hash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

	tests := []struct {
		name         string
		passwordHash *string
		want         bool
	}{
		{name: "no password set", passwordHash: nil, want: false},
		{name: "empty hash counts as no password", passwordHash: &empty, want: false},
		{name: "a stored hash means the link is protected", passwordHash: &hash, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := entity.Link{
				ShortCode:    "abc123",
				OriginalURL:  "https://example.com/target",
				PasswordHash: tt.passwordHash,
			}
			if got := linkInfoFromEntity(row).HasPassword; got != tt.want {
				t.Errorf("HasPassword = %v, want %v", got, tt.want)
			}
		})
	}
}
