package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const omdbBaseURL = "https://www.omdbapi.com/"

type OMDBClient struct {
	APIKey string
	cache  ProviderCache

	mu           sync.RWMutex
	ratingByIMDb map[string]float64
}

func NewOMDBClient(apiKey string) *OMDBClient {
	if apiKey == "" {
		return nil
	}
	return &OMDBClient{
		APIKey:       apiKey,
		ratingByIMDb: map[string]float64{},
	}
}

func (c *OMDBClient) SetCache(cache ProviderCache) {
	if c == nil {
		return
	}
	c.cache = cache
}

func (c *OMDBClient) GetIMDbRatingByID(ctx context.Context, imdbID string) (float64, error) {
	if c == nil || c.APIKey == "" || imdbID == "" {
		return 0, nil
	}
	c.mu.RLock()
	if rating, ok := c.ratingByIMDb[imdbID]; ok {
		c.mu.RUnlock()
		return rating, nil
	}
	c.mu.RUnlock()
	u := fmt.Sprintf("%s?apikey=%s&i=%s", omdbBaseURL, url.QueryEscape(c.APIKey), url.QueryEscape(imdbID))
	resp, err := doCachedJSONRequest(ctx, http.DefaultClient, c.cache, "omdb", http.MethodGet, u, nil, nil, 30*24*time.Hour, 1)
	if err != nil {
		return 0, err
	}

	var payload struct {
		Response   string `json:"Response"`
		IMDbRating string `json:"imdbRating"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return 0, err
	}
	if !strings.EqualFold(payload.Response, "True") || payload.IMDbRating == "" || payload.IMDbRating == "N/A" {
		c.mu.Lock()
		c.ratingByIMDb[imdbID] = 0
		c.mu.Unlock()
		return 0, nil
	}
	rating, err := strconv.ParseFloat(payload.IMDbRating, 64)
	if err != nil {
		return 0, nil
	}
	c.mu.Lock()
	c.ratingByIMDb[imdbID] = rating
	c.mu.Unlock()
	return rating, nil
}
