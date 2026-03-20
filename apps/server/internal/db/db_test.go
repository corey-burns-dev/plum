package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"plum/internal/metadata"

	_ "modernc.org/sqlite"
)

// newTestDB connects to SQLite (PLUM_TEST_DATABASE_URL or :memory:) and creates schema.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn := os.Getenv("PLUM_TEST_DATABASE_URL")
	if conn == "" {
		conn = ":memory:"
	}
	db, err := sql.Open("sqlite", conn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("pragma foreign_keys: %v", err)
	}
	if err := createSchema(db); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	// Insert a test user and two libraries (tv and movie) for scans
	_, _ = db.Exec(`DELETE FROM libraries`)
	_, _ = db.Exec(`DELETE FROM users`)
	var userID int
	err = db.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "test@test.com", "hash", time.Now().UTC()).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	now := time.Now().UTC()
	_, err = db.Exec(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?), (?, 'Movies', 'movie', ?, ?)`,
		userID, "TV", "tv", "/tv", now, userID, "/movies", now)
	if err != nil {
		t.Fatalf("insert libraries: %v", err)
	}
	return db
}

func getLibraryID(t *testing.T, db *sql.DB, typ string) int {
	t.Helper()
	var id int
	err := db.QueryRow(`SELECT id FROM libraries WHERE type = ? LIMIT 1`, typ).Scan(&id)
	if err != nil {
		t.Fatalf("get library id for %s: %v", typ, err)
	}
	return id
}

func createLibraryForTest(t *testing.T, db *sql.DB, typ, path string) int {
	t.Helper()
	var userID int
	if err := db.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	var id int
	if err := db.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID, fmt.Sprintf("%s library", typ), typ, path, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("create library %s: %v", typ, err)
	}
	return id
}

func columnExistsForTest(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scan pragma table_info(%s): %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma table_info(%s): %v", table, err)
	}
	return false
}

func indexExistsForTest(t *testing.T, db *sql.DB, table, index string) bool {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", table))
	if err != nil {
		t.Fatalf("pragma index_list(%s): %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan pragma index_list(%s): %v", table, err)
		}
		if name == index {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma index_list(%s): %v", table, err)
	}
	return false
}

func TestCreateSchema_MigratesLegacyTables(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dbConn.Close()

	legacySchema := `
CREATE TABLE users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL
);
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at DATETIME NOT NULL,
  expires_at DATETIME NOT NULL
);
CREATE TABLE libraries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('tv','movie','music','anime')),
  path TEXT NOT NULL,
  created_at DATETIME NOT NULL
);
CREATE TABLE media_global (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL CHECK (kind IN ('movie','tv','anime','music')),
  ref_id INTEGER NOT NULL
);
CREATE TABLE movies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  tmdb_id INTEGER,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0
);
CREATE TABLE tv_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  tmdb_id INTEGER,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0
);
CREATE TABLE anime_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  tmdb_id INTEGER,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0
);
CREATE TABLE music_tracks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE library_job_status (
  library_id INTEGER PRIMARY KEY REFERENCES libraries(id) ON DELETE CASCADE,
  phase TEXT NOT NULL
);`
	if _, err := dbConn.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	if err := createSchema(dbConn); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	for _, tc := range []struct {
		table  string
		column string
	}{
		{table: "libraries", column: "preferred_audio_language"},
		{table: "libraries", column: "preferred_subtitle_language"},
		{table: "libraries", column: "subtitles_enabled_by_default"},
		{table: "movies", column: "match_status"},
		{table: "movies", column: "tvdb_id"},
		{table: "movies", column: "imdb_id"},
		{table: "movies", column: "imdb_rating"},
		{table: "tv_episodes", column: "season"},
		{table: "tv_episodes", column: "episode"},
		{table: "tv_episodes", column: "metadata_review_needed"},
		{table: "tv_episodes", column: "metadata_confirmed"},
		{table: "tv_episodes", column: "thumbnail_path"},
		{table: "anime_episodes", column: "metadata_review_needed"},
		{table: "anime_episodes", column: "metadata_confirmed"},
		{table: "music_tracks", column: "artist"},
		{table: "music_tracks", column: "album"},
		{table: "music_tracks", column: "poster_path"},
		{table: "music_tracks", column: "musicbrainz_artist_id"},
		{table: "music_tracks", column: "musicbrainz_release_group_id"},
		{table: "music_tracks", column: "musicbrainz_release_id"},
		{table: "music_tracks", column: "musicbrainz_recording_id"},
		{table: "library_job_status", column: "identify_phase"},
		{table: "library_job_status", column: "queued_at"},
		{table: "library_job_status", column: "estimated_items"},
		{table: "library_job_status", column: "updated_at"},
	} {
		if !columnExistsForTest(t, dbConn, tc.table, tc.column) {
			t.Fatalf("expected %s.%s to exist after migration", tc.table, tc.column)
		}
	}

	for _, tc := range []struct {
		table string
		index string
	}{
		{table: "subtitles", index: "idx_subtitles_path"},
		{table: "library_job_status", index: "idx_library_job_status_phase_updated_at"},
		{table: "movies", index: "idx_movies_library_match_status"},
		{table: "tv_episodes", index: "idx_tv_episodes_library_match_status"},
		{table: "anime_episodes", index: "idx_anime_episodes_library_match_status"},
	} {
		if !indexExistsForTest(t, dbConn, tc.table, tc.index) {
			t.Fatalf("expected index %s on %s", tc.index, tc.table)
		}
	}
}

func TestInsertScannedItem_RollsBackOnMediaGlobalFailure(t *testing.T) {
	dbConn := newTestDB(t)
	libraryID := getLibraryID(t, dbConn, "movie")

	_, _, err := insertScannedItem(context.Background(), dbConn, "movies", "invalid", libraryID, MediaItem{
		Title:       "Broken Insert",
		Path:        "/movies/broken.mp4",
		Duration:    60,
		MatchStatus: MatchStatusLocal,
	})
	if err == nil {
		t.Fatal("expected insertScannedItem to fail")
	}

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM movies WHERE path = ?`, "/movies/broken.mp4").Scan(&count); err != nil {
		t.Fatalf("count movies: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected rollback to remove inserted movie row, found %d", count)
	}
}

