package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const tvdbBaseURL = "https://api4.thetvdb.com/v4"

// TVDBClient implements TVProvider for TheTVDB API v4.
type TVDBClient struct {
	APIKey string
	Pin    string // optional, for user-supported keys
	cache  ProviderCache

	mu    sync.Mutex
	token string
	exp   time.Time
}

func (c *TVDBClient) ProviderName() string {
	return "tvdb"
}

// NewTVDBClient returns a TVDB client. If apiKey is empty, all methods will return no results.
func NewTVDBClient(apiKey, pin string) *TVDBClient {
	return &TVDBClient{APIKey: apiKey, Pin: pin}
}

func (c *TVDBClient) SetCache(cache ProviderCache) {
	c.cache = cache
}

type tvdbLoginRequest struct {
	APIKey string `json:"apikey"`
	Pin    string `json:"pin,omitempty"`
}

type tvdbLoginResponse struct {
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
}

func (c *TVDBClient) ensureToken(ctx context.Context) error {
	if c.APIKey == "" {
		return fmt.Errorf("tvdb: no api key")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.exp) > 5*time.Minute {
		return nil
	}
	body, _ := json.Marshal(tvdbLoginRequest{APIKey: c.APIKey, Pin: c.Pin})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tvdbBaseURL+"/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tvdb login: %s", resp.Status)
	}
	var login tvdbLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		return err
	}
	c.token = login.Data.Token
	c.exp = time.Now().Add(30 * 24 * time.Hour) // 1 month
	return nil
}

func (c *TVDBClient) do(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	u := tvdbBaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return http.DefaultClient.Do(req)
}

type tvdbSearchResponse struct {
	Data []tvdbSearchResult `json:"data"`
}

type tvdbSearchResult struct {
	ID        string `json:"id"`
	TVDBID    string `json:"tvdb_id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Overview  string `json:"overview"`
	ImageURL  string `json:"image_url"`
	Poster    string `json:"poster"`
	Thumbnail string `json:"thumbnail"`
	Year      string `json:"year"`
	Type      string `json:"type"`
}

func (c *TVDBClient) SearchTV(ctx context.Context, query string) ([]MatchResult, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("type", "series")
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	rawURL := tvdbBaseURL + "/search?" + q.Encode()
	resp, err := doCachedJSONRequest(ctx, http.DefaultClient, c.cache, "tvdb", http.MethodGet, rawURL, nil, map[string]string{
		"Authorization": "Bearer " + c.token,
	}, 24*time.Hour, 1)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return nil, fmt.Errorf("tvdb: unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tvdb search: status %d", resp.StatusCode)
	}
	var search tvdbSearchResponse
	if err := json.Unmarshal(resp.Body, &search); err != nil {
		return nil, err
	}
	out := make([]MatchResult, 0, len(search.Data))
	for _, r := range search.Data {
		id := r.ID
		if id == "" {
			id = r.TVDBID
		}
		title := r.Name
		if title == "" {
			title = r.Title
		}
		img := r.ImageURL
		if img == "" {
			img = r.Poster
		}
		if img == "" {
			img = r.Thumbnail
		}
		if img != "" && img[0] == '/' {
			img = "https://artworks.thetvdb.com" + img
		}
		out = append(out, MatchResult{
			Title:       title,
			Overview:    r.Overview,
			PosterURL:   img,
			ReleaseDate: r.Year,
			Provider:    "tvdb",
			ExternalID:  id,
		})
	}
	return out, nil
}

type tvdbSeriesEpisodesResponse struct {
	Data struct {
		Episodes []tvdbEpisode `json:"episodes"`
		Series   *tvdbSeries   `json:"series"`
	} `json:"data"`
}

type tvdbSeries struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	FirstAired string `json:"firstAired"`
}

type tvdbEpisode struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Aired        string `json:"aired"`
	Image        string `json:"image"`
	SeasonNumber int    `json:"seasonNumber"`
	Number       int    `json:"number"`
}

func (c *TVDBClient) GetEpisode(ctx context.Context, seriesID string, season, episode int) (*MatchResult, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("page", "0")
	if season > 0 {
		q.Set("season", strconv.Itoa(season))
	}
	if episode > 0 {
		q.Set("episodeNumber", strconv.Itoa(episode))
	}
	path := "/series/" + url.PathEscape(seriesID) + "/episodes/default"
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	rawURL := tvdbBaseURL + path + "?" + q.Encode()
	resp, err := doCachedJSONRequest(ctx, http.DefaultClient, c.cache, "tvdb", http.MethodGet, rawURL, nil, map[string]string{
		"Authorization": "Bearer " + c.token,
	}, 7*24*time.Hour, 1)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return nil, fmt.Errorf("tvdb: unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tvdb episodes: status %d", resp.StatusCode)
	}
	var epResp tvdbSeriesEpisodesResponse
	if err := json.Unmarshal(resp.Body, &epResp); err != nil {
		return nil, err
	}
	series := epResp.Data.Series
	episodes := epResp.Data.Episodes
	var ep *tvdbEpisode
	for i := range episodes {
		e := &episodes[i]
		if (season <= 0 || e.SeasonNumber == season) && (episode <= 0 || e.Number == episode) {
			ep = e
			break
		}
	}
	if ep == nil && len(episodes) > 0 {
		ep = &episodes[0]
	}
	if ep == nil {
		return nil, nil
	}
	title := ep.Name
	if series != nil {
		title = fmt.Sprintf("%s - S%02dE%02d - %s", series.Name, season, episode, ep.Name)
	}
	posterURL := ep.Image
	if posterURL == "" && series != nil && series.Image != "" {
		posterURL = series.Image
	}
	if posterURL != "" && posterURL[0] == '/' {
		posterURL = "https://artworks.thetvdb.com" + posterURL
	}
	externalID := seriesID
	if series != nil && series.ID > 0 {
		externalID = strconv.Itoa(series.ID)
	}
	return &MatchResult{
		Title:       title,
		Overview:    ep.Overview,
		PosterURL:   posterURL,
		BackdropURL: posterURL,
		ReleaseDate: ep.Aired,
		Provider:    "tvdb",
		ExternalID:  externalID,
	}, nil
}
