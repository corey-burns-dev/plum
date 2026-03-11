package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"plum/internal/metadata"
)

// SkipFFprobeInScan is set by tests to skip ffprobe during scan (avoids blocking on fake files).
var SkipFFprobeInScan bool

const (
	MatchStatusIdentified = "identified"
	MatchStatusLocal      = "local"
	MatchStatusUnmatched  = "unmatched"
)

type ScanResult struct {
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Removed   int `json:"removed"`
	Unmatched int `json:"unmatched"`
	Skipped   int `json:"skipped"`
}

var (
	videoExtensions = map[string]struct{}{
		".mp4": {}, ".mkv": {}, ".mov": {}, ".avi": {}, ".webm": {}, ".ts": {}, ".m4v": {},
	}
	audioExtensions = map[string]struct{}{
		".mp3": {}, ".flac": {}, ".m4a": {}, ".aac": {}, ".ogg": {}, ".opus": {}, ".wav": {}, ".alac": {},
	}
	readAudioMetadata = metadata.ReadAudioMetadata
)

type Subtitle struct {
	ID       int    `json:"id"`
	MediaID  int    `json:"media_id"`
	Title    string `json:"title"`
	Language string `json:"language"`
	Format   string `json:"format"`
	Path     string `json:"path"`
}

type EmbeddedSubtitle struct {
	MediaID     int    `json:"media_id"`
	StreamIndex int    `json:"stream_index"`
	Language    string `json:"language"`
	Title       string `json:"title"`
}

type MediaItem struct {
	ID                int                `json:"id"`
	LibraryID         int                `json:"library_id"`
	Title             string             `json:"title"`
	Path              string             `json:"path"`
	Duration          int                `json:"duration"`
	Type              string             `json:"type"`
	MatchStatus       string             `json:"match_status,omitempty"`
	Subtitles         []Subtitle         `json:"subtitles"`
	EmbeddedSubtitles []EmbeddedSubtitle `json:"embeddedSubtitles"`
	TMDBID            int                `json:"tmdb_id"`
	TVDBID            string             `json:"tvdb_id,omitempty"`
	Overview          string             `json:"overview"`
	PosterPath        string             `json:"poster_path"`
	BackdropPath      string             `json:"backdrop_path"`
	ReleaseDate       string             `json:"release_date"`
	VoteAverage       float64            `json:"vote_average"`
	Artist            string             `json:"artist,omitempty"`
	Album             string             `json:"album,omitempty"`
	AlbumArtist       string             `json:"album_artist,omitempty"`
	DiscNumber        int                `json:"disc_number,omitempty"`
	TrackNumber       int                `json:"track_number,omitempty"`
	ReleaseYear       int                `json:"release_year,omitempty"`
	// Season and Episode are set for tv/anime episodes; 0 when not applicable.
	Season  int `json:"season,omitempty"`
	Episode int `json:"episode,omitempty"`
	// ThumbnailPath is set for video items when a frame thumbnail has been generated (e.g. episode still).
	ThumbnailPath string `json:"thumbnail_path,omitempty"`
}

