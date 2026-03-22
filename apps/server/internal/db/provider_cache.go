package db

import (
	"context"
	"database/sql"
	"time"

	"plum/internal/metadata"
)

// MetadataProviderCacheStore persists provider responses in SQLite.
type MetadataProviderCacheStore struct {
	DB *sql.DB
}

func NewMetadataProviderCacheStore(db *sql.DB) *MetadataProviderCacheStore {
	return &MetadataProviderCacheStore{DB: db}
}

func (s *MetadataProviderCacheStore) Get(ctx context.Context, key metadata.ProviderCacheKey, now time.Time) (*metadata.ProviderCacheEntry, bool, error) {
	if s == nil || s.DB == nil {
		return nil, false, nil
	}
	var (
		responseJSON  []byte
		statusCode    int
		fetchedAtRaw  string
		expiresAtRaw  string
		schemaVersion int
		contentHash   string
	)
	err := s.DB.QueryRowContext(ctx, `SELECT response_json, status_code, fetched_at, expires_at, schema_version, content_hash
FROM metadata_provider_cache
WHERE provider = ? AND method = ? AND url_path = ? AND query_hash = ? AND body_hash = ?`,
		key.Provider, key.Method, key.URLPath, key.QueryHash, key.BodyHash).
		Scan(&responseJSON, &statusCode, &fetchedAtRaw, &expiresAtRaw, &schemaVersion, &contentHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	fetchedAt, _ := time.Parse(time.RFC3339, fetchedAtRaw)
	expiresAt, _ := time.Parse(time.RFC3339, expiresAtRaw)
	_, _ = s.DB.ExecContext(ctx, `UPDATE metadata_provider_cache
SET last_accessed_at = ?, hit_count = hit_count + 1
WHERE provider = ? AND method = ? AND url_path = ? AND query_hash = ? AND body_hash = ?`,
		now.UTC().Format(time.RFC3339), key.Provider, key.Method, key.URLPath, key.QueryHash, key.BodyHash)

	return &metadata.ProviderCacheEntry{
		ResponseJSON:  responseJSON,
		StatusCode:    statusCode,
		FetchedAt:     fetchedAt,
		ExpiresAt:     expiresAt,
		SchemaVersion: schemaVersion,
		ContentHash:   contentHash,
	}, true, nil
}

func (s *MetadataProviderCacheStore) Put(ctx context.Context, key metadata.ProviderCacheKey, entry metadata.ProviderCacheEntry, now time.Time) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO metadata_provider_cache (
provider, method, url_path, query_hash, body_hash, response_json, fetched_at, expires_at, schema_version, content_hash, status_code, last_accessed_at, hit_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
ON CONFLICT(provider, method, url_path, query_hash, body_hash) DO UPDATE SET
response_json = excluded.response_json,
fetched_at = excluded.fetched_at,
expires_at = excluded.expires_at,
schema_version = excluded.schema_version,
content_hash = excluded.content_hash,
status_code = excluded.status_code,
last_accessed_at = excluded.last_accessed_at`,
		key.Provider,
		key.Method,
		key.URLPath,
		key.QueryHash,
		key.BodyHash,
		entry.ResponseJSON,
		entry.FetchedAt.UTC().Format(time.RFC3339),
		entry.ExpiresAt.UTC().Format(time.RFC3339),
		entry.SchemaVersion,
		entry.ContentHash,
		entry.StatusCode,
		now.UTC().Format(time.RFC3339),
	)
	return err
}
