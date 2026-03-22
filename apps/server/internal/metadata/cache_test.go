package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoCachedJSONRequest_UsesPersistentCacheUntilExpiry(t *testing.T) {
	cache := &inMemoryProviderCache{entries: make(map[string]*ProviderCacheEntry)}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int32{"value": call})
	}))
	defer server.Close()

	ctx := context.Background()
	first, err := doCachedJSONRequest(ctx, http.DefaultClient, cache, "tmdb", http.MethodGet, server.URL+"/search?q=one", nil, nil, time.Hour, 1)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	second, err := doCachedJSONRequest(ctx, http.DefaultClient, cache, "tmdb", http.MethodGet, server.URL+"/search?q=one", nil, nil, time.Hour, 1)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one upstream call, got %d", calls.Load())
	}
	if string(first.Body) != string(second.Body) {
		t.Fatalf("expected cached body match")
	}

	shortTTL, err := doCachedJSONRequest(ctx, http.DefaultClient, cache, "tmdb", http.MethodGet, server.URL+"/search?q=two", nil, nil, 10*time.Millisecond, 1)
	if err != nil {
		t.Fatalf("short ttl request: %v", err)
	}
	_ = shortTTL
	time.Sleep(25 * time.Millisecond)
	if _, err := doCachedJSONRequest(ctx, http.DefaultClient, cache, "tmdb", http.MethodGet, server.URL+"/search?q=two", nil, nil, 10*time.Millisecond, 1); err != nil {
		t.Fatalf("expired request: %v", err)
	}
	if calls.Load() < 3 {
		t.Fatalf("expected cache expiry refetch, calls=%d", calls.Load())
	}
}

func TestDoCachedJSONRequest_DoesNotCacheUnsuccessfulResponses(t *testing.T) {
	cache := &inMemoryProviderCache{entries: make(map[string]*ProviderCacheEntry)}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int32{"value": call})
	}))
	defer server.Close()

	ctx := context.Background()
	first, err := doCachedJSONRequest(ctx, http.DefaultClient, cache, "tmdb", http.MethodGet, server.URL+"/search?q=failure", nil, nil, time.Hour, 1)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if first.StatusCode != http.StatusInternalServerError {
		t.Fatalf("first status = %d", first.StatusCode)
	}
	if len(cache.entries) != 0 {
		t.Fatalf("expected no cache entry after failure, got %d", len(cache.entries))
	}

	second, err := doCachedJSONRequest(ctx, http.DefaultClient, cache, "tmdb", http.MethodGet, server.URL+"/search?q=failure", nil, nil, time.Hour, 1)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if second.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d", second.StatusCode)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two upstream calls, got %d", calls.Load())
	}
	if len(cache.entries) != 1 {
		t.Fatalf("expected one cached success, got %d", len(cache.entries))
	}
}

type inMemoryProviderCache struct {
	mu      sync.Mutex
	entries map[string]*ProviderCacheEntry
}

func (c *inMemoryProviderCache) cacheKey(key ProviderCacheKey) string {
	return key.Provider + "|" + key.Method + "|" + key.URLPath + "|" + key.QueryHash + "|" + key.BodyHash
}

func (c *inMemoryProviderCache) Get(_ context.Context, key ProviderCacheKey, _ time.Time) (*ProviderCacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[c.cacheKey(key)]
	if !ok {
		return nil, false, nil
	}
	cp := *entry
	return &cp, true, nil
}

func (c *inMemoryProviderCache) Put(_ context.Context, key ProviderCacheKey, entry ProviderCacheEntry, _ time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := entry
	c.entries[c.cacheKey(key)] = &cp
	return nil
}
