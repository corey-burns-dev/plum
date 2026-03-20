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

const (
	musicBrainzBaseURL  = "https://musicbrainz.org/ws/2"
	coverArtArchiveBase = "https://coverartarchive.org"
)

type MusicBrainzClient struct {
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string

	mu            sync.Mutex
	nextRequestAt time.Time
}

type musicBrainzSearchResponse struct {
	Recordings []musicBrainzRecording `json:"recordings"`
}

type musicBrainzRecording struct {
	ID               string                  `json:"id"`
	Score            int                     `json:"score"`
	Title            string                  `json:"title"`
	FirstReleaseDate string                  `json:"first-release-date"`
	ArtistCredit     []musicBrainzNameCredit `json:"artist-credit"`
	Releases         []musicBrainzRelease    `json:"releases"`
}

type musicBrainzNameCredit struct {
	Name   string               `json:"name"`
	Artist musicBrainzArtistRef `json:"artist"`
}

type musicBrainzArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type musicBrainzRelease struct {
	ID           string                     `json:"id"`
	Title        string                     `json:"title"`
	Date         string                     `json:"date"`
	ReleaseGroup musicBrainzReleaseGroupRef `json:"release-group"`
}

type musicBrainzReleaseGroupRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func NewMusicBrainzClient(contactURL string) *MusicBrainzClient {
	userAgent := "Plum/0.1"
	contactURL = strings.TrimSpace(contactURL)
	if contactURL != "" {
		userAgent += " (" + contactURL + ")"
	}
	return &MusicBrainzClient{
		BaseURL:    musicBrainzBaseURL,
		HTTPClient: http.DefaultClient,
		UserAgent:  userAgent,
	}
}

func (c *MusicBrainzClient) IdentifyMusic(ctx context.Context, info MusicInfo) *MusicMatchResult {
	if c == nil || strings.TrimSpace(info.Title) == "" {
		return nil
	}
	recordings, err := c.searchRecordings(ctx, info)
	if err != nil || len(recordings) == 0 {
		return nil
	}
	best := bestMusicBrainzRecording(info, recordings)
	if best == nil {
		return nil
	}
	release := chooseMusicBrainzRelease(info, best.Releases)
	artistName, artistID := musicBrainzPrimaryArtist(best.ArtistCredit)
	albumTitle := ""
	releaseYear := parseMusicBrainzYear(best.FirstReleaseDate)
	releaseID := ""
	releaseGroupID := ""
	if release != nil {
		albumTitle = strings.TrimSpace(release.Title)
		if year := parseMusicBrainzYear(release.Date); year > 0 {
			releaseYear = year
		}
		releaseID = strings.TrimSpace(release.ID)
		releaseGroupID = strings.TrimSpace(release.ReleaseGroup.ID)
		if albumTitle == "" {
			albumTitle = strings.TrimSpace(release.ReleaseGroup.Title)
		}
	}
	if albumTitle == "" {
		albumTitle = strings.TrimSpace(info.Album)
	}
	if releaseYear == 0 {
		releaseYear = info.ReleaseYear
	}
	if artistName == "" {
		artistName = firstNonEmptyMusic(info.Artist, info.AlbumArtist)
	}
	return &MusicMatchResult{
		Title:          firstNonEmptyMusic(strings.TrimSpace(best.Title), info.Title),
		Artist:         artistName,
		Album:          albumTitle,
		AlbumArtist:    firstNonEmptyMusic(artistName, info.AlbumArtist),
		PosterURL:      musicBrainzCoverArtURL(releaseGroupID, releaseID),
		ReleaseYear:    releaseYear,
		DiscNumber:     info.DiscNumber,
		TrackNumber:    info.TrackNumber,
		Provider:       "musicbrainz",
		RecordingID:    strings.TrimSpace(best.ID),
		ReleaseID:      releaseID,
		ReleaseGroupID: releaseGroupID,
		ArtistID:       artistID,
	}
}

func (c *MusicBrainzClient) searchRecordings(ctx context.Context, info MusicInfo) ([]musicBrainzRecording, error) {
	query := buildMusicBrainzRecordingQuery(info)
	if query == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("query", query)
	values.Set("fmt", "json")
	values.Set("limit", "5")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/recording?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if err := c.waitTurn(ctx); err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("musicbrainz search failed: %s", resp.Status)
	}
	var payload musicBrainzSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Recordings, nil
}

