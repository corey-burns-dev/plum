package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"plum/internal/metadata"
)

type musicOnlyIdentifier struct {
	result *metadata.MusicMatchResult
}

func (m *musicOnlyIdentifier) IdentifyTV(context.Context, metadata.MediaInfo) *metadata.MatchResult {
	return nil
}

func (m *musicOnlyIdentifier) IdentifyAnime(context.Context, metadata.MediaInfo) *metadata.MatchResult {
	return nil
}

func (m *musicOnlyIdentifier) IdentifyMovie(context.Context, metadata.MediaInfo) *metadata.MatchResult {
	return nil
}

func (m *musicOnlyIdentifier) IdentifyMusic(context.Context, metadata.MusicInfo) *metadata.MusicMatchResult {
	return m.result
}

func TestGetHomeDashboardForUser_PromotesNextEpisodeAfterCompletion(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	userID := getSingleUserID(t, dbConn)
	tvLibraryID := getLibraryID(t, dbConn, LibraryTypeTV)
	movieLibraryID := getLibraryID(t, dbConn, LibraryTypeMovie)

	firstEpisodeID := insertEpisodeForDashboardTest(t, dbConn, tvLibraryID, "Space Show - S01E01 - Pilot", "/tv/Space Show/S01E01.mkv", 1, 1)
	secondEpisodeID := insertEpisodeForDashboardTest(t, dbConn, tvLibraryID, "Space Show - S01E02 - Arrival", "/tv/Space Show/S01E02.mkv", 1, 2)
	movieID := insertMovieForDashboardTest(t, dbConn, movieLibraryID, "Arrival", "/movies/Arrival (2016)/Arrival.mkv")

	if err := UpsertPlaybackProgress(dbConn, userID, firstEpisodeID, 1800, 1800, true); err != nil {
		t.Fatalf("complete episode: %v", err)
	}
	if err := UpsertPlaybackProgress(dbConn, userID, movieID, 1200, 7200, false); err != nil {
		t.Fatalf("partial movie: %v", err)
	}
	setPlaybackTimestamp(t, dbConn, userID, firstEpisodeID, "2026-03-20T10:00:00Z")
	setPlaybackTimestamp(t, dbConn, userID, movieID, "2026-03-19T10:00:00Z")

	dashboard, err := GetHomeDashboardForUser(dbConn, userID)
	if err != nil {
		t.Fatalf("get home dashboard: %v", err)
	}
	if len(dashboard.ContinueWatching) != 2 {
		t.Fatalf("entries = %+v", dashboard.ContinueWatching)
	}
	showEntry := dashboard.ContinueWatching[0]
	if showEntry.Kind != "show" || showEntry.Media.ID != secondEpisodeID {
		t.Fatalf("expected next-up show entry, got %+v", showEntry)
	}
	if showEntry.ShowTitle != "Space Show" || showEntry.EpisodeLabel != "S01E02" {
		t.Fatalf("unexpected show labels: %+v", showEntry)
	}
	if showEntry.Media.ProgressPercent != 0 {
		t.Fatalf("expected next-up episode progress to be empty, got %.2f", showEntry.Media.ProgressPercent)
	}
	if dashboard.ContinueWatching[1].Kind != "movie" || dashboard.ContinueWatching[1].Media.ID != movieID {
		t.Fatalf("expected movie entry second, got %+v", dashboard.ContinueWatching[1])
	}
}

