package db

import (
	"context"
	"testing"
	"time"

	"plum/internal/metadata"
)

func TestMetadataProviderCacheStore_PutAndGet(t *testing.T) {
	dbConn := newTestDB(t)
	store := NewMetadataProviderCacheStore(dbConn)
	now := time.Now().UTC()
	key := metadata.ProviderCacheKey{
		Provider:  "tmdb",
		Method:    "GET",
		URLPath:   "/search/tv",
		QueryHash: "q1",
		BodyHash:  "b1",
	}
	entry := metadata.ProviderCacheEntry{
		ResponseJSON:  []byte(`{"results":[1]}`),
		StatusCode:    200,
		FetchedAt:     now,
		ExpiresAt:     now.Add(time.Hour),
		SchemaVersion: 1,
		ContentHash:   "hash-a",
	}
	if err := store.Put(context.Background(), key, entry, now); err != nil {
		t.Fatalf("put cache entry: %v", err)
	}
	got, found, err := store.Get(context.Background(), key, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("get cache entry: %v", err)
	}
	if !found || got == nil {
		t.Fatalf("expected cache hit")
	}
	if got.StatusCode != entry.StatusCode || string(got.ResponseJSON) != string(entry.ResponseJSON) {
		t.Fatalf("unexpected cache entry: %#v", got)
	}
}