func TestHandleStreamSubtitle_DoesNotWritePartialResponseOnConversionError(t *testing.T) {
	dbConn := newTestDB(t)
	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID, "Movies 2", "movie", "/movies2", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var refID int
	if err := dbConn.QueryRow(`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		libraryID, "Missing", "/movies/missing.mp4", 100, MatchStatusLocal).Scan(&refID); err != nil {
		t.Fatalf("insert movie: %v", err)
	}
	var mediaID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, LibraryTypeMovie, refID).Scan(&mediaID); err != nil {
		t.Fatalf("insert media_global: %v", err)
	}
	var subtitleID int
	if err := dbConn.QueryRow(`INSERT INTO subtitles (media_id, title, language, format, path) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		mediaID, "Broken", "en", "srt", "/does/not/exist.srt").Scan(&subtitleID); err != nil {
		t.Fatalf("insert subtitle: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/subtitles/%d", subtitleID), nil)
	err := HandleStreamSubtitle(rec, req, dbConn, subtitleID)
	if err == nil {
		t.Fatal("expected subtitle conversion error")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty response body on subtitle error, got %q", rec.Body.String())
	}
}

func TestHandleStreamEmbeddedSubtitle_ReturnsNotFoundForMissingStream(t *testing.T) {
	dbConn := newTestDB(t)
	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID, "TV 2", "tv", "/tv2", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var refID int
	if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID, "Missing Stream - S01E01", "/tv2/Missing Stream/Season 1/Episode 1.mkv", 100, MatchStatusLocal, 1, 1).Scan(&refID); err != nil {
		t.Fatalf("insert tv episode: %v", err)
	}
	var mediaID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, LibraryTypeTV, refID).Scan(&mediaID); err != nil {
		t.Fatalf("insert media_global: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO embedded_subtitles (media_id, stream_index, language, title) VALUES (?, ?, ?, ?)`,
		mediaID, 1, "en", "English"); err != nil {
		t.Fatalf("insert embedded subtitle: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/media/%d/subtitles/embedded/%d", mediaID, 99), nil)
	err := HandleStreamEmbeddedSubtitle(rec, req, dbConn, mediaID, 99)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty response body on embedded subtitle error, got %q", rec.Body.String())
	}
}

