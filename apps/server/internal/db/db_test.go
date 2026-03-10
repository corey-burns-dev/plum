package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	addedTV, err := HandleScanLibrary(ctx, db, tvRoot, LibraryTypeTV, "", tvLibID)
	if err != nil {
		t.Fatalf("scan tv library: %v", err)
	}
	if addedTV != 2 {
		t.Fatalf("expected 2 tv items added, got %d", addedTV)
	}

	addedMovies, err := HandleScanLibrary(ctx, db, movieRoot, LibraryTypeMovie, "", movieLibID)
	if err != nil {
		t.Fatalf("scan movie library: %v", err)
	}
	if addedMovies != 1 {
		t.Fatalf("expected 1 movie item added, got %d", addedMovies)
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

	addedFirst, err := HandleScanLibrary(ctx, db, filepath.Join(tmp, "SomeShow"), LibraryTypeTV, "", tvLibID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if addedFirst != 1 {
		t.Fatalf("expected 1 item on first scan, got %d", addedFirst)
	}

	addedSecond, err := HandleScanLibrary(ctx, db, filepath.Join(tmp, "SomeShow"), LibraryTypeTV, "", tvLibID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if addedSecond != 0 {
		t.Fatalf("expected 0 items on second scan, got %d", addedSecond)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tv_episodes WHERE library_id = ?`, tvLibID).Scan(&count); err != nil {
		t.Fatalf("count tv_episodes: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 tv_episode row after two scans, got %d", count)
	}
}