func TestGetHomeDashboardForUser_PrefersActiveEpisodeOverNextUp(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	userID := getSingleUserID(t, dbConn)
	tvLibraryID := getLibraryID(t, dbConn, LibraryTypeTV)

	firstEpisodeID := insertEpisodeForDashboardTest(t, dbConn, tvLibraryID, "Space Show - S01E01 - Pilot", "/tv/Space Show/S01E01.mkv", 1, 1)
	secondEpisodeID := insertEpisodeForDashboardTest(t, dbConn, tvLibraryID, "Space Show - S01E02 - Arrival", "/tv/Space Show/S01E02.mkv", 1, 2)
	_ = insertEpisodeForDashboardTest(t, dbConn, tvLibraryID, "Space Show - S01E03 - Departure", "/tv/Space Show/S01E03.mkv", 1, 3)

	if err := UpsertPlaybackProgress(dbConn, userID, firstEpisodeID, 1800, 1800, true); err != nil {
		t.Fatalf("complete episode: %v", err)
	}
	if err := UpsertPlaybackProgress(dbConn, userID, secondEpisodeID, 900, 1800, false); err != nil {
		t.Fatalf("partial episode: %v", err)
	}
	setPlaybackTimestamp(t, dbConn, userID, firstEpisodeID, "2026-03-19T10:00:00Z")
	setPlaybackTimestamp(t, dbConn, userID, secondEpisodeID, "2026-03-20T10:00:00Z")

	dashboard, err := GetHomeDashboardForUser(dbConn, userID)
	if err != nil {
		t.Fatalf("get home dashboard: %v", err)
	}
	if len(dashboard.ContinueWatching) != 1 {
		t.Fatalf("entries = %+v", dashboard.ContinueWatching)
	}
	entry := dashboard.ContinueWatching[0]
	if entry.Media.ID != secondEpisodeID || entry.EpisodeLabel != "S01E02" {
		t.Fatalf("expected partially watched episode, got %+v", entry)
	}
	if entry.RemainingSeconds <= 0 {
		t.Fatalf("expected remaining time, got %+v", entry)
	}
}

func TestGetHomeDashboardForUser_DropsShowWhenNoNextEpisodeExists(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	userID := getSingleUserID(t, dbConn)
	tvLibraryID := getLibraryID(t, dbConn, LibraryTypeTV)

	finalEpisodeID := insertEpisodeForDashboardTest(t, dbConn, tvLibraryID, "Mini Series - S01E01 - Finale", "/tv/Mini Series/S01E01.mkv", 1, 1)
	if err := UpsertPlaybackProgress(dbConn, userID, finalEpisodeID, 1800, 1800, true); err != nil {
		t.Fatalf("complete episode: %v", err)
	}

	dashboard, err := GetHomeDashboardForUser(dbConn, userID)
	if err != nil {
		t.Fatalf("get home dashboard: %v", err)
	}
	if len(dashboard.ContinueWatching) != 0 {
		t.Fatalf("expected no continue-watching entries, got %+v", dashboard.ContinueWatching)
	}
}

func TestGetHomeDashboardForUser_IncludesNonMusicLibrariesInContinueWatching(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	userID := getSingleUserID(t, dbConn)
	movieLibraryID := getLibraryID(t, dbConn, LibraryTypeMovie)
	animeLibraryID := createLibraryForTest(t, dbConn, LibraryTypeAnime, "/anime-dashboard")
	musicLibraryID := createLibraryForTest(t, dbConn, LibraryTypeMusic, "/music-dashboard")

	movieID := insertMovieForDashboardTest(t, dbConn, movieLibraryID, "Arrival", "/movies/Arrival.mkv")
	animeEpisodeID := insertEpisodeForDashboardTestWithKind(
		t,
		dbConn,
		animeLibraryID,
		LibraryTypeAnime,
		"Sky Quest - S01E01 - Launch",
		"/anime/Sky Quest/S01E01.mkv",
		1,
		1,
	)
	musicID := insertMusicTrackForDashboardTest(t, dbConn, musicLibraryID, "Track One", "/music/Track One.mp3")

	if err := UpsertPlaybackProgress(dbConn, userID, movieID, 1200, 7200, false); err != nil {
		t.Fatalf("partial movie: %v", err)
	}
	if err := UpsertPlaybackProgress(dbConn, userID, animeEpisodeID, 600, 1800, false); err != nil {
		t.Fatalf("partial anime episode: %v", err)
	}
	if err := UpsertPlaybackProgress(dbConn, userID, musicID, 30, 240, false); err != nil {
		t.Fatalf("partial music track: %v", err)
	}
	setPlaybackTimestamp(t, dbConn, userID, movieID, "2026-03-19T10:00:00Z")
	setPlaybackTimestamp(t, dbConn, userID, animeEpisodeID, "2026-03-20T10:00:00Z")
	setPlaybackTimestamp(t, dbConn, userID, musicID, "2026-03-21T10:00:00Z")

	dashboard, err := GetHomeDashboardForUser(dbConn, userID)
	if err != nil {
		t.Fatalf("get home dashboard: %v", err)
	}
	if len(dashboard.ContinueWatching) != 2 {
		t.Fatalf("entries = %+v", dashboard.ContinueWatching)
	}
	if dashboard.ContinueWatching[0].Kind != "show" || dashboard.ContinueWatching[0].Media.ID != animeEpisodeID {
		t.Fatalf("expected anime entry first, got %+v", dashboard.ContinueWatching[0])
	}
	if dashboard.ContinueWatching[1].Kind != "movie" || dashboard.ContinueWatching[1].Media.ID != movieID {
		t.Fatalf("expected movie entry second, got %+v", dashboard.ContinueWatching[1])
	}
	for _, entry := range dashboard.ContinueWatching {
		if entry.Media.ID == musicID || entry.Media.Type == LibraryTypeMusic {
			t.Fatalf("expected music to be excluded from continue watching, got %+v", dashboard.ContinueWatching)
		}
	}
}