func TestListIdentifiableByLibrary_SkipsConfirmedEpisodes(t *testing.T) {
	dbConn := newTestDB(t)
	tvLibID := getLibraryID(t, dbConn, "tv")

	if _, err := dbConn.Exec(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, tmdb_id, metadata_confirmed, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tvLibID,
		"Confirmed Show - S01E01",
		"/tv/Confirmed Show/Season 1/Confirmed Show - S01E01.mkv",
		120,
		MatchStatusIdentified,
		123,
		true,
		1,
		1,
	); err != nil {
		t.Fatalf("insert confirmed episode: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tvLibID,
		"Unmatched Show - S01E02",
		"/tv/Unmatched Show/Season 1/Unmatched Show - S01E02.mkv",
		120,
		MatchStatusUnmatched,
		1,
		2,
	); err != nil {
		t.Fatalf("insert unmatched episode: %v", err)
	}

	rows, err := ListIdentifiableByLibrary(dbConn, tvLibID)
	if err != nil {
		t.Fatalf("ListIdentifiableByLibrary: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 identifiable row, got %d", len(rows))
	}
	if rows[0].Title != "Unmatched Show - S01E02" {
		t.Fatalf("unexpected identifiable row: %#v", rows[0])
	}
}

// TestHandleScanLibrary_RecursesSubdirectories verifies that HandleScanLibrary walks
// nested directories and inserts into the correct category tables (tv_episodes, movies).
func TestHandleScanLibrary_RecursesSubdirectories(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, "tv")
	movieLibID := getLibraryID(t, db, "movie")

	tmp := t.TempDir()
	tvRoot := filepath.Join(tmp, "SomeShow")
	movieRoot := filepath.Join(tmp, "Movies")

	if err := os.MkdirAll(filepath.Join(tvRoot, "Season 1"), 0o755); err != nil {
		t.Fatalf("mkdir tv tree: %v", err)
	}
	if err := os.MkdirAll(movieRoot, 0o755); err != nil {
		t.Fatalf("mkdir movies tree: %v", err)
	}

	tvFile1 := filepath.Join(tvRoot, "Season 1", "episode.s01e01.mkv")
	tvFile2 := filepath.Join(tvRoot, "Season 1", "episode.s01e02.mkv")
	movieFile := filepath.Join(movieRoot, "movie1.mp4")

	for _, p := range []string{tvFile1, tvFile2, movieFile} {
		if err := os.WriteFile(p, []byte("test"), 0o644); err != nil {
			t.Fatalf("write fake media %s: %v", p, err)
		}
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	ctx := context.Background()

	addedTV, err := HandleScanLibrary(ctx, db, tvRoot, LibraryTypeTV, tvLibID, nil)
	if err != nil {
		t.Fatalf("scan tv library: %v", err)
	}
	if addedTV.Added != 2 {
		t.Fatalf("expected 2 tv items added, got %d", addedTV.Added)
	}

	addedMovies, err := HandleScanLibrary(ctx, db, movieRoot, LibraryTypeMovie, movieLibID, nil)
	if err != nil {
		t.Fatalf("scan movie library: %v", err)
	}
	if addedMovies.Added != 1 {
		t.Fatalf("expected 1 movie item added, got %d", addedMovies.Added)
	}

	var countTV, countMovies int
	_ = db.QueryRow(`SELECT COUNT(*) FROM tv_episodes WHERE library_id = ?`, tvLibID).Scan(&countTV)
	_ = db.QueryRow(`SELECT COUNT(*) FROM movies WHERE library_id = ?`, movieLibID).Scan(&countMovies)
	if countTV != 2 || countMovies != 1 {
		t.Fatalf("expected 2 tv_episodes and 1 movie; got tv=%d movie=%d", countTV, countMovies)
	}

	rows, err := db.Query(`SELECT m.path FROM tv_episodes m WHERE m.library_id = ? UNION ALL SELECT m.path FROM movies m WHERE m.library_id = ?`, tvLibID, movieLibID)
	if err != nil {
		t.Fatalf("query paths: %v", err)
	}
	defer rows.Close()
	var foundTV1, foundTV2, foundMovie bool
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan path: %v", err)
		}
		switch p {
		case tvFile1:
			foundTV1 = true
		case tvFile2:
			foundTV2 = true
		case movieFile:
			foundMovie = true
		}
	}
	if !foundTV1 || !foundTV2 || !foundMovie {
		t.Fatalf("expected paths for tv1=%v tv2=%v movie=%v", foundTV1, foundTV2, foundMovie)
	}
}

