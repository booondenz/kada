package blocklist

import (
	"testing"

	"github.com/booondenz/kada/internal/domain/entity"
)

func listOf(rows ...entity.BlockedHost) *List {
	l := New(nil)
	l.store(rows)
	return l
}

func TestBlockedMatchesTheHostAndEverySubdomainOfIt(t *testing.T) {
	l := listOf(
		entity.BlockedHost{Host: "evil.example", Category: "gambling"},
		entity.BlockedHost{Host: "casino.test", Category: "gambling"},
	)

	blocked := []string{
		"https://evil.example/",
		"https://evil.example/a/b?c=d",
		"https://www.evil.example/",
		"https://a.b.c.evil.example/",
		"https://EVIL.EXAMPLE/",
		"https://evil.example./", // a trailing dot is the same host
		"http://casino.test:8080/room",
	}
	for _, raw := range blocked {
		if category, ok := l.Blocked(raw); !ok || category != "gambling" {
			t.Errorf("Blocked(%q) = %q,%v, want gambling,true", raw, category, ok)
		}
	}

	allowed := []string{
		"https://example.com/",
		"https://notevil.example/",  // a suffix in the middle of a label is not a subdomain
		"https://evil.example.net/", // a different TLD
		"https://example.org/?next=evil.example",
	}
	for _, raw := range allowed {
		if category, ok := l.Blocked(raw); ok {
			t.Errorf("Blocked(%q) = %q,true, want false", raw, category)
		}
	}
}

// A literal address is matched as an address, not walked label by label: 1.2.3.4 must not block 2.3.4.
func TestBlockedMatchesALiteralAddressExactly(t *testing.T) {
	l := listOf(entity.BlockedHost{Host: "1.2.3.4", Category: "malware"})

	if _, ok := l.Blocked("http://1.2.3.4/payload"); !ok {
		t.Error("expected 1.2.3.4 to be blocked")
	}
	if _, ok := l.Blocked("http://11.2.3.4/"); ok {
		t.Error("an address that merely ends in 1.2.3.4 must not be blocked")
	}
}

func TestBlockedIsEmptyUntilSomethingIsLoaded(t *testing.T) {
	l := New(nil)

	if _, ok := l.Blocked("https://evil.example/"); ok {
		t.Error("an empty list must block nothing")
	}
	if _, ok := l.Blocked("not a url"); ok {
		t.Error("an unparseable target is refused by the scheme check, not here")
	}
}

func TestStoreNormalisesTheRows(t *testing.T) {
	l := listOf(
		entity.BlockedHost{Host: "  Evil.Example.  ", Category: ""},
		entity.BlockedHost{Host: "", Category: "gambling"},
	)

	category, ok := l.Blocked("https://www.evil.example/")
	if !ok || category != "custom" {
		t.Errorf("Blocked = %q,%v, want custom,true", category, ok)
	}
}