func TestGetHomeDashboardForUser_EmitsEmptyMediaSlicesInJSON(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	userID := getSingleUserID(t, dbConn)
	tvLibraryID := getLibraryID(t, dbConn, LibraryTypeTV)

	episodeID := insertEpisodeForDashboardTest(
		t,
		dbConn,
		tvLibraryID,
		"Space Show - S01E01 - Pilot",
		"/tv/Space Show/S01E01.mkv",
		1,
		1,
	)
	if err := UpsertPlaybackProgress(dbConn, userID, episodeID, 900, 1800, false); err != nil {
		t.Fatalf("partial episode: %v", err)
	}
	setPlaybackTimestamp(t, dbConn, userID, episodeID, "2026-03-20T10:00:00Z")

	dashboard, err := GetHomeDashboardForUser(dbConn, userID)
	if err != nil {
		t.Fatalf("get home dashboard: %v", err)
	}
	if len(dashboard.ContinueWatching) != 1 {
		t.Fatalf("entries = %+v", dashboard.ContinueWatching)
	}

	payload, err := json.Marshal(dashboard)
	if err != nil {
		t.Fatalf("marshal dashboard: %v", err)
	}

	var decoded struct {
		ContinueWatching []struct {
			Media struct {
				Subtitles           []json.RawMessage `json:"subtitles"`
				EmbeddedSubtitles   []json.RawMessage `json:"embeddedSubtitles"`
				EmbeddedAudioTracks []json.RawMessage `json:"embeddedAudioTracks"`
			} `json:"media"`
		} `json:"continueWatching"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal dashboard: %v", err)
	}
	if len(decoded.ContinueWatching) != 1 {
		t.Fatalf("decoded entries = %+v", decoded.ContinueWatching)
	}

	media := decoded.ContinueWatching[0].Media
	if media.Subtitles == nil {
		t.Fatal("expected subtitles to encode as an empty array, got null")
	}
	if media.EmbeddedSubtitles == nil {
		t.Fatal("expected embeddedSubtitles to encode as an empty array, got null")
	}
	if media.EmbeddedAudioTracks == nil {
		t.Fatal("expected embeddedAudioTracks to encode as an empty array, got null")
	}
}

func TestGetHomeDashboardForUser_RecentlyAddedMixesLibrariesAndGroupsShows(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	userID := getSingleUserID(t, dbConn)
	movieLibraryID := getLibraryID(t, dbConn, LibraryTypeMovie)
	tvLibraryID := getLibraryID(t, dbConn, LibraryTypeTV)
	animeLibraryID := createLibraryForTest(t, dbConn, LibraryTypeAnime, "/anime-recent")
	musicLibraryID := createLibraryForTest(t, dbConn, LibraryTypeMusic, "/music-recent")

	movieID := insertMovieForDashboardTest(t, dbConn, movieLibraryID, "Arrival", "/movies/Arrival.mkv")
	_ = insertEpisodeForDashboardTestWithKind(
		t,
		dbConn,
		tvLibraryID,
		LibraryTypeTV,
		"Space Show - S01E01 - Pilot",
		"/tv/Space Show/S01E01.mkv",
		1,
		1,
	)
	newestTVEpisodeID := insertEpisodeForDashboardTestWithKind(
		t,
		dbConn,
		tvLibraryID,
		LibraryTypeTV,
		"Space Show - S01E02 - Arrival",
		"/tv/Space Show/S01E02.mkv",
		1,
		2,
	)
	animeEpisodeID := insertEpisodeForDashboardTestWithKind(
		t,
		dbConn,
		animeLibraryID,
		LibraryTypeAnime,
		"Ninja Show - S01E01 - Begin",
		"/anime/Ninja Show/S01E01.mkv",
		1,
		1,
	)
	musicID := insertMusicTrackForDashboardTest(t, dbConn, musicLibraryID, "Track One", "/music/Track One.mp3")

	dashboard, err := GetHomeDashboardForUser(dbConn, userID)
	if err != nil {
		t.Fatalf("get home dashboard: %v", err)
	}
	if len(dashboard.RecentlyAdded) != 3 {
		t.Fatalf("recently added = %+v", dashboard.RecentlyAdded)
	}
	if dashboard.RecentlyAdded[0].Kind != "show" || dashboard.RecentlyAdded[0].Media.ID != animeEpisodeID {
		t.Fatalf("expected anime show first, got %+v", dashboard.RecentlyAdded[0])
	}
	if dashboard.RecentlyAdded[1].Kind != "show" || dashboard.RecentlyAdded[1].Media.ID != newestTVEpisodeID {
		t.Fatalf("expected grouped TV show second, got %+v", dashboard.RecentlyAdded[1])
	}
	if dashboard.RecentlyAdded[1].ShowTitle != "Space Show" || dashboard.RecentlyAdded[1].EpisodeLabel != "S01E02" {
		t.Fatalf("unexpected grouped TV labels: %+v", dashboard.RecentlyAdded[1])
	}
	if dashboard.RecentlyAdded[2].Kind != "movie" || dashboard.RecentlyAdded[2].Media.ID != movieID {
		t.Fatalf("expected movie third, got %+v", dashboard.RecentlyAdded[2])
	}
	for _, entry := range dashboard.RecentlyAdded {
		if entry.Media.ID == musicID || entry.Media.Type == LibraryTypeMusic {
			t.Fatalf("expected music to be excluded from recently added, got %+v", dashboard.RecentlyAdded)
		}
	}
}

func TestHandleScanLibrary_IdentifiesMusicWithProviderMetadata(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	musicLibraryID := createLibraryForTest(t, dbConn, LibraryTypeMusic, "/music")
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Artist", "Album")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir music tree: %v", err)
	}
	trackPath := filepath.Join(root, "Track 01.mp3")
	if err := os.WriteFile(trackPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake track: %v", err)
	}

	prevReadAudio := readAudioMetadata
	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = false
	readAudioMetadata = func(context.Context, string) (metadata.MusicMetadata, int, error) {
		return metadata.MusicMetadata{
			Title:       "Tagged Track",
			Artist:      "Tagged Artist",
			Album:       "Tagged Album",
			AlbumArtist: "Tagged Album Artist",
			TrackNumber: 1,
			ReleaseYear: 2024,
		}, 245, nil
	}
	defer func() {
		readAudioMetadata = prevReadAudio
		SkipFFprobeInScan = prevSkip
	}()

	identifier := &musicOnlyIdentifier{
		result: &metadata.MusicMatchResult{
			Title:          "Provider Track",
			Artist:         "Provider Artist",
			Album:          "Provider Album",
			AlbumArtist:    "Provider Album Artist",
			PosterURL:      "https://coverartarchive.org/release-group/rg-1/front-250",
			ReleaseYear:    1999,
			Provider:       "musicbrainz",
			ArtistID:       "artist-1",
			ReleaseGroupID: "rg-1",
			ReleaseID:      "rel-1",
			RecordingID:    "rec-1",
		},
	}

	result, err := HandleScanLibrary(context.Background(), dbConn, filepath.Join(tmp, "Artist"), LibraryTypeMusic, musicLibraryID, identifier)
	if err != nil {
		t.Fatalf("scan music: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}

	var (
		title, artist, album, albumArtist, posterPath    string
		recordingID, releaseID, releaseGroupID, artistID string
		matchStatus                                      string
		releaseYear                                      int
	)
	if err := dbConn.QueryRow(`SELECT title, artist, album, album_artist, COALESCE(poster_path, ''), COALESCE(musicbrainz_recording_id, ''), COALESCE(musicbrainz_release_id, ''), COALESCE(musicbrainz_release_group_id, ''), COALESCE(musicbrainz_artist_id, ''), release_year, match_status FROM music_tracks WHERE library_id = ?`, musicLibraryID).
		Scan(&title, &artist, &album, &albumArtist, &posterPath, &recordingID, &releaseID, &releaseGroupID, &artistID, &releaseYear, &matchStatus); err != nil {
		t.Fatalf("query music row: %v", err)
	}
	if title != "Provider Track" || artist != "Provider Artist" || album != "Provider Album" || albumArtist != "Provider Album Artist" {
		t.Fatalf("unexpected provider music metadata: title=%q artist=%q album=%q albumArtist=%q", title, artist, album, albumArtist)
	}
	if posterPath == "" || recordingID != "rec-1" || releaseID != "rel-1" || releaseGroupID != "rg-1" || artistID != "artist-1" {
		t.Fatalf("unexpected provider ids: poster=%q recording=%q release=%q group=%q artist=%q", posterPath, recordingID, releaseID, releaseGroupID, artistID)
	}
	if releaseYear != 1999 || matchStatus != MatchStatusIdentified {
		t.Fatalf("unexpected provider status: year=%d status=%q", releaseYear, matchStatus)
	}
}

func TestHandleScanLibrary_ReidentifiesMusicOnRescan(t *testing.T) {
	dbConn := newTestDB(t)
	t.Cleanup(func() { _ = dbConn.Close() })

	musicLibraryID := createLibraryForTest(t, dbConn, LibraryTypeMusic, "/music")
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Artist", "Album")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir music tree: %v", err)
	}
	trackPath := filepath.Join(root, "Track 01.mp3")
	if err := os.WriteFile(trackPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fake track: %v", err)
	}

	prevReadAudio := readAudioMetadata
	prevSkip := SkipFFprobeInScan
	SkipFFprobeInScan = false
	readAudioMetadata = func(context.Context, string) (metadata.MusicMetadata, int, error) {
		return metadata.MusicMetadata{
			Title:       "Local Track",
			Artist:      "Local Artist",
			Album:       "Local Album",
			AlbumArtist: "Local Artist",
			TrackNumber: 1,
		}, 200, nil
	}
	defer func() {
		readAudioMetadata = prevReadAudio
		SkipFFprobeInScan = prevSkip
	}()

	identifier := &musicOnlyIdentifier{
		result: &metadata.MusicMatchResult{
			Title:       "First Match",
			Artist:      "First Artist",
			Album:       "First Album",
			RecordingID: "rec-1",
			Provider:    "musicbrainz",
		},
	}

	if _, err := HandleScanLibrary(context.Background(), dbConn, filepath.Join(tmp, "Artist"), LibraryTypeMusic, musicLibraryID, identifier); err != nil {
		t.Fatalf("first scan music: %v", err)
	}

	identifier.result = &metadata.MusicMatchResult{
		Title:       "Second Match",
		Artist:      "Second Artist",
		Album:       "Second Album",
		RecordingID: "rec-2",
		Provider:    "musicbrainz",
	}

	result, err := HandleScanLibrary(context.Background(), dbConn, filepath.Join(tmp, "Artist"), LibraryTypeMusic, musicLibraryID, identifier)
	if err != nil {
		t.Fatalf("second scan music: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("unexpected second scan result: %+v", result)
	}

	var title, artist, album, recordingID string
	if err := dbConn.QueryRow(`SELECT title, artist, album, COALESCE(musicbrainz_recording_id, '') FROM music_tracks WHERE library_id = ?`, musicLibraryID).
		Scan(&title, &artist, &album, &recordingID); err != nil {
		t.Fatalf("query music row: %v", err)
	}
	if title != "Second Match" || artist != "Second Artist" || album != "Second Album" || recordingID != "rec-2" {
		t.Fatalf("expected re-identified metadata, got title=%q artist=%q album=%q recording=%q", title, artist, album, recordingID)
	}
}

func getSingleUserID(t *testing.T, dbConn *sql.DB) int {
	t.Helper()
	var userID int
	if err := dbConn.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Fatalf("get user id: %v", err)
	}
	return userID
}

func insertEpisodeForDashboardTest(t *testing.T, dbConn *sql.DB, libraryID int, title, path string, season, episode int) int {
	return insertEpisodeForDashboardTestWithKind(t, dbConn, libraryID, LibraryTypeTV, title, path, season, episode)
}

func insertEpisodeForDashboardTestWithKind(t *testing.T, dbConn *sql.DB, libraryID int, kind, title, path string, season, episode int) int {
	t.Helper()
	var refID int
	table := "tv_episodes"
	if kind == LibraryTypeAnime {
		table = "anime_episodes"
	}
	if err := dbConn.QueryRow(`INSERT INTO `+table+` (library_id, title, path, duration, match_status, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID, title, path, 1800, MatchStatusLocal, season, episode).Scan(&refID); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	var globalID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, kind, refID).Scan(&globalID); err != nil {
		t.Fatalf("insert global episode: %v", err)
	}
	return globalID
}