// TestHandleScanLibrary_IsIdempotent verifies that rescanning the same root does
// not create duplicate rows in the category table.
func TestHandleScanLibrary_IsIdempotent(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, "tv")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "SomeShow", "Season 1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	file := filepath.Join(root, "episode.s01e01.mkv")
	if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
		t.Fatalf("write fake media: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	ctx := context.Background()

	addedFirst, err := HandleScanLibrary(ctx, db, filepath.Join(tmp, "SomeShow"), LibraryTypeTV, tvLibID, nil)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if addedFirst.Added != 1 {
		t.Fatalf("expected 1 item on first scan, got %d", addedFirst.Added)
	}

	addedSecond, err := HandleScanLibrary(ctx, db, filepath.Join(tmp, "SomeShow"), LibraryTypeTV, tvLibID, nil)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if addedSecond.Added != 0 {
		t.Fatalf("expected 0 items on second scan, got %d", addedSecond.Added)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tv_episodes WHERE library_id = ?`, tvLibID).Scan(&count); err != nil {
		t.Fatalf("count tv_episodes: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 tv_episode row after two scans, got %d", count)
	}
}

func TestHandleScanLibraryWithOptions_ProgressSeesImportedRowsBeforeCompletion(t *testing.T) {
	dbConn, err := InitDB(filepath.Join(t.TempDir(), "plum.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"progress@test.com",
		"hash",
		time.Now().UTC(),
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	tvLibID := createLibraryForTest(t, dbConn, LibraryTypeTV, "/tv-progress")
	root := t.TempDir()
	showDir := filepath.Join(root, "Fast Show", "Season 1")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatalf("mkdir show dir: %v", err)
	}
	for i := 1; i <= 2; i++ {
		file := filepath.Join(showDir, fmt.Sprintf("Fast Show - S01E0%d.mkv", i))
		if err := os.WriteFile(file, []byte("not a real video"), 0o644); err != nil {
			t.Fatalf("write media file: %v", err)
		}
	}

	visibleDuringProgress := false
	result, err := HandleScanLibraryWithOptions(context.Background(), dbConn, root, LibraryTypeTV, tvLibID, ScanOptions{
		Progress: func(progress ScanProgress) {
			if progress.Processed != 1 || visibleDuringProgress {
				return
			}
			var count int
			if err := dbConn.QueryRow(`SELECT COUNT(1) FROM tv_episodes WHERE library_id = ?`, tvLibID).Scan(&count); err != nil {
				t.Fatalf("count imported rows during progress: %v", err)
			}
			if count > 0 {
				visibleDuringProgress = true
			}
		},
	})
	if err != nil {
		t.Fatalf("scan library: %v", err)
	}
	if result.Added != 2 {
		t.Fatalf("added = %d, want 2", result.Added)
	}
	if !visibleDuringProgress {
		t.Fatal("expected imported rows to be visible before scan completion")
	}
}

// mockIdentifier returns fixed metadata for tests.
type mockIdentifier struct {
	tvResult    *metadata.MatchResult
	animeResult *metadata.MatchResult
	movieResult *metadata.MatchResult
}

func (m *mockIdentifier) IdentifyTV(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
	return m.tvResult
}

func (m *mockIdentifier) IdentifyAnime(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
	return m.animeResult
}

func (m *mockIdentifier) IdentifyMovie(_ context.Context, _ metadata.MediaInfo) *metadata.MatchResult {
	return m.movieResult
}

type funcIdentifier struct {
	tv    func(metadata.MediaInfo) *metadata.MatchResult
	anime func(metadata.MediaInfo) *metadata.MatchResult
	movie func(metadata.MediaInfo) *metadata.MatchResult
}

func (f *funcIdentifier) IdentifyTV(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if f.tv == nil {
		return nil
	}
	return f.tv(info)
}

func (f *funcIdentifier) IdentifyAnime(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if f.anime == nil {
		return nil
	}
	return f.anime(info)
}

func (f *funcIdentifier) IdentifyMovie(_ context.Context, info metadata.MediaInfo) *metadata.MatchResult {
	if f.movie == nil {
		return nil
	}
	return f.movie(info)
}

// TestHandleScanLibrary_WithMockIdentifier verifies that when a mock identifier returns a match,
// the stored row has the expected title and overview.
func TestHandleScanLibrary_WithMockIdentifier(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, "tv")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Show", "Season 1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	file := filepath.Join(root, "Show.S01E03.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake media: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	ctx := context.Background()
	mock := &mockIdentifier{
		tvResult: &metadata.MatchResult{
			Title:       "Mock Show - S01E03 - The Episode",
			Overview:    "Mock overview for testing.",
			PosterURL:   "https://example.com/poster.jpg",
			BackdropURL: "https://example.com/backdrop.jpg",
			ReleaseDate: "2024-01-15",
			VoteAverage: 8.5,
			Provider:    "tmdb",
			ExternalID:  "12345",
		},
	}

	added, err := HandleScanLibrary(ctx, db, filepath.Join(tmp, "Show"), LibraryTypeTV, tvLibID, mock)
	if err != nil {
		t.Fatalf("scan with mock: %v", err)
	}
	if added.Added != 1 {
		t.Fatalf("expected 1 item added, got %d", added.Added)
	}

	var title, overview string
	err = db.QueryRow(`SELECT title, overview FROM tv_episodes WHERE library_id = ? AND path = ?`, tvLibID, file).Scan(&title, &overview)
	if err != nil {
		t.Fatalf("query stored row: %v", err)
	}
	if title != "Mock Show - S01E03 - The Episode" {
		t.Errorf("title: got %q", title)
	}
	if overview != "Mock overview for testing." {
		t.Errorf("overview: got %q", overview)
	}
}

func TestHandleScanLibrary_RefreshesMetadataWhenExplicitIDAppears(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, "tv")

	tmp := t.TempDir()
	showRoot := filepath.Join(tmp, "Show")
	seasonRoot := filepath.Join(showRoot, "Season 1")
	if err := os.MkdirAll(seasonRoot, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	file := filepath.Join(seasonRoot, "Show.S01E01.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake media: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	ctx := context.Background()
	initial := &mockIdentifier{
		tvResult: &metadata.MatchResult{
			Title:      "Wrong Show - S01E01 - Wrong",
			Overview:   "wrong",
			Provider:   "tmdb",
			ExternalID: "111",
		},
	}
	if _, err := HandleScanLibrary(ctx, db, tmp, LibraryTypeTV, tvLibID, initial); err != nil {
		t.Fatalf("first scan: %v", err)
	}

	nfo := `<tvshow><uniqueid type="tmdb">222</uniqueid></tvshow>`
	if err := os.WriteFile(filepath.Join(showRoot, "tvshow.nfo"), []byte(nfo), 0o644); err != nil {
		t.Fatalf("write nfo: %v", err)
	}

	refresh := &funcIdentifier{
		tv: func(info metadata.MediaInfo) *metadata.MatchResult {
			if info.TMDBID != 222 {
				t.Fatalf("expected TMDBID 222 from nfo, got %d", info.TMDBID)
			}
			return &metadata.MatchResult{
				Title:      "Right Show - S01E01 - Pilot",
				Overview:   "right",
				Provider:   "tmdb",
				ExternalID: "222",
			}
		},
	}
	if _, err := HandleScanLibrary(ctx, db, tmp, LibraryTypeTV, tvLibID, refresh); err != nil {
		t.Fatalf("second scan: %v", err)
	}

	var title, overview string
	var tmdbID int
	err := db.QueryRow(`SELECT title, overview, COALESCE(tmdb_id, 0) FROM tv_episodes WHERE library_id = ? AND path = ?`, tvLibID, file).Scan(&title, &overview, &tmdbID)
	if err != nil {
		t.Fatalf("query stored row: %v", err)
	}
	if title != "Right Show - S01E01 - Pilot" {
		t.Fatalf("title = %q", title)
	}
	if overview != "right" {
		t.Fatalf("overview = %q", overview)
	}
	if tmdbID != 222 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
}

func TestListIdentifiableByLibrary_IncludesRowsWithMetadata(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, "tv")

	tmp := t.TempDir()
	showRoot := filepath.Join(tmp, "Show", "Season 1")
	if err := os.MkdirAll(showRoot, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	file := filepath.Join(showRoot, "Show.S01E01.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake media: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	ctx := context.Background()
	mock := &mockIdentifier{
		tvResult: &metadata.MatchResult{
			Title:      "Show - S01E01 - Pilot",
			Provider:   "tmdb",
			ExternalID: "333",
		},
	}
	if _, err := HandleScanLibrary(ctx, db, filepath.Join(tmp, "Show"), LibraryTypeTV, tvLibID, mock); err != nil {
		t.Fatalf("scan: %v", err)
	}

	rows, err := ListIdentifiableByLibrary(db, tvLibID)
	if err != nil {
		t.Fatalf("list rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Path != file {
		t.Fatalf("path = %q", rows[0].Path)
	}
}

func TestHandleScanLibrary_SkipsMovieExtrasAndSamples(t *testing.T) {
	db := newTestDB(t)
	movieLibID := getLibraryID(t, db, "movie")

	tmp := t.TempDir()
	movieRoot := filepath.Join(tmp, "Movie (2010)")
	if err := os.MkdirAll(filepath.Join(movieRoot, "Extras"), 0o755); err != nil {
		t.Fatalf("mkdir movie tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieRoot, "movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write movie: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieRoot, "Extras", "behind-the-scenes.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	result, err := HandleScanLibrary(context.Background(), db, movieRoot, LibraryTypeMovie, movieLibID, nil)
	if err != nil {
		t.Fatalf("scan movies: %v", err)
	}
	if result.Added != 1 || result.Skipped != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}
}

func TestEstimateLibraryFiles_CountsSupportedMediaAndSkipsMovieExtras(t *testing.T) {
	root := t.TempDir()
	movieRoot := filepath.Join(root, "Movie (2010)")
	if err := os.MkdirAll(filepath.Join(movieRoot, "Extras"), 0o755); err != nil {
		t.Fatalf("mkdir movie tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieRoot, "movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write movie: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieRoot, "Extras", "featurette.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieRoot, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}

	count, err := EstimateLibraryFiles(context.Background(), root, LibraryTypeMovie)
	if err != nil {
		t.Fatalf("estimate files: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d", count)
	}
}

func TestHandleScanLibrary_ImportsMovieLayouts(t *testing.T) {
	db := newTestDB(t)
	movieLibID := getLibraryID(t, db, "movie")

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	cases := []struct {
		name      string
		setup     func(root string) (string, error)
		wantTitle string
	}{
		{
			name: "file in root",
			setup: func(root string) (string, error) {
				path := filepath.Join(root, "Movie (2010).mkv")
				return path, os.WriteFile(path, []byte("x"), 0o644)
			},
			wantTitle: "Movie",
		},
		{
			name: "movie in own folder",
			setup: func(root string) (string, error) {
				dir := filepath.Join(root, "Movie (2010)")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", err
				}
				path := filepath.Join(dir, "movie.mkv")
				return path, os.WriteFile(path, []byte("x"), 0o644)
			},
			wantTitle: "Movie",
		},
		{
			name: "collection disc layout",
			setup: func(root string) (string, error) {
				dir := filepath.Join(root, "Collection", "Movie (2010)", "Disc 1")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", err
				}
				path := filepath.Join(dir, "movie.mkv")
				return path, os.WriteFile(path, []byte("x"), 0o644)
			},
			wantTitle: "Movie",
		},
		{
			name: "noisy release filename in folder",
			setup: func(root string) (string, error) {
				dir := filepath.Join(root, "Die My Love (2025)")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", err
				}
				path := filepath.Join(dir, "Die My Love 2025 BluRay 1080p DD 5 1 x264-BHDStudio.mp4")
				return path, os.WriteFile(path, []byte("x"), 0o644)
			},
			wantTitle: "Die My Love",
		},
		{
			name: "release prefix in root filename",
			setup: func(root string) (string, error) {
				path := filepath.Join(root, "[MrManager] Riding Bean (1989) BDRemux (Dual Audio, Special Features).mkv")
				return path, os.WriteFile(path, []byte("x"), 0o644)
			},
			wantTitle: "Riding Bean",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			path, err := tc.setup(tmp)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			result, err := HandleScanLibrary(context.Background(), db, tmp, LibraryTypeMovie, movieLibID, nil)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if result.Added != 1 {
				t.Fatalf("unexpected scan result: %+v", result)
			}
			var title string
			if err := db.QueryRow(`SELECT title FROM movies WHERE library_id = ? AND path = ?`, movieLibID, path).Scan(&title); err != nil {
				t.Fatalf("query row: %v", err)
			}
			if title != tc.wantTitle {
				t.Fatalf("title = %q", title)
			}
			if _, err := db.Exec(`DELETE FROM media_global`); err != nil {
				t.Fatalf("clear media_global: %v", err)
			}
			if _, err := db.Exec(`DELETE FROM movies WHERE library_id = ?`, movieLibID); err != nil {
				t.Fatalf("clear movies: %v", err)
			}
		})
	}
}

func TestHandleScanLibrary_ImportsAnimeSeasonFolderWithSuffix(t *testing.T) {
	db := newTestDB(t)
	animeLibID := createLibraryForTest(t, db, LibraryTypeAnime, "/anime")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "D", "Season 01 [127]")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir anime tree: %v", err)
	}
	file := filepath.Join(root, "Dragon Ball (1986) - S01E01 - Secret of the Dragon Balls [SDTV][AAC 2.0][x265].mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write anime file: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	result, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "D"), LibraryTypeAnime, animeLibID, nil)
	if err != nil {
		t.Fatalf("scan anime: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}

	var title string
	var season, episode int
	if err := db.QueryRow(`SELECT title, season, episode FROM anime_episodes WHERE library_id = ? AND path = ?`, animeLibID, file).Scan(&title, &season, &episode); err != nil {
		t.Fatalf("query anime row: %v", err)
	}
	if title != "Dragon Ball - S01E01" {
		t.Fatalf("title = %q", title)
	}
	if season != 1 || episode != 1 {
		t.Fatalf("season=%d episode=%d", season, episode)
	}
}

func TestHandleScanLibrary_DoesNotTreatResolutionAsSeasonInStructuredAnimeFolder(t *testing.T) {
	db := newTestDB(t)
	animeLibID := createLibraryForTest(t, db, LibraryTypeAnime, "/anime")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Anime Show", "Season 01")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir anime tree: %v", err)
	}
	file := filepath.Join(root, "[Group] Anime Show - 01 [1440x1080 x264].mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write anime file: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	result, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Anime Show"), LibraryTypeAnime, animeLibID, nil)
	if err != nil {
		t.Fatalf("scan anime: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}

	var title string
	var season, episode int
	if err := db.QueryRow(`SELECT title, season, episode FROM anime_episodes WHERE library_id = ? AND path = ?`, animeLibID, file).Scan(&title, &season, &episode); err != nil {
		t.Fatalf("query anime row: %v", err)
	}
	if season != 1 || episode != 1 {
		t.Fatalf("season=%d episode=%d", season, episode)
	}
	if title != "Anime Show - S01E01" {
		t.Fatalf("title = %q", title)
	}
}

func TestHandleScanLibrary_DefaultsSeasonOneForShowFolderEpisodeTitle(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, LibraryTypeTV)

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Show")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir tv tree: %v", err)
	}
	file := filepath.Join(root, "Pilot.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write tv file: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	result, err := HandleScanLibrary(context.Background(), db, tmp, LibraryTypeTV, tvLibID, nil)
	if err != nil {
		t.Fatalf("scan tv: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}

	var title string
	var season, episode int
	if err := db.QueryRow(`SELECT title, season, episode FROM tv_episodes WHERE library_id = ? AND path = ?`, tvLibID, file).Scan(&title, &season, &episode); err != nil {
		t.Fatalf("query tv row: %v", err)
	}
	if title != "Show" {
		t.Fatalf("title = %q", title)
	}
	if season != 1 || episode != 0 {
		t.Fatalf("season=%d episode=%d", season, episode)
	}
}

func TestHandleScanLibrary_ImportsAbsoluteEpisodeAnimeAsUnmatched(t *testing.T) {
	db := newTestDB(t)
	animeLibID := createLibraryForTest(t, db, LibraryTypeAnime, "/anime")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Frieren")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir anime tree: %v", err)
	}
	file := filepath.Join(root, "[SubsPlease] Frieren - 12 [1080p].mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write anime file: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	result, err := HandleScanLibrary(context.Background(), db, root, LibraryTypeAnime, animeLibID, &funcIdentifier{})
	if err != nil {
		t.Fatalf("scan anime: %v", err)
	}
	if result.Added != 1 || result.Unmatched != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}

	var title, status string
	var season, episode int
	if err := db.QueryRow(`SELECT title, match_status, season, episode FROM anime_episodes WHERE library_id = ? AND path = ?`, animeLibID, file).Scan(&title, &status, &season, &episode); err != nil {
		t.Fatalf("query anime row: %v", err)
	}
	if title != "Frieren - S00E00" && title != "Frieren" {
		t.Fatalf("title = %q", title)
	}
	if status != MatchStatusUnmatched {
		t.Fatalf("status = %q", status)
	}
	if season != 0 || episode != 0 {
		t.Fatalf("season=%d episode=%d", season, episode)
	}
}

func TestHandleScanLibrary_UsesAnimeIdentifier(t *testing.T) {
	db := newTestDB(t)
	animeLibID := createLibraryForTest(t, db, LibraryTypeAnime, "/anime")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Frieren", "Season 1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir anime tree: %v", err)
	}
	file := filepath.Join(root, "Frieren - S01E12.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write anime file: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	identifier := &funcIdentifier{
		anime: func(info metadata.MediaInfo) *metadata.MatchResult {
			if info.Title != "frieren" {
				t.Fatalf("title = %q", info.Title)
			}
			if info.Season != 1 || info.Episode != 12 {
				t.Fatalf("unexpected anime info: %+v", info)
			}
			return &metadata.MatchResult{
				Title:      "Frieren - S01E12 - Episode",
				Provider:   "tmdb",
				ExternalID: "777",
			}
		},
	}

	result, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Frieren"), LibraryTypeAnime, animeLibID, identifier)
	if err != nil {
		t.Fatalf("scan anime: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}

	var title string
	var tmdbID int
	if err := db.QueryRow(`SELECT title, COALESCE(tmdb_id, 0) FROM anime_episodes WHERE library_id = ? AND path = ?`, animeLibID, file).Scan(&title, &tmdbID); err != nil {
		t.Fatalf("query anime row: %v", err)
	}
	if title != "Frieren - S01E12 - Episode" {
		t.Fatalf("title = %q", title)
	}
	if tmdbID != 777 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
}

func TestHandleScanLibrary_ReidentifiesAnimeRowsThatOnlyHaveTVDBMetadata(t *testing.T) {
	db := newTestDB(t)
	animeLibID := createLibraryForTest(t, db, LibraryTypeAnime, "/anime")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Frieren", "Season 1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir anime tree: %v", err)
	}
	file := filepath.Join(root, "Frieren - S01E12.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write anime file: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	first := &funcIdentifier{
		anime: func(info metadata.MediaInfo) *metadata.MatchResult {
			return &metadata.MatchResult{
				Title:      "Frieren - S01E12 - Episode",
				Provider:   "tvdb",
				ExternalID: "series-55",
			}
		},
	}
	if _, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Frieren"), LibraryTypeAnime, animeLibID, first); err != nil {
		t.Fatalf("first scan anime: %v", err)
	}

	second := &funcIdentifier{
		anime: func(info metadata.MediaInfo) *metadata.MatchResult {
			return &metadata.MatchResult{
				Title:      "Frieren - S01E12 - Episode",
				Provider:   "tmdb",
				ExternalID: "777",
			}
		},
	}
	if _, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Frieren"), LibraryTypeAnime, animeLibID, second); err != nil {
		t.Fatalf("second scan anime: %v", err)
	}

	var tmdbID int
	var tvdbID sql.NullString
	if err := db.QueryRow(`SELECT COALESCE(tmdb_id, 0), tvdb_id FROM anime_episodes WHERE library_id = ? AND path = ?`, animeLibID, file).Scan(&tmdbID, &tvdbID); err != nil {
		t.Fatalf("query anime row: %v", err)
	}
	if tmdbID != 777 {
		t.Fatalf("tmdb_id = %d", tmdbID)
	}
	if tvdbID.Valid {
		t.Fatalf("tvdb_id = %q", tvdbID.String)
	}
}

func TestHandleScanLibrary_StoresUnmatchedStatus(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, "tv")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Show", "Season 1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	file := filepath.Join(root, "Show.S01E01.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake media: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	result, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Show"), LibraryTypeTV, tvLibID, &funcIdentifier{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if result.Unmatched != 1 {
		t.Fatalf("result = %+v", result)
	}
	var status string
	if err := db.QueryRow(`SELECT match_status FROM tv_episodes WHERE library_id = ? AND path = ?`, tvLibID, file).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != MatchStatusUnmatched {
		t.Fatalf("status = %q", status)
	}
}

func TestHandleScanLibrary_PrunesRemovedFiles(t *testing.T) {
	db := newTestDB(t)
	tvLibID := getLibraryID(t, db, "tv")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Show", "Season 1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	file := filepath.Join(root, "Show.S01E01.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake media: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	if _, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Show"), LibraryTypeTV, tvLibID, nil); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	result, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Show"), LibraryTypeTV, tvLibID, nil)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if result.Removed != 1 {
		t.Fatalf("expected one removed file, got %+v", result)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tv_episodes WHERE library_id = ?`, tvLibID).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after prune, got %d", count)
	}
}

