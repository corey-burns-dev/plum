package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"plum/internal/db"
	"plum/internal/metadata"

	_ "modernc.org/sqlite"
)

func TestUpdateLibraryPlaybackPreferences(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"test@test.com",
		"hash",
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"Anime",
		db.LibraryTypeAnime,
		"/anime",
		now,
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	handler := &LibraryHandler{DB: dbConn}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/libraries/"+strconv.Itoa(libraryID)+"/playback-preferences",
		strings.NewReader(`{"preferred_audio_language":"ja","preferred_subtitle_language":"en","subtitles_enabled_by_default":true}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.UpdateLibraryPlaybackPreferences(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		PreferredAudioLanguage    string `json:"preferred_audio_language"`
		PreferredSubtitleLanguage string `json:"preferred_subtitle_language"`
		SubtitlesEnabledByDefault bool   `json:"subtitles_enabled_by_default"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.PreferredAudioLanguage != "ja" {
		t.Fatalf("preferred_audio_language = %q", payload.PreferredAudioLanguage)
	}
	if payload.PreferredSubtitleLanguage != "en" {
		t.Fatalf("preferred_subtitle_language = %q", payload.PreferredSubtitleLanguage)
	}
	if !payload.SubtitlesEnabledByDefault {
		t.Fatalf("subtitles_enabled_by_default = false")
	}

	var (
		preferredAudio    sql.NullString
		preferredSubtitle sql.NullString
		subtitlesEnabled  sql.NullBool
	)
	if err := dbConn.QueryRow(
		`SELECT preferred_audio_language, preferred_subtitle_language, subtitles_enabled_by_default FROM libraries WHERE id = ?`,
		libraryID,
	).Scan(&preferredAudio, &preferredSubtitle, &subtitlesEnabled); err != nil {
		t.Fatalf("query library: %v", err)
	}
	if preferredAudio.String != "ja" || preferredSubtitle.String != "en" || !subtitlesEnabled.Bool {
		t.Fatalf("unexpected library prefs: audio=%q subtitle=%q enabled=%v", preferredAudio.String, preferredSubtitle.String, subtitlesEnabled.Bool)
	}
}

func TestUpdateLibraryPlaybackPreferences_PreservesAutomationWhenOmitted(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"test@test.com",
		"hash",
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (
			user_id, name, type, path, preferred_audio_language, preferred_subtitle_language,
			subtitles_enabled_by_default, watcher_enabled, watcher_mode, scan_interval_minutes, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"TV",
		db.LibraryTypeTV,
		"/tv",
		"en",
		"en",
		true,
		true,
		db.LibraryWatcherModePoll,
		15,
		now,
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	handler := &LibraryHandler{DB: dbConn}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/libraries/"+strconv.Itoa(libraryID)+"/playback-preferences",
		strings.NewReader(`{"preferred_audio_language":"ja","preferred_subtitle_language":"fr","subtitles_enabled_by_default":false}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.UpdateLibraryPlaybackPreferences(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		WatcherEnabled      bool   `json:"watcher_enabled"`
		WatcherMode         string `json:"watcher_mode"`
		ScanIntervalMinutes int    `json:"scan_interval_minutes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.WatcherEnabled || payload.WatcherMode != db.LibraryWatcherModePoll || payload.ScanIntervalMinutes != 15 {
		t.Fatalf("unexpected automation response: %+v", payload)
	}

	var (
		watcherEnabled bool
		watcherMode    string
		scanInterval   int
	)
	if err := dbConn.QueryRow(
		`SELECT watcher_enabled, watcher_mode, scan_interval_minutes FROM libraries WHERE id = ?`,
		libraryID,
	).Scan(&watcherEnabled, &watcherMode, &scanInterval); err != nil {
		t.Fatalf("query library automation: %v", err)
	}
	if !watcherEnabled || watcherMode != db.LibraryWatcherModePoll || scanInterval != 15 {
		t.Fatalf("unexpected library automation: enabled=%v mode=%q interval=%d", watcherEnabled, watcherMode, scanInterval)
	}
}

type identifyStub struct {
	tv    func(context.Context, metadata.MediaInfo) *metadata.MatchResult
	movie func(context.Context, metadata.MediaInfo) *metadata.MatchResult
	anime func(context.Context, metadata.MediaInfo) *metadata.MatchResult
}

type seriesQueryStub struct {
	searchTV   func(context.Context, string) ([]metadata.MatchResult, error)
	getEpisode func(context.Context, string, string, int, int) (*metadata.MatchResult, error)
}

type seriesDetailsStub struct {
	getSeriesDetails func(context.Context, int) (*metadata.SeriesDetails, error)
}

type discoverStub struct {
	getDiscover            func(context.Context) (*metadata.DiscoverResponse, error)
	searchDiscover         func(context.Context, string) (*metadata.DiscoverSearchResponse, error)
	getDiscoverTitleDetail func(context.Context, metadata.DiscoverMediaType, int) (*metadata.DiscoverTitleDetails, error)
}

func (s *seriesQueryStub) SearchTV(ctx context.Context, query string) ([]metadata.MatchResult, error) {
	if s.searchTV == nil {
		return nil, nil
	}
	return s.searchTV(ctx, query)
}

func (s *seriesQueryStub) GetEpisode(
	ctx context.Context,
	provider string,
	seriesID string,
	season int,
	episode int,
) (*metadata.MatchResult, error) {
	if s.getEpisode == nil {
		return nil, nil
	}
	return s.getEpisode(ctx, provider, seriesID, season, episode)
}

func (s *seriesDetailsStub) GetSeriesDetails(ctx context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
	if s.getSeriesDetails == nil {
		return nil, nil
	}
	return s.getSeriesDetails(ctx, tmdbID)
}

func (s *discoverStub) GetDiscover(ctx context.Context) (*metadata.DiscoverResponse, error) {
	if s.getDiscover == nil {
		return nil, nil
	}
	return s.getDiscover(ctx)
}

func (s *discoverStub) SearchDiscover(ctx context.Context, query string) (*metadata.DiscoverSearchResponse, error) {
	if s.searchDiscover == nil {
		return nil, nil
	}
	return s.searchDiscover(ctx, query)
}

func (s *discoverStub) GetDiscoverTitleDetails(ctx context.Context, mediaType metadata.DiscoverMediaType, tmdbID int) (*metadata.DiscoverTitleDetails, error) {
	if s.getDiscoverTitleDetail == nil {
		return nil, nil
	}
	return s.getDiscoverTitleDetail(ctx, mediaType, tmdbID)
}

func (s *identifyStub) IdentifyTV(ctx context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if s.tv == nil {
		return nil
	}
	return s.tv(ctx, info)
}

func (s *identifyStub) IdentifyAnime(ctx context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if s.anime == nil {
		return nil
	}
	return s.anime(ctx, info)
}

func (s *identifyStub) IdentifyMovie(ctx context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if s.movie == nil {
		return nil
	}
	return s.movie(ctx, info)
}

func TestIdentifyLibrary_UsesRelativeMovieParsing(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Movies", db.LibraryTypeMovie, "/movies", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var movieID int
	path := "/movies/Die My Love (2025)/Die My Love 2025 BluRay 1080p DD 5 1 x264-BHDStudio.mp4"
	if err := dbConn.QueryRow(`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`, libraryID, "Die My Love 2025 BluRay 1080p DD 5 1 x264-BHDStudio", path, 0, db.MatchStatusUnmatched).Scan(&movieID); err != nil {
		t.Fatalf("insert movie: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			movie: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				if info.Title != "die my love" {
					t.Fatalf("title = %q", info.Title)
				}
				if info.Year != 2025 {
					t.Fatalf("year = %d", info.Year)
				}
				return &metadata.MatchResult{
					Title:      "Die My Love",
					Provider:   "tmdb",
					ExternalID: "456",
				}
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 1 || payload.Failed != 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	var title, matchStatus string
	var tmdbID int
	if err := dbConn.QueryRow(`SELECT title, match_status, COALESCE(tmdb_id, 0) FROM movies WHERE id = ?`, movieID).Scan(&title, &matchStatus, &tmdbID); err != nil {
		t.Fatalf("query movie: %v", err)
	}
	if title != "Die My Love" {
		t.Fatalf("title = %q", title)
	}
	if matchStatus != db.MatchStatusIdentified {
		t.Fatalf("match_status = %q", matchStatus)
	}
	if tmdbID != 456 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
}

func TestIdentifyLibrary_UsesAnimeIdentifier(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Anime", db.LibraryTypeAnime, "/anime", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	path := "/anime/Frieren/Season 1/Frieren - S01E12.mkv"
	if err := dbConn.QueryRow(`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, libraryID, "Frieren - S01E12", path, 0, db.MatchStatusUnmatched, 1, 12).Scan(&episodeID); err != nil {
		t.Fatalf("insert anime episode: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			anime: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				if info.Title != "frieren" {
					t.Fatalf("title = %q", info.Title)
				}
				if info.Season != 1 || info.Episode != 12 {
					t.Fatalf("unexpected episode info: %+v", info)
				}
				return &metadata.MatchResult{
					Title:      "Frieren - S01E12 - Episode",
					Provider:   "tmdb",
					ExternalID: "123",
				}
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var title, matchStatus string
	if err := dbConn.QueryRow(`SELECT title, match_status FROM anime_episodes WHERE id = ?`, episodeID).Scan(&title, &matchStatus); err != nil {
		t.Fatalf("query anime episode: %v", err)
	}
	if title != "Frieren - S01E12 - Episode" {
		t.Fatalf("title = %q", title)
	}
	if matchStatus != db.MatchStatusIdentified {
		t.Fatalf("match_status = %q", matchStatus)
	}
}

func TestIdentifyLibrary_UsesAnimeSearchFallbackAndAutoConfirmsExactMatch(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Anime", db.LibraryTypeAnime, "/anime", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	path := "/anime/Frieren/Season 1/Frieren - S01E12.mkv"
	if err := dbConn.QueryRow(`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, libraryID, "Frieren - S01E12", path, 0, db.MatchStatusUnmatched, 1, 12).Scan(&episodeID); err != nil {
		t.Fatalf("insert anime episode: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			anime: func(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				if query != "Frieren" {
					t.Fatalf("query = %q", query)
				}
				return []metadata.MatchResult{
					{
						Title:      "Frieren",
						Provider:   "tmdb",
						ExternalID: "123",
					},
				}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				if provider != "tmdb" || seriesID != "123" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				if season != 1 || episode != 12 {
					t.Fatalf("unexpected episode = S%02dE%02d", season, episode)
				}
				return &metadata.MatchResult{
					Title:      "Frieren - S01E12 - Episode",
					Provider:   "tmdb",
					ExternalID: "123",
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 1 || payload.Failed != 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	var title, matchStatus string
	var tmdbID int
	var reviewNeeded bool
	if err := dbConn.QueryRow(`SELECT title, match_status, COALESCE(tmdb_id, 0), COALESCE(metadata_review_needed, 0) FROM anime_episodes WHERE id = ?`, episodeID).Scan(&title, &matchStatus, &tmdbID, &reviewNeeded); err != nil {
		t.Fatalf("query anime episode: %v", err)
	}
	if title != "Frieren - S01E12 - Episode" {
		t.Fatalf("title = %q", title)
	}
	if matchStatus != db.MatchStatusIdentified {
		t.Fatalf("match_status = %q", matchStatus)
	}
	if tmdbID != 123 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
	if reviewNeeded {
		t.Fatal("expected metadata_review_needed to be false")
	}
}

func TestIdentifyLibrary_UsesTVSearchFallbackAndAutoConfirmsExactMatch(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	path := "/tv/Slow Horses/Season 1/Slow Horses - S01E01.mkv"
	if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, libraryID, "Slow Horses - S01E01", path, 0, db.MatchStatusUnmatched, 1, 1).Scan(&episodeID); err != nil {
		t.Fatalf("insert tv episode: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			tv: func(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				if query != "Slow Horses" {
					t.Fatalf("query = %q", query)
				}
				return []metadata.MatchResult{
					{
						Title:      "Slow Horses",
						Provider:   "tmdb",
						ExternalID: "321",
					},
				}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				if provider != "tmdb" || seriesID != "321" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				if season != 1 || episode != 1 {
					t.Fatalf("unexpected episode = S%02dE%02d", season, episode)
				}
				return &metadata.MatchResult{
					Title:      "Slow Horses - S01E01 - Failure's Contagious",
					Provider:   "tmdb",
					ExternalID: "321",
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 1 || payload.Failed != 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	var title, matchStatus string
	var tmdbID int
	var reviewNeeded bool
	if err := dbConn.QueryRow(`SELECT title, match_status, COALESCE(tmdb_id, 0), COALESCE(metadata_review_needed, 0) FROM tv_episodes WHERE id = ?`, episodeID).Scan(&title, &matchStatus, &tmdbID, &reviewNeeded); err != nil {
		t.Fatalf("query tv episode: %v", err)
	}
	if title != "Slow Horses - S01E01 - Failure's Contagious" {
		t.Fatalf("title = %q", title)
	}
	if matchStatus != db.MatchStatusIdentified {
		t.Fatalf("match_status = %q", matchStatus)
	}
	if tmdbID != 321 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
	if reviewNeeded {
		t.Fatal("expected metadata_review_needed to be false")
	}
}

func TestIdentifyLibrary_UsesTVSearchFallbackAndMarksAmbiguousMatchForReview(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "ambiguous@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	path := "/tv/Slow Horses/Season 1/Slow Horses - S01E01.mkv"
	if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, libraryID, "Slow Horses - S01E01", path, 0, db.MatchStatusUnmatched, 1, 1).Scan(&episodeID); err != nil {
		t.Fatalf("insert tv episode: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			tv: func(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				if query != "Slow Horses" {
					t.Fatalf("query = %q", query)
				}
				return []metadata.MatchResult{
					{
						Title:      "Slow Horses",
						Provider:   "tmdb",
						ExternalID: "321",
					},
					{
						Title:      "Slow Horses",
						Provider:   "tmdb",
						ExternalID: "654",
					},
				}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				if provider != "tmdb" {
					t.Fatalf("unexpected provider = %q", provider)
				}
				if seriesID != "321" && seriesID != "654" {
					t.Fatalf("unexpected series id = %q", seriesID)
				}
				if season != 1 || episode != 1 {
					t.Fatalf("unexpected episode = S%02dE%02d", season, episode)
				}
				return &metadata.MatchResult{
					Title:      "Slow Horses - S01E01 - Failure's Contagious",
					Provider:   "tmdb",
					ExternalID: seriesID,
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 1 || payload.Failed != 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	var title, matchStatus string
	var tmdbID int
	var reviewNeeded bool
	if err := dbConn.QueryRow(`SELECT title, match_status, COALESCE(tmdb_id, 0), COALESCE(metadata_review_needed, 0) FROM tv_episodes WHERE id = ?`, episodeID).Scan(&title, &matchStatus, &tmdbID, &reviewNeeded); err != nil {
		t.Fatalf("query tv episode: %v", err)
	}
	if title != "Slow Horses - S01E01 - Failure's Contagious" {
		t.Fatalf("title = %q", title)
	}
	if matchStatus != db.MatchStatusIdentified {
		t.Fatalf("match_status = %q", matchStatus)
	}
	if tmdbID != 321 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
	if !reviewNeeded {
		t.Fatal("expected metadata_review_needed to be true")
	}
}

func TestIdentifyLibrary_AnimeSearchFallbackPrefersShowTitleFromEpisodeTitle(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Anime", db.LibraryTypeAnime, "/anime", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	path := "/anime/Fallback Folder/Season 1/Episode 12.mkv"
	if err := dbConn.QueryRow(`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, libraryID, "Correct Show - S01E12", path, 0, db.MatchStatusUnmatched, 1, 12).Scan(&episodeID); err != nil {
		t.Fatalf("insert anime episode: %v", err)
	}
	_ = episodeID

	searchCalls := make([]string, 0, 2)
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			anime: func(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				searchCalls = append(searchCalls, query)
				if query != "Correct Show" {
					return nil, nil
				}
				return []metadata.MatchResult{{Title: "Correct Show", Provider: "tmdb", ExternalID: "123"}}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				return &metadata.MatchResult{
					Title:      "Correct Show - S01E12 - Episode",
					Provider:   "tmdb",
					ExternalID: "123",
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(searchCalls) == 0 || searchCalls[0] != "Correct Show" {
		t.Fatalf("unexpected search calls: %#v", searchCalls)
	}
}

func TestIdentifyShow_ClearsMetadataReviewNeeded(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Anime", db.LibraryTypeAnime, "/anime", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	if err := dbConn.QueryRow(`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, tmdb_id, metadata_review_needed, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Frieren - S01E12 - Episode",
		"/anime/Frieren/Season 1/Frieren - S01E12.mkv",
		0,
		db.MatchStatusIdentified,
		123,
		true,
		1,
		12,
	).Scan(&episodeID); err != nil {
		t.Fatalf("insert anime episode: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?)`, db.LibraryTypeAnime, episodeID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		SeriesQuery: &seriesQueryStub{
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				if provider != "tmdb" || seriesID != "456" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				return &metadata.MatchResult{
					Title:      "Frieren - S01E12 - Episode",
					Provider:   "tmdb",
					ExternalID: "456",
				}, nil
			},
		},
	}

	body := strings.NewReader(`{"showKey":"tmdb-123","tmdbId":456}`)
	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/shows/identify", body)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyShow(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var reviewNeeded bool
	var metadataConfirmed bool
	var tmdbID int
	if err := dbConn.QueryRow(`SELECT COALESCE(metadata_review_needed, 0), COALESCE(metadata_confirmed, 0), COALESCE(tmdb_id, 0) FROM anime_episodes WHERE id = ?`, episodeID).Scan(&reviewNeeded, &metadataConfirmed, &tmdbID); err != nil {
		t.Fatalf("query anime episode: %v", err)
	}
	if reviewNeeded {
		t.Fatal("expected metadata_review_needed to be cleared")
	}
	if !metadataConfirmed {
		t.Fatal("expected metadata_confirmed to be set")
	}
	if tmdbID != 456 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
}

func TestIdentifyShow_UsesTitleShowKeyForUnmatchedRows(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Missing Show (2024) - S01E01 - Pilot",
		"/tv/Missing Show (2024)/Season 1/Missing Show (2024) - S01E01.mkv",
		0,
		db.MatchStatusUnmatched,
		1,
		1,
	).Scan(&episodeID); err != nil {
		t.Fatalf("insert tv episode: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?)`, db.LibraryTypeTV, episodeID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		SeriesQuery: &seriesQueryStub{
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				if provider != "tmdb" || seriesID != "456" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				if season != 1 || episode != 1 {
					t.Fatalf("unexpected episode = S%02dE%02d", season, episode)
				}
				return &metadata.MatchResult{
					Title:      "Missing Show - S01E01 - Pilot",
					Provider:   "tmdb",
					ExternalID: "456",
				}, nil
			},
		},
	}

	body := strings.NewReader(`{"showKey":"title-missingshow2024","tmdbId":456}`)
	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/shows/identify", body)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyShow(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Updated int `json:"updated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Updated != 1 {
		t.Fatalf("updated = %d", payload.Updated)
	}
	var tmdbID int
	var metadataConfirmed bool
	if err := dbConn.QueryRow(`SELECT COALESCE(tmdb_id, 0), COALESCE(metadata_confirmed, 0) FROM tv_episodes WHERE id = ?`, episodeID).Scan(&tmdbID, &metadataConfirmed); err != nil {
		t.Fatalf("query tv episode: %v", err)
	}
	if tmdbID != 456 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
	if !metadataConfirmed {
		t.Fatal("expected metadata_confirmed to be set")
	}
}

func TestIdentifyShow_OnlyUpdatesEpisodesForMatchingYearQualifiedShowKey(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	insertEpisode := func(title, path string) int {
		var episodeID int
		if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID,
			title,
			path,
			0,
			db.MatchStatusUnmatched,
			1,
			1,
		).Scan(&episodeID); err != nil {
			t.Fatalf("insert tv episode %q: %v", title, err)
		}
		if _, err := dbConn.Exec(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?)`, db.LibraryTypeTV, episodeID); err != nil {
			t.Fatalf("insert media global row for %q: %v", title, err)
		}
		return episodeID
	}

	episode1978ID := insertEpisode(
		"Battlestar Galactica (1978) - S01E01 - Saga of a Star World",
		"/tv/Battlestar Galactica (1978)/Season 1/Battlestar Galactica (1978) - S01E01.mkv",
	)
	episode2004ID := insertEpisode(
		"Battlestar Galactica (2004) - S01E01 - 33",
		"/tv/Battlestar Galactica (2004)/Season 1/Battlestar Galactica (2004) - S01E01.mkv",
	)

	handler := &LibraryHandler{
		DB: dbConn,
		SeriesQuery: &seriesQueryStub{
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				if provider != "tmdb" || seriesID != "456" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				if season != 1 || episode != 1 {
					t.Fatalf("unexpected episode = S%02dE%02d", season, episode)
				}
				return &metadata.MatchResult{
					Title:      "Battlestar Galactica - S01E01 - Saga of a Star World",
					Provider:   "tmdb",
					ExternalID: "456",
				}, nil
			},
		},
	}

	body := strings.NewReader(`{"showKey":"title-battlestargalactica1978","tmdbId":456}`)
	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/shows/identify", body)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyShow(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var tmdbID1978 int
	if err := dbConn.QueryRow(`SELECT COALESCE(tmdb_id, 0) FROM tv_episodes WHERE id = ?`, episode1978ID).Scan(&tmdbID1978); err != nil {
		t.Fatalf("query 1978 episode: %v", err)
	}
	if tmdbID1978 != 456 {
		t.Fatalf("1978 tmdb_id = %d", tmdbID1978)
	}

	var tmdbID2004 int
	if err := dbConn.QueryRow(`SELECT COALESCE(tmdb_id, 0) FROM tv_episodes WHERE id = ?`, episode2004ID).Scan(&tmdbID2004); err != nil {
		t.Fatalf("query 2004 episode: %v", err)
	}
	if tmdbID2004 != 0 {
		t.Fatalf("expected 2004 tmdb_id to remain unset, got %d", tmdbID2004)
	}
}

func TestRefreshShow_UsesSeriesDetailsForCanonicalMetadata(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, tmdb_id, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Series Name - S01E01 - Episode One",
		"/tv/Series Name/Season 1/Series Name - S01E01.mkv",
		0,
		db.MatchStatusUnmatched,
		456,
		1,
		1,
	).Scan(&episodeID); err != nil {
		t.Fatalf("insert tv episode: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?)`, db.LibraryTypeTV, episodeID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Series: &seriesDetailsStub{
			getSeriesDetails: func(_ context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
				if tmdbID != 456 {
					t.Fatalf("tmdbID = %d", tmdbID)
				}
				return &metadata.SeriesDetails{
					Name:         "Series Name",
					Overview:     "series overview",
					PosterPath:   "series poster",
					BackdropPath: "series backdrop",
					FirstAirDate: "2024-01-01",
					IMDbID:       "ttseries",
				}, nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				if provider != "tmdb" || seriesID != "456" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				if season != 1 || episode != 1 {
					t.Fatalf("unexpected episode = S%02dE%02d", season, episode)
				}
				return &metadata.MatchResult{
					Title:       "Series Name - S01E01 - Episode One",
					Overview:    "episode overview",
					PosterURL:   "episode poster",
					BackdropURL: "episode backdrop",
					ReleaseDate: "2024-01-02",
					VoteAverage: 7.5,
					IMDbID:      "ttepisode",
					IMDbRating:  8.1,
					Provider:    "tmdb",
					ExternalID:  "456",
				}, nil
			},
		},
	}

	body := strings.NewReader(`{"showKey":"tmdb-456"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/shows/refresh", body)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.RefreshShow(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var showID, seasonID int
	if err := dbConn.QueryRow(`SELECT COALESCE(show_id, 0), COALESCE(season_id, 0) FROM tv_episodes WHERE id = ?`, episodeID).Scan(&showID, &seasonID); err != nil {
		t.Fatalf("query episode links: %v", err)
	}
	if showID == 0 || seasonID == 0 {
		t.Fatalf("expected show/season links, got show=%d season=%d", showID, seasonID)
	}

	var showTitle, showOverview, showPoster, showBackdrop, showFirstAir, showIMDbID string
	if err := dbConn.QueryRow(`SELECT title, COALESCE(overview, ''), COALESCE(poster_path, ''), COALESCE(backdrop_path, ''), COALESCE(first_air_date, ''), COALESCE(imdb_id, '') FROM shows WHERE id = ?`, showID).
		Scan(&showTitle, &showOverview, &showPoster, &showBackdrop, &showFirstAir, &showIMDbID); err != nil {
		t.Fatalf("query show row: %v", err)
	}
	if showTitle != "Series Name" {
		t.Fatalf("show title = %q", showTitle)
	}
	if showOverview != "series overview" || showPoster != "series poster" || showBackdrop != "series backdrop" || showFirstAir != "2024-01-01" || showIMDbID != "ttseries" {
		t.Fatalf("unexpected show metadata: overview=%q poster=%q backdrop=%q first_air=%q imdb=%q", showOverview, showPoster, showBackdrop, showFirstAir, showIMDbID)
	}

	var seasonTitle, seasonOverview, seasonPoster, seasonAir string
	if err := dbConn.QueryRow(`SELECT title, COALESCE(overview, ''), COALESCE(poster_path, ''), COALESCE(air_date, '') FROM seasons WHERE id = ?`, seasonID).
		Scan(&seasonTitle, &seasonOverview, &seasonPoster, &seasonAir); err != nil {
		t.Fatalf("query season row: %v", err)
	}
	if seasonTitle != "Season 1" {
		t.Fatalf("season title = %q", seasonTitle)
	}
	if seasonOverview != "series overview" || seasonPoster != "series poster" || seasonAir != "2024-01-01" {
		t.Fatalf("unexpected season metadata: overview=%q poster=%q air=%q", seasonOverview, seasonPoster, seasonAir)
	}

	var episodeTitle, episodeOverview, episodePoster, episodeBackdrop, releaseDate string
	if err := dbConn.QueryRow(`SELECT title, COALESCE(overview, ''), COALESCE(poster_path, ''), COALESCE(backdrop_path, ''), COALESCE(release_date, '') FROM tv_episodes WHERE id = ?`, episodeID).
		Scan(&episodeTitle, &episodeOverview, &episodePoster, &episodeBackdrop, &releaseDate); err != nil {
		t.Fatalf("query episode row: %v", err)
	}
	if episodeTitle != "Series Name - S01E01 - Episode One" {
		t.Fatalf("episode title = %q", episodeTitle)
	}
	if episodeOverview != "episode overview" || episodePoster != "episode poster" || episodeBackdrop != "episode backdrop" || releaseDate != "2024-01-02" {
		t.Fatalf("unexpected episode metadata: overview=%q poster=%q backdrop=%q release=%q", episodeOverview, episodePoster, episodeBackdrop, releaseDate)
	}
}

func TestConfirmShow_ClearsMetadataReviewNeeded(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Anime", db.LibraryTypeAnime, "/anime", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	if err := dbConn.QueryRow(`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, tmdb_id, metadata_review_needed, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Frieren - S01E12 - Episode",
		"/anime/Frieren/Season 1/Frieren - S01E12.mkv",
		0,
		db.MatchStatusIdentified,
		123,
		true,
		1,
		12,
	).Scan(&episodeID); err != nil {
		t.Fatalf("insert anime episode: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?)`, db.LibraryTypeAnime, episodeID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/shows/confirm", strings.NewReader(`{"showKey":"tmdb-123"}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler := &LibraryHandler{DB: dbConn}
	handler.ConfirmShow(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Updated int `json:"updated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Updated != 1 {
		t.Fatalf("updated = %d", payload.Updated)
	}
	var reviewNeeded bool
	var metadataConfirmed bool
	if err := dbConn.QueryRow(`SELECT COALESCE(metadata_review_needed, 0), COALESCE(metadata_confirmed, 0) FROM anime_episodes WHERE id = ?`, episodeID).Scan(&reviewNeeded, &metadataConfirmed); err != nil {
		t.Fatalf("query anime episode: %v", err)
	}
	if reviewNeeded {
		t.Fatal("expected metadata_review_needed to be cleared")
	}
	if !metadataConfirmed {
		t.Fatal("expected metadata_confirmed to be set")
	}
}

func TestIdentifyLibrary_TimesOutHungRowsAndSkipsFinishedRows(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	prevInitialTimeout := identifyInitialTimeout
	prevRetryTimeout := identifyRetryTimeout
	prevWorkers := identifyMovieWorkers
	prevRateInterval := identifyMovieRateLimit
	prevRateBurst := identifyMovieRateBurst
	identifyInitialTimeout = 10 * time.Millisecond
	identifyRetryTimeout = 25 * time.Millisecond
	identifyMovieWorkers = 2
	identifyMovieRateLimit = time.Millisecond
	identifyMovieRateBurst = 1
	t.Cleanup(func() {
		identifyInitialTimeout = prevInitialTimeout
		identifyRetryTimeout = prevRetryTimeout
		identifyMovieWorkers = prevWorkers
		identifyMovieRateLimit = prevRateInterval
		identifyMovieRateBurst = prevRateBurst
	})

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Movies", db.LibraryTypeMovie, "/movies", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	stuckPath := "/movies/Stuck Movie (2024)/Stuck Movie (2024).mp4"
	var ignoredID int
	if err := dbConn.QueryRow(`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`, libraryID, "Stuck Movie", stuckPath, 0, db.MatchStatusUnmatched).Scan(&ignoredID); err != nil {
		t.Fatalf("insert stuck movie: %v", err)
	}
	var quickID int
	quickPath := "/movies/Quick Movie (2024)/Quick Movie (2024).mp4"
	if err := dbConn.QueryRow(`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`, libraryID, "Quick Movie", quickPath, 0, db.MatchStatusLocal).Scan(&quickID); err != nil {
		t.Fatalf("insert quick movie: %v", err)
	}
	var finishedID int
	finishedPath := "/movies/Finished Movie (2024)/Finished Movie (2024).mp4"
	if err := dbConn.QueryRow(`INSERT INTO movies (library_id, title, path, duration, match_status, tmdb_id, poster_path, imdb_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, libraryID, "Finished Movie", finishedPath, 0, db.MatchStatusIdentified, 999, "/poster.jpg", "tt9999999").Scan(&finishedID); err != nil {
		t.Fatalf("insert finished movie: %v", err)
	}

	var calls int32
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			movie: func(ctx context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				atomic.AddInt32(&calls, 1)
				switch info.Title {
				case "stuck movie":
					<-ctx.Done()
					return nil
				case "quick movie":
					return &metadata.MatchResult{
						Title:      "Quick Movie",
						Provider:   "tmdb",
						ExternalID: "456",
					}
				default:
					t.Fatalf("unexpected title = %q", info.Title)
					return nil
				}
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 1 || payload.Failed != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("identifier call count = %d", got)
	}

	var quickTitle, quickStatus string
	var quickTMDBID int
	if err := dbConn.QueryRow(`SELECT title, match_status, COALESCE(tmdb_id, 0) FROM movies WHERE id = ?`, quickID).Scan(&quickTitle, &quickStatus, &quickTMDBID); err != nil {
		t.Fatalf("query quick movie: %v", err)
	}
	if quickTitle != "Quick Movie" || quickStatus != db.MatchStatusIdentified || quickTMDBID != 456 {
		t.Fatalf("unexpected quick movie state: title=%q status=%q tmdb=%d", quickTitle, quickStatus, quickTMDBID)
	}

	var finishedTitle, finishedStatus string
	var finishedTMDBID int
	if err := dbConn.QueryRow(`SELECT title, match_status, COALESCE(tmdb_id, 0) FROM movies WHERE id = ?`, finishedID).Scan(&finishedTitle, &finishedStatus, &finishedTMDBID); err != nil {
		t.Fatalf("query finished movie: %v", err)
	}
	if finishedTitle != "Finished Movie" || finishedStatus != db.MatchStatusIdentified || finishedTMDBID != 999 {
		t.Fatalf("unexpected finished movie state: title=%q status=%q tmdb=%d", finishedTitle, finishedStatus, finishedTMDBID)
	}
}

func TestIdentifyLibrary_DedupesDuplicateMovieLookupsWithinRun(t *testing.T) {
	dbConn, err := db.InitDB(filepath.Join(t.TempDir(), "plum.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	prevWorkers := identifyMovieWorkers
	prevRateInterval := identifyMovieRateLimit
	prevRateBurst := identifyMovieRateBurst
	identifyMovieWorkers = 4
	identifyMovieRateLimit = time.Millisecond
	identifyMovieRateBurst = 4
	t.Cleanup(func() {
		identifyMovieWorkers = prevWorkers
		identifyMovieRateLimit = prevRateInterval
		identifyMovieRateBurst = prevRateBurst
	})

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "dedupe@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Movies", db.LibraryTypeMovie, "/movies", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	for _, row := range []struct {
		title string
		path  string
	}{
		{title: "Shared Movie", path: "/movies/Shared Movie (2024)/Shared Movie (2024) [1080p].mp4"},
		{title: "Shared Movie", path: "/movies/Shared Movie (2024)/Shared Movie (2024) [4k].mkv"},
		{title: "Unique Movie", path: "/movies/Unique Movie (2024)/Unique Movie (2024).mp4"},
	} {
		if _, err := dbConn.Exec(`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?)`, libraryID, row.title, row.path, 0, db.MatchStatusLocal); err != nil {
			t.Fatalf("insert movie %q: %v", row.path, err)
		}
	}

	var (
		mu    sync.Mutex
		calls = map[string]int{}
	)
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			movie: func(ctx context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				key := metadata.NormalizeTitle(info.Title)
				mu.Lock()
				calls[key]++
				mu.Unlock()
				return &metadata.MatchResult{
					Title:      info.Title,
					Provider:   "tmdb",
					ExternalID: "101",
				}
			},
		},
	}

	result, err := handler.identifyLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("identify library: %v", err)
	}
	if result.Identified != 3 || result.Failed != 0 {
		rows, queryErr := dbConn.Query(`SELECT title, path, match_status, COALESCE(tmdb_id, 0) FROM movies WHERE library_id = ? ORDER BY path`, libraryID)
		if queryErr != nil {
			t.Fatalf("unexpected result: %+v (query err: %v)", result, queryErr)
		}
		defer rows.Close()
		var states []string
		for rows.Next() {
			var title, path, status string
			var tmdbID int
			if err := rows.Scan(&title, &path, &status, &tmdbID); err != nil {
				t.Fatalf("scan state: %v", err)
			}
			states = append(states, fmt.Sprintf("%s|%s|%s|%d", title, path, status, tmdbID))
		}
		t.Fatalf("unexpected result: %+v states=%v calls=%v", result, states, calls)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls["shared movie"] != 1 {
		t.Fatalf("shared movie calls = %d, want 1", calls["shared movie"])
	}
	if calls["unique movie"] != 1 {
		t.Fatalf("unique movie calls = %d, want 1", calls["unique movie"])
	}
}

func TestIdentifyLibrary_GroupsTVEpisodesByShow(t *testing.T) {
	dbConn, err := db.InitDB(filepath.Join(t.TempDir(), "plum.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	prevWorkers := identifyEpisodeWorkers
	prevRateInterval := identifyEpisodeRateLimit
	prevRateBurst := identifyEpisodeRateBurst
	identifyEpisodeWorkers = 4
	identifyEpisodeRateLimit = time.Millisecond
	identifyEpisodeRateBurst = 4
	t.Cleanup(func() {
		identifyEpisodeWorkers = prevWorkers
		identifyEpisodeRateLimit = prevRateInterval
		identifyEpisodeRateBurst = prevRateBurst
	})

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "tv-group@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	for episode := 1; episode <= 2; episode++ {
		path := fmt.Sprintf("/tv/Slow Horses/Season 1/Slow Horses - S01E%02d.mkv", episode)
		title := fmt.Sprintf("Slow Horses - S01E%02d", episode)
		if _, err := dbConn.Exec(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			libraryID,
			title,
			path,
			0,
			db.MatchStatusUnmatched,
			1,
			episode,
		); err != nil {
			t.Fatalf("insert episode %d: %v", episode, err)
		}
	}

	var (
		searchCalls  int32
		detailsCalls int32
		episodeCalls int32
		queueCalls   int32
	)
	searchIndex := NewSearchIndexManager(dbConn, nil, nil)
	searchIndex.onQueue = func(gotLibraryID int, full bool) {
		if gotLibraryID != libraryID {
			t.Fatalf("queued library id = %d", gotLibraryID)
		}
		if full {
			t.Fatal("expected incremental queue")
		}
		atomic.AddInt32(&queueCalls, 1)
	}
	searchIndex.refresh = func(gotLibraryID int, full bool) error {
		if gotLibraryID != libraryID {
			t.Fatalf("refresh library id = %d", gotLibraryID)
		}
		if full {
			t.Fatal("expected incremental refresh")
		}
		return nil
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			tv: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				t.Fatalf("unexpected per-row TV identify for %+v", info)
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				atomic.AddInt32(&searchCalls, 1)
				if !strings.EqualFold(query, "Slow Horses") {
					t.Fatalf("query = %q", query)
				}
				return []metadata.MatchResult{{
					Title:      "Slow Horses",
					Provider:   "tmdb",
					ExternalID: "321",
				}}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				atomic.AddInt32(&episodeCalls, 1)
				if provider != "tmdb" || seriesID != "321" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				return &metadata.MatchResult{
					Title:      fmt.Sprintf("Slow Horses - S01E%02d - Episode %d", episode, episode),
					Provider:   "tmdb",
					ExternalID: "321",
				}, nil
			},
		},
		Series: &seriesDetailsStub{
			getSeriesDetails: func(_ context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
				atomic.AddInt32(&detailsCalls, 1)
				if tmdbID != 321 {
					t.Fatalf("tmdb id = %d", tmdbID)
				}
				return &metadata.SeriesDetails{Name: "Slow Horses"}, nil
			},
		},
		SearchIndex: searchIndex,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 2 || payload.Failed != 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := atomic.LoadInt32(&searchCalls); got != 1 {
		t.Fatalf("search calls = %d", got)
	}
	if got := atomic.LoadInt32(&detailsCalls); got != 1 {
		t.Fatalf("series detail calls = %d", got)
	}
	if got := atomic.LoadInt32(&episodeCalls); got != 2 {
		t.Fatalf("episode calls = %d", got)
	}
	if got := atomic.LoadInt32(&queueCalls); got != 1 {
		t.Fatalf("queue calls = %d", got)
	}
	var reviewNeededCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM tv_episodes WHERE library_id = ? AND COALESCE(metadata_review_needed, 0) = 1`, libraryID).Scan(&reviewNeededCount); err != nil {
		t.Fatalf("count review-needed episodes: %v", err)
	}
	if reviewNeededCount != 0 {
		t.Fatalf("review-needed episodes = %d", reviewNeededCount)
	}
}

func TestIdentifyLibrary_GroupsTVDBEpisodesAndFallsBackToTitleSearch(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	prevWorkers := identifyEpisodeWorkers
	prevRateInterval := identifyEpisodeRateLimit
	prevRateBurst := identifyEpisodeRateBurst
	identifyEpisodeWorkers = 1
	identifyEpisodeRateLimit = time.Millisecond
	identifyEpisodeRateBurst = 1
	t.Cleanup(func() {
		identifyEpisodeWorkers = prevWorkers
		identifyEpisodeRateLimit = prevRateInterval
		identifyEpisodeRateBurst = prevRateBurst
	})

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "tvdb-group@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, tvdb_id, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID,
		"Slow Horses - S01E01",
		"/tv/Slow Horses/Season 1/Slow Horses - S01E01.mkv",
		0,
		db.MatchStatusIdentified,
		"tvdb-321",
		1,
		1,
	); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	var (
		searchCalls  int32
		episodeCalls int32
	)
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			tv: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				t.Fatalf("unexpected per-row TV identify for %+v", info)
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				atomic.AddInt32(&searchCalls, 1)
				if !strings.EqualFold(query, "Slow Horses") {
					t.Fatalf("query = %q", query)
				}
				return []metadata.MatchResult{{
					Title:      "Slow Horses",
					Provider:   "tmdb",
					ExternalID: "321",
				}}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				atomic.AddInt32(&episodeCalls, 1)
				if provider != "tmdb" || seriesID != "321" {
					t.Fatalf("unexpected provider/series = %q/%q", provider, seriesID)
				}
				return &metadata.MatchResult{
					Title:      "Slow Horses - S01E01 - Episode 1",
					Provider:   "tmdb",
					ExternalID: "321",
				}, nil
			},
		},
		Series: &seriesDetailsStub{
			getSeriesDetails: func(_ context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
				return &metadata.SeriesDetails{Name: "Slow Horses"}, nil
			},
		},
	}

	result, err := handler.identifyLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("identify library: %v", err)
	}
	if result.Identified != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := atomic.LoadInt32(&searchCalls); got != 1 {
		t.Fatalf("search calls = %d", got)
	}
	if got := atomic.LoadInt32(&episodeCalls); got != 1 {
		t.Fatalf("episode calls = %d", got)
	}
}

