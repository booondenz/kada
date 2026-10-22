// Package blocklist holds the hosts this service refuses to send visitors to.
//
// A short link is a promise that the URL behind it is safe to open, and the URL is chosen by whoever created
// the link. The categories that make that promise false - 色情, 赌博, 毒品, 诈骗, 钓鱼 - are not something a
// code change can recognize, so they are kept in a table that an operator edits, and this package turns that
// table into an answer fast enough for the redirect path.
//
// The answer is served from an in-memory snapshot rather than a query, because the redirect path asks once
// per request and is the hot path of the whole service. A new row is in force from the next Reload - the
// background Run performs it every interval, and a writer that needs the change applied before it answers
// calls Reload itself instead of waiting.
package blocklist

import (
	"context"
	"log"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain/entity"
)

// List answers "is this host blocked" from the snapshot it last loaded.
type List struct {
	db *gorm.DB
	// hosts maps a lowercased host to the category that blocked it. The pointer is swapped whole, so a
	// request either sees the previous list or the next one and never a half-built map.
	hosts atomic.Pointer[map[string]string]
}

// New returns a list that blocks nothing until Reload succeeds.
func New(db *gorm.DB) *List {
	l := &List{db: db}
	l.store(nil)
	return l
}

// Reload reads blocked_hosts and makes the result the list in force.
func (l *List) Reload(ctx context.Context) error {
	if l.db == nil {
		return nil
	}
	var rows []entity.BlockedHost
	if err := l.db.WithContext(ctx).Select("host", "category").Find(&rows).Error; err != nil {
		return err
	}
	l.store(rows)
	return nil
}

// Run reloads the list every interval until ctx is canceled.
func (l *List) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.Reload(ctx); err != nil {
				log.Printf("reload blocked hosts failed: %v", err)
			}
		}
	}
}

// Blocked reports whether targetURL points at a blocked host, and the category that blocked it.
//
// An unparseable URL is not blocked: the scheme check that already runs ahead of this one refuses it for a
// reason it can name, and answering "blocked" here would hide that.
func (l *List) Blocked(targetURL string) (string, bool) {
	hosts := l.hosts.Load()
	if hosts == nil || len(*hosts) == 0 {
		return "", false
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))

	// A literal address can only match the row written for that address: walking its "suffixes" would turn
	// a block on 1.2.3.4 into a block on 2.3.4.
	if net.ParseIP(host) != nil {
		category, ok := (*hosts)[host]
		return category, ok
	}

	for h := host; h != ""; {
		if category, ok := (*hosts)[h]; ok {
			return category, true
		}
		i := strings.IndexByte(h, '.')
		if i < 0 {
			break
		}
		h = h[i+1:]
	}
	return "", false
}

// store replaces the snapshot. Eager rather than lazy because the whole point is that a lookup never does
// work beyond one map access per label.
func (l *List) store(rows []entity.BlockedHost) {
	hosts := make(map[string]string, len(rows))
	for _, r := range rows {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.Host), "."))
		if host == "" {
			continue
		}
		category := strings.TrimSpace(r.Category)
		if category == "" {
			category = "custom"
		}
		hosts[host] = category
	}
	l.hosts.Store(&hosts)
}
