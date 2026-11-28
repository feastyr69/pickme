package store

import (
	"encoding/json"
	"sync"
	"time"
)

const (
	readCacheTTL      = time.Minute
	readCacheMaxItems = 256
	readCacheMaxBytes = 32 << 20
	readCacheMaxItem  = 2 << 20
)

type readCacheEntry struct {
	data      []byte
	expiresAt time.Time
}

// MemoryCache stores short-lived JSON snapshots of read-only store results.
type MemoryCache struct {
	mu       sync.Mutex
	entries  map[string]readCacheEntry
	totalLen int
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{entries: make(map[string]readCacheEntry)}
}

func (c *MemoryCache) Get(key string, target any) bool {
	if c == nil {
		return false
	}

	now := time.Now()
	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok && !now.Before(entry.expiresAt) {
		c.deleteLocked(key)
		ok = false
	}
	data := append([]byte(nil), entry.data...)
	c.mu.Unlock()
	if !ok {
		return false
	}
	if err := json.Unmarshal(data, target); err != nil {
		c.Delete(key)
		return false
	}
	return true
}

func (c *MemoryCache) Set(key string, value any) {
	if c == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 || len(data) > readCacheMaxItem {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteLocked(key)
	for len(c.entries) >= readCacheMaxItems || c.totalLen+len(data) > readCacheMaxBytes {
		c.evictEarliestLocked()
	}
	c.entries[key] = readCacheEntry{data: data, expiresAt: time.Now().Add(readCacheTTL)}
	c.totalLen += len(data)
}

func (c *MemoryCache) Delete(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.deleteLocked(key)
	c.mu.Unlock()
}

func (c *MemoryCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]readCacheEntry)
	c.totalLen = 0
	c.mu.Unlock()
}

func (c *MemoryCache) deleteLocked(key string) {
	if entry, ok := c.entries[key]; ok {
		c.totalLen -= len(entry.data)
		delete(c.entries, key)
	}
}

func (c *MemoryCache) evictEarliestLocked() {
	var key string
	var earliest time.Time
	for candidate, entry := range c.entries {
		if earliest.IsZero() || entry.expiresAt.Before(earliest) {
			key = candidate
			earliest = entry.expiresAt
		}
	}
	if !earliest.IsZero() {
		c.deleteLocked(key)
	}
}
