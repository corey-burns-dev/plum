package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const tmdbBaseURL = "https://api.themoviedb.org/3"
const tmdbImageBase = "https://image.tmdb.org/t/p"

type TMDBClient struct {
	APIKey string
	cache  ProviderCache

	mu           sync.RWMutex
	tvDetails    map[int]*TMDBResult
	movieDetails map[int]*TMDBResult
	tvIMDbIDs    map[int]string
	movieIMDbIDs map[int]string
}

func (c *TMDBClient) ProviderName() string {
	return "tmdb"
}

type TMDBResult struct {
	ID           int     `json:"id"`
	Name         string  `json:"name,omitempty"`
	Title        string  `json:"title,omitempty"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	ReleaseDate  string  `json:"release_date,omitempty"`
	FirstAirDate string  `json:"first_air_date,omitempty"`
	VoteAverage  float64 `json:"vote_average"`
}

type tmdbExternalIDsResponse struct {
	IMDbID string `json:"imdb_id"`
}

type TMDBSearchResponse struct {
	Results []TMDBResult `json:"results"`
}

func NewTMDBClient(apiKey string) *TMDBClient {
	return &TMDBClient{
		APIKey:       apiKey,
		tvDetails:    map[int]*TMDBResult{},
		movieDetails: map[int]*TMDBResult{},
		tvIMDbIDs:    map[int]string{},
		movieIMDbIDs: map[int]string{},
	}
}

func (c *TMDBClient) SetCache(cache ProviderCache) {
	c.cache = cache
}

func (c *TMDBClient) SearchTV(ctx context.Context, query string) ([]MatchResult, error) {
	u := fmt.Sprintf("%s/search/tv?api_key=%s&query=%s", tmdbBaseURL, c.APIKey, url.QueryEscape(query))
	resp, err := doCachedJSONRequest(ctx, http.DefaultClient, c.cache, "tmdb", http.MethodGet, u, nil, nil, 24*time.Hour, 1)
	if err != nil {
		return nil, err
	}
	var res TMDBSearchResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	out := make([]MatchResult, 0, len(res.Results))
	for _, r := range res.Results {
		out = append(out, c.tmdbResultToMatch(r, r.Name, r.FirstAirDate))
	}
	return out, nil
}

func (c *TMDBClient) SearchMovie(ctx context.Context, query string) ([]MatchResult, error) {
	u := fmt.Sprintf("%s/search/movie?api_key=%s&query=%s", tmdbBaseURL, c.APIKey, url.QueryEscape(query))
	resp, err := doCachedJSONRequest(ctx, http.DefaultClient, c.cache, "tmdb", http.MethodGet, u, nil, nil, 24*time.Hour, 1)
	if err != nil {
		return nil, err
	}
	var res TMDBSearchResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	out := make([]MatchResult, 0, len(res.Results))
	for _, r := range res.Results {
		out = append(out, c.tmdbResultToMatch(r, r.Title, r.ReleaseDate))
	}
	return out, nil
}

func (c *TMDBClient) GetMovie(ctx context.Context, movieID string) (*MatchResult, error) {
	id, err := strconv.Atoi(movieID)
	if err != nil {
		return nil, err
	}
	detail, err := c.getMovieDetails(id)
	if err != nil || detail == nil {
		return nil, err
	}
	m := c.tmdbResultToMatch(*detail, detail.Title, detail.ReleaseDate)
	m.IMDbID, _ = c.getMovieIMDbID(ctx, id)
	return &m, nil
}

func (c *TMDBClient) GetEpisode(ctx context.Context, seriesID string, season, episode int) (*MatchResult, error) {
	tvID, err := strconv.Atoi(seriesID)
	if err != nil {
		return nil, err
	}
	ep, err := c.getEpisodeDetails(tvID, season, episode)
	if err != nil {
		return nil, err
	}
	series, _ := c.getTVDetails(tvID)
	posterPath := ep.PosterPath
	if posterPath == "" && series != nil {
		posterPath = series.PosterPath
	}
	releaseDate := ep.ReleaseDate
	if releaseDate == "" && series != nil {
		releaseDate = series.FirstAirDate
	}
	title := ep.Name
	if title == "" && series != nil {
		title = fmt.Sprintf("%s - S%02dE%02d", series.Name, season, episode)
	} else if series != nil {
		title = fmt.Sprintf("%s - S%02dE%02d - %s", series.Name, season, episode, ep.Name)
	}
	m := c.tmdbResultToMatch(TMDBResult{
		Overview:     ep.Overview,
		PosterPath:   posterPath,
		BackdropPath: ep.BackdropPath,
		ReleaseDate:  releaseDate,
		VoteAverage:  ep.VoteAverage,
	}, title, releaseDate)
	if m.BackdropURL == "" && series != nil {
		m.BackdropURL = tmdbImageURL(series.BackdropPath, "w500")
	}
	m.IMDbID, _ = c.getTVIMDbID(ctx, tvID)
	m.Provider = "tmdb"
	// Use series ID (tvID) so all episodes of the same show share one tmdb_id for grouping.
	m.ExternalID = strconv.Itoa(tvID)
	return &m, nil
}

func (c *TMDBClient) tmdbResultToMatch(r TMDBResult, title, releaseDate string) MatchResult {
	return MatchResult{
		Title:       title,
		Overview:    r.Overview,
		PosterURL:   tmdbImageURL(r.PosterPath, "w500"),
		BackdropURL: tmdbImageURL(r.BackdropPath, "w500"),
		ReleaseDate: releaseDate,
		VoteAverage: r.VoteAverage,
		Provider:    "tmdb",
		ExternalID:  strconv.Itoa(r.ID),
	}
}

func (c *TMDBClient) getTVDetails(id int) (*TMDBResult, error) {
	c.mu.RLock()
	if cached, ok := c.tvDetails[id]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()
	u := fmt.Sprintf("%s/tv/%d?api_key=%s", tmdbBaseURL, id, c.APIKey)
	resp, err := doCachedJSONRequest(context.Background(), http.DefaultClient, c.cache, "tmdb", http.MethodGet, u, nil, nil, 7*24*time.Hour, 1)
	if err != nil {
		return nil, err
	}
	var res TMDBResult
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.tvDetails[id] = &res
	c.mu.Unlock()
	return &res, nil
}

func (c *TMDBClient) getMovieDetails(id int) (*TMDBResult, error) {
	c.mu.RLock()
	if cached, ok := c.movieDetails[id]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()
	u := fmt.Sprintf("%s/movie/%d?api_key=%s", tmdbBaseURL, id, c.APIKey)
	resp, err := doCachedJSONRequest(context.Background(), http.DefaultClient, c.cache, "tmdb", http.MethodGet, u, nil, nil, 7*24*time.Hour, 1)
	if err != nil {
		return nil, err
	}
	var res TMDBResult
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.movieDetails[id] = &res
	c.mu.Unlock()
	return &res, nil
}

// GetSeriesDetails returns TV series metadata by TMDB ID for the show-detail UI.
func (c *TMDBClient) GetSeriesDetails(ctx context.Context, tmdbID int) (*SeriesDetails, error) {
	detail, err := c.getTVDetails(tmdbID)
	if err != nil || detail == nil {
		return nil, err
	}
	imdbID, _ := c.getTVIMDbID(ctx, tmdbID)
	return &SeriesDetails{
		Name:         detail.Name,
		Overview:     detail.Overview,
		PosterPath:   tmdbImageURL(detail.PosterPath, "w500"),
		BackdropPath: tmdbImageURL(detail.BackdropPath, "w500"),
		FirstAirDate: detail.FirstAirDate,
		IMDbID:       imdbID,
	}, nil
}

func (c *TMDBClient) getMovieIMDbID(ctx context.Context, id int) (string, error) {
	c.mu.RLock()
	if cached, ok := c.movieIMDbIDs[id]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()
	imdbID, err := c.getIMDbID(ctx, fmt.Sprintf("%s/movie/%d/external_ids?api_key=%s", tmdbBaseURL, id, c.APIKey))
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.movieIMDbIDs[id] = imdbID
	c.mu.Unlock()
	return imdbID, nil
}

func (c *TMDBClient) getTVIMDbID(ctx context.Context, id int) (string, error) {
	c.mu.RLock()
	if cached, ok := c.tvIMDbIDs[id]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()
	imdbID, err := c.getIMDbID(ctx, fmt.Sprintf("%s/tv/%d/external_ids?api_key=%s", tmdbBaseURL, id, c.APIKey))
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.tvIMDbIDs[id] = imdbID
	c.mu.Unlock()
	return imdbID, nil
}

func (c *TMDBClient) getIMDbID(ctx context.Context, endpoint string) (string, error) {
	resp, err := doCachedJSONRequest(ctx, http.DefaultClient, c.cache, "tmdb", http.MethodGet, endpoint, nil, nil, 30*24*time.Hour, 1)
	if err != nil {
		return "", err
	}
	var payload tmdbExternalIDsResponse
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return "", err
	}
	return payload.IMDbID, nil
}

func (c *TMDBClient) getEpisodeDetails(tvID, season, episode int) (*TMDBResult, error) {
	u := fmt.Sprintf("%s/tv/%d/season/%d/episode/%d?api_key=%s", tmdbBaseURL, tvID, season, episode, c.APIKey)
	resp, err := doCachedJSONRequest(context.Background(), http.DefaultClient, c.cache, "tmdb", http.MethodGet, u, nil, nil, 7*24*time.Hour, 1)
	if err != nil {
		return nil, err
	}
	var res TMDBResult
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func tmdbImageURL(path, size string) string {
	if path == "" {
		return ""
	}
	if size == "" {
		size = "w500"
	}
	return fmt.Sprintf("%s/%s%s", tmdbImageBase, size, path)
}

// GetPosterURL returns the full TMDB poster URL for a path (e.g. from DB).
// Kept for backward compatibility with existing code that stores paths.
func GetPosterURL(path string, size string) string {
	return tmdbImageURL(path, size)
}
