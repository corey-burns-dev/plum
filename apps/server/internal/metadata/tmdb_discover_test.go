package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type memoryProviderCache struct {
	mu      sync.Mutex
	entries map[ProviderCacheKey]ProviderCacheEntry
}

func newMemoryProviderCache() *memoryProviderCache {
	return &memoryProviderCache{
		entries: make(map[ProviderCacheKey]ProviderCacheEntry),
	}
}

func (c *memoryProviderCache) Get(_ context.Context, key ProviderCacheKey, now time.Time) (*ProviderCacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || now.After(entry.ExpiresAt) {
		return nil, false, nil
	}
	copyEntry := entry
	copyEntry.ResponseJSON = append([]byte(nil), entry.ResponseJSON...)
	return &copyEntry, true, nil
}

func (c *memoryProviderCache) Put(_ context.Context, key ProviderCacheKey, entry ProviderCacheEntry, _ time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.ResponseJSON = append([]byte(nil), entry.ResponseJSON...)
	c.entries[key] = entry
	return nil
}

func TestTMDBClientGetDiscoverMapsShelvesAndUsesCache(t *testing.T) {
	var (
		mu    sync.Mutex
		hits  = make(map[string]int)
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/trending/all/day":
			_, _ = w.Write([]byte(`{"results":[{"id":1,"media_type":"movie","title":"Big Movie","poster_path":"/movie.jpg","release_date":"2025-01-01","vote_average":8.2},{"id":2,"media_type":"tv","name":"Big Show","poster_path":"/show.jpg","first_air_date":"2024-02-02","vote_average":8.7},{"id":999,"media_type":"person","name":"Ignore Me"}]}`))
		case "/movie/popular":
			_, _ = w.Write([]byte(`{"results":[{"id":3,"title":"Popular Movie","poster_path":"/popular-movie.jpg","release_date":"2025-03-03","vote_average":7.7}]}`))
		case "/movie/now_playing":
			_, _ = w.Write([]byte(`{"results":[{"id":4,"title":"Now Playing","poster_path":"/now.jpg","release_date":"2025-04-04","vote_average":7.5}]}`))
		case "/movie/upcoming":
			_, _ = w.Write([]byte(`{"results":[{"id":5,"title":"Coming Soon","poster_path":"/upcoming.jpg","release_date":"2025-06-06","vote_average":7.1}]}`))
		case "/tv/popular":
			_, _ = w.Write([]byte(`{"results":[{"id":6,"name":"Popular Show","poster_path":"/popular-show.jpg","first_air_date":"2024-05-05","vote_average":8.0}]}`))
		case "/tv/on_the_air":
			_, _ = w.Write([]byte(`{"results":[{"id":7,"name":"On Air Show","poster_path":"/on-air.jpg","first_air_date":"2024-07-07","vote_average":8.3}]}`))
		case "/movie/top_rated":
			_, _ = w.Write([]byte(`{"results":[{"id":8,"title":"Top Movie","poster_path":"/top-movie.jpg","release_date":"2020-08-08","vote_average":9.0}]}`))
		case "/tv/top_rated":
			_, _ = w.Write([]byte(`{"results":[{"id":9,"name":"Top Show","poster_path":"/top-show.jpg","first_air_date":"2021-09-09","vote_average":9.1}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTMDBClient("test-key")
	client.baseURL = server.URL
	client.SetCache(newMemoryProviderCache())

	first, err := client.GetDiscover(context.Background())
	if err != nil {
		t.Fatalf("first get discover: %v", err)
	}
	second, err := client.GetDiscover(context.Background())
	if err != nil {
		t.Fatalf("second get discover: %v", err)
	}

	if len(first.Shelves) != 7 || len(second.Shelves) != 7 {
		t.Fatalf("shelves = %+v %+v", first.Shelves, second.Shelves)
	}
	if got := len(first.Shelves[0].Items); got != 2 {
		t.Fatalf("trending items = %d", got)
	}
	if first.Shelves[0].Items[0].MediaType != DiscoverMediaTypeMovie {
		t.Fatalf("first trending type = %q", first.Shelves[0].Items[0].MediaType)
	}
	if first.Shelves[0].Items[1].MediaType != DiscoverMediaTypeTV {
		t.Fatalf("second trending type = %q", first.Shelves[0].Items[1].MediaType)
	}
	if first.Shelves[6].Items[0].TMDBID != 8 || first.Shelves[6].Items[1].TMDBID != 9 {
		t.Fatalf("top rated interleave = %+v", first.Shelves[6].Items)
	}

	for _, path := range []string{
		"/trending/all/day",
		"/movie/popular",
		"/movie/now_playing",
		"/movie/upcoming",
		"/tv/popular",
		"/tv/on_the_air",
		"/movie/top_rated",
		"/tv/top_rated",
	} {
		mu.Lock()
		count := hits[path]
		mu.Unlock()
		if count != 1 {
			t.Fatalf("expected %s to be fetched once, got %d", path, count)
		}
	}
}

func TestTMDBClientSearchAndDetailMapDiscoverPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":11,"title":"Search Movie","poster_path":"/movie.jpg","release_date":"2024-01-01","vote_average":7.4}]}`))
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":12,"name":"Search Show","poster_path":"/show.jpg","first_air_date":"2023-02-02","vote_average":8.5}]}`))
		case "/movie/11":
			_, _ = w.Write([]byte(`{"id":11,"title":"Search Movie","overview":"Movie overview","poster_path":"/movie.jpg","backdrop_path":"/movie-backdrop.jpg","release_date":"2024-01-01","vote_average":7.4,"status":"Released","runtime":123,"genres":[{"name":"Drama"}],"videos":{"results":[{"name":"Trailer","site":"YouTube","key":"movie123","type":"Trailer","official":true}]}}`))
		case "/movie/11/external_ids":
			_, _ = w.Write([]byte(`{"imdb_id":"tt0011"}`))
		case "/tv/12":
			_, _ = w.Write([]byte(`{"id":12,"name":"Search Show","overview":"Show overview","poster_path":"/show.jpg","backdrop_path":"/show-backdrop.jpg","first_air_date":"2023-02-02","vote_average":8.5,"status":"Returning Series","episode_run_time":[47],"number_of_seasons":3,"number_of_episodes":24,"genres":[{"name":"Sci-Fi"}],"videos":{"results":[{"name":"Teaser","site":"YouTube","key":"show123","type":"Teaser","official":false}]}}`))
		case "/tv/12/external_ids":
			_, _ = w.Write([]byte(`{"imdb_id":"tt0012"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTMDBClient("test-key")
	client.baseURL = server.URL

	search, err := client.SearchDiscover(context.Background(), "search")
	if err != nil {
		t.Fatalf("search discover: %v", err)
	}
	if len(search.Movies) != 1 || len(search.TV) != 1 {
		t.Fatalf("search = %+v", search)
	}
	if search.Movies[0].Title != "Search Movie" || search.TV[0].Title != "Search Show" {
		t.Fatalf("search items = %+v", search)
	}

	movieDetails, err := client.GetDiscoverTitleDetails(context.Background(), DiscoverMediaTypeMovie, 11)
	if err != nil {
		t.Fatalf("movie details: %v", err)
	}
	if movieDetails.IMDbID != "tt0011" || movieDetails.Runtime != 123 || len(movieDetails.Videos) != 1 {
		t.Fatalf("movie details = %+v", movieDetails)
	}

	tvDetails, err := client.GetDiscoverTitleDetails(context.Background(), DiscoverMediaTypeTV, 12)
	if err != nil {
		t.Fatalf("tv details: %v", err)
	}
	if tvDetails.IMDbID != "tt0012" || tvDetails.Runtime != 47 || tvDetails.NumberOfSeasons != 3 {
		t.Fatalf("tv details = %+v", tvDetails)
	}
}
