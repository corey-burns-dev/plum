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
	"strings"
	"time"

	"plum/internal/metadata"
)

// SkipFFprobeInScan is set by tests to skip ffprobe during scan (avoids blocking on fake files).
var SkipFFprobeInScan bool

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
	Subtitles         []Subtitle         `json:"subtitles"`
	EmbeddedSubtitles []EmbeddedSubtitle `json:"embeddedSubtitles"`
	TMDBID            int                `json:"tmdb_id"`
	Overview          string             `json:"overview"`
	PosterPath        string             `json:"poster_path"`
	BackdropPath      string             `json:"backdrop_path"`
	ReleaseDate       string             `json:"release_date"`
	VoteAverage       float64            `json:"vote_average"`
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
  tmdb_id INTEGER,
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
  tmdb_id INTEGER,
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
  tmdb_id INTEGER,
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
  duration INTEGER NOT NULL DEFAULT 0
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
	_, err := db.Exec(schema)
	return err
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
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration FROM music_tracks m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id ORDER BY g.id`
			args = []interface{}{k}
		} else {
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.tmdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average FROM ` + table + ` m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id ORDER BY g.id`
			args = []interface{}{k}
		}
		rows, err := db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m MediaItem
			m.Type = k
			var overview, posterPath, backdropPath, releaseDate sql.NullString
			var voteAvg sql.NullFloat64
			var tmdbID sql.NullInt64
			if table == "music_tracks" {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration)
			} else {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &tmdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg)
				m.TMDBID = int(tmdbID.Int64)
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
	var overview, posterPath, backdropPath, releaseDate sql.NullString
	var voteAvg sql.NullFloat64
	var tmdbID sql.NullInt64
	if table == "music_tracks" {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration FROM music_tracks m WHERE m.id = ?`, refID).Scan(&refID, &libID, &title, &path, &duration)
	} else {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration, m.tmdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average FROM `+table+` m WHERE m.id = ?`, refID).
			Scan(&refID, &libID, &title, &path, &duration, &tmdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg)
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
	q := `SELECT g.id, m.library_id, m.title, m.path, m.duration, m.tmdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average
FROM ` + table + ` m
JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ?
ORDER BY g.id`
	if table == "music_tracks" {
		q = `SELECT g.id, m.library_id, m.title, m.path, m.duration
FROM music_tracks m
JOIN media_global g ON g.kind = 'music' AND g.ref_id = m.id
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
		var overview, posterPath, backdropPath, releaseDate sql.NullString
		var voteAvg sql.NullFloat64
		var tmdbID sql.NullInt64
		if table == "music_tracks" {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration)
		} else {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &tmdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg)
			m.TMDBID = int(tmdbID.Int64)
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
// It returns the number of new records added.
func HandleScanLibrary(ctx context.Context, dbConn *sql.DB, root, mediaType, tmdbKey string, libraryID int) (int, error) {
	if root == "" {
		return 0, fmt.Errorf("path is required")
	}
	if mediaType == "" {
		mediaType = LibraryTypeMovie
	}
	if libraryID <= 0 {
		return 0, fmt.Errorf("library id is required")
	}

	kind := mediaType
	table := mediaTableForKind(kind)
	tmdb := metadata.NewTMDBClient(tmdbKey)

	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("path not found: %q — when running in Docker, use the container path (e.g. /tv, /movies, /music), not the host path", root)
		}
		return 0, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("path is not a directory")
	}

	exts := map[string]struct{}{
		".mp4": {}, ".mkv": {}, ".mov": {}, ".avi": {}, ".webm": {}, ".ts": {}, ".m4v": {},
	}

	added := 0
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

		var refID int
		var tmdbID int
		var existingGlobalID int
		var rowErr error
		if table == "music_tracks" {
			rowErr = dbConn.QueryRow(`SELECT m.id FROM music_tracks m WHERE m.library_id = ? AND m.path = ?`, libraryID, path).Scan(&refID)
			tmdbID = 0
		} else {
			rowErr = dbConn.QueryRow(`SELECT m.id, COALESCE(m.tmdb_id, 0) FROM `+table+` m WHERE m.library_id = ? AND m.path = ?`, libraryID, path).Scan(&refID, &tmdbID)
		}
		if rowErr == nil {
			_ = dbConn.QueryRow(`SELECT id FROM media_global WHERE kind = ? AND ref_id = ?`, kind, refID).Scan(&existingGlobalID)
		}
		isNew := errors.Is(rowErr, sql.ErrNoRows)
		if rowErr != nil && !isNew {
			return rowErr
		}

		title := strings.TrimSuffix(d.Name(), ext)
		if title == "" {
			title = d.Name()
		}

		var mItem MediaItem
		mItem.Title = title
		mItem.Path = path
		mItem.Type = kind

		doTMDB := (kind == LibraryTypeTV || kind == LibraryTypeAnime || kind == LibraryTypeMovie) && tmdbKey != ""
		if doTMDB {
			switch kind {
			case LibraryTypeTV, LibraryTypeAnime:
				info := metadata.ParseFilename(d.Name())
				results, _ := tmdb.SearchTV(info.Title)
				if len(results) > 0 {
					series := results[0]
					if info.Season > 0 && info.Episode > 0 {
						episode, err := tmdb.GetEpisodeDetails(series.ID, info.Season, info.Episode)
						if err == nil {
							mItem.TMDBID = episode.ID
							mItem.Title = fmt.Sprintf("%s - S%02dE%02d - %s", series.Name, info.Season, info.Episode, episode.Name)
							mItem.Overview = episode.Overview
							mItem.PosterPath = episode.PosterPath
							if mItem.PosterPath == "" {
								mItem.PosterPath = series.PosterPath
							}
							mItem.BackdropPath = series.BackdropPath
							mItem.ReleaseDate = episode.ReleaseDate
							mItem.VoteAverage = episode.VoteAverage
						}
					} else {
						mItem.TMDBID = series.ID
						mItem.Title = series.Name
						mItem.Overview = series.Overview
						mItem.PosterPath = series.PosterPath
						mItem.BackdropPath = series.BackdropPath
						mItem.ReleaseDate = series.FirstAirDate
						mItem.VoteAverage = series.VoteAverage
					}
				}
			case LibraryTypeMovie:
				info := metadata.ParseFilename(d.Name())
				results, _ := tmdb.SearchMovie(info.Title)
				if len(results) > 0 {
					movie := results[0]
					mItem.TMDBID = movie.ID
					mItem.Title = movie.Title
					mItem.Overview = movie.Overview
					mItem.PosterPath = movie.PosterPath
					mItem.BackdropPath = movie.BackdropPath
					mItem.ReleaseDate = movie.ReleaseDate
					mItem.VoteAverage = movie.VoteAverage
				}
			}
		}

		var globalID int
		if isNew {
			if table == "music_tracks" {
				err = dbConn.QueryRowContext(ctx, `INSERT INTO music_tracks (library_id, title, path, duration) VALUES (?, ?, ?, 0) RETURNING id`, libraryID, mItem.Title, mItem.Path).Scan(&refID)
			} else {
				err = dbConn.QueryRowContext(ctx, `INSERT INTO `+table+` (library_id, title, path, duration, tmdb_id, overview, poster_path, backdrop_path, release_date, vote_average) VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?, ?) RETURNING id`,
					libraryID, mItem.Title, mItem.Path, mItem.TMDBID, nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage)).Scan(&refID)
			}
			if err != nil {
				return err
			}
			err = dbConn.QueryRowContext(ctx, `INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, kind, refID).Scan(&globalID)
			if err != nil {
				return err
			}
			added++
		} else {
			globalID = existingGlobalID
			if tmdbID == 0 && table != "music_tracks" {
				_, _ = dbConn.ExecContext(ctx, `UPDATE `+table+` SET title = ?, tmdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ? WHERE id = ?`,
					mItem.Title, mItem.TMDBID, nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), refID)
			}
		}

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

		return nil
	})
	if err != nil {
		return added, err
	}
	return added, nil
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
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
