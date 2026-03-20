package db

import (
	"database/sql"
	"testing"
	"time"
)

func TestGetHomeDashboardForUser_FiltersByUserID(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	user1ID := createUserForPrivacyTest(t, dbConn, "user1@example.com")
	user2ID := createUserForPrivacyTest(t, dbConn, "user2@example.com")

	lib1ID := createLibraryForPrivacyTest(t, dbConn, LibraryTypeMovie, "Movies 1", "/user1-movies", user1ID)
	lib2ID := createLibraryForPrivacyTest(t, dbConn, LibraryTypeMovie, "Movies 2", "/user2-movies", user2ID)

	insertMovieForDashboardTest(t, dbConn, lib1ID, "User 1 Movie", "/user1-movies/movie1.mkv")
	insertMovieForDashboardTest(t, dbConn, lib2ID, "User 2 Movie", "/user2-movies/movie2.mkv")

	// Check User 1's dashboard
	dashboard1, err := GetHomeDashboardForUser(dbConn, user1ID)
	if err != nil {
		t.Fatalf("get dashboard 1: %v", err)
	}
	if len(dashboard1.RecentlyAdded) != 1 {
		t.Fatalf("expected 1 item for user 1, got %d", len(dashboard1.RecentlyAdded))
	}
	if dashboard1.RecentlyAdded[0].Media.Title != "User 1 Movie" {
		t.Fatalf("expected 'User 1 Movie', got '%s'", dashboard1.RecentlyAdded[0].Media.Title)
	}

	// Check User 2's dashboard
	dashboard2, err := GetHomeDashboardForUser(dbConn, user2ID)
	if err != nil {
		t.Fatalf("get dashboard 2: %v", err)
	}
	if len(dashboard2.RecentlyAdded) != 1 {
		t.Fatalf("expected 1 item for user 2, got %d", len(dashboard2.RecentlyAdded))
	}
	if dashboard2.RecentlyAdded[0].Media.Title != "User 2 Movie" {
		t.Fatalf("expected 'User 2 Movie', got '%s'", dashboard2.RecentlyAdded[0].Media.Title)
	}

	// Check GetAllMediaForUser
	items1, err := GetAllMediaForUser(dbConn, user1ID)
	if err != nil {
		t.Fatalf("get all media 1: %v", err)
	}
	if len(items1) != 1 {
		t.Fatalf("expected 1 item for user 1 (GetAllMediaForUser), got %d", len(items1))
	}

	items2, err := GetAllMediaForUser(dbConn, user2ID)
	if err != nil {
		t.Fatalf("get all media 2: %v", err)
	}
	if len(items2) != 1 {
		t.Fatalf("expected 1 item for user 2 (GetAllMediaForUser), got %d", len(items2))
	}
}

func createUserForPrivacyTest(t *testing.T, db *sql.DB, email string) int {
	t.Helper()
	var id int
	err := db.QueryRow("INSERT INTO users (email, password_hash, created_at) VALUES (?, 'hash', ?) RETURNING id", email, time.Now().UTC()).Scan(&id)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func createLibraryForPrivacyTest(t *testing.T, db *sql.DB, kind string, name, path string, userID int) int {
	t.Helper()
	var id int
	err := db.QueryRow("INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id",
		userID, name, string(kind), path, time.Now().UTC()).Scan(&id)
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	return id
}
