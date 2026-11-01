package cache

import (
	"time"
)

// CacheEntry represents an in-memory cached HTTP response.
type CacheEntry struct {
	Key          string            // Format: METHOD + " " + URI, e.g., "GET /index.html"
	StatusCode   int               // HTTP response status code
	Headers      map[string]string // Retained origin response headers
	Body         []byte            // Cached response body payload
	SizeBytes    int64             // Total memory footprint (payload bytes + metadata)
	CreatedAt    time.Time         // Timestamp when entry was stored
	TTL          time.Duration     // Freshness lifetime derived from Cache-Control max-age (0 = persistent)
	ETag         string            // Entity tag for conditional revalidation
	LastModified string            // Origin Last-Modified timestamp string
}

// ComputeSize calculates the memory footprint of the CacheEntry in bytes.
func (e *CacheEntry) ComputeSize() int64 {
	size := int64(len(e.Key) + len(e.Body) + len(e.ETag) + len(e.LastModified))
	for k, v := range e.Headers {
		size += int64(len(k) + len(v))
	}
	// Constant overhead for struct fields, map headers, and pointer metadata
	size += 128
	return size
}

// EnsureSize guarantees SizeBytes is populated.
func (e *CacheEntry) EnsureSize() {
	if e.SizeBytes <= 0 {
		e.SizeBytes = e.ComputeSize()
	}
}

// IsFresh checks whether the cached entry is still within its freshness lifetime.
func (e *CacheEntry) IsFresh() bool {
	if e.TTL <= 0 {
		return true // Considered fresh until explicitly evicted or purged
	}
	return time.Since(e.CreatedAt) < e.TTL
}

// CloneHeaders returns a deep copy of the headers map to prevent data races.
func (e *CacheEntry) CloneHeaders() map[string]string {
	if e.Headers == nil {
		return make(map[string]string)
	}
	cloned := make(map[string]string, len(e.Headers))
	for k, v := range e.Headers {
		cloned[k] = v
	}
	return cloned
}

