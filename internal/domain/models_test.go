package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// A link's password hash must never leave the database. LinkInfo is encoded into the Redis cache by
// CacheService.SetLink and into API responses, so a field able to hold the hash would put it in both.
// The account model already guards itself this way (TestUserHidesPasswordHash).
func TestLinkInfoKeepsThePasswordHashOutOfThePayload(t *testing.T) {
	encoded, err := json.Marshal(LinkInfo{ID: 1, ShortCode: "abc123", HasPassword: true})
	if err != nil {
		t.Fatalf("marshal LinkInfo: %v", err)
	}

	payload := string(encoded)
	if strings.Contains(payload, "password_hash") {
		t.Errorf("LinkInfo must not carry the password hash, got: %s", payload)
	}
	if !strings.Contains(payload, `"has_password":true`) {
		t.Errorf("LinkInfo must report whether a password is set, got: %s", payload)
	}
}

// The cache stores LinkInfo as JSON, so the flag that decides whether a visitor is shown the password
// page has to survive the round trip: a field that came back false would quietly unprotect every link
// until its TTL expired.
func TestLinkInfoPasswordFlagSurvivesTheCacheRoundTrip(t *testing.T) {
	encoded, err := json.Marshal(LinkInfo{ShortCode: "abc123", HasPassword: true})
	if err != nil {
		t.Fatalf("marshal LinkInfo: %v", err)
	}

	var decoded LinkInfo
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal LinkInfo: %v", err)
	}
	if !decoded.HasPassword {
		t.Error("HasPassword must survive the JSON round trip the cache performs")
	}
}
