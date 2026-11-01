package cache

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLRUEvictionByByteCapacity(t *testing.T) {
	// 1000 byte capacity
	lru := NewLRUCache(1000)

	entry1 := &CacheEntry{Key: "GET /1", SizeBytes: 400, StatusCode: 200}
	entry2 := &CacheEntry{Key: "GET /2", SizeBytes: 400, StatusCode: 200}
	entry3 := &CacheEntry{Key: "GET /3", SizeBytes: 400, StatusCode: 200}

	lru.Set(entry1)
	lru.Set(entry2)

	if lru.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", lru.Len())
	}
	if lru.CurrentBytes() != 800 {
		t.Fatalf("expected 800 bytes, got %d", lru.CurrentBytes())
	}

	// Inserting entry3 (400 bytes) exceeds 1000 capacity (800 + 400 = 1200 > 1000)
	// Entry 1 (oldest/least recently used) must be evicted
	lru.Set(entry3)

	if lru.Len() != 2 {
		t.Fatalf("expected 2 items after eviction, got %d", lru.Len())
	}
	if lru.CurrentBytes() != 800 {
		t.Fatalf("expected 800 bytes after eviction, got %d", lru.CurrentBytes())
	}

	// Entry 1 should be gone
	if _, ok := lru.Get("GET /1"); ok {
		t.Errorf("expected GET /1 to be evicted")
	}
	// Entry 2 and Entry 3 should be present
	if _, ok := lru.Get("GET /2"); !ok {
		t.Errorf("expected GET /2 to be present")
	}
	if _, ok := lru.Get("GET /3"); !ok {
		t.Errorf("expected GET /3 to be present")
	}
}

func TestAccessUpdatesMRUOrder(t *testing.T) {
	// Capacity 1000 bytes
	lru := NewLRUCache(1000)

	entryA := &CacheEntry{Key: "GET /A", SizeBytes: 400, StatusCode: 200}
	entryB := &CacheEntry{Key: "GET /B", SizeBytes: 400, StatusCode: 200}
	entryC := &CacheEntry{Key: "GET /C", SizeBytes: 400, StatusCode: 200}

	lru.Set(entryA)
	lru.Set(entryB)

	// Access Entry A, promoting it to Most Recently Used (MRU)
	if _, ok := lru.Get("GET /A"); !ok {
		t.Fatalf("failed to retrieve entry A")
	}

	// Insert Entry C. Since A was refreshed, B is now the Least Recently Used (LRU)
	lru.Set(entryC)

	// Entry B must be evicted
	if _, ok := lru.Get("GET /B"); ok {
		t.Errorf("expected Entry B to be evicted")
	}
	// Entry A and C must remain
	if _, ok := lru.Get("GET /A"); !ok {
		t.Errorf("expected Entry A to remain in cache")
	}
	if _, ok := lru.Get("GET /C"); !ok {
		t.Errorf("expected Entry C to remain in cache")
	}
}

func TestConditional304Validation(t *testing.T) {
	etag := "\"etag-123\""
	entry := &CacheEntry{
		Key:          "GET /data",
		StatusCode:   200,
		ETag:         etag,
		LastModified: "Wed, 21 Oct 2025 07:28:00 GMT",
	}

	// 1. Matching ETag with exact quotes
	headersMatchingETag := map[string]string{
		"If-None-Match": "\"etag-123\"",
	}
	if !CheckConditionalValidation(headersMatchingETag, entry) {
		t.Errorf("expected true (304) for matching ETag")
	}

	// 2. Matching ETag without quotes
	headersUnquotedETag := map[string]string{
		"If-None-Match": "etag-123",
	}
	if !CheckConditionalValidation(headersUnquotedETag, entry) {
		t.Errorf("expected true (304) for unquoted matching ETag")
	}

	// 3. Unmatched ETag
	headersUnmatchedETag := map[string]string{
		"If-None-Match": "\"etag-999\"",
	}
	if CheckConditionalValidation(headersUnmatchedETag, entry) {
		t.Errorf("expected false for unmatched ETag")
	}

	// 4. Wildcard If-None-Match
	headersWildcard := map[string]string{
		"If-None-Match": "*",
	}
	if !CheckConditionalValidation(headersWildcard, entry) {
		t.Errorf("expected true (304) for wildcard If-None-Match")
	}

	// 5. Matching Last-Modified
	headersMatchingDate := map[string]string{
		"If-Modified-Since": "Wed, 21 Oct 2025 07:28:00 GMT",
	}
	if !CheckConditionalValidation(headersMatchingDate, entry) {
		t.Errorf("expected true (304) for matching Last-Modified")
	}

	// 6. Newer client date (origin is older)
	headersNewerDate := map[string]string{
		"If-Modified-Since": "Wed, 21 Oct 2025 08:00:00 GMT",
	}
	if !CheckConditionalValidation(headersNewerDate, entry) {
		t.Errorf("expected true (304) when client cached copy is newer than origin")
	}

	// 7. Older client date (origin is modified after client copy)
	headersOlderDate := map[string]string{
		"If-Modified-Since": "Wed, 21 Oct 2025 06:00:00 GMT",
	}
	if CheckConditionalValidation(headersOlderDate, entry) {
		t.Errorf("expected false when origin is newer than client timestamp")
	}
}