type User struct {
	ID           int       `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    int       `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Library holds a user-defined media library. Type must be one of: "tv", "movie", "music".
// TV and movie libraries use TMDB for metadata; music libraries do not.
type Library struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

// ValidLibraryTypes are the allowed Library.Type values used for identification and scanning.
// Each type maps to a separate table (movies, tv_episodes, anime_episodes, music_tracks).
const (
	LibraryTypeTV    = "tv"
	LibraryTypeMovie = "movie"
	LibraryTypeMusic = "music"
	LibraryTypeAnime = "anime"
)

// sqlitePragmas are applied to every new connection via the DSN so pool connections
// all have foreign_keys and busy_timeout set (connection-specific in SQLite).
const sqlitePragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

func InitDB(conn string) (*sql.DB, error) {
	if conn == "" {
		conn = "./data/plum.db"
	}
	dsn := conn
	if strings.Contains(dsn, "?") {
		dsn += "&" + sqlitePragmas
	} else {
		dsn += "?" + sqlitePragmas
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func createSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at DATETIME NOT NULL,
  expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS libraries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('tv','movie','music','anime')),
  path TEXT NOT NULL,
  created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_libraries_user_id ON libraries(user_id);

-- media_global maps API global id -> (kind, ref_id) for the category table.
CREATE TABLE IF NOT EXISTS media_global (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL CHECK (kind IN ('movie','tv','anime','music')),
  ref_id INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_media_global_kind_ref ON media_global(kind, ref_id);

-- Category tables: each library type writes only to its own table.
CREATE TABLE IF NOT EXISTS movies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  match_status TEXT NOT NULL DEFAULT 'local',
  tmdb_id INTEGER,
  tvdb_id TEXT,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_movies_library_id ON movies(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_movies_library_path ON movies(library_id, path);

CREATE TABLE IF NOT EXISTS tv_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  match_status TEXT NOT NULL DEFAULT 'local',
  tmdb_id INTEGER,
  tvdb_id TEXT,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tv_episodes_library_id ON tv_episodes(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tv_episodes_library_path ON tv_episodes(library_id, path);

CREATE TABLE IF NOT EXISTS anime_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  match_status TEXT NOT NULL DEFAULT 'local',
  tmdb_id INTEGER,
  tvdb_id TEXT,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_anime_episodes_library_id ON anime_episodes(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_anime_episodes_library_path ON anime_episodes(library_id, path);

CREATE TABLE IF NOT EXISTS music_tracks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  match_status TEXT NOT NULL DEFAULT 'local',
  artist TEXT,
  album TEXT,
  album_artist TEXT,
  disc_number INTEGER NOT NULL DEFAULT 0,
  track_number INTEGER NOT NULL DEFAULT 0,
  release_year INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_music_tracks_library_id ON music_tracks(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_music_tracks_library_path ON music_tracks(library_id, path);

CREATE TABLE IF NOT EXISTS subtitles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  media_id INTEGER NOT NULL,
  title TEXT NOT NULL,
  language TEXT NOT NULL,
  format TEXT NOT NULL,
  path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_subtitles_media_id ON subtitles(media_id);

CREATE TABLE IF NOT EXISTS embedded_subtitles (
  media_id INTEGER NOT NULL,
  stream_index INTEGER NOT NULL,
  language TEXT NOT NULL,
  title TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_embedded_subtitles_media_id ON embedded_subtitles(media_id);
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	// Migration: add tvdb_id to category tables if missing (existing DBs).
	for _, table := range []string{"movies", "tv_episodes", "anime_episodes"} {
		_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN tvdb_id TEXT", table))
	}
	for _, table := range []string{"movies", "tv_episodes", "anime_episodes", "music_tracks"} {
		_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN match_status TEXT NOT NULL DEFAULT 'local'", table))
	}
	// Migration: add season/episode to TV and anime tables.
	for _, table := range []string{"tv_episodes", "anime_episodes"} {
		_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN season INTEGER", table))
		_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN episode INTEGER", table))
	}
	// Migration: add thumbnail_path for video episode thumbnails.
	for _, table := range []string{"tv_episodes", "anime_episodes"} {
		_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN thumbnail_path TEXT", table))
	}
	for _, stmt := range []string{
		`ALTER TABLE music_tracks ADD COLUMN artist TEXT`,
		`ALTER TABLE music_tracks ADD COLUMN album TEXT`,
		`ALTER TABLE music_tracks ADD COLUMN album_artist TEXT`,
		`ALTER TABLE music_tracks ADD COLUMN disc_number INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE music_tracks ADD COLUMN track_number INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE music_tracks ADD COLUMN release_year INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = db.Exec(stmt)
	}
	return nil
}

func SeedSample(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media_global`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return nil
}

func GetAllMedia(db *sql.DB) ([]MediaItem, error) {
	items, err := queryAllMediaByKind(db, "")
	if err != nil {
		return nil, err
	}
	return attachSubtitlesBatch(db, items)
}

// queryAllMediaByKind returns media from category tables joined with media_global.
// If kind is "", queries all four categories and merges; otherwise only that kind.
func queryAllMediaByKind(db *sql.DB, kind string) ([]MediaItem, error) {
	kinds := []string{"movie", "tv", "anime", "music"}
	if kind != "" {
		kinds = []string{kind}
	}
	var items []MediaItem
	for _, k := range kinds {
		table := mediaTableForKind(k)
		var q string
		var args []interface{}
		if table == "music_tracks" {
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.artist, m.album, m.album_artist, COALESCE(m.disc_number, 0), COALESCE(m.track_number, 0), COALESCE(m.release_year, 0) FROM music_tracks m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id ORDER BY g.id`
			args = []interface{}{k}
		} else if table == "tv_episodes" || table == "anime_episodes" {
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, COALESCE(m.season, 0), COALESCE(m.episode, 0), m.thumbnail_path FROM ` + table + ` m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id ORDER BY g.id`
			args = []interface{}{k}
		} else {
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average FROM ` + table + ` m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id ORDER BY g.id`
			args = []interface{}{k}
		}
		rows, err := db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m MediaItem
			m.Type = k
			var overview, posterPath, backdropPath, releaseDate, thumbnailPath sql.NullString
			var matchStatus sql.NullString
			var voteAvg sql.NullFloat64
			var tmdbID sql.NullInt64
			var tvdbID sql.NullString
			var artist, album, albumArtist sql.NullString
			if table == "music_tracks" {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &matchStatus, &artist, &album, &albumArtist, &m.DiscNumber, &m.TrackNumber, &m.ReleaseYear)
				if artist.Valid {
					m.Artist = artist.String
				}
				if album.Valid {
					m.Album = album.String
				}
				if albumArtist.Valid {
					m.AlbumArtist = albumArtist.String
				}
			} else if table == "tv_episodes" || table == "anime_episodes" {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &m.Season, &m.Episode, &thumbnailPath)
				m.TMDBID = int(tmdbID.Int64)
				if tvdbID.Valid {
					m.TVDBID = tvdbID.String
				}
				if overview.Valid {
					m.Overview = overview.String
				}
				if posterPath.Valid {
					m.PosterPath = posterPath.String
				}
				if backdropPath.Valid {
					m.BackdropPath = backdropPath.String
				}
				if releaseDate.Valid {
					m.ReleaseDate = releaseDate.String
				}
				if voteAvg.Valid {
					m.VoteAverage = voteAvg.Float64
				}
				if thumbnailPath.Valid {
					m.ThumbnailPath = thumbnailPath.String
				}
			} else {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg)
				m.TMDBID = int(tmdbID.Int64)
				if tvdbID.Valid {
					m.TVDBID = tvdbID.String
				}
				if overview.Valid {
					m.Overview = overview.String
				}
				if posterPath.Valid {
					m.PosterPath = posterPath.String
				}
				if backdropPath.Valid {
					m.BackdropPath = backdropPath.String
				}
				if releaseDate.Valid {
					m.ReleaseDate = releaseDate.String
				}
				if voteAvg.Valid {
					m.VoteAverage = voteAvg.Float64
				}
			}
			if matchStatus.Valid {
				m.MatchStatus = matchStatus.String
			}
			if err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func mediaTableForKind(kind string) string {
	return MediaTableForKind(kind)
}

// MediaTableForKind returns the category table name for a library kind (exported for use by http handlers).
func MediaTableForKind(kind string) string {
	switch kind {
	case "movie":
		return "movies"
	case "tv":
		return "tv_episodes"
	case "anime":
		return "anime_episodes"
	case "music":
		return "music_tracks"
	default:
		return "movies"
	}
}

// IdentificationRow is a library media row eligible for metadata identification or repair.
type IdentificationRow struct {
	RefID   int
	Kind    string
	Title   string
	Path    string
	Season  int
	Episode int
}

// ListIdentifiableByLibrary returns all non-music media rows in the library for identification or repair.
func ListIdentifiableByLibrary(db *sql.DB, libraryID int) ([]IdentificationRow, error) {
	var typ string
	if err := db.QueryRow(`SELECT type FROM libraries WHERE id = ?`, libraryID).Scan(&typ); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	table := mediaTableForKind(typ)
	if table == "music_tracks" {
		return nil, nil
	}
	var q string
	var args []interface{}
	if table == "tv_episodes" || table == "anime_episodes" {
		q = `SELECT m.id, m.title, m.path, COALESCE(m.season, 0), COALESCE(m.episode, 0) FROM ` + table + ` m
WHERE m.library_id = ?`
		args = []interface{}{libraryID}
	} else {
		q = `SELECT m.id, m.title, m.path FROM ` + table + ` m
WHERE m.library_id = ?`
		args = []interface{}{libraryID}
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IdentificationRow
	for rows.Next() {
		var row IdentificationRow
		row.Kind = typ
		if table == "tv_episodes" || table == "anime_episodes" {
			err = rows.Scan(&row.RefID, &row.Title, &row.Path, &row.Season, &row.Episode)
		} else {
			err = rows.Scan(&row.RefID, &row.Title, &row.Path)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateMediaMetadata updates a single category row with identified metadata (title, overview, poster, tmdb_id, etc.).
func UpdateMediaMetadata(db *sql.DB, table string, refID int, title string, overview, posterPath, backdropPath, releaseDate string, voteAvg float64, tmdbID int, tvdbID string, season, episode int) error {
	if table == "tv_episodes" || table == "anime_episodes" {
		_, err := db.Exec(`UPDATE `+table+` SET title = ?, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ?, season = ?, episode = ? WHERE id = ?`,
			title, MatchStatusIdentified, tmdbID, nullStr(tvdbID), nullStr(overview), nullStr(posterPath), nullStr(backdropPath), nullStr(releaseDate), nullFloat64(voteAvg), season, episode, refID)
		return err
	}
	_, err := db.Exec(`UPDATE `+table+` SET title = ?, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ? WHERE id = ?`,
		title, MatchStatusIdentified, tmdbID, nullStr(tvdbID), nullStr(overview), nullStr(posterPath), nullStr(backdropPath), nullStr(releaseDate), nullFloat64(voteAvg), refID)
	return err
}

// ShowEpisodeRef identifies one episode row for refresh/identify (global id, category ref_id, kind, season, episode, tmdb_id).
type ShowEpisodeRef struct {
	GlobalID int
	RefID    int
	Kind     string
	Season   int
	Episode  int
	TMDBID   int
}

// showKeyFromItem returns the same key the frontend uses: "tmdb-{id}" when tmdb_id set, else show name from title.
func showKeyFromItem(tmdbID int, title string) string {
	if tmdbID > 0 {
		return fmt.Sprintf("tmdb-%d", tmdbID)
	}
	// "Show Name - S01E02 - Episode" -> "Show Name"
	if i := strings.Index(strings.ToLower(title), " - s"); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	if i := strings.Index(title, " - "); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	return strings.TrimSpace(title)
}

// ListShowEpisodeRefs returns all episode refs (globalID, refID, kind, season, episode) for the given library and showKey.
// Only TV and anime libraries are supported; returns nil when library type is not tv/anime.
func ListShowEpisodeRefs(db *sql.DB, libraryID int, showKey string) ([]ShowEpisodeRef, error) {
	var typ string
	if err := db.QueryRow(`SELECT type FROM libraries WHERE id = ?`, libraryID).Scan(&typ); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	table := mediaTableForKind(typ)
	if table != "tv_episodes" && table != "anime_episodes" {
		return nil, nil
	}
	q := `SELECT g.id, m.id, COALESCE(m.season, 0), COALESCE(m.episode, 0), COALESCE(m.tmdb_id, 0), m.title
FROM ` + table + ` m
JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ?
ORDER BY g.id`
	rows, err := db.Query(q, typ, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShowEpisodeRef
	for rows.Next() {
		var globalID, refID, season, episode, tmdbID int
		var title string
		if err := rows.Scan(&globalID, &refID, &season, &episode, &tmdbID, &title); err != nil {
			return nil, err
		}
		key := showKeyFromItem(tmdbID, title)
		if key != showKey {
			continue
		}
		out = append(out, ShowEpisodeRef{GlobalID: globalID, RefID: refID, Kind: typ, Season: season, Episode: episode, TMDBID: tmdbID})
	}
	return out, rows.Err()
}

// attachSubtitlesBatch loads subtitles and embedded_subtitles for all items in one query each and attaches them.
func attachSubtitlesBatch(db *sql.DB, items []MediaItem) ([]MediaItem, error) {
	if len(items) == 0 {
		return items, nil
	}
	ids := make([]int, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	subsByID, err := getSubtitlesByMediaIDs(db, ids)
	if err != nil {
		return nil, err
	}
	embByID, err := getEmbeddedSubtitlesByMediaIDs(db, ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Subtitles = subsByID[items[i].ID]
		items[i].EmbeddedSubtitles = embByID[items[i].ID]
	}
	return items, nil
}

func getSubtitlesByMediaIDs(db *sql.DB, mediaIDs []int) (map[int][]Subtitle, error) {
	if len(mediaIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(mediaIDs))
	args := make([]interface{}, len(mediaIDs))
	for i := range mediaIDs {
		placeholders[i] = "?"
		args[i] = mediaIDs[i]
	}
	query := `SELECT id, media_id, title, language, format, path FROM subtitles WHERE media_id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int][]Subtitle)
	for rows.Next() {
		var s Subtitle
		if err := rows.Scan(&s.ID, &s.MediaID, &s.Title, &s.Language, &s.Format, &s.Path); err != nil {
			return nil, err
		}
		out[s.MediaID] = append(out[s.MediaID], s)
	}
	return out, rows.Err()
}

func getEmbeddedSubtitlesByMediaIDs(db *sql.DB, mediaIDs []int) (map[int][]EmbeddedSubtitle, error) {
	if len(mediaIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(mediaIDs))
	args := make([]interface{}, len(mediaIDs))
	for i := range mediaIDs {
		placeholders[i] = "?"
		args[i] = mediaIDs[i]
	}
	query := `SELECT media_id, stream_index, language, title FROM embedded_subtitles WHERE media_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY media_id, stream_index`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int][]EmbeddedSubtitle)
	for rows.Next() {
		var s EmbeddedSubtitle
		if err := rows.Scan(&s.MediaID, &s.StreamIndex, &s.Language, &s.Title); err != nil {
			return nil, err
		}
		out[s.MediaID] = append(out[s.MediaID], s)
	}
	return out, rows.Err()
}

func GetMediaByID(db *sql.DB, id int) (*MediaItem, error) {
	var kind string
	var refID int
	err := db.QueryRow(`SELECT kind, ref_id FROM media_global WHERE id = ?`, id).Scan(&kind, &refID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	table := mediaTableForKind(kind)
	var libID int
	var title, path string
	var duration int
	var season, episode int
	var overview, posterPath, backdropPath, releaseDate, thumbnailPath, matchStatus sql.NullString
	var voteAvg sql.NullFloat64
	var tmdbID sql.NullInt64
	var tvdbID sql.NullString
	var artist, album, albumArtist sql.NullString
	var discNumber, trackNumber, releaseYear int
	if table == "music_tracks" {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.artist, m.album, m.album_artist, COALESCE(m.disc_number, 0), COALESCE(m.track_number, 0), COALESCE(m.release_year, 0) FROM music_tracks m WHERE m.id = ?`, refID).
			Scan(&refID, &libID, &title, &path, &duration, &matchStatus, &artist, &album, &albumArtist, &discNumber, &trackNumber, &releaseYear)
	} else if table == "tv_episodes" || table == "anime_episodes" {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, COALESCE(m.season, 0), COALESCE(m.episode, 0), m.thumbnail_path FROM `+table+` m WHERE m.id = ?`, refID).
			Scan(&refID, &libID, &title, &path, &duration, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &season, &episode, &thumbnailPath)
	} else {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average FROM `+table+` m WHERE m.id = ?`, refID).
			Scan(&refID, &libID, &title, &path, &duration, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m := MediaItem{
		ID:        id,
		LibraryID: libID,
		Title:     title,
		Path:      path,
		Duration:  duration,
		Type:      kind,
	}
	if matchStatus.Valid {
		m.MatchStatus = matchStatus.String
	}
	if table == "tv_episodes" || table == "anime_episodes" {
		m.Season = season
		m.Episode = episode
		if thumbnailPath.Valid {
			m.ThumbnailPath = thumbnailPath.String
		}
	} else if table == "music_tracks" {
		if artist.Valid {
			m.Artist = artist.String
		}
		if album.Valid {
			m.Album = album.String
		}
		if albumArtist.Valid {
			m.AlbumArtist = albumArtist.String
		}
		m.DiscNumber = discNumber
		m.TrackNumber = trackNumber
		m.ReleaseYear = releaseYear
	}
	if overview.Valid {
		m.Overview = overview.String
	}
	if posterPath.Valid {
		m.PosterPath = posterPath.String
	}
	if backdropPath.Valid {
		m.BackdropPath = backdropPath.String
	}
	if releaseDate.Valid {
		m.ReleaseDate = releaseDate.String
	}
	if voteAvg.Valid {
		m.VoteAverage = voteAvg.Float64
	}
	if tmdbID.Valid {
		m.TMDBID = int(tmdbID.Int64)
	}
	if tvdbID.Valid {
		m.TVDBID = tvdbID.String
	}
	subs, err := getSubtitlesForMedia(db, id)
	if err != nil {
		return nil, err
	}
	m.Subtitles = subs
	emb, err := getEmbeddedSubtitlesForMedia(db, id)
	if err != nil {
		return nil, err
	}
	m.EmbeddedSubtitles = emb
	return &m, nil
}

// GetMediaByLibraryID returns all media for a library (one category table only), no N+1.
func GetMediaByLibraryID(db *sql.DB, libraryID int) ([]MediaItem, error) {
	var typ string
	err := db.QueryRow(`SELECT type FROM libraries WHERE id = ?`, libraryID).Scan(&typ)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	items, err := queryMediaByLibraryID(db, libraryID, typ)
	if err != nil {
		return nil, err
	}
	return attachSubtitlesBatch(db, items)
}

// queryMediaByLibraryID queries the single category table for this library.
func queryMediaByLibraryID(db *sql.DB, libraryID int, kind string) ([]MediaItem, error) {
	table := mediaTableForKind(kind)
	q := `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average
FROM ` + table + ` m
JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ?
ORDER BY g.id`
	if table == "music_tracks" {
		q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.artist, m.album, m.album_artist, COALESCE(m.disc_number, 0), COALESCE(m.track_number, 0), COALESCE(m.release_year, 0)
FROM music_tracks m
JOIN media_global g ON g.kind = 'music' AND g.ref_id = m.id
WHERE m.library_id = ?
ORDER BY g.id`
	} else if table == "tv_episodes" || table == "anime_episodes" {
		q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, COALESCE(m.season, 0), COALESCE(m.episode, 0), m.thumbnail_path
FROM ` + table + ` m
JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ?
ORDER BY g.id`
	}
	var rows *sql.Rows
	var err error
	if table == "music_tracks" {
		rows, err = db.Query(q, libraryID)
	} else {
		rows, err = db.Query(q, kind, libraryID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MediaItem
	for rows.Next() {
		var m MediaItem
		m.Type = kind
		m.LibraryID = libraryID
		var overview, posterPath, backdropPath, releaseDate, thumbnailPath, matchStatus sql.NullString
		var voteAvg sql.NullFloat64
		var tmdbID sql.NullInt64
		var tvdbID sql.NullString
		var artist, album, albumArtist sql.NullString
		if table == "music_tracks" {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &matchStatus, &artist, &album, &albumArtist, &m.DiscNumber, &m.TrackNumber, &m.ReleaseYear)
			if artist.Valid {
				m.Artist = artist.String
			}
			if album.Valid {
				m.Album = album.String
			}
			if albumArtist.Valid {
				m.AlbumArtist = albumArtist.String
			}
		} else if table == "tv_episodes" || table == "anime_episodes" {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &m.Season, &m.Episode, &thumbnailPath)
			m.TMDBID = int(tmdbID.Int64)
			if tvdbID.Valid {
				m.TVDBID = tvdbID.String
			}
			if overview.Valid {
				m.Overview = overview.String
			}
			if posterPath.Valid {
				m.PosterPath = posterPath.String
			}
			if backdropPath.Valid {
				m.BackdropPath = backdropPath.String
			}
			if releaseDate.Valid {
				m.ReleaseDate = releaseDate.String
			}
			if voteAvg.Valid {
				m.VoteAverage = voteAvg.Float64
			}
			if thumbnailPath.Valid {
				m.ThumbnailPath = thumbnailPath.String
			}
		} else {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg)
			m.TMDBID = int(tmdbID.Int64)
			if tvdbID.Valid {
				m.TVDBID = tvdbID.String
			}
			if overview.Valid {
				m.Overview = overview.String
			}
			if posterPath.Valid {
				m.PosterPath = posterPath.String
			}
			if backdropPath.Valid {
				m.BackdropPath = backdropPath.String
			}
			if releaseDate.Valid {
				m.ReleaseDate = releaseDate.String
			}
			if voteAvg.Valid {
				m.VoteAverage = voteAvg.Float64
			}
		}
		if matchStatus.Valid {
			m.MatchStatus = matchStatus.String
		}
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func getSubtitlesForMedia(db *sql.DB, mediaID int) ([]Subtitle, error) {
	rows, err := db.Query(`SELECT id, media_id, title, language, format, path FROM subtitles WHERE media_id = ?`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subtitle
	for rows.Next() {
		var s Subtitle
		if err := rows.Scan(&s.ID, &s.MediaID, &s.Title, &s.Language, &s.Format, &s.Path); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func getEmbeddedSubtitlesForMedia(db *sql.DB, mediaID int) ([]EmbeddedSubtitle, error) {
	rows, err := db.Query(`SELECT media_id, stream_index, language, title FROM embedded_subtitles WHERE media_id = ? ORDER BY stream_index`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []EmbeddedSubtitle
	for rows.Next() {
		var s EmbeddedSubtitle
		if err := rows.Scan(&s.MediaID, &s.StreamIndex, &s.Language, &s.Title); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func GetSubtitleByID(db *sql.DB, id int) (*Subtitle, error) {
	var s Subtitle
	err := db.QueryRow(`SELECT id, media_id, title, language, format, path FROM subtitles WHERE id = ?`, id).
		Scan(&s.ID, &s.MediaID, &s.Title, &s.Language, &s.Format, &s.Path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func HandleListMedia(w http.ResponseWriter, r *http.Request, dbConn *sql.DB) {
	items, err := GetAllMedia(dbConn)
	if err != nil {
		log.Printf("list media: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(items); err != nil {
		log.Printf("encode media: %v", err)
	}
}

var ErrNotFound = errors.New("not found")

func getMediaDuration(path string) (int, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var d float64
	if _, err := fmt.Sscanf(string(out), "%f", &d); err != nil {
		return 0, err
	}
	return int(d), nil
}

// probeEmbeddedSubtitles uses ffprobe to discover subtitle streams embedded in the video file.
// It uses a short timeout so that invalid or test files do not hang the scan.
func probeEmbeddedSubtitles(ctx context.Context, path string) ([]EmbeddedSubtitle, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "ffprobe",
		"-v", "error",
		"-select_streams", "s",
		"-show_entries", "stream=index:stream_tags=language,title",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Streams []struct {
			Index int `json:"index"`
			Tags  struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}

	var subs []EmbeddedSubtitle
	for _, s := range parsed.Streams {
		lang := s.Tags.Language
		if lang == "" {
			lang = "und"
		}
		title := s.Tags.Title
		if title == "" {
			title = lang
		}
		subs = append(subs, EmbeddedSubtitle{
			StreamIndex: s.Index,
			Language:    lang,
			Title:       title,
		})
	}
	return subs, nil
}

func scanForSubtitles(ctx context.Context, dbConn *sql.DB, mediaID int, videoPath string) error {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, base) {
			ext := strings.ToLower(filepath.Ext(name))
			if ext == ".srt" || ext == ".vtt" || ext == ".ass" || ext == ".ssa" {
				path := filepath.Join(dir, name)
				var existing int
				if err := dbConn.QueryRow(`SELECT COUNT(1) FROM subtitles WHERE path = ?`, path).Scan(&existing); err != nil {
					return err
				}
				if existing > 0 {
					continue
				}

				lang := "und"
				parts := strings.Split(strings.TrimSuffix(name, ext), ".")
				if len(parts) > 1 {
					lastPart := parts[len(parts)-1]
					if len(lastPart) == 2 || len(lastPart) == 3 {
						lang = lastPart
					}
				}

				_, err := dbConn.ExecContext(ctx,
					`INSERT INTO subtitles (media_id, title, language, format, path) VALUES (?, ?, ?, ?, ?)`,
					mediaID, name, lang, ext[1:], path,
				)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// HandleScanLibrary walks the given filesystem path and inserts supported media files
// into the category table for this library type only (movies, tv_episodes, anime_episodes, or music_tracks).
// libraryID must be > 0. mediaType must be tv, movie, music, or anime.
// id may be nil; then no metadata lookup is performed.
func HandleScanLibrary(ctx context.Context, dbConn *sql.DB, root, mediaType string, libraryID int, id metadata.Identifier) (ScanResult, error) {
	result := ScanResult{}
	if root == "" {
		return result, fmt.Errorf("path is required")
	}
	if mediaType == "" {
		mediaType = LibraryTypeMovie
	}
	if libraryID <= 0 {
		return result, fmt.Errorf("library id is required")
	}

	kind := mediaType
	table := mediaTableForKind(kind)
	exts := allowedExtensions(kind)

	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("path not found: %q — when running in Docker, use the container path (e.g. /tv, /movies, /music), not the host path", root)
		}
		return result, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("path is not a directory")
	}

	seenPaths := map[string]struct{}{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := exts[ext]; !ok {
			return nil
		}
		relPath, _ := filepath.Rel(root, path)
		if shouldSkipScanPath(kind, relPath, d.Name()) {
			result.Skipped++
			return nil
		}
		seenPaths[path] = struct{}{}

		existing, err := lookupExistingMedia(dbConn, table, kind, libraryID, path)
		if err != nil {
			return err
		}
		isNew := existing.RefID == 0

		title := strings.TrimSuffix(d.Name(), ext)
		if title == "" {
			title = d.Name()
		}

		mItem := MediaItem{
			Title:       title,
			Path:        path,
			Type:        kind,
			MatchStatus: MatchStatusLocal,
		}
		var fileInfo metadata.MediaInfo
		switch kind {
		case LibraryTypeMusic:
			pathInfo := metadata.ParsePathForMusic(relPath, d.Name())
			audioMeta := metadata.MusicMetadata{}
			if !SkipFFprobeInScan {
				if probed, duration, err := readAudioMetadata(ctx, path); err == nil {
					audioMeta = probed
					mItem.Duration = duration
				}
			}
			merged := metadata.MergeMusicMetadata(pathInfo, audioMeta, title)
			mItem.Title = merged.Title
			mItem.Artist = merged.Artist
			mItem.Album = merged.Album
			mItem.AlbumArtist = merged.AlbumArtist
			mItem.DiscNumber = merged.DiscNumber
			mItem.TrackNumber = merged.TrackNumber
			mItem.ReleaseYear = merged.ReleaseYear
		case LibraryTypeMovie:
			movieInfo := metadata.ParseMovie(relPath, d.Name())
			mItem.Title = metadata.MovieDisplayTitle(movieInfo, title)
			fileInfo = metadata.MovieMediaInfo(movieInfo)
		case LibraryTypeTV, LibraryTypeAnime:
			fileInfo = metadata.ParseFilename(d.Name())
			pathInfo := metadata.ParsePathForTV(relPath, d.Name())
			merged := metadata.MergePathInfo(pathInfo, fileInfo)
			showRoot := metadata.ShowRootPath(root, path)
			metadata.ApplyShowNFO(&merged, showRoot)
			if kind == LibraryTypeAnime {
				if merged.IsSpecial && merged.Episode > 0 {
					merged.Season = 0
				}
			}
			mItem.Season = merged.Season
			mItem.Episode = merged.Episode
			mItem.Title = buildEpisodeDisplayTitle(pathInfo.ShowName, merged, title, fileInfo.Title)
			fileInfo = merged
		}
		if mItem.Duration == 0 && kind != LibraryTypeMusic && !SkipFFprobeInScan {
			if duration, err := getMediaDuration(path); err == nil {
				mItem.Duration = duration
			}
		}

		identifyInfo := fileInfo
		hasMetadata := existingHasMetadata(kind, existing)
		forceRefresh := kind != LibraryTypeMusic && hasExplicitProviderID(identifyInfo)
		shouldIdentify := id != nil &&
			(kind == LibraryTypeTV || kind == LibraryTypeAnime || kind == LibraryTypeMovie) &&
			(!hasMetadata || forceRefresh)
		if shouldIdentify {
			switch kind {
			case LibraryTypeTV:
				if res := id.IdentifyTV(ctx, identifyInfo); res != nil {
					mItem.Title = res.Title
					mItem.Overview = res.Overview
					mItem.PosterPath = res.PosterURL
					mItem.BackdropPath = res.BackdropURL
					mItem.ReleaseDate = res.ReleaseDate
					mItem.VoteAverage = res.VoteAverage
					if res.Provider == "tmdb" {
						if id, err := parseInt(res.ExternalID); err == nil {
							mItem.TMDBID = id
						}
					} else if res.Provider == "tvdb" {
						mItem.TVDBID = res.ExternalID
					}
					mItem.MatchStatus = MatchStatusIdentified
				} else {
					mItem.MatchStatus = MatchStatusUnmatched
				}
			case LibraryTypeAnime:
				if res := id.IdentifyAnime(ctx, identifyInfo); res != nil {
					mItem.Title = res.Title
					mItem.Overview = res.Overview
					mItem.PosterPath = res.PosterURL
					mItem.BackdropPath = res.BackdropURL
					mItem.ReleaseDate = res.ReleaseDate
					mItem.VoteAverage = res.VoteAverage
					if res.Provider == "tmdb" {
						if id, err := parseInt(res.ExternalID); err == nil {
							mItem.TMDBID = id
						}
					} else if res.Provider == "tvdb" {
						mItem.TVDBID = res.ExternalID
					}
					mItem.MatchStatus = MatchStatusIdentified
				} else {
					mItem.MatchStatus = MatchStatusUnmatched
				}
			case LibraryTypeMovie:
				if res := id.IdentifyMovie(ctx, identifyInfo); res != nil {
					mItem.Title = res.Title
					mItem.Overview = res.Overview
					mItem.PosterPath = res.PosterURL
					mItem.BackdropPath = res.BackdropURL
					mItem.ReleaseDate = res.ReleaseDate
					mItem.VoteAverage = res.VoteAverage
					if res.Provider == "tmdb" {
						if id, err := parseInt(res.ExternalID); err == nil {
							mItem.TMDBID = id
						}
					} else if res.Provider == "tvdb" {
						mItem.TVDBID = res.ExternalID
					}
					mItem.MatchStatus = MatchStatusIdentified
				} else {
					mItem.MatchStatus = MatchStatusUnmatched
				}
			}
		} else if existing.MatchStatus != "" {
			mItem.MatchStatus = existing.MatchStatus
		}
		if shouldIdentify && mItem.MatchStatus == MatchStatusUnmatched {
			result.Unmatched++
		}

		var globalID int
		if isNew {
			_, globalID, err = insertScannedItem(ctx, dbConn, table, kind, libraryID, mItem)
			if err != nil {
				return err
			}
			result.Added++
		} else {
			globalID = existing.GlobalID
			if err := updateScannedItem(ctx, dbConn, table, existing.RefID, mItem); err != nil {
				return err
			}
			result.Updated++
		}

		if kind != LibraryTypeMusic {
			if err := scanForSubtitles(ctx, dbConn, globalID, path); err != nil {
				log.Printf("scan subtitles for %s: %v", path, err)
			}

			var embeddedSubs []EmbeddedSubtitle
			if !SkipFFprobeInScan {
				embeddedSubs, _ = probeEmbeddedSubtitles(ctx, path)
			}
			if len(embeddedSubs) > 0 {
				if _, err := dbConn.ExecContext(ctx, `DELETE FROM embedded_subtitles WHERE media_id = ?`, globalID); err != nil {
					log.Printf("clear embedded_subtitles for media %d: %v", globalID, err)
				} else {
					for _, s := range embeddedSubs {
						if _, err := dbConn.ExecContext(ctx, `INSERT INTO embedded_subtitles (media_id, stream_index, language, title) VALUES (?, ?, ?, ?)`, globalID, s.StreamIndex, s.Language, s.Title); err != nil {
							log.Printf("insert embedded_subtitles for media %d: %v", globalID, err)
						}
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		return result, err
	}
	removed, err := pruneMissingMedia(ctx, dbConn, table, kind, libraryID, seenPaths)
	if err != nil {
		return result, err
	}
	result.Removed = removed
	return result, nil
}

type existingMediaRow struct {
	RefID       int
	GlobalID    int
	TMDBID      int
	TVDBID      string
	MatchStatus string
}

func allowedExtensions(kind string) map[string]struct{} {
	if kind == LibraryTypeMusic {
		return audioExtensions
	}
	return videoExtensions
}

func shouldSkipScanPath(kind, relPath, filename string) bool {
	if kind != LibraryTypeMovie {
		return false
	}
	return metadata.ParseMovie(relPath, filename).IsExtra
}

func buildEpisodeDisplayTitle(showName string, info metadata.MediaInfo, fallbackTitle, fileTitle string) string {
	displayShow := strings.TrimSpace(showName)
	if normalized := strings.ToLower(displayShow); strings.HasPrefix(normalized, "season ") || strings.HasPrefix(normalized, "s0") {
		displayShow = ""
	}
	if candidate := prettifyDisplayTitle(info.Title); candidate != "" && (displayShow == "" || len(displayShow) <= 2) {
		displayShow = candidate
	}
	if displayShow == "" && info.Title != "" {
		displayShow = prettifyDisplayTitle(info.Title)
	}
	if displayShow == "" {
		displayShow = fallbackTitle
	}
	if info.Episode > 0 {
		title := fmt.Sprintf("%s - S%02dE%02d", displayShow, info.Season, info.Episode)
		extraTitle := prettifyTitle(fileTitle)
		if extraTitle != "" &&
			!metadata.IsGenericEpisodeTitle(fileTitle, info.Season, info.Episode) &&
			!strings.EqualFold(metadata.NormalizeSeriesTitle(extraTitle), metadata.NormalizeSeriesTitle(displayShow)) {
			title += " - " + extraTitle
		}
		return title
	}
	return displayShow
}

func prettifyTitle(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(s, filepath.Ext(s)))
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.TrimSpace(s)
}

func prettifyDisplayTitle(s string) string {
	s = prettifyTitle(s)
	if s == strings.ToLower(s) {
		words := strings.Fields(s)
		for i, word := range words {
			if word == "" {
				continue
			}
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
		return strings.Join(words, " ")
	}
	return s
}

func lookupExistingMedia(dbConn *sql.DB, table, kind string, libraryID int, path string) (existingMediaRow, error) {
	var row existingMediaRow
	if table == "music_tracks" {
		err := dbConn.QueryRow(`SELECT m.id, COALESCE(m.match_status, 'local') FROM music_tracks m WHERE m.library_id = ? AND m.path = ?`, libraryID, path).Scan(&row.RefID, &row.MatchStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return row, nil
		}
		if err != nil {
			return row, err
		}
	} else {
		var tvdbID sql.NullString
		err := dbConn.QueryRow(`SELECT m.id, COALESCE(m.tmdb_id, 0), m.tvdb_id, COALESCE(m.match_status, 'local') FROM `+table+` m WHERE m.library_id = ? AND m.path = ?`, libraryID, path).
			Scan(&row.RefID, &row.TMDBID, &tvdbID, &row.MatchStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return row, nil
		}
		if err != nil {
			return row, err
		}
		if tvdbID.Valid {
			row.TVDBID = tvdbID.String
		}
	}
	_ = dbConn.QueryRow(`SELECT id FROM media_global WHERE kind = ? AND ref_id = ?`, kind, row.RefID).Scan(&row.GlobalID)
	return row, nil
}

func insertScannedItem(ctx context.Context, dbConn *sql.DB, table, kind string, libraryID int, mItem MediaItem) (int, int, error) {
	var refID int
	if table == "music_tracks" {
		err := dbConn.QueryRowContext(ctx, `INSERT INTO music_tracks (library_id, title, path, duration, match_status, artist, album, album_artist, disc_number, track_number, release_year) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, mItem.Title, mItem.Path, mItem.Duration, mItem.MatchStatus, nullStr(mItem.Artist), nullStr(mItem.Album), nullStr(mItem.AlbumArtist), mItem.DiscNumber, mItem.TrackNumber, mItem.ReleaseYear).Scan(&refID)
		if err != nil {
			return 0, 0, err
		}
	} else if table == "tv_episodes" || table == "anime_episodes" {
		err := dbConn.QueryRowContext(ctx, `INSERT INTO `+table+` (library_id, title, path, duration, match_status, tmdb_id, tvdb_id, overview, poster_path, backdrop_path, release_date, vote_average, season, episode) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, mItem.Title, mItem.Path, mItem.Duration, mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), mItem.Season, mItem.Episode).Scan(&refID)
		if err != nil {
			return 0, 0, err
		}
	} else {
		err := dbConn.QueryRowContext(ctx, `INSERT INTO `+table+` (library_id, title, path, duration, match_status, tmdb_id, tvdb_id, overview, poster_path, backdrop_path, release_date, vote_average) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, mItem.Title, mItem.Path, mItem.Duration, mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage)).Scan(&refID)
		if err != nil {
			return 0, 0, err
		}
	}
	var globalID int
	err := dbConn.QueryRowContext(ctx, `INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, kind, refID).Scan(&globalID)
	if err != nil {
		return 0, 0, err
	}
	return refID, globalID, nil
}

func updateScannedItem(ctx context.Context, dbConn *sql.DB, table string, refID int, mItem MediaItem) error {
	if table == "music_tracks" {
		_, err := dbConn.ExecContext(ctx, `UPDATE music_tracks SET title = ?, path = ?, duration = ?, match_status = ?, artist = ?, album = ?, album_artist = ?, disc_number = ?, track_number = ?, release_year = ? WHERE id = ?`,
			mItem.Title, mItem.Path, mItem.Duration, mItem.MatchStatus, nullStr(mItem.Artist), nullStr(mItem.Album), nullStr(mItem.AlbumArtist), mItem.DiscNumber, mItem.TrackNumber, mItem.ReleaseYear, refID)
		return err
	}
	if table == "tv_episodes" || table == "anime_episodes" {
		_, err := dbConn.ExecContext(ctx, `UPDATE `+table+` SET title = ?, path = ?, duration = ?, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ?, season = ?, episode = ? WHERE id = ?`,
			mItem.Title, mItem.Path, mItem.Duration, mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), mItem.Season, mItem.Episode, refID)
		return err
	}
	_, err := dbConn.ExecContext(ctx, `UPDATE `+table+` SET title = ?, path = ?, duration = ?, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ? WHERE id = ?`,
		mItem.Title, mItem.Path, mItem.Duration, mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), refID)
	return err
}

func pruneMissingMedia(ctx context.Context, dbConn *sql.DB, table, kind string, libraryID int, seenPaths map[string]struct{}) (int, error) {
	rows, err := dbConn.Query(`SELECT m.id, m.path, COALESCE(g.id, 0) FROM `+table+` m LEFT JOIN media_global g ON g.kind = ? AND g.ref_id = m.id WHERE m.library_id = ?`, kind, libraryID)
	if err != nil {
		return 0, err
	}
	type staleRow struct {
		refID    int
		globalID int
		path     string
	}
	var stale []staleRow
	for rows.Next() {
		var refID, globalID int
		var path string
		if err := rows.Scan(&refID, &path, &globalID); err != nil {
			rows.Close()
			return 0, err
		}
		if _, ok := seenPaths[path]; ok {
			continue
		}
		stale = append(stale, staleRow{refID: refID, globalID: globalID, path: path})
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	removed := 0
	for _, row := range stale {
		if row.globalID > 0 {
			if _, err := dbConn.ExecContext(ctx, `DELETE FROM subtitles WHERE media_id = ?`, row.globalID); err != nil {
				return removed, err
			}
			if _, err := dbConn.ExecContext(ctx, `DELETE FROM embedded_subtitles WHERE media_id = ?`, row.globalID); err != nil {
				return removed, err
			}
			if _, err := dbConn.ExecContext(ctx, `DELETE FROM media_global WHERE id = ?`, row.globalID); err != nil {
				return removed, err
			}
		}
		if _, err := dbConn.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = ?`, row.refID); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func hasExplicitProviderID(info metadata.MediaInfo) bool {
	return info.TMDBID > 0 || info.TVDBID != ""
}

func existingHasMetadata(kind string, row existingMediaRow) bool {
	if row.TMDBID != 0 {
		return true
	}
	if kind == LibraryTypeAnime {
		return false
	}
	return row.TVDBID != ""
}

func nullFloat64(v float64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

// HandleStreamMedia looks up a media item and serves the file contents.
func HandleStreamMedia(w http.ResponseWriter, r *http.Request, dbConn *sql.DB, id int) error {
	item, err := GetMediaByID(dbConn, id)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrNotFound
	}

	http.ServeFile(w, r, item.Path)
	return nil
}

// HandleStreamSubtitle looks up a subtitle and serves it as VTT.
func HandleStreamSubtitle(w http.ResponseWriter, r *http.Request, dbConn *sql.DB, id int) error {
	s, err := GetSubtitleByID(dbConn, id)
	if err != nil {
		return err
	}
	if s == nil {
		return ErrNotFound
	}

	if s.Format == "vtt" {
		w.Header().Set("Content-Type", "text/vtt")
		http.ServeFile(w, r, s.Path)
		return nil
	}

	if s.Format == "srt" || s.Format == "ass" || s.Format == "ssa" {
		w.Header().Set("Content-Type", "text/vtt")
		// Convert subtitle to VTT using ffmpeg and stream it
		cmd := exec.Command("ffmpeg", "-i", s.Path, "-f", "webvtt", "-")
		cmd.Stdout = w
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ffmpeg error: %w", err)
		}
		return nil
	}

	return fmt.Errorf("unsupported subtitle format: %s", s.Format)
}

// HandleStreamEmbeddedSubtitle extracts an embedded subtitle stream and serves it as VTT.
func HandleStreamEmbeddedSubtitle(w http.ResponseWriter, r *http.Request, dbConn *sql.DB, mediaID int, streamIndex int) error {
	item, err := GetMediaByID(dbConn, mediaID)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrNotFound
	}

	w.Header().Set("Content-Type", "text/vtt")

	// Use the global stream index reported by ffprobe with -map 0:<index>.
	cmd := exec.Command("ffmpeg", "-i", item.Path, "-map", fmt.Sprintf("0:%d", streamIndex), "-f", "webvtt", "-")
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg error: %w", err)
	}
	return nil
}

// GenerateThumbnail extracts a single frame from the video at ~1 minute (or start if shorter) and writes it to outputPath as JPEG.
func GenerateThumbnail(ctx context.Context, videoPath, outputPath string) error {
	// -ss before -i for fast seek; 10s so short videos still get a frame; -vframes 1; -q:v 2 for good quality
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-ss", "10", "-i", videoPath, "-vframes", "1", "-q:v", "2", outputPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg thumbnail: %w", err)
	}
	return nil
}

