package metadata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const tmdbBaseURL = "https://api.themoviedb.org/3"

type TMDBClient struct {
	APIKey string
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

type TMDBSearchResponse struct {
	Results []TMDBResult `json:"results"`
}

func NewTMDBClient(apiKey string) *TMDBClient {
	return &TMDBClient{APIKey: apiKey}
}

func (c *TMDBClient) SearchTV(query string) ([]TMDBResult, error) {
	u := fmt.Sprintf("%s/search/tv?api_key=%s&query=%s", tmdbBaseURL, c.APIKey, url.QueryEscape(query))
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res TMDBSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Results, nil
}

func (c *TMDBClient) SearchMovie(query string) ([]TMDBResult, error) {
	u := fmt.Sprintf("%s/search/movie?api_key=%s&query=%s", tmdbBaseURL, c.APIKey, url.QueryEscape(query))
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res TMDBSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Results, nil
}

func (c *TMDBClient) GetTVDetails(id int) (*TMDBResult, error) {
	u := fmt.Sprintf("%s/tv/%d?api_key=%s", tmdbBaseURL, id, c.APIKey)
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res TMDBResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *TMDBClient) GetEpisodeDetails(tvID, season, episode int) (*TMDBResult, error) {
	u := fmt.Sprintf("%s/tv/%d/season/%d/episode/%d?api_key=%s", tmdbBaseURL, tvID, season, episode, c.APIKey)
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res TMDBResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

type MediaInfo struct {
	Title   string
	Season  int
	Episode int
	IsTV    bool
}

var (
	tvRegex1 = regexp.MustCompile(`(?i)(.*?)s(\d+)e(\d+)`)
	tvRegex2 = regexp.MustCompile(`(?i)(.*?)(\d+)x(\d+)`)
)

func ParseFilename(filename string) MediaInfo {
	filename = strings.ReplaceAll(filename, ".", " ")
	filename = strings.ReplaceAll(filename, "_", " ")

	if m := tvRegex1.FindStringSubmatch(filename); len(m) == 4 {
		s, _ := strconv.Atoi(m[2])
		e, _ := strconv.Atoi(m[3])
		return MediaInfo{
			Title:   strings.TrimSpace(m[1]),
			Season:  s,
			Episode: e,
			IsTV:    true,
		}
	}

	if m := tvRegex2.FindStringSubmatch(filename); len(m) == 4 {
		s, _ := strconv.Atoi(m[2])
		e, _ := strconv.Atoi(m[3])
		return MediaInfo{
			Title:   strings.TrimSpace(m[1]),
			Season:  s,
			Episode: e,
			IsTV:    true,
		}
	}

	return MediaInfo{
		Title: strings.TrimSpace(filename),
		IsTV:  false,
	}
}

func GetPosterURL(path string, size string) string {
	if path == "" {
		return ""
	}
	if size == "" {
		size = "w500"
	}
	return fmt.Sprintf("https://image.tmdb.org/t/p/%s%s", size, path)
}