func insertMovieForDashboardTest(t *testing.T, dbConn *sql.DB, libraryID int, title, path string) int {
	t.Helper()
	var refID int
	if err := dbConn.QueryRow(`INSERT INTO movies (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		libraryID, title, path, 7200, MatchStatusLocal).Scan(&refID); err != nil {
		t.Fatalf("insert movie: %v", err)
	}
	var globalID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, LibraryTypeMovie, refID).Scan(&globalID); err != nil {
		t.Fatalf("insert global movie: %v", err)
	}
	return globalID
}

func insertMusicTrackForDashboardTest(t *testing.T, dbConn *sql.DB, libraryID int, title, path string) int {
	t.Helper()
	var refID int
	if err := dbConn.QueryRow(`INSERT INTO music_tracks (library_id, title, path, duration, match_status) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		libraryID, title, path, 240, MatchStatusLocal).Scan(&refID); err != nil {
		t.Fatalf("insert music track: %v", err)
	}
	var globalID int
	if err := dbConn.QueryRow(`INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, LibraryTypeMusic, refID).Scan(&globalID); err != nil {
		t.Fatalf("insert global music track: %v", err)
	}
	return globalID
}

func setPlaybackTimestamp(t *testing.T, dbConn *sql.DB, userID, mediaID int, timestamp string) {
	t.Helper()
	if _, err := dbConn.Exec(`UPDATE playback_progress SET last_watched_at = ?, updated_at = ? WHERE user_id = ? AND media_id = ?`, timestamp, timestamp, userID, mediaID); err != nil {
		t.Fatalf("set playback timestamp: %v", err)
	}
}
