package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestIdentifyLibrary_UsesAnimeSearchFallbackAndMarksReviewNeeded(t *testing.T) {
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
	if !reviewNeeded {
		t.Fatal("expected metadata_review_needed to be true")
	}
}

func TestIdentifyLibrary_UsesTVSearchFallbackAndMarksReviewNeeded(t *testing.T) {
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
	prevWorkers := identifyLibraryWorkers
	prevRateInterval := identifyRateLimitInterval
	identifyInitialTimeout = 10 * time.Millisecond
	identifyRetryTimeout = 25 * time.Millisecond
	identifyLibraryWorkers = 2
	identifyRateLimitInterval = time.Millisecond
	t.Cleanup(func() {
		identifyInitialTimeout = prevInitialTimeout
		identifyRetryTimeout = prevRetryTimeout
		identifyLibraryWorkers = prevWorkers
		identifyRateLimitInterval = prevRateInterval
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

	estimateStarted := make(chan struct{}, 1)
	releaseEstimate := make(chan struct{})
	var releaseEstimateOnce sync.Once
	originalEstimate := estimateLibraryFiles
	estimateLibraryFiles = func(ctx context.Context, root, mediaType string) (int, error) {
		estimateStarted <- struct{}{}
		<-releaseEstimate
		return originalEstimate(ctx, root, mediaType)
	}
	t.Cleanup(func() {
		estimateLibraryFiles = originalEstimate
		releaseEstimateOnce.Do(func() {
			close(releaseEstimate)
		})
	})

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
		t.Fatalf("estimated items = %d", status.EstimatedItems)
	}

	select {
	case <-estimateStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected background estimate to start")
	}

	releaseEstimateOnce.Do(func() {
		close(releaseEstimate)
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		status = scanJobs.status(7)
		if status.EstimatedItems == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected estimated items update, got %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
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
