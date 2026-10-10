package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
)

// fakeLinkCache is an in-memory LinkCache, so the redirect path can be exercised without Redis.
type fakeLinkCache struct {
	mu      sync.Mutex
	links   map[string]*domain.LinkInfo
	missing map[string]bool
}

func newFakeLinkCache() *fakeLinkCache {
	return &fakeLinkCache{
		links:   make(map[string]*domain.LinkInfo),
		missing: make(map[string]bool),
	}
}

func (f *fakeLinkCache) GetLink(_ context.Context, shortCode string) (*domain.LinkInfo, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.links[shortCode]
	return info, ok
}

func (f *fakeLinkCache) SetLink(_ context.Context, info *domain.LinkInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[info.ShortCode] = info
}

func (f *fakeLinkCache) InvalidateLink(_ context.Context, shortCode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.links, shortCode)
	delete(f.missing, shortCode)
}

func (f *fakeLinkCache) MarkMissing(_ context.Context, shortCode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.missing[shortCode] = true
	delete(f.links, shortCode)
}

func (f *fakeLinkCache) ClearMissing(_ context.Context, shortCode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.missing, shortCode)
}

func (f *fakeLinkCache) IsMissing(_ context.Context, shortCode string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.missing[shortCode]
}

// newMissingLinkDB returns a dry-run handle that counts its queries and answers every one of them with
// "no rows".
//
// A DryRun session never reaches gorm.Scan, which is where First raises gorm.ErrRecordNotFound, so the
// error the negative cache keys off has to be injected here. The count is what the tests read.
func newMissingLinkDB(t *testing.T, delay time.Duration) (*gorm.DB, *int64) {
	t.Helper()

	db := newDryRunDB(t)
	var queries int64

	err := db.Callback().Query().After("gorm:query").Register("test:count_as_missing", func(tx *gorm.DB) {
		atomic.AddInt64(&queries, 1)
		if delay > 0 {
			// Held open so that concurrent callers overlap inside one load.
			time.Sleep(delay)
		}
		// AddError returns the error it stored; there is nothing useful to do with it in a callback, but
		// discarding it silently is what gosec G104 reports.
		_ = tx.AddError(gorm.ErrRecordNotFound)
	})
	if err != nil {
		t.Fatalf("register the query callback: %v", err)
	}

	return db, &queries
}

func TestGetByCodeRemembersAMissingShortCode(t *testing.T) {
	db, queries := newMissingLinkDB(t, 0)
	cache := newFakeLinkCache()
	svc := &LinkService{db: db, cache: cache}
	ctx := context.Background()

	for call := 1; call <= 3; call++ {
		if _, err := svc.GetByCode(ctx, "absent1234"); !errors.Is(err, domain.ErrLinkNotFound) {
			t.Fatalf("call %d returned %v, want ErrLinkNotFound", call, err)
		}
	}

	if got := atomic.LoadInt64(queries); got != 1 {
		t.Errorf("three lookups of a missing code reached the database %d times, want 1", got)
	}
	if !cache.IsMissing(ctx, "absent1234") {
		t.Error("the missing code was not remembered")
	}
}

func TestGetByCodeServesACachedLinkWithoutQuerying(t *testing.T) {
	db, queries := newMissingLinkDB(t, 0)
	cache := newFakeLinkCache()
	ctx := context.Background()
	cache.SetLink(ctx, &domain.LinkInfo{ShortCode: "abc123", OriginalURL: "https://example.com/target"})
	svc := &LinkService{db: db, cache: cache}

	info, err := svc.GetByCode(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetByCode returned %v", err)
	}
	if info.OriginalURL != "https://example.com/target" {
		t.Errorf("OriginalURL = %q, want the cached target", info.OriginalURL)
	}
	if got := atomic.LoadInt64(queries); got != 0 {
		t.Errorf("a cache hit reached the database %d times, want 0", got)
	}
}

// An absent key is exactly when a burst of requests for it arrives together, so the read they all need
// has to happen once rather than once each.
func TestGetByCodeCollapsesConcurrentMissesIntoOneRead(t *testing.T) {
	db, queries := newMissingLinkDB(t, 50*time.Millisecond)
	cache := newFakeLinkCache()
	svc := &LinkService{db: db, cache: cache}
	ctx := context.Background()

	const callers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := svc.GetByCode(ctx, "absent1234"); !errors.Is(err, domain.ErrLinkNotFound) {
				t.Errorf("concurrent lookup returned %v, want ErrLinkNotFound", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(queries); got != 1 {
		t.Errorf("%d concurrent lookups reached the database %d times, want 1", callers, got)
	}
}

// Creating a link and renaming one both cache the link through this path. A "missing" verdict that
// outlived the link it denied would shadow that link until the TTL expired.
func TestCachingALinkClearsItsMissingEntry(t *testing.T) {
	cache := newFakeLinkCache()
	svc := &LinkService{cache: cache}
	ctx := context.Background()

	cache.MarkMissing(ctx, "abc123")
	svc.cacheLink(ctx, &domain.LinkInfo{ShortCode: "abc123", OriginalURL: "https://example.com/target"})

	if cache.IsMissing(ctx, "abc123") {
		t.Error("the missing entry outlived the link it denied")
	}
	if _, ok := cache.GetLink(ctx, "abc123"); !ok {
		t.Error("the link was not cached")
	}
}