// UpdateThumbnailPath sets thumbnail_path on the category row for the given global media ID.
func UpdateThumbnailPath(dbConn *sql.DB, globalID int, relativePath string) error {
	var kind string
	var refID int
	err := dbConn.QueryRow(`SELECT kind, ref_id FROM media_global WHERE id = ?`, globalID).Scan(&kind, &refID)
	if err != nil {
		return err
	}
	table := mediaTableForKind(kind)
	if table != "tv_episodes" && table != "anime_episodes" {
		return fmt.Errorf("thumbnail only supported for tv/anime, got %s", kind)
	}
	_, err = dbConn.Exec(`UPDATE `+table+` SET thumbnail_path = ? WHERE id = ?`, relativePath, refID)
	return err
}

// HandleServeThumbnail serves the thumbnail image for a media item, generating it on demand if missing.
func HandleServeThumbnail(w http.ResponseWriter, r *http.Request, dbConn *sql.DB, globalID int, thumbDir string) error {
	item, err := GetMediaByID(dbConn, globalID)
	if err != nil || item == nil {
		return ErrNotFound
	}
	if item.Type == "music" || item.Type == "movie" {
		return ErrNotFound
	}
	relPath := fmt.Sprintf("%d.jpg", globalID)
	absPath := filepath.Join(thumbDir, relPath)
	if item.ThumbnailPath != "" {
		existing := filepath.Join(thumbDir, item.ThumbnailPath)
		if _, err := os.Stat(existing); err == nil {
			w.Header().Set("Content-Type", "image/jpeg")
			http.ServeFile(w, r, existing)
			return nil
		}
	}
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return fmt.Errorf("mkdir thumbnails: %w", err)
	}
	if err := GenerateThumbnail(r.Context(), item.Path, absPath); err != nil {
		log.Printf("generate thumbnail for media %d: %v", globalID, err)
		return fmt.Errorf("thumbnail generation failed: %w", err)
	}
	if err := UpdateThumbnailPath(dbConn, globalID, relPath); err != nil {
		_ = os.Remove(absPath)
		return err
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, absPath)
	return nil
}