func TestIdentifyLibrary_GroupedEpisodeRetryUsesLongerTimeoutAndFreshLookup(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	prevInitialTimeout := identifyInitialTimeout
	prevRetryTimeout := identifyRetryTimeout
	prevWorkers := identifyEpisodeWorkers
	prevRateInterval := identifyEpisodeRateLimit
	prevRateBurst := identifyEpisodeRateBurst
	identifyInitialTimeout = 5 * time.Millisecond
	identifyRetryTimeout = 50 * time.Millisecond
	identifyEpisodeWorkers = 1
	identifyEpisodeRateLimit = time.Millisecond
	identifyEpisodeRateBurst = 1
	t.Cleanup(func() {
		identifyInitialTimeout = prevInitialTimeout
		identifyRetryTimeout = prevRetryTimeout
		identifyEpisodeWorkers = prevWorkers
		identifyEpisodeRateLimit = prevRateInterval
		identifyEpisodeRateBurst = prevRateBurst
	})

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "retry-group@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, tmdb_id, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID,
		"Slow Horses - S01E01",
		"/tv/Slow Horses/Season 1/Slow Horses - S01E01.mkv",
		0,
		db.MatchStatusIdentified,
		321,
		1,
		1,
	); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	var episodeCalls int32
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			tv: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				t.Fatalf("unexpected per-row TV identify for %+v", info)
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				t.Fatalf("unexpected fallback search for %q", query)
				return nil, nil
			},
			getEpisode: func(ctx context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				atomic.AddInt32(&episodeCalls, 1)
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("expected identify timeout on grouped lookup")
				}
				if time.Until(deadline) < 20*time.Millisecond {
					return nil, nil
				}
				return &metadata.MatchResult{
					Title:      "Slow Horses - S01E01 - Episode 1",
					Provider:   "tmdb",
					ExternalID: "321",
				}, nil
			},
		},
		Series: &seriesDetailsStub{
			getSeriesDetails: func(_ context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
				return &metadata.SeriesDetails{Name: "Slow Horses"}, nil
			},
		},
	}

	result, err := handler.identifyLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("identify library: %v", err)
	}
	if result.Identified != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := atomic.LoadInt32(&episodeCalls); got != 2 {
		t.Fatalf("episode calls = %d", got)
	}
}

