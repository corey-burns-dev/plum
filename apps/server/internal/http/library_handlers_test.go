package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"plum/internal/db"
	"plum/internal/metadata"

	_ "modernc.org/sqlite"
)

type identifyStub struct {
	tv    func(context.Context, metadata.MediaInfo) *metadata.MatchResult
	movie func(context.Context, metadata.MediaInfo) *metadata.MatchResult
	anime func(context.Context, metadata.MediaInfo) *metadata.MatchResult
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
	if err := dbConn.QueryRow(`INSERT INTO movies (library_id, title, path, duration, match_status, tmdb_id, poster_path) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, libraryID, "Finished Movie", finishedPath, 0, db.MatchStatusIdentified, 999, "/poster.jpg").Scan(&finishedID); err != nil {
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
