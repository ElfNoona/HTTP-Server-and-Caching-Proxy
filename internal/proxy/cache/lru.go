package cache

import (
	"container/list"
	"sync"
)

// LRUCache is a thread-safe in-memory cache with O(1) lookups and strict LRU byte-capacity eviction.
type LRUCache struct {
	maxBytes     int64
	currentBytes int64
	items        map[string]*list.Element
	evictList    *list.List
	mu           sync.RWMutex
}

// NewLRUCache creates an LRUCache configured with the given maximum byte capacity.
// If maxBytes <= 0, it defaults to 50 MB.
func NewLRUCache(maxBytes int64) *LRUCache {
	if maxBytes <= 0 {
		maxBytes = 50 * 1024 * 1024 // 50 MB default
	}
	return &LRUCache{
		maxBytes:  maxBytes,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

// removeElement removes a list element from the LRU list, drops it from items, and adjusts memory tracking.
// Caller must hold c.mu Lock.
func (c *LRUCache) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	entry := elem.Value.(*CacheEntry)
	delete(c.items, entry.Key)
	c.currentBytes -= entry.SizeBytes
	if c.currentBytes < 0 {
		c.currentBytes = 0
	}
}

// Get retrieves a cached entry by key, moving it to the front of the MRU list.
// If the entry has expired (stale), it is removed and (nil, false) is returned.
func (c *LRUCache) Get(key string) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		return nil, false
	}

	entry := elem.Value.(*CacheEntry)
	if !entry.IsFresh() {
		c.removeElement(elem)
		return nil, false
	}

	c.evictList.MoveToFront(elem)
	return entry, true
}

// Set inserts or updates an entry in the cache.
// It evicts least recently used items as necessary to stay within maxBytes.
func (c *LRUCache) Set(entry *CacheEntry) {
	if entry == nil {
		return
	}
	entry.EnsureSize()

	c.mu.Lock()
	defer c.mu.Unlock()

	// If entry size is larger than total cache capacity, do not cache
	if entry.SizeBytes > c.maxBytes {
		return
	}

	// Update existing entry
	if elem, exists := c.items[entry.Key]; exists {
		oldEntry := elem.Value.(*CacheEntry)
		c.currentBytes -= oldEntry.SizeBytes
		elem.Value = entry
		c.evictList.MoveToFront(elem)
		c.currentBytes += entry.SizeBytes
		return
	}

	// Evict LRU entries from back until there is room for the new entry
	for c.currentBytes+entry.SizeBytes > c.maxBytes && c.evictList.Len() > 0 {
		back := c.evictList.Back()
		if back == nil {
			break
		}
		c.removeElement(back)
	}

	elem := c.evictList.PushFront(entry)
	c.items[entry.Key] = elem
	c.currentBytes += entry.SizeBytes
}

// Delete removes an entry by key from the cache.
// Returns true if the key was found and deleted.
func (c *LRUCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.items[key]; exists {
		c.removeElement(elem)
		return true
	}
	return false
}

// CurrentBytes returns the total memory footprint currently occupied by cached entries.
func (c *LRUCache) CurrentBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentBytes
}

// Len returns the number of items stored in the cache.
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Purge removes all entries and resets the cache size.
func (c *LRUCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.evictList.Init()
	c.currentBytes = 0
}