func TestIdentifyLibrary_GroupedEpisodesFallbackOnlyForUnresolvedRows(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	prevWorkers := identifyEpisodeWorkers
	prevRateInterval := identifyEpisodeRateLimit
	prevRateBurst := identifyEpisodeRateBurst
	identifyEpisodeWorkers = 4
	identifyEpisodeRateLimit = time.Millisecond
	identifyEpisodeRateBurst = 4
	t.Cleanup(func() {
		identifyEpisodeWorkers = prevWorkers
		identifyEpisodeRateLimit = prevRateInterval
		identifyEpisodeRateBurst = prevRateBurst
	})

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "tv-partial@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	for episode := 1; episode <= 2; episode++ {
		path := fmt.Sprintf("/tv/Slow Horses/Season 1/Slow Horses - S01E%02d.mkv", episode)
		title := fmt.Sprintf("Slow Horses - S01E%02d", episode)
		if _, err := dbConn.Exec(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			libraryID,
			title,
			path,
			0,
			db.MatchStatusUnmatched,
			1,
			episode,
		); err != nil {
			t.Fatalf("insert episode %d: %v", episode, err)
		}
	}

	var (
		searchCalls  int32
		detailsCalls int32
		episodeCalls int32
	)
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			tv: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				t.Fatalf("unexpected per-row TV identify for %+v", info)
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				atomic.AddInt32(&searchCalls, 1)
				return []metadata.MatchResult{{Title: "Slow Horses", Provider: "tmdb", ExternalID: "321"}}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				atomic.AddInt32(&episodeCalls, 1)
				if episode == 2 {
					return nil, nil
				}
				return &metadata.MatchResult{
					Title:      "Slow Horses - S01E01 - Pilot",
					Provider:   "tmdb",
					ExternalID: "321",
				}, nil
			},
		},
		Series: &seriesDetailsStub{
			getSeriesDetails: func(_ context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
				atomic.AddInt32(&detailsCalls, 1)
				return &metadata.SeriesDetails{Name: "Slow Horses"}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 1 || payload.Failed != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := atomic.LoadInt32(&searchCalls); got != 1 {
		t.Fatalf("search calls = %d", got)
	}
	if got := atomic.LoadInt32(&detailsCalls); got != 1 {
		t.Fatalf("series detail calls = %d", got)
	}
	if got := atomic.LoadInt32(&episodeCalls); got != 3 {
		t.Fatalf("episode calls = %d", got)
	}
}

func TestIdentifyLibrary_GroupsSafeAnimeAndLeavesAbsoluteEpisodesOnResidualPath(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	prevWorkers := identifyEpisodeWorkers
	prevRateInterval := identifyEpisodeRateLimit
	prevRateBurst := identifyEpisodeRateBurst
	identifyEpisodeWorkers = 4
	identifyEpisodeRateLimit = time.Millisecond
	identifyEpisodeRateBurst = 4
	t.Cleanup(func() {
		identifyEpisodeWorkers = prevWorkers
		identifyEpisodeRateLimit = prevRateInterval
		identifyEpisodeRateBurst = prevRateBurst
	})

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "anime-group@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Anime", db.LibraryTypeAnime, "/anime", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	for episode := 1; episode <= 2; episode++ {
		path := fmt.Sprintf("/anime/Frieren/Season 1/Frieren - S01E%02d.mkv", episode)
		title := fmt.Sprintf("Frieren - S01E%02d", episode)
		if _, err := dbConn.Exec(`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			libraryID,
			title,
			path,
			0,
			db.MatchStatusUnmatched,
			1,
			episode,
		); err != nil {
			t.Fatalf("insert safe anime episode %d: %v", episode, err)
		}
	}
	var absoluteID int
	if err := dbConn.QueryRow(`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Frieren - 12",
		"/anime/Frieren/Frieren - 12.mkv",
		0,
		db.MatchStatusUnmatched,
		0,
		0,
	).Scan(&absoluteID); err != nil {
		t.Fatalf("insert absolute anime episode: %v", err)
	}

	var (
		searchCalls   int32
		episodeCalls  int32
		identifyCalls int32
	)
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			anime: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				atomic.AddInt32(&identifyCalls, 1)
				if info.AbsoluteEpisode != 12 {
					t.Fatalf("unexpected residual anime info: %+v", info)
				}
				return &metadata.MatchResult{
					Title:      "Frieren - Episode 12",
					Provider:   "tmdb",
					ExternalID: "123",
				}
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				atomic.AddInt32(&searchCalls, 1)
				if !strings.EqualFold(query, "Frieren") {
					t.Fatalf("query = %q", query)
				}
				return []metadata.MatchResult{{Title: "Frieren", Provider: "tmdb", ExternalID: "123"}}, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				atomic.AddInt32(&episodeCalls, 1)
				return &metadata.MatchResult{
					Title:      fmt.Sprintf("Frieren - S01E%02d - Episode %d", episode, episode),
					Provider:   "tmdb",
					ExternalID: "123",
				}, nil
			},
		},
		Series: &seriesDetailsStub{
			getSeriesDetails: func(_ context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
				return &metadata.SeriesDetails{Name: "Frieren"}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/identify", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.IdentifyLibrary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Identified int `json:"identified"`
		Failed     int `json:"failed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Identified != 3 || payload.Failed != 0 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := atomic.LoadInt32(&searchCalls); got != 1 {
		t.Fatalf("search calls = %d", got)
	}
	if got := atomic.LoadInt32(&episodeCalls); got != 2 {
		t.Fatalf("episode calls = %d", got)
	}
	if got := atomic.LoadInt32(&identifyCalls); got != 1 {
		t.Fatalf("anime identify calls = %d", got)
	}

	var absoluteTitle string
	if err := dbConn.QueryRow(`SELECT title FROM anime_episodes WHERE id = ?`, absoluteID).Scan(&absoluteTitle); err != nil {
		t.Fatalf("query absolute anime episode: %v", err)
	}
	if absoluteTitle != "Frieren - Episode 12" {
		t.Fatalf("absolute episode title = %q", absoluteTitle)
	}
}

func TestIdentifyLibrary_DoesNotCountMatchedTVMetadataRefreshAsFailed(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "tv-refresh@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	if _, err := dbConn.Exec(
		`INSERT INTO tv_episodes (
			library_id, title, path, duration, match_status, season, episode, tmdb_id, poster_path, imdb_id, last_metadata_refresh_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID,
		"Slow Horses - S01E01 - Pilot",
		"/tv/Slow Horses/Season 1/Slow Horses - S01E01.mkv",
		0,
		db.MatchStatusIdentified,
		1,
		1,
		321,
		"",
		"",
		"",
	); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	var getEpisodeCalls int32
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			tv: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				t.Fatalf("unexpected per-row TV identify for %+v", info)
				return nil
			},
		},
		SeriesQuery: &seriesQueryStub{
			searchTV: func(_ context.Context, query string) ([]metadata.MatchResult, error) {
				t.Fatalf("unexpected search fallback query=%q", query)
				return nil, nil
			},
			getEpisode: func(_ context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
				atomic.AddInt32(&getEpisodeCalls, 1)
				if provider != "tmdb" || seriesID != "321" || season != 1 || episode != 1 {
					t.Fatalf("unexpected episode lookup provider=%s series=%s season=%d episode=%d", provider, seriesID, season, episode)
				}
				return &metadata.MatchResult{
					Title:      "Slow Horses - S01E01 - Pilot",
					Provider:   "tmdb",
					ExternalID: "321",
				}, nil
			},
		},
		Series: &seriesDetailsStub{
			getSeriesDetails: func(_ context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
				if tmdbID != 321 {
					t.Fatalf("unexpected series details lookup tmdbID=%d", tmdbID)
				}
				return &metadata.SeriesDetails{Name: "Slow Horses"}, nil
			},
		},
	}

	result, err := handler.identifyLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("identify library: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("failed = %d", result.Failed)
	}
	if result.Identified != 1 {
		t.Fatalf("identified = %d", result.Identified)
	}
	if got := atomic.LoadInt32(&getEpisodeCalls); got != 1 {
		t.Fatalf("episode calls = %d", got)
	}
	if states := handler.identifyRun.stateForLibrary(libraryID); len(states) != 0 {
		t.Fatalf("unexpected identify states: %+v", states)
	}
}