func TestHandleScanLibrary_ImportsMusicExtensionsAndTags(t *testing.T) {
	db := newTestDB(t)
	musicLibID := createLibraryForTest(t, db, LibraryTypeMusic, "/music")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Artist", "Album", "Disc 2")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir music tree: %v", err)
	}
	file := filepath.Join(root, "01 - Placeholder.flac")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake audio: %v", err)
	}

	prevSkip := SkipFFprobeInScan
	prevReadAudio := readAudioMetadata
	SkipFFprobeInScan = false
	readAudioMetadata = func(_ context.Context, _ string) (metadata.MusicMetadata, int, error) {
		return metadata.MusicMetadata{
			Title:       "Tagged Track",
			Artist:      "Tagged Artist",
			Album:       "Tagged Album",
			AlbumArtist: "Tagged Album Artist",
			DiscNumber:  2,
			TrackNumber: 1,
			ReleaseYear: 2024,
		}, 245, nil
	}
	defer func() {
		SkipFFprobeInScan = prevSkip
		readAudioMetadata = prevReadAudio
	}()

	result, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Artist"), LibraryTypeMusic, musicLibID, nil)
	if err != nil {
		t.Fatalf("scan music: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}

	var title, artist, album, albumArtist, status string
	var duration, discNumber, trackNumber, releaseYear int
	if err := db.QueryRow(`SELECT title, artist, album, album_artist, duration, disc_number, track_number, release_year, match_status FROM music_tracks WHERE library_id = ?`, musicLibID).
		Scan(&title, &artist, &album, &albumArtist, &duration, &discNumber, &trackNumber, &releaseYear, &status); err != nil {
		t.Fatalf("query music row: %v", err)
	}
	if title != "Tagged Track" || artist != "Tagged Artist" || album != "Tagged Album" || albumArtist != "Tagged Album Artist" {
		t.Fatalf("unexpected music metadata: title=%q artist=%q album=%q albumArtist=%q", title, artist, album, albumArtist)
	}
	if duration != 245 || discNumber != 2 || trackNumber != 1 || releaseYear != 2024 {
		t.Fatalf("unexpected numeric metadata: duration=%d disc=%d track=%d year=%d", duration, discNumber, trackNumber, releaseYear)
	}
	if status != MatchStatusLocal {
		t.Fatalf("status = %q", status)
	}
}

func TestHandleScanLibrary_ImportsSupportedMusicExtensions(t *testing.T) {
	db := newTestDB(t)
	musicLibID := createLibraryForTest(t, db, LibraryTypeMusic, "/music")

	tmp := t.TempDir()
	root := filepath.Join(tmp, "Artist", "Album")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir music tree: %v", err)
	}
	extensions := []string{".mp3", ".flac", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".alac"}
	for i, ext := range extensions {
		path := filepath.Join(root, fmt.Sprintf("Track-%d%s", i+1, ext))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", ext, err)
		}
	}

	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = true
	defer func() { SkipFFprobeInScan = prevSkip }()

	result, err := HandleScanLibrary(context.Background(), db, filepath.Join(tmp, "Artist"), LibraryTypeMusic, musicLibID, nil)
	if err != nil {
		t.Fatalf("scan music: %v", err)
	}
	if result.Added != len(extensions) {
		t.Fatalf("expected %d imported tracks, got %+v", len(extensions), result)
	}
}
