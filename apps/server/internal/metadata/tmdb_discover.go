package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	tmdbDiscoverShelfTTL  = 6 * time.Hour
	tmdbDiscoverDetailTTL = 24 * time.Hour
)

type tmdbDiscoverListItem struct {
	ID           int     `json:"id"`
	MediaType    string  `json:"media_type,omitempty"`
	Name         string  `json:"name,omitempty"`
	Title        string  `json:"title,omitempty"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	ReleaseDate  string  `json:"release_date,omitempty"`
	FirstAirDate string  `json:"first_air_date,omitempty"`
	VoteAverage  float64 `json:"vote_average"`
}

type tmdbDiscoverListResponse struct {
	Results []tmdbDiscoverListItem `json:"results"`
}

type tmdbGenre struct {
	Name string `json:"name"`
}

type tmdbVideo struct {
	Name     string `json:"name"`
	Site     string `json:"site"`
	Key      string `json:"key"`
	Type     string `json:"type"`
	Official bool   `json:"official"`
}

type tmdbVideosResponse struct {
	Results []tmdbVideo `json:"results"`
}

type tmdbMovieDetailResponse struct {
	ID           int                `json:"id"`
	Title        string             `json:"title"`
	Overview     string             `json:"overview"`
	PosterPath   string             `json:"poster_path"`
	BackdropPath string             `json:"backdrop_path"`
	ReleaseDate  string             `json:"release_date"`
	VoteAverage  float64            `json:"vote_average"`
	Status       string             `json:"status"`
	Runtime      int                `json:"runtime"`
	Genres       []tmdbGenre        `json:"genres"`
	Videos       tmdbVideosResponse `json:"videos"`
}

type tmdbTVDetailResponse struct {
	ID               int                `json:"id"`
	Name             string             `json:"name"`
	Overview         string             `json:"overview"`
	PosterPath       string             `json:"poster_path"`
	BackdropPath     string             `json:"backdrop_path"`
	FirstAirDate     string             `json:"first_air_date"`
	VoteAverage      float64            `json:"vote_average"`
	Status           string             `json:"status"`
	EpisodeRunTime   []int              `json:"episode_run_time"`
	NumberOfSeasons  int                `json:"number_of_seasons"`
	NumberOfEpisodes int                `json:"number_of_episodes"`
	Genres           []tmdbGenre        `json:"genres"`
	Videos           tmdbVideosResponse `json:"videos"`
}

type tmdbDiscoverHTTPError struct {
	StatusCode int
}

func (e *tmdbDiscoverHTTPError) Error() string {
	return fmt.Sprintf("tmdb discover: status %d", e.StatusCode)
}

func isTMDBDiscoverHTTPStatus(err error, statusCode int) bool {
	var httpErr *tmdbDiscoverHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == statusCode
}

func (c *TMDBClient) GetDiscover(ctx context.Context) (*DiscoverResponse, error) {
	if err := c.requireTMDB(); err != nil {
		return nil, err
	}

	trending, err := c.fetchDiscoverList(ctx, "/trending/all/day", "", tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}
	popularMovies, err := c.fetchDiscoverList(ctx, "/movie/popular", string(DiscoverMediaTypeMovie), tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}
	nowPlaying, err := c.fetchDiscoverList(ctx, "/movie/now_playing", string(DiscoverMediaTypeMovie), tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}
	upcoming, err := c.fetchDiscoverList(ctx, "/movie/upcoming", string(DiscoverMediaTypeMovie), tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}
	popularTV, err := c.fetchDiscoverList(ctx, "/tv/popular", string(DiscoverMediaTypeTV), tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}
	onTheAir, err := c.fetchDiscoverList(ctx, "/tv/on_the_air", string(DiscoverMediaTypeTV), tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}
	topRatedMovies, err := c.fetchDiscoverList(ctx, "/movie/top_rated", string(DiscoverMediaTypeMovie), tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}
	topRatedTV, err := c.fetchDiscoverList(ctx, "/tv/top_rated", string(DiscoverMediaTypeTV), tmdbDiscoverShelfTTL)
	if err != nil {
		return nil, err
	}

	return &DiscoverResponse{
		Shelves: []DiscoverShelf{
			{ID: "trending", Title: "Trending Now", Items: trending},
			{ID: "popular-movies", Title: "Popular Movies", Items: popularMovies},
			{ID: "now-playing", Title: "Now Playing", Items: nowPlaying},
			{ID: "upcoming", Title: "Upcoming Movies", Items: upcoming},
			{ID: "popular-tv", Title: "Popular TV", Items: popularTV},
			{ID: "on-the-air", Title: "On The Air", Items: onTheAir},
			{ID: "top-rated", Title: "Top Rated Picks", Items: interleaveDiscoverItems(topRatedMovies, topRatedTV, 20)},
		},
	}, nil
}

func (c *TMDBClient) SearchDiscover(ctx context.Context, query string) (*DiscoverSearchResponse, error) {
	if err := c.requireTMDB(); err != nil {
		return nil, err
	}
	if len(query) < 2 {
		return &DiscoverSearchResponse{Movies: []DiscoverItem{}, TV: []DiscoverItem{}}, nil
	}

	movies, err := c.fetchSearchDiscoverList(ctx, "/search/movie", query, string(DiscoverMediaTypeMovie))
	if err != nil {
		return nil, err
	}
	tv, err := c.fetchSearchDiscoverList(ctx, "/search/tv", query, string(DiscoverMediaTypeTV))
	if err != nil {
		return nil, err
	}

	return &DiscoverSearchResponse{
		Movies: movies,
		TV:     tv,
	}, nil
}

func (c *TMDBClient) GetDiscoverTitleDetails(ctx context.Context, mediaType DiscoverMediaType, tmdbID int) (*DiscoverTitleDetails, error) {
	if err := c.requireTMDB(); err != nil {
		return nil, err
	}
	if tmdbID <= 0 {
		return nil, nil
	}

	switch mediaType {
	case DiscoverMediaTypeMovie:
		var payload tmdbMovieDetailResponse
		if err := c.fetchJSON(ctx, c.discoverURL(fmt.Sprintf("/movie/%d", tmdbID), map[string]string{
			"append_to_response": "videos",
		}), tmdbDiscoverDetailTTL, &payload); err != nil {
			if isTMDBDiscoverHTTPStatus(err, http.StatusNotFound) {
				return nil, nil
			}
			return nil, err
		}
		imdbID, _ := c.getMovieIMDbID(ctx, tmdbID)
		return &DiscoverTitleDetails{
			MediaType:    DiscoverMediaTypeMovie,
			TMDBID:       payload.ID,
			Title:        payload.Title,
			Overview:     payload.Overview,
			PosterPath:   payload.PosterPath,
			BackdropPath: payload.BackdropPath,
			ReleaseDate:  payload.ReleaseDate,
			VoteAverage:  payload.VoteAverage,
			IMDbID:       imdbID,
			Status:       payload.Status,
			Genres:       tmdbGenresToNames(payload.Genres),
			Runtime:      payload.Runtime,
			Videos:       tmdbVideosToDiscover(payload.Videos.Results),
		}, nil
	case DiscoverMediaTypeTV:
		var payload tmdbTVDetailResponse
		if err := c.fetchJSON(ctx, c.discoverURL(fmt.Sprintf("/tv/%d", tmdbID), map[string]string{
			"append_to_response": "videos",
		}), tmdbDiscoverDetailTTL, &payload); err != nil {
			if isTMDBDiscoverHTTPStatus(err, http.StatusNotFound) {
				return nil, nil
			}
			return nil, err
		}
		imdbID, _ := c.getTVIMDbID(ctx, tmdbID)
		return &DiscoverTitleDetails{
			MediaType:        DiscoverMediaTypeTV,
			TMDBID:           payload.ID,
			Title:            payload.Name,
			Overview:         payload.Overview,
			PosterPath:       payload.PosterPath,
			BackdropPath:     payload.BackdropPath,
			FirstAirDate:     payload.FirstAirDate,
			VoteAverage:      payload.VoteAverage,
			IMDbID:           imdbID,
			Status:           payload.Status,
			Genres:           tmdbGenresToNames(payload.Genres),
			Runtime:          firstInt(payload.EpisodeRunTime),
			NumberOfSeasons:  payload.NumberOfSeasons,
			NumberOfEpisodes: payload.NumberOfEpisodes,
			Videos:           tmdbVideosToDiscover(payload.Videos.Results),
		}, nil
	default:
		return nil, nil
	}
}

func (c *TMDBClient) requireTMDB() error {
	if c == nil || c.APIKey == "" {
		return ErrTMDBNotConfigured
	}
	return nil
}

func (c *TMDBClient) discoverURL(path string, params map[string]string) string {
	values := url.Values{}
	values.Set("api_key", c.APIKey)
	for key, value := range params {
		if value != "" {
			values.Set(key, value)
		}
	}
	return fmt.Sprintf("%s%s?%s", c.resolveBaseURL(), path, values.Encode())
}

func (c *TMDBClient) resolveBaseURL() string {
	if c != nil && c.baseURL != "" {
		return c.baseURL
	}
	return tmdbBaseURL
}

func (c *TMDBClient) fetchDiscoverList(ctx context.Context, path string, fallbackType string, ttl time.Duration) ([]DiscoverItem, error) {
	var payload tmdbDiscoverListResponse
	if err := c.fetchJSON(ctx, c.discoverURL(path, nil), ttl, &payload); err != nil {
		return nil, err
	}
	return mapTMDBDiscoverItems(payload.Results, fallbackType), nil
}

func (c *TMDBClient) fetchSearchDiscoverList(ctx context.Context, path string, query string, fallbackType string) ([]DiscoverItem, error) {
	var payload tmdbDiscoverListResponse
	if err := c.fetchJSON(ctx, c.discoverURL(path, map[string]string{
		"query": query,
	}), tmdbDiscoverShelfTTL, &payload); err != nil {
		return nil, err
	}
	return mapTMDBDiscoverItems(payload.Results, fallbackType), nil
}

func (c *TMDBClient) fetchJSON(ctx context.Context, rawURL string, ttl time.Duration, dest any) error {
	resp, err := doCachedJSONRequest(ctx, http.DefaultClient, c.cache, "tmdb", http.MethodGet, rawURL, nil, nil, ttl, 1)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &tmdbDiscoverHTTPError{StatusCode: resp.StatusCode}
	}
	return json.Unmarshal(resp.Body, dest)
}

func mapTMDBDiscoverItems(items []tmdbDiscoverListItem, fallbackType string) []DiscoverItem {
	out := make([]DiscoverItem, 0, len(items))
	for _, item := range items {
		mapped, ok := mapTMDBDiscoverItem(item, fallbackType)
		if !ok {
			continue
		}
		out = append(out, mapped)
		if len(out) == 20 {
			break
		}
	}
	return out
}

func mapTMDBDiscoverItem(item tmdbDiscoverListItem, fallbackType string) (DiscoverItem, bool) {
	mediaType := item.MediaType
	if mediaType == "" {
		mediaType = fallbackType
	}
	if mediaType != string(DiscoverMediaTypeMovie) && mediaType != string(DiscoverMediaTypeTV) {
		return DiscoverItem{}, false
	}

	title := item.Title
	if mediaType == string(DiscoverMediaTypeTV) && title == "" {
		title = item.Name
	}
	if title == "" {
		title = item.Name
	}
	if title == "" || item.ID <= 0 {
		return DiscoverItem{}, false
	}

	return DiscoverItem{
		MediaType:    DiscoverMediaType(mediaType),
		TMDBID:       item.ID,
		Title:        title,
		Overview:     item.Overview,
		PosterPath:   item.PosterPath,
		BackdropPath: item.BackdropPath,
		ReleaseDate:  item.ReleaseDate,
		FirstAirDate: item.FirstAirDate,
		VoteAverage:  item.VoteAverage,
	}, true
}

func interleaveDiscoverItems(primary []DiscoverItem, secondary []DiscoverItem, limit int) []DiscoverItem {
	if limit <= 0 {
		return []DiscoverItem{}
	}
	out := make([]DiscoverItem, 0, limit)
	for i := 0; len(out) < limit && (i < len(primary) || i < len(secondary)); i++ {
		if i < len(primary) {
			out = append(out, primary[i])
			if len(out) == limit {
				break
			}
		}
		if i < len(secondary) {
			out = append(out, secondary[i])
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

func tmdbGenresToNames(genres []tmdbGenre) []string {
	if len(genres) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(genres))
	for _, genre := range genres {
		if genre.Name == "" {
			continue
		}
		out = append(out, genre.Name)
	}
	return out
}

func tmdbVideosToDiscover(videos []tmdbVideo) []DiscoverTitleVideo {
	if len(videos) == 0 {
		return []DiscoverTitleVideo{}
	}
	out := make([]DiscoverTitleVideo, 0, len(videos))
	for _, video := range videos {
		if video.Key == "" || video.Site == "" {
			continue
		}
		out = append(out, DiscoverTitleVideo{
			Name:     video.Name,
			Site:     video.Site,
			Key:      video.Key,
			Type:     video.Type,
			Official: video.Official,
		})
	}
	return out
}

func firstInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return values[0]
}
