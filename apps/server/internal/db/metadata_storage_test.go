package db

import (
	"testing"
	"time"
)

func TestUpdateMediaMetadataWithState_UpsertsShowSeasonAndVersions(t *testing.T) {
	dbConn := newTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
		"meta@example.com", "hash", 1, now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID, "TV", LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var refID int
	if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID, "Show - S01E01 - Pilot", "/tv/Show/Season 1/E01.mkv", 1800, MatchStatusLocal, 1, 1).Scan(&refID); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	if err := UpdateMediaMetadataWithState(dbConn, "tv_episodes", refID, "Show - S01E01 - Pilot", "overview a", "poster-a", "backdrop-a", "2020-01-01", 7.1, "tt0001", 7.6, 100, "", 1, 1, false, true); err != nil {
		t.Fatalf("first metadata update: %v", err)
	}

	var (
		showID1, seasonID1, episodeVersion1 int
		showVersion1, seasonVersion1        int
	)
	if err := dbConn.QueryRow(`SELECT COALESCE(show_id,0), COALESCE(season_id,0), COALESCE(metadata_version,0) FROM tv_episodes WHERE id = ?`, refID).
		Scan(&showID1, &seasonID1, &episodeVersion1); err != nil {
		t.Fatalf("load episode links: %v", err)
	}
	if showID1 == 0 || seasonID1 == 0 {
		t.Fatalf("expected show/season links to be set, got show=%d season=%d", showID1, seasonID1)
	}
	if err := dbConn.QueryRow(`SELECT metadata_version FROM shows WHERE id = ?`, showID1).Scan(&showVersion1); err != nil {
		t.Fatalf("load show version: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT metadata_version FROM seasons WHERE id = ?`, seasonID1).Scan(&seasonVersion1); err != nil {
		t.Fatalf("load season version: %v", err)
	}

	if err := UpdateMediaMetadataWithState(dbConn, "tv_episodes", refID, "Show - S01E01 - Pilot", "overview a", "poster-a", "backdrop-a", "2020-01-01", 7.1, "tt0001", 7.6, 100, "", 1, 1, false, true); err != nil {
		t.Fatalf("same metadata update: %v", err)
	}
	var (
		episodeVersion2 int
		showVersion2    int
		seasonVersion2  int
	)
	if err := dbConn.QueryRow(`SELECT COALESCE(metadata_version,0) FROM tv_episodes WHERE id = ?`, refID).Scan(&episodeVersion2); err != nil {
		t.Fatalf("load episode version2: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT metadata_version FROM shows WHERE id = ?`, showID1).Scan(&showVersion2); err != nil {
		t.Fatalf("load show version2: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT metadata_version FROM seasons WHERE id = ?`, seasonID1).Scan(&seasonVersion2); err != nil {
		t.Fatalf("load season version2: %v", err)
	}
	if episodeVersion2 != episodeVersion1 || showVersion2 != showVersion1 || seasonVersion2 != seasonVersion1 {
		t.Fatalf("expected stable versions on unchanged metadata: episode %d->%d show %d->%d season %d->%d", episodeVersion1, episodeVersion2, showVersion1, showVersion2, seasonVersion1, seasonVersion2)
	}

	if err := UpdateMediaMetadataWithState(dbConn, "tv_episodes", refID, "Show - S01E01 - Pilot", "overview b", "poster-a", "backdrop-a", "2020-01-01", 7.1, "tt0001", 7.6, 100, "", 1, 1, false, true); err != nil {
		t.Fatalf("changed metadata update: %v", err)
	}
	var (
		episodeVersion3 int
		showVersion3    int
		seasonVersion3  int
	)
	if err := dbConn.QueryRow(`SELECT COALESCE(metadata_version,0) FROM tv_episodes WHERE id = ?`, refID).Scan(&episodeVersion3); err != nil {
		t.Fatalf("load episode version3: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT metadata_version FROM shows WHERE id = ?`, showID1).Scan(&showVersion3); err != nil {
		t.Fatalf("load show version3: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT metadata_version FROM seasons WHERE id = ?`, seasonID1).Scan(&seasonVersion3); err != nil {
		t.Fatalf("load season version3: %v", err)
	}
	if episodeVersion3 <= episodeVersion2 || showVersion3 <= showVersion2 || seasonVersion3 <= seasonVersion2 {
		t.Fatalf("expected versions to increase on metadata change: episode %d->%d show %d->%d season %d->%d", episodeVersion2, episodeVersion3, showVersion2, showVersion3, seasonVersion2, seasonVersion3)
	}
}

func TestUpdateMediaMetadataWithState_DoesNotMergeDifferentTMDBShowsWithSameTitleKey(t *testing.T) {
	dbConn := newTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339)
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
		"meta@example.com", "hash", 1, now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var libraryID int
	if err := dbConn.QueryRow(`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		userID, "TV", LibraryTypeTV, "/tv", now).Scan(&libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	var refIDs [2]int
	for i, title := range []string{
		"Shared Show - S01E01 - Pilot",
		"Shared Show - S01E02 - Second",
	} {
		if err := dbConn.QueryRow(`INSERT INTO tv_episodes (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, title, "/tv/shared/"+title, 1800, MatchStatusLocal, 1, i+1).Scan(&refIDs[i]); err != nil {
			t.Fatalf("insert episode %d: %v", i, err)
		}
	}

	if err := UpdateMediaMetadataWithState(dbConn, "tv_episodes", refIDs[0], "Shared Show - S01E01 - Pilot", "overview a", "poster-a", "backdrop-a", "2020-01-01", 7.1, "tt0001", 7.6, 111, "", 1, 1, false, true); err != nil {
		t.Fatalf("first metadata update: %v", err)
	}
	if err := UpdateMediaMetadataWithState(dbConn, "tv_episodes", refIDs[1], "Shared Show - S01E02 - Second", "overview b", "poster-b", "backdrop-b", "2020-01-08", 7.2, "tt0002", 7.7, 222, "", 1, 2, false, true); err != nil {
		t.Fatalf("second metadata update: %v", err)
	}

	var showCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM shows WHERE library_id = ? AND kind = ?`, libraryID, LibraryTypeTV).Scan(&showCount); err != nil {
		t.Fatalf("count shows: %v", err)
	}
	if showCount != 2 {
		t.Fatalf("expected two distinct shows, got %d", showCount)
	}

	var showID1, showID2 int
	if err := dbConn.QueryRow(`SELECT COALESCE(show_id, 0) FROM tv_episodes WHERE id = ?`, refIDs[0]).Scan(&showID1); err != nil {
		t.Fatalf("query episode 1 show_id: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT COALESCE(show_id, 0) FROM tv_episodes WHERE id = ?`, refIDs[1]).Scan(&showID2); err != nil {
		t.Fatalf("query episode 2 show_id: %v", err)
	}
	if showID1 == 0 || showID2 == 0 {
		t.Fatalf("expected show links to be set, got %d and %d", showID1, showID2)
	}
	if showID1 == showID2 {
		t.Fatalf("expected distinct show rows, got shared show_id %d", showID1)
	}
}