func (c *MusicBrainzClient) client() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *MusicBrainzClient) waitTurn(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	start := c.nextRequestAt
	if start.Before(now) {
		start = now
	}
	c.nextRequestAt = start.Add(time.Second)
	c.mu.Unlock()

	wait := time.Until(start)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func buildMusicBrainzRecordingQuery(info MusicInfo) string {
	var parts []string
	if title := strings.TrimSpace(info.Title); title != "" {
		parts = append(parts, `recording:"`+escapeMusicBrainzQuery(title)+`"`)
	}
	if artist := strings.TrimSpace(firstNonEmptyMusic(info.Artist, info.AlbumArtist)); artist != "" {
		parts = append(parts, `artist:"`+escapeMusicBrainzQuery(artist)+`"`)
	}
	if album := strings.TrimSpace(info.Album); album != "" {
		parts = append(parts, `release:"`+escapeMusicBrainzQuery(album)+`"`)
	}
	return strings.Join(parts, " AND ")
}

func escapeMusicBrainzQuery(value string) string {
	replacer := strings.NewReplacer(
		`\\`, `\\\\`,
		`"`, `\"`,
	)
	return replacer.Replace(strings.TrimSpace(value))
}

func bestMusicBrainzRecording(info MusicInfo, recordings []musicBrainzRecording) *musicBrainzRecording {
	if len(recordings) == 0 {
		return nil
	}
	bestIdx := -1
	bestScore := -1
	for i := range recordings {
		score := scoreMusicBrainzRecording(info, recordings[i])
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 || bestScore < 0 {
		return nil
	}
	return &recordings[bestIdx]
}

func scoreMusicBrainzRecording(info MusicInfo, recording musicBrainzRecording) int {
	score := recording.Score * 10
	title := normalizeMusicBrainzValue(info.Title)
	if title != "" {
		recordingTitle := normalizeMusicBrainzValue(recording.Title)
		switch {
		case recordingTitle == title:
			score += 100
		case strings.Contains(recordingTitle, title) || strings.Contains(title, recordingTitle):
			score += 35
		}
	}
	artist := normalizeMusicBrainzValue(firstNonEmptyMusic(info.Artist, info.AlbumArtist))
	if artist != "" {
		recordingArtist := normalizeMusicBrainzValue(musicBrainzPrimaryArtistName(recording.ArtistCredit))
		switch {
		case recordingArtist == artist:
			score += 90
		case strings.Contains(recordingArtist, artist) || strings.Contains(artist, recordingArtist):
			score += 30
		}
	}
	album := normalizeMusicBrainzValue(info.Album)
	if album != "" {
		release := chooseMusicBrainzRelease(info, recording.Releases)
		releaseTitle := ""
		if release != nil {
			releaseTitle = firstNonEmptyMusic(release.Title, release.ReleaseGroup.Title)
		}
		switch normalizeMusicBrainzValue(releaseTitle) {
		case album:
			score += 80
		case "":
		default:
			if strings.Contains(normalizeMusicBrainzValue(releaseTitle), album) || strings.Contains(album, normalizeMusicBrainzValue(releaseTitle)) {
				score += 25
			}
		}
	}
	if info.ReleaseYear > 0 {
		year := parseMusicBrainzYear(recording.FirstReleaseDate)
		if release := chooseMusicBrainzRelease(info, recording.Releases); release != nil {
			if releaseYear := parseMusicBrainzYear(release.Date); releaseYear > 0 {
				year = releaseYear
			}
		}
		if year == info.ReleaseYear {
			score += 30
		}
	}
	return score
}

func chooseMusicBrainzRelease(info MusicInfo, releases []musicBrainzRelease) *musicBrainzRelease {
	if len(releases) == 0 {
		return nil
	}
	album := normalizeMusicBrainzValue(info.Album)
	bestIdx := 0
	bestScore := -1
	for i := range releases {
		score := 0
		title := normalizeMusicBrainzValue(firstNonEmptyMusic(releases[i].Title, releases[i].ReleaseGroup.Title))
		if album != "" {
			switch {
			case title == album:
				score += 100
			case strings.Contains(title, album) || strings.Contains(album, title):
				score += 25
			}
		}
		if info.ReleaseYear > 0 {
			if year := parseMusicBrainzYear(releases[i].Date); year == info.ReleaseYear {
				score += 20
			}
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return &releases[bestIdx]
}

func musicBrainzPrimaryArtist(credits []musicBrainzNameCredit) (string, string) {
	for _, credit := range credits {
		if name := strings.TrimSpace(credit.Artist.Name); name != "" {
			return name, strings.TrimSpace(credit.Artist.ID)
		}
		if name := strings.TrimSpace(credit.Name); name != "" {
			return name, strings.TrimSpace(credit.Artist.ID)
		}
	}
	return "", ""
}

func musicBrainzPrimaryArtistName(credits []musicBrainzNameCredit) string {
	name, _ := musicBrainzPrimaryArtist(credits)
	return name
}

func musicBrainzCoverArtURL(releaseGroupID, releaseID string) string {
	if releaseGroupID != "" {
		return coverArtArchiveBase + "/release-group/" + releaseGroupID + "/front-250"
	}
	if releaseID != "" {
		return coverArtArchiveBase + "/release/" + releaseID + "/front-250"
	}
	return ""
}

func parseMusicBrainzYear(value string) int {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return 0
	}
	year, err := strconv.Atoi(value[:4])
	if err != nil {
		return 0
	}
	return year
}

func normalizeMusicBrainzValue(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func firstNonEmptyMusic(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