func TestCacheControlDirectives(t *testing.T) {
	// Parse max-age, no-cache, no-store
	ccVal := "public, max-age=5, no-cache"
	maxAge, noCache, noStore := ParseCacheControl(ccVal)
	if maxAge != 5*time.Second {
		t.Errorf("expected max-age=5s, got %v", maxAge)
	}
	if !noCache {
		t.Errorf("expected noCache=true")
	}
	if noStore {
		t.Errorf("expected noStore=false")
	}

	// Test no-store
	_, _, noStoreOnly := ParseCacheControl("no-store")
	if !noStoreOnly {
		t.Errorf("expected noStore=true")
	}

	// Test IsCacheable with no-store
	headersWithNoStore := map[string]string{"Cache-Control": "no-store"}
	if IsCacheable("GET", 200, headersWithNoStore) {
		t.Errorf("expected IsCacheable=false when Cache-Control: no-store")
	}

	// Test IsCacheable with unsupported method
	if IsCacheable("POST", 200, nil) {
		t.Errorf("expected IsCacheable=false for POST")
	}

	// Test IsCacheable with non-200 status
	if IsCacheable("GET", 404, nil) {
		t.Errorf("expected IsCacheable=false for 404")
	}

	// Test TTL expiration
	entry := &CacheEntry{
		Key:        "GET /short-lived",
		StatusCode: 200,
		CreatedAt:  time.Now().Add(-100 * time.Millisecond),
		TTL:        50 * time.Millisecond,
		SizeBytes:  100,
	}
	if entry.IsFresh() {
		t.Errorf("expected entry to be stale after TTL has passed")
	}

	lru := NewLRUCache(1000)
	lru.Set(entry)
	if _, ok := lru.Get("GET /short-lived"); ok {
		t.Errorf("expected Get on expired entry to return false and evict")
	}
}

func TestConcurrentAccess(t *testing.T) {
	lru := NewLRUCache(1024 * 1024) // 1 MB
	numGoroutines := 50
	operationsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				key := fmt.Sprintf("GET /resource-%d", (id+j)%20)
				entry := &CacheEntry{
					Key:        key,
					StatusCode: 200,
					Body:       []byte("cached-payload"),
					SizeBytes:  64,
				}

				// Interleave Set, Get, and Delete
				lru.Set(entry)
				_, _ = lru.Get(key)
				if j%5 == 0 {
					lru.Delete(key)
				}
				_ = lru.Len()
				_ = lru.CurrentBytes()
			}
		}(i)
	}

	wg.Wait()
}

func TestComputeETag(t *testing.T) {
	body1 := []byte("hello world")
	body2 := []byte("hello world")
	body3 := []byte("different payload")

	etag1 := ComputeETag(body1)
	etag2 := ComputeETag(body2)
	etag3 := ComputeETag(body3)

	if etag1 != etag2 {
		t.Errorf("identical payloads should produce identical ETags: %s != %s", etag1, etag2)
	}
	if etag1 == etag3 {
		t.Errorf("different payloads should produce different ETags: %s == %s", etag1, etag3)
	}
	if !strings.HasPrefix(etag1, "\"") || !strings.HasSuffix(etag1, "\"") {
		t.Errorf("ETag should be quoted: %s", etag1)
	}
}

func TestLRU_Purge(t *testing.T) {
	lru := NewLRUCache(1000)
	lru.Set(&CacheEntry{Key: "GET /1", SizeBytes: 100})
	lru.Set(&CacheEntry{Key: "GET /2", SizeBytes: 200})

	if lru.Len() != 2 || lru.CurrentBytes() != 300 {
		t.Fatalf("unexpected state before purge: len=%d bytes=%d", lru.Len(), lru.CurrentBytes())
	}

	lru.Purge()

	if lru.Len() != 0 {
		t.Errorf("expected 0 items after purge, got %d", lru.Len())
	}
	if lru.CurrentBytes() != 0 {
		t.Errorf("expected 0 bytes after purge, got %d", lru.CurrentBytes())
	}
}

