package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ProviderCacheKey identifies one cache entry for a provider request.
type ProviderCacheKey struct {
	Provider  string
	Method    string
	URLPath   string
	QueryHash string
	BodyHash  string
}

// ProviderCacheEntry stores one cached provider response body + metadata.
type ProviderCacheEntry struct {
	ResponseJSON  []byte
	StatusCode    int
	FetchedAt     time.Time
	ExpiresAt     time.Time
	SchemaVersion int
	ContentHash   string
}

// ProviderCache is implemented by persistent stores (for example SQLite).
type ProviderCache interface {
	Get(ctx context.Context, key ProviderCacheKey, now time.Time) (*ProviderCacheEntry, bool, error)
	Put(ctx context.Context, key ProviderCacheKey, entry ProviderCacheEntry, now time.Time) error
}

type cachedHTTPResponse struct {
	Body       []byte
	StatusCode int
}

func doCachedJSONRequest(
	ctx context.Context,
	httpClient *http.Client,
	cache ProviderCache,
	provider string,
	method string,
	rawURL string,
	body []byte,
	headers map[string]string,
	ttl time.Duration,
	schemaVersion int,
) (*cachedHTTPResponse, error) {
	key, err := providerCacheKey(provider, method, rawURL, body)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if cache != nil {
		if entry, found, err := cache.Get(ctx, key, now); err == nil && found && now.Before(entry.ExpiresAt) {
			return &cachedHTTPResponse{
				Body:       append([]byte(nil), entry.ResponseJSON...),
				StatusCode: entry.StatusCode,
			}, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for headerKey, headerValue := range headers {
		req.Header.Set(headerKey, headerValue)
	}

	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if cache != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		_ = cache.Put(ctx, key, ProviderCacheEntry{
			ResponseJSON:  append([]byte(nil), rawBody...),
			StatusCode:    resp.StatusCode,
			FetchedAt:     now,
			ExpiresAt:     now.Add(ttl),
			SchemaVersion: schemaVersion,
			ContentHash:   hashBytes(rawBody),
		}, now)
	}
	return &cachedHTTPResponse{Body: rawBody, StatusCode: resp.StatusCode}, nil
}

func providerCacheKey(provider, method, rawURL string, body []byte) (ProviderCacheKey, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ProviderCacheKey{}, err
	}
	return ProviderCacheKey{
		Provider:  strings.ToLower(strings.TrimSpace(provider)),
		Method:    strings.ToUpper(strings.TrimSpace(method)),
		URLPath:   strings.TrimSpace(u.Path),
		QueryHash: hashString(canonicalQuery(u.Query())),
		BodyHash:  hashBytes(body),
	}, nil
}

func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		items := append([]string(nil), values[key]...)
		sort.Strings(items)
		parts = append(parts, key+"="+strings.Join(items, ","))
	}
	return strings.Join(parts, "&")
}

func hashString(value string) string {
	return hashBytes([]byte(value))
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
