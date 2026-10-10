package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/chun/kada-backend/internal/domain"
)

// missingTTL is how long a short code that was looked up and did not resolve is remembered. It is far
// shorter than the link TTL on purpose: the entry exists to stop a scanner turning into a row of
// database queries, not to be a source of truth, and every second of it is a second in which a code
// created just after the lookup would be denied. Creating or renaming a link clears it explicitly, so
// this window only matters if that clear fails.
const missingTTL = 60 * time.Second

// LinkCache is what LinkService needs from a cache. It is an interface so the redirect path can be
// tested without Redis.
//
// A nil *CacheService must never be stored in it: a nil pointer inside an interface is not nil, so the
// `cache != nil` guards would all pass and the first call would dereference it.
type LinkCache interface {
	GetLink(ctx context.Context, shortCode string) (*domain.LinkInfo, bool)
	SetLink(ctx context.Context, info *domain.LinkInfo)
	InvalidateLink(ctx context.Context, shortCode string)
	MarkMissing(ctx context.Context, shortCode string)
	ClearMissing(ctx context.Context, shortCode string)
	IsMissing(ctx context.Context, shortCode string) bool
}

// CacheService is a Redis cache service
type CacheService struct {
	client *redis.Client
	ttl    time.Duration
}

func NewCacheService(client *redis.Client) *CacheService {
	return &CacheService{
		client: client,
		ttl:    10 * time.Minute,
	}
}

// key builds a cache key
func (cs *CacheService) key(prefix, identifier string) string {
	return fmt.Sprintf("cache:%s:%s", prefix, identifier)
}

// GetLink gets link info from the cache (by short code)
func (cs *CacheService) GetLink(ctx context.Context, shortCode string) (*domain.LinkInfo, bool) {
	data, err := cs.client.Get(ctx, cs.key("link", shortCode)).Bytes()
	if err != nil {
		return nil, false
	}

	var info domain.LinkInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, false
	}
	return &info, true
}

// SetLink caches link info
func (cs *CacheService) SetLink(ctx context.Context, info *domain.LinkInfo) {
	key := cs.key("link", info.ShortCode)
	data, err := json.Marshal(info)
	if err != nil {
		return
	}
	cs.client.Set(ctx, key, data, cs.ttl)
}

// InvalidateLink invalidates the cached link.
//
// Both keys go: the link, and the record that the code was missing. A code that is being renamed or
// deleted must not be denied afterwards by a verdict that was true a moment ago.
//
// The error is logged rather than dropped: the redirect path trusts the cached entry to carry the
// link's password state, so a DEL that fails silently can leave a link reachable without its password
// until the TTL expires.
func (cs *CacheService) InvalidateLink(ctx context.Context, shortCode string) {
	if err := cs.client.Del(ctx, cs.key("link", shortCode), cs.missingKey(shortCode)).Err(); err != nil {
		log.Printf("invalidate cached link %q failed, the stale entry now stands until its TTL: %v", shortCode, err)
	}
}

// missingKey is a key of its own rather than a sentinel stored under the link key: a sentinel would be
// read back as a LinkInfo, and a JSON decode of "null" is indistinguishable from a cached link.
func (cs *CacheService) missingKey(shortCode string) string {
	return cs.key("linkmiss", shortCode)
}

// MarkMissing remembers that a short code did not resolve to a link
func (cs *CacheService) MarkMissing(ctx context.Context, shortCode string) {
	if err := cs.client.Set(ctx, cs.missingKey(shortCode), "1", missingTTL).Err(); err != nil {
		log.Printf("remember missing short code %q failed: %v", shortCode, err)
	}
}

// ClearMissing drops the record that a short code was missing
func (cs *CacheService) ClearMissing(ctx context.Context, shortCode string) {
	if err := cs.client.Del(ctx, cs.missingKey(shortCode)).Err(); err != nil {
		log.Printf("clear missing short code %q failed: %v", shortCode, err)
	}
}

// IsMissing reports whether a short code was recently looked up and did not exist
func (cs *CacheService) IsMissing(ctx context.Context, shortCode string) bool {
	n, err := cs.client.Exists(ctx, cs.missingKey(shortCode)).Result()
	if err != nil {
		// A Redis failure must not be read as "this code is missing": it has to send the request to the
		// database, which is the one answer that cannot be wrong.
		log.Printf("read missing short code %q failed: %v", shortCode, err)
		return false
	}
	return n > 0
}
