package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"plum/internal/db"
	"plum/internal/metadata"

	_ "modernc.org/sqlite"
)

type identifyStub struct {
	movie func(metadata.MediaInfo) *metadata.MatchResult
	anime func(metadata.MediaInfo) *metadata.MatchResult
}

func (s *identifyStub) IdentifyTV(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
	return nil
}

func (s *identifyStub) IdentifyAnime(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if s.anime == nil {
		return nil
	}
	return s.anime(info)
}

func (s *identifyStub) IdentifyMovie(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if s.movie == nil {
		return nil
	}
	return s.movie(info)
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
			movie: func(info metadata.MediaInfo) *metadata.MatchResult {
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
			anime: func(info metadata.MediaInfo) *metadata.MatchResult {
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