func TestIdentifyLibrary_DoesNotCountMatchedMovieMetadataRefreshAsFailed(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "movie-refresh@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Movies", db.LibraryTypeMovie, "/movies", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	if _, err := dbConn.Exec(
		`INSERT INTO movies (
			library_id, title, path, duration, match_status, tmdb_id, poster_path, imdb_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID,
		"Die My Love",
		"/movies/Die My Love (2025)/Die My Love.mp4",
		0,
		db.MatchStatusIdentified,
		444,
		"",
		"",
	); err != nil {
		t.Fatalf("insert movie: %v", err)
	}

	var identifyCalls int32
	handler := &LibraryHandler{
		DB: dbConn,
		Meta: &identifyStub{
			movie: func(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
				atomic.AddInt32(&identifyCalls, 1)
				if info.TMDBID != 444 {
					t.Fatalf("unexpected movie info: %+v", info)
				}
				return &metadata.MatchResult{
					Title:      "Die My Love",
					Provider:   "tmdb",
					ExternalID: "444",
				}
			},
		},
	}

	result, err := handler.identifyLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("identify library: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("failed = %d", result.Failed)
	}
	if result.Identified != 1 {
		t.Fatalf("identified = %d", result.Identified)
	}
	if got := atomic.LoadInt32(&identifyCalls); got != 1 {
		t.Fatalf("identify calls = %d", got)
	}
	if states := handler.identifyRun.stateForLibrary(libraryID); len(states) != 0 {
		t.Fatalf("unexpected identify states: %+v", states)
	}
}

func TestIdentifyConfigForKind_UsesIndependentEpisodeTuning(t *testing.T) {
	prevMovieWorkers := identifyMovieWorkers
	prevMovieRateLimit := identifyMovieRateLimit
	prevMovieRateBurst := identifyMovieRateBurst
	prevEpisodeWorkers := identifyEpisodeWorkers
	prevEpisodeRateLimit := identifyEpisodeRateLimit
	prevEpisodeRateBurst := identifyEpisodeRateBurst

	identifyMovieWorkers = 6
	identifyMovieRateLimit = 100 * time.Millisecond
	identifyMovieRateBurst = 6
	identifyEpisodeWorkers = 4
	identifyEpisodeRateLimit = 150 * time.Millisecond
	identifyEpisodeRateBurst = 4
	t.Cleanup(func() {
		identifyMovieWorkers = prevMovieWorkers
		identifyMovieRateLimit = prevMovieRateLimit
		identifyMovieRateBurst = prevMovieRateBurst
		identifyEpisodeWorkers = prevEpisodeWorkers
		identifyEpisodeRateLimit = prevEpisodeRateLimit
		identifyEpisodeRateBurst = prevEpisodeRateBurst
	})

	movieConfig := identifyConfigForKind(db.LibraryTypeMovie)
	episodeConfig := identifyConfigForKind(db.LibraryTypeTV)

	if movieConfig.workers != 6 || movieConfig.rateInterval != 100*time.Millisecond || movieConfig.rateBurst != 6 {
		t.Fatalf("unexpected movie config: %+v", movieConfig)
	}
	if episodeConfig.workers != 4 || episodeConfig.rateInterval != 150*time.Millisecond || episodeConfig.rateBurst != 4 {
		t.Fatalf("unexpected episodic config: %+v", episodeConfig)
	}
}

func TestSearchIndexManager_QueueWhileRunningSchedulesRerun(t *testing.T) {
	manager := NewSearchIndexManager(nil, nil, nil)
	firstRunStarted := make(chan struct{}, 1)
	releaseFirstRun := make(chan struct{})

	var (
		mu    sync.Mutex
		calls []bool
	)
	manager.refresh = func(libraryID int, full bool) error {
		mu.Lock()
		calls = append(calls, full)
		callCount := len(calls)
		mu.Unlock()

		if libraryID != 7 {
			t.Fatalf("library id = %d", libraryID)
		}
		if callCount == 1 {
			firstRunStarted <- struct{}{}
			<-releaseFirstRun
		}
		return nil
	}

	manager.Queue(7, false)
	select {
	case <-firstRunStarted:
	case <-time.After(time.Second):
		t.Fatal("expected first refresh to start")
	}

	manager.Queue(7, false)
	close(releaseFirstRun)

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		callCount := len(calls)
		mu.Unlock()
		if callCount == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected rerun after in-flight queue, got %d refreshes", callCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("refresh calls = %d", len(calls))
	}
	if calls[0] || calls[1] {
		t.Fatalf("unexpected full refresh flags: %+v", calls)
	}
}

func TestLibraryScanStatus_ReturnsIdleWhenNoScanHasStarted(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	handler := &LibraryHandler{
		DB:       dbConn,
		ScanJobs: NewLibraryScanManager(dbConn, nil, nil),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/libraries/"+strconv.Itoa(libraryID)+"/scan", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.GetLibraryScanStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		LibraryID int    `json:"libraryId"`
		Phase     string `json:"phase"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.LibraryID != libraryID || payload.Phase != "idle" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestStartLibraryScan_ImportsMediaInBackground(t *testing.T) {
	dbConn, err := db.InitDB(filepath.Join(t.TempDir(), "plum.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	root := t.TempDir()
	showDir := filepath.Join(root, "Test Show", "Season 1")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatalf("mkdir show dir: %v", err)
	}
	file := filepath.Join(showDir, "Test Show - S01E01.mkv")
	if err := os.WriteFile(file, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, root, now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	handler := &LibraryHandler{
		DB:       dbConn,
		ScanJobs: scanJobs,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/scan/start?identify=false", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.StartLibraryScan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		status := scanJobs.status(libraryID)
		if status.Phase == libraryScanPhaseCompleted {
			break
		}
		if status.Phase == libraryScanPhaseFailed {
			t.Fatalf("scan failed: %+v", status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan did not complete: %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(1) FROM tv_episodes WHERE library_id = ?`, libraryID).Scan(&count); err != nil {
		t.Fatalf("count imported rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 imported row, got %d", count)
	}
}

func TestLibraryScanManager_RecoverResumesQueuedScan(t *testing.T) {
	dbConn, err := db.InitDB(filepath.Join(t.TempDir(), "plum.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	root := t.TempDir()
	showDir := filepath.Join(root, "Recovered Show", "Season 1")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatalf("mkdir show dir: %v", err)
	}
	file := filepath.Join(showDir, "Recovered Show - S01E01.mkv")
	if err := os.WriteFile(file, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, root, now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	if err := db.UpsertLibraryJobStatus(dbConn, db.LibraryJobStatus{
		LibraryID:         libraryID,
		Path:              root,
		Type:              db.LibraryTypeTV,
		Phase:             libraryScanPhaseQueued,
		IdentifyPhase:     libraryIdentifyPhaseIdle,
		IdentifyRequested: false,
		StartedAt:         now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed library job status: %v", err)
	}

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	if err := scanJobs.Recover(); err != nil {
		t.Fatalf("recover scan jobs: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		status := scanJobs.status(libraryID)
		if status.Phase == libraryScanPhaseCompleted {
			break
		}
		if status.Phase == libraryScanPhaseFailed {
			t.Fatalf("scan failed after recovery: %+v", status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovered scan did not complete: %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(1) FROM tv_episodes WHERE library_id = ?`, libraryID).Scan(&count); err != nil {
		t.Fatalf("count imported rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 imported row after recovery, got %d", count)
	}
}

func TestLibraryScanManager_RecoverPreservesFIFOOrder(t *testing.T) {
	dbConn, err := db.InitDB(filepath.Join(t.TempDir(), "plum.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	bigRoot := t.TempDir()
	bigShowDir := filepath.Join(bigRoot, "Big Show", "Season 1")
	if err := os.MkdirAll(bigShowDir, 0o755); err != nil {
		t.Fatalf("mkdir big show dir: %v", err)
	}
	for i := 1; i <= 3; i++ {
		file := filepath.Join(bigShowDir, "Big Show - S01E0"+strconv.Itoa(i)+".mkv")
		if err := os.WriteFile(file, []byte("not a real video"), 0o644); err != nil {
			t.Fatalf("write big media file: %v", err)
		}
	}

	smallRoot := t.TempDir()
	smallShowDir := filepath.Join(smallRoot, "Small Show", "Season 1")
	if err := os.MkdirAll(smallShowDir, 0o755); err != nil {
		t.Fatalf("mkdir small show dir: %v", err)
	}
	smallFile := filepath.Join(smallShowDir, "Small Show - S01E01.mkv")
	if err := os.WriteFile(smallFile, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write small media file: %v", err)
	}

	var bigLibraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Big TV", db.LibraryTypeTV, bigRoot, now).Scan(&bigLibraryID); err != nil {
		t.Fatalf("insert big library: %v", err)
	}
	var smallLibraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "Small TV", db.LibraryTypeTV, smallRoot, now).Scan(&smallLibraryID); err != nil {
		t.Fatalf("insert small library: %v", err)
	}

	for _, status := range []db.LibraryJobStatus{
		{
			LibraryID:         bigLibraryID,
			Path:              bigRoot,
			Type:              db.LibraryTypeTV,
			Phase:             libraryScanPhaseQueued,
			IdentifyPhase:     libraryIdentifyPhaseIdle,
			IdentifyRequested: false,
			QueuedAt:          now.Add(-1 * time.Minute).Format(time.RFC3339),
			StartedAt:         now.Add(-1 * time.Minute).Format(time.RFC3339),
		},
		{
			LibraryID:         smallLibraryID,
			Path:              smallRoot,
			Type:              db.LibraryTypeTV,
			Phase:             libraryScanPhaseQueued,
			IdentifyPhase:     libraryIdentifyPhaseIdle,
			IdentifyRequested: false,
			QueuedAt:          now.Format(time.RFC3339),
			StartedAt:         now.Format(time.RFC3339),
		},
	} {
		if err := db.UpsertLibraryJobStatus(dbConn, status); err != nil {
			t.Fatalf("seed library job status: %v", err)
		}
	}

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	if err := scanJobs.Recover(); err != nil {
		t.Fatalf("recover scan jobs: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		bigStatus := scanJobs.status(bigLibraryID)
		smallStatus := scanJobs.status(smallLibraryID)
		if (bigStatus.Phase == libraryScanPhaseScanning || bigStatus.Phase == libraryScanPhaseCompleted) && smallStatus.QueuePosition == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("unexpected statuses big=%+v small=%+v", bigStatus, smallStatus)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLibraryScanManager_RequeueDoesNotDuplicateQueuedJob(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	bigRoot := t.TempDir()
	bigDir := filepath.Join(bigRoot, "Big Show", "Season 1")
	if err := os.MkdirAll(bigDir, 0o755); err != nil {
		t.Fatalf("mkdir big dir: %v", err)
	}
	for i := 1; i <= 3; i++ {
		file := filepath.Join(bigDir, "Big Show - S01E0"+strconv.Itoa(i)+".mkv")
		if err := os.WriteFile(file, []byte("not a real video"), 0o644); err != nil {
			t.Fatalf("write big file: %v", err)
		}
	}

	smallRoot := t.TempDir()
	smallDir := filepath.Join(smallRoot, "Small Show", "Season 1")
	if err := os.MkdirAll(smallDir, 0o755); err != nil {
		t.Fatalf("mkdir small dir: %v", err)
	}
	smallFile := filepath.Join(smallDir, "Small Show - S01E01.mkv")
	if err := os.WriteFile(smallFile, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	bigStatus := scanJobs.start(1, bigRoot, db.LibraryTypeTV, false, nil)
	if bigStatus.Phase == "" {
		t.Fatal("expected big status")
	}

	firstQueued := scanJobs.start(2, smallRoot, db.LibraryTypeTV, false, nil)
	secondQueued := scanJobs.start(2, smallRoot, db.LibraryTypeTV, false, nil)

	if firstQueued.LibraryID != secondQueued.LibraryID {
		t.Fatalf("requeue returned different jobs: %+v vs %+v", firstQueued, secondQueued)
	}
	if len(scanJobs.jobs) != 2 {
		t.Fatalf("expected 2 jobs tracked, got %d", len(scanJobs.jobs))
	}

	status := scanJobs.status(2)
	if status.QueuePosition != 2 {
		t.Fatalf("queue position = %d", status.QueuePosition)
	}
}

func TestLibraryScanManager_CompletedScanAdvancesQueueWhileFirstLibraryEnriches(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	originalDiscovery := scanLibraryDiscovery
	originalEnrichment := enrichLibraryTasks
	enrichmentStarted := make(chan struct{}, 1)
	secondScanStarted := make(chan struct{}, 1)
	releaseEnrichment := make(chan struct{})
	var releaseOnce sync.Once
	firstRoot := filepath.Join(t.TempDir(), "library-a")
	secondRoot := filepath.Join(t.TempDir(), "library-b")
	scanLibraryDiscovery = func(
		ctx context.Context,
		dbConn *sql.DB,
		root, mediaType string,
		libraryID int,
		options db.ScanOptions,
	) (db.ScanDelta, error) {
		if root == secondRoot {
			select {
			case secondScanStarted <- struct{}{}:
			default:
			}
		}
		return db.ScanDelta{
			Result: db.ScanResult{Added: 1},
			TouchedFiles: []db.EnrichmentTask{{
				LibraryID: libraryID,
				Kind:      mediaType,
				Path:      filepath.Join(root, "file.mkv"),
			}},
		}, nil
	}
	enrichLibraryTasks = func(
		ctx context.Context,
		dbConn *sql.DB,
		root, mediaType string,
		libraryID int,
		tasks []db.EnrichmentTask,
		options db.ScanOptions,
	) error {
		if root == firstRoot {
			select {
			case enrichmentStarted <- struct{}{}:
			default:
			}
			<-releaseEnrichment
		}
		return nil
	}
	t.Cleanup(func() {
		scanLibraryDiscovery = originalDiscovery
		enrichLibraryTasks = originalEnrichment
		releaseOnce.Do(func() {
			close(releaseEnrichment)
		})
	})

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	scanJobs.start(1, firstRoot, db.LibraryTypeTV, false, nil)
	scanJobs.start(2, secondRoot, db.LibraryTypeTV, false, nil)

	select {
	case <-enrichmentStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected first library enrichment to start")
	}

	select {
	case <-secondScanStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected second library scan to start while first enriches")
	}

	firstStatus := scanJobs.status(1)
	if firstStatus.Phase != libraryScanPhaseCompleted || !firstStatus.Enriching {
		t.Fatalf("unexpected first status while second scans: %+v", firstStatus)
	}
	secondStatus := scanJobs.status(2)
	if secondStatus.Phase != libraryScanPhaseScanning && secondStatus.Phase != libraryScanPhaseCompleted {
		t.Fatalf("unexpected second status while first enriches: %+v", secondStatus)
	}

	releaseOnce.Do(func() {
		close(releaseEnrichment)
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		status := scanJobs.status(1)
		if !status.Enriching {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected first enrichment to finish, got %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLibraryScanManager_EnrichmentFailureMarksJobFailedAndSchedulesRetry(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	originalDiscovery := scanLibraryDiscovery
	originalEnrichment := enrichLibraryTasks
	enrichErr := errors.New("hash failed")
	scanLibraryDiscovery = func(
		ctx context.Context,
		dbConn *sql.DB,
		root, mediaType string,
		libraryID int,
		options db.ScanOptions,
	) (db.ScanDelta, error) {
		return db.ScanDelta{
			Result: db.ScanResult{Added: 1},
			TouchedFiles: []db.EnrichmentTask{{
				LibraryID: libraryID,
				Kind:      mediaType,
				Path:      filepath.Join(root, "file.mkv"),
			}},
		}, nil
	}
	enrichLibraryTasks = func(
		ctx context.Context,
		dbConn *sql.DB,
		root, mediaType string,
		libraryID int,
		tasks []db.EnrichmentTask,
		options db.ScanOptions,
	) error {
		return enrichErr
	}
	t.Cleanup(func() {
		scanLibraryDiscovery = originalDiscovery
		enrichLibraryTasks = originalEnrichment
	})

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	scanJobs.start(1, "/tv", db.LibraryTypeTV, false, nil)

	deadline := time.Now().Add(2 * time.Second)
	for {
		status := scanJobs.status(1)
		if status.Phase == libraryScanPhaseFailed {
			if status.Enriching {
				t.Fatalf("expected enrichment to stop after failure, got %+v", status)
			}
			if status.Added != 1 || status.Processed != 1 {
				t.Fatalf("expected discovery counts to be preserved, got %+v", status)
			}
			if status.Error != enrichErr.Error() {
				t.Fatalf("error = %q, want %q", status.Error, enrichErr.Error())
			}
			if status.RetryCount != 1 || status.NextRetryAt == "" {
				t.Fatalf("expected retry to be scheduled, got %+v", status)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected enrichment failure to mark job failed, got %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLibraryScanManager_PreservesRerunPartialSubpathsWhileScanning(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	scanJobs.mu.Lock()
	scanJobs.jobs[1] = libraryScanStatus{LibraryID: 1, Phase: libraryScanPhaseScanning}
	scanJobs.mu.Unlock()

	scanJobs.start(1, "/tv", db.LibraryTypeTV, false, []string{"Show A"})

	got := scanJobs.reruns[1].subpaths
	if len(got) != 1 || got[0] != "Show A" {
		t.Fatalf("rerun subpaths = %#v", got)
	}
}

func TestLibraryScanManager_QueueAutomatedScanUsesParentDirectoryForMissingPath(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	root := t.TempDir()
	seasonDir := filepath.Join(root, "Show A", "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("mkdir season dir: %v", err)
	}
	episodePath := filepath.Join(seasonDir, "Show A - S01E01.mkv")
	if err := os.WriteFile(episodePath, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write episode: %v", err)
	}
	if err := os.Remove(episodePath); err != nil {
		t.Fatalf("remove episode: %v", err)
	}

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	scanJobs.queueAutomatedScan(1, root, db.LibraryTypeTV, episodePath)

	got := scanJobs.scanSubpaths(1)
	want := filepath.Join("Show A", "Season 1")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("scan subpaths = %#v, want [%q]", got, want)
	}
}

func TestDetectLibraryPollChanges_IgnoresUnchangedSnapshots(t *testing.T) {
	root := t.TempDir()
	showDir := filepath.Join(root, "Show A")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatalf("mkdir show dir: %v", err)
	}
	episodePath := filepath.Join(showDir, "Show A - S01E01.mkv")
	if err := os.WriteFile(episodePath, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write episode: %v", err)
	}

	first, err := snapshotLibraryPollState(root)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	second, err := snapshotLibraryPollState(root)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	if got := detectLibraryPollChanges(root, first, second); len(got) != 0 {
		t.Fatalf("unexpected changed paths: %#v", got)
	}
}

func TestDetectLibraryPollChanges_ReportsChangedEntries(t *testing.T) {
	root := t.TempDir()
	showDir := filepath.Join(root, "Show A")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatalf("mkdir show dir: %v", err)
	}
	episodePath := filepath.Join(showDir, "Show A - S01E01.mkv")
	if err := os.WriteFile(episodePath, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write episode: %v", err)
	}

	first, err := snapshotLibraryPollState(root)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if err := os.WriteFile(episodePath, []byte("updated video bytes"), 0o644); err != nil {
		t.Fatalf("rewrite episode: %v", err)
	}
	second, err := snapshotLibraryPollState(root)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	got := detectLibraryPollChanges(root, first, second)
	if len(got) != 1 || got[0] != episodePath {
		t.Fatalf("changed paths = %#v, want [%q]", got, episodePath)
	}
}

func TestStartLibraryScan_RejectsInvalidSubpath(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	root := t.TempDir()
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`, userID, "TV", db.LibraryTypeTV, root, now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	handler := &LibraryHandler{
		DB:       dbConn,
		ScanJobs: NewLibraryScanManager(dbConn, nil, nil),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+strconv.Itoa(libraryID)+"/scan/start?subpath=../outside", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.StartLibraryScan(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLibraryScanManager_StartDoesNotBlockOnEstimate(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	root := t.TempDir()
	showDir := filepath.Join(root, "Show", "Season 1")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatalf("mkdir show dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "Show - S01E01.mkv"), []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write episode: %v", err)
	}
	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "estimate@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO libraries (id, user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?, ?)`, 7, userID, "TV", db.LibraryTypeTV, root, now); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	startedAt := time.Now()
	status := scanJobs.start(7, root, db.LibraryTypeTV, false, nil)
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("start took too long: %s", elapsed)
	}
	if status.LibraryID != 7 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.EstimatedItems != 0 {
		t.Fatalf("estimated items = %d, want 0", status.EstimatedItems)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		status = scanJobs.status(7)
		if status.Phase == libraryScanPhaseCompleted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected scan to complete, got %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLibraryScanManager_StartClearsPendingRetryState(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	root := t.TempDir()
	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	scanJobs.mu.Lock()
	scanJobs.jobs[7] = libraryScanStatus{
		LibraryID:   7,
		Phase:       libraryScanPhaseCompleted,
		RetryCount:  3,
		MaxRetries:  3,
		NextRetryAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		Error:       "temporary failure",
		LastError:   "temporary failure",
	}
	scanJobs.retryTimers[7] = time.AfterFunc(time.Hour, func() {})
	scanJobs.mu.Unlock()

	status := scanJobs.start(7, root, db.LibraryTypeTV, false, nil)

	if status.RetryCount != 0 || status.NextRetryAt != "" || status.Error != "" || status.LastError != "" {
		t.Fatalf("unexpected retry state after start: %+v", status)
	}
	if _, ok := scanJobs.retryTimers[7]; ok {
		t.Fatal("expected pending retry timer to be cleared")
	}
}

func TestLibraryScanManager_FinishSuccessResetsRetryCount(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	scanJobs.mu.Lock()
	scanJobs.jobs[9] = libraryScanStatus{
		LibraryID:   9,
		Phase:       libraryScanPhaseScanning,
		RetryCount:  2,
		MaxRetries:  3,
		NextRetryAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		Error:       "temporary failure",
		LastError:   "temporary failure",
	}
	scanJobs.retryTimers[9] = time.AfterFunc(time.Hour, func() {})
	scanJobs.activeScanID = 9
	scanJobs.mu.Unlock()

	scanJobs.finish(9, libraryScanPhaseCompleted, db.ScanResult{}, "")

	status := scanJobs.status(9)
	if status.RetryCount != 0 || status.NextRetryAt != "" || status.Error != "" || status.LastError != "" {
		t.Fatalf("unexpected retry state after finish: %+v", status)
	}
	if _, ok := scanJobs.retryTimers[9]; ok {
		t.Fatal("expected pending retry timer to be cleared")
	}
}

func TestLibraryScanManager_StatusWarnsWhenCompletedScanFindsNoFiles(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	scanJobs := NewLibraryScanManager(dbConn, nil, nil)
	scanJobs.jobs[7] = libraryScanStatus{
		LibraryID: 7,
		Phase:     libraryScanPhaseCompleted,
	}
	scanJobs.paths[7] = "/movies"

	status := scanJobs.status(7)

	if !strings.Contains(status.Error, "No media files were found under /movies") {
		t.Fatalf("unexpected warning: %q", status.Error)
	}
}

func TestListLibraryMedia_EmbeddedSubtitlesUseCamelCaseStreamIndex(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"test@test.com",
		"hash",
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"TV",
		db.LibraryTypeTV,
		"/tv",
		now,
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var episodeID int
	if err := dbConn.QueryRow(
		`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Test Show - S01E01",
		"/tv/Test Show/Season 1/Test Show - S01E01.mkv",
		0,
		db.MatchStatusLocal,
		1,
		1,
	).Scan(&episodeID); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	var mediaID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, db.LibraryTypeTV, episodeID).
		Scan(&mediaID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO embedded_subtitles (media_id, stream_index, language, title) VALUES (?, ?, ?, ?)`,
		mediaID,
		3,
		"eng",
		"English",
	); err != nil {
		t.Fatalf("insert embedded subtitle: %v", err)
	}

	handler := &LibraryHandler{DB: dbConn}
	req := httptest.NewRequest(http.MethodGet, "/api/libraries/"+strconv.Itoa(libraryID)+"/media", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.ListLibraryMedia(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(payload))
	}
	embedded, ok := payload[0]["embeddedSubtitles"].([]any)
	if !ok || len(embedded) != 1 {
		t.Fatalf("unexpected embeddedSubtitles payload: %#v", payload[0]["embeddedSubtitles"])
	}
	entry, ok := embedded[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected embedded subtitle entry: %#v", embedded[0])
	}
	if _, exists := entry["media_id"]; exists {
		t.Fatalf("embedded subtitle should not include media_id: %#v", entry)
	}
	if _, exists := entry["stream_index"]; exists {
		t.Fatalf("embedded subtitle should not include stream_index: %#v", entry)
	}
	if got, ok := entry["streamIndex"].(float64); !ok || got != 3 {
		t.Fatalf("embedded subtitle streamIndex = %#v", entry["streamIndex"])
	}
}

func TestListLibraryMedia_IncludesIdentifyStateOverlay(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"identify@test.com",
		"hash",
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"Movies",
		db.LibraryTypeMovie,
		"/movies",
		now,
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var movieID int
	moviePath := "/movies/Queued Movie (2025)/Queued Movie.mkv"
	if err := dbConn.QueryRow(
		`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Queued Movie",
		moviePath,
		0,
		db.MatchStatusLocal,
	).Scan(&movieID); err != nil {
		t.Fatalf("insert movie: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?)`, db.LibraryTypeMovie, movieID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}

	tracker := newIdentifyRunTracker()
	tracker.startLibrary(libraryID, []db.IdentificationRow{{
		RefID: movieID,
		Kind:  db.LibraryTypeMovie,
		Path:  moviePath,
	}})
	tracker.setState(libraryID, db.LibraryTypeMovie, moviePath, "identifying")

	handler := &LibraryHandler{DB: dbConn, identifyRun: tracker}
	req := httptest.NewRequest(http.MethodGet, "/api/libraries/"+strconv.Itoa(libraryID)+"/media", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.ListLibraryMedia(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(payload))
	}
	if got := payload[0]["identify_state"]; got != "identifying" {
		t.Fatalf("identify_state = %#v", got)
	}
}

func TestListLibraryMedia_EmbeddedAudioTracksUseCamelCaseStreamIndex(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer dbConn.Close()

	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP) RETURNING id`,
		"audio@test.local",
		"hash",
		true,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP) RETURNING id`,
		userID,
		"Movies",
		db.LibraryTypeMovie,
		"/movies",
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	var movieID int
	if err := dbConn.QueryRow(
		`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Track Test",
		"/movies/Track Test (2025)/Track Test.mkv",
		0,
		db.MatchStatusLocal,
	).Scan(&movieID); err != nil {
		t.Fatalf("insert movie: %v", err)
	}

	var mediaID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, db.LibraryTypeMovie, movieID).
		Scan(&mediaID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO embedded_audio_tracks (media_id, stream_index, language, title) VALUES (?, ?, ?, ?)`,
		mediaID,
		2,
		"jpn",
		"Japanese Stereo",
	); err != nil {
		t.Fatalf("insert embedded audio track: %v", err)
	}

	handler := &LibraryHandler{DB: dbConn}
	req := httptest.NewRequest(http.MethodGet, "/api/libraries/"+strconv.Itoa(libraryID)+"/media", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.ListLibraryMedia(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(payload))
	}
	audioTracks, ok := payload[0]["embeddedAudioTracks"].([]any)
	if !ok || len(audioTracks) != 1 {
		t.Fatalf("unexpected embeddedAudioTracks payload: %#v", payload[0]["embeddedAudioTracks"])
	}
	entry, ok := audioTracks[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected embedded audio track entry: %#v", audioTracks[0])
	}
	if _, exists := entry["media_id"]; exists {
		t.Fatalf("embedded audio track should not include media_id: %#v", entry)
	}
	if _, exists := entry["stream_index"]; exists {
		t.Fatalf("embedded audio track should not include stream_index: %#v", entry)
	}
	if got, ok := entry["streamIndex"].(float64); !ok || got != 2 {
		t.Fatalf("embedded audio track streamIndex = %#v", entry["streamIndex"])
	}
}

func TestListLibraryMedia_SubtitlesOmitInternalFields(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer dbConn.Close()

	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP) RETURNING id`,
		"subtitle@test.local",
		"hash",
		true,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP) RETURNING id`,
		userID,
		"Movies",
		db.LibraryTypeMovie,
		"/movies",
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	var movieID int
	if err := dbConn.QueryRow(
		`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		"Subtitle Test",
		"/movies/Subtitle Test (2025)/Subtitle Test.mkv",
		0,
		db.MatchStatusLocal,
	).Scan(&movieID); err != nil {
		t.Fatalf("insert movie: %v", err)
	}

	var mediaID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, db.LibraryTypeMovie, movieID).
		Scan(&mediaID); err != nil {
		t.Fatalf("insert media global row: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO subtitles (media_id, title, language, format, path) VALUES (?, ?, ?, ?, ?)`,
		mediaID,
		"English",
		"eng",
		"srt",
		"/movies/Subtitle Test (2025)/Subtitle Test.eng.srt",
	); err != nil {
		t.Fatalf("insert subtitle: %v", err)
	}

	handler := &LibraryHandler{DB: dbConn}
	req := httptest.NewRequest(http.MethodGet, "/api/libraries/"+strconv.Itoa(libraryID)+"/media", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.ListLibraryMedia(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(payload))
	}
	subtitles, ok := payload[0]["subtitles"].([]any)
	if !ok || len(subtitles) != 1 {
		t.Fatalf("unexpected subtitles payload: %#v", payload[0]["subtitles"])
	}
	entry, ok := subtitles[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected subtitle entry: %#v", subtitles[0])
	}
	if _, exists := entry["media_id"]; exists {
		t.Fatalf("subtitle should not include media_id: %#v", entry)
	}
	if _, exists := entry["path"]; exists {
		t.Fatalf("subtitle should not include path: %#v", entry)
	}
}

func TestListLibraryMedia_EmptyLibraryReturnsJSONArray(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer dbConn.Close()

	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP) RETURNING id`,
		"empty@test.local",
		"hash",
		true,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP) RETURNING id`,
		userID,
		"Movies",
		db.LibraryTypeMovie,
		"/movies",
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	handler := &LibraryHandler{DB: dbConn}
	req := httptest.NewRequest(http.MethodGet, "/api/libraries/"+strconv.Itoa(libraryID)+"/media", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(libraryID))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.ListLibraryMedia(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", rec.Body.String())
	}
}

func TestGetDiscover_AttachesMovieTVAndAnimeLibraryMatches(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"discover@test.com",
		"hash",
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var movieLibraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"Movies",
		db.LibraryTypeMovie,
		"/movies",
		now,
	).Scan(&movieLibraryID); err != nil {
		t.Fatalf("insert movie library: %v", err)
	}
	var tvLibraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"TV",
		db.LibraryTypeTV,
		"/tv",
		now,
	).Scan(&tvLibraryID); err != nil {
		t.Fatalf("insert tv library: %v", err)
	}
	var animeLibraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"Anime",
		db.LibraryTypeAnime,
		"/anime",
		now,
	).Scan(&animeLibraryID); err != nil {
		t.Fatalf("insert anime library: %v", err)
	}

	if _, err := dbConn.Exec(
		`INSERT INTO movies (library_id, title, path, duration, match_status, tmdb_id) VALUES (?, ?, ?, ?, ?, ?)`,
		movieLibraryID,
		"Movie Match",
		"/movies/movie-match.mkv",
		0,
		db.MatchStatusIdentified,
		101,
	); err != nil {
		t.Fatalf("insert movie: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, tmdb_id, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tvLibraryID,
		"TV Match - S01E01 - Pilot",
		"/tv/tv-match-s01e01.mkv",
		0,
		db.MatchStatusIdentified,
		202,
		1,
		1,
	); err != nil {
		t.Fatalf("insert tv episode: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO anime_episodes (library_id, title, path, duration, match_status, tmdb_id, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		animeLibraryID,
		"Anime Match - S01E01 - Start",
		"/anime/anime-match-s01e01.mkv",
		0,
		db.MatchStatusIdentified,
		303,
		1,
		1,
	); err != nil {
		t.Fatalf("insert anime episode: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Discover: &discoverStub{
			getDiscover: func(context.Context) (*metadata.DiscoverResponse, error) {
				return &metadata.DiscoverResponse{
					Shelves: []metadata.DiscoverShelf{
						{
							ID:    "trending",
							Title: "Trending Now",
							Items: []metadata.DiscoverItem{
								{MediaType: metadata.DiscoverMediaTypeMovie, TMDBID: 101, Title: "Movie Match"},
								{MediaType: metadata.DiscoverMediaTypeTV, TMDBID: 202, Title: "TV Match"},
								{MediaType: metadata.DiscoverMediaTypeTV, TMDBID: 303, Title: "Anime Match"},
							},
						},
					},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/discover", nil)
	req = req.WithContext(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}))
	rec := httptest.NewRecorder()

	handler.GetDiscover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload metadata.DiscoverResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	items := payload.Shelves[0].Items
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if got := items[0].LibraryMatches[0].Kind; got != "movie" {
		t.Fatalf("movie kind = %q", got)
	}
	if got := items[1].LibraryMatches[0].ShowKey; got != "tmdb-202" {
		t.Fatalf("tv show key = %q", got)
	}
	if got := items[2].LibraryMatches[0].LibraryType; got != db.LibraryTypeAnime {
		t.Fatalf("anime library type = %q", got)
	}
}

func TestGetDiscover_ReturnsServiceUnavailableWhenTMDBMissing(t *testing.T) {
	handler := &LibraryHandler{
		Discover: &discoverStub{
			getDiscover: func(context.Context) (*metadata.DiscoverResponse, error) {
				return nil, metadata.ErrTMDBNotConfigured
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/discover", nil)
	req = req.WithContext(withUser(req.Context(), &db.User{ID: 1, IsAdmin: true}))
	rec := httptest.NewRecorder()

	handler.GetDiscover(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "TMDB_API_KEY") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestGetDiscoverTitleDetails_AttachesLibraryMatch(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"detail@test.com",
		"hash",
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"TV",
		db.LibraryTypeTV,
		"/tv",
		now,
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, tmdb_id, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID,
		"Detail Match - S01E01 - Start",
		"/tv/detail-match-s01e01.mkv",
		0,
		db.MatchStatusIdentified,
		404,
		1,
		1,
	); err != nil {
		t.Fatalf("insert tv episode: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Discover: &discoverStub{
			getDiscoverTitleDetail: func(context.Context, metadata.DiscoverMediaType, int) (*metadata.DiscoverTitleDetails, error) {
				return &metadata.DiscoverTitleDetails{
					MediaType:    metadata.DiscoverMediaTypeTV,
					TMDBID:       404,
					Title:        "Detail Match",
					Overview:     "Overview",
					Genres:       []string{"Drama"},
					Videos:       []metadata.DiscoverTitleVideo{},
					PosterPath:   "/poster.jpg",
					BackdropPath: "/backdrop.jpg",
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/discover/tv/404", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mediaType", "tv")
	rctx.URLParams.Add("tmdbId", "404")
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	handler.GetDiscoverTitleDetails(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload metadata.DiscoverTitleDetails
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.LibraryMatches) != 1 {
		t.Fatalf("library matches = %+v", payload.LibraryMatches)
	}
	if payload.LibraryMatches[0].ShowKey != "tmdb-404" {
		t.Fatalf("show key = %q", payload.LibraryMatches[0].ShowKey)
	}
}

func TestGetDiscover_DoesNotMatchTVShowsWithoutActiveEpisodes(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"stale-show@test.com",
		"hash",
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID,
		"TV",
		db.LibraryTypeTV,
		"/tv",
		now,
	).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var showID int
	if err := dbConn.QueryRow(
		`INSERT INTO shows (library_id, kind, tmdb_id, title, title_key, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID,
		db.LibraryTypeTV,
		777,
		"Gone Show",
		"goneshow",
		now.Format(time.RFC3339),
		now.Format(time.RFC3339),
	).Scan(&showID); err != nil {
		t.Fatalf("insert show: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, tmdb_id, show_id, season, episode, missing_since) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID,
		"Gone Show - S01E01 - Pilot",
		"/tv/gone-show-s01e01.mkv",
		0,
		db.MatchStatusIdentified,
		777,
		showID,
		1,
		1,
		now.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert missing tv episode: %v", err)
	}

	handler := &LibraryHandler{
		DB: dbConn,
		Discover: &discoverStub{
			getDiscover: func(context.Context) (*metadata.DiscoverResponse, error) {
				return &metadata.DiscoverResponse{
					Shelves: []metadata.DiscoverShelf{
						{
							ID:    "trending",
							Title: "Trending",
							Items: []metadata.DiscoverItem{
								{MediaType: metadata.DiscoverMediaTypeTV, TMDBID: 777, Title: "Gone Show"},
							},
						},
					},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/discover", nil)
	req = req.WithContext(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}))
	rec := httptest.NewRecorder()

	handler.GetDiscover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload metadata.DiscoverResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Shelves) != 1 || len(payload.Shelves[0].Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.Shelves[0].Items[0].LibraryMatches) != 0 {
		t.Fatalf("expected no library matches, got %+v", payload.Shelves[0].Items[0].LibraryMatches)
	}
}
