package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"plum/internal/metadata"
)

// SkipFFprobeInScan is set by tests to skip ffprobe during scan (avoids blocking on fake files).
var SkipFFprobeInScan bool

var (
	showKeyNonAlnumRegexp = regexp.MustCompile(`[^a-z0-9]+`)
)

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

type ScanProgress struct {
	Processed int
	Result    ScanResult
}

type ScanOptions struct {
	Identifier             metadata.Identifier
	MusicIdentifier        metadata.MusicIdentifier
	ProbeMedia             bool
	ProbeEmbeddedSubtitles bool
	ScanSidecarSubtitles   bool
	Subpaths               []string
	Progress               func(ScanProgress)
}

var (
	videoExtensions = map[string]struct{}{
		".mp4": {}, ".mkv": {}, ".mov": {}, ".avi": {}, ".webm": {}, ".ts": {}, ".m4v": {},
	}
	audioExtensions = map[string]struct{}{
		".mp3": {}, ".flac": {}, ".m4a": {}, ".aac": {}, ".ogg": {}, ".opus": {}, ".wav": {}, ".alac": {},
	}
	readAudioMetadata = metadata.ReadAudioMetadata
	computeMediaHash  = computeFileHash
)

type Subtitle struct {
	ID       int    `json:"id"`
	MediaID  int    `json:"-"`
	Title    string `json:"title"`
	Language string `json:"language"`
	Format   string `json:"format"`
	Path     string `json:"-"`
}

type EmbeddedSubtitle struct {
	MediaID     int    `json:"-"`
	StreamIndex int    `json:"streamIndex"`
	Language    string `json:"language"`
	Title       string `json:"title"`
}

type EmbeddedAudioTrack struct {
	MediaID     int    `json:"-"`
	StreamIndex int    `json:"streamIndex"`
	Language    string `json:"language"`
	Title       string `json:"title"`
}

type MediaItem struct {
	ID                        int                  `json:"id"`
	LibraryID                 int                  `json:"library_id"`
	Title                     string               `json:"title"`
	Path                      string               `json:"path"`
	Duration                  int                  `json:"duration"`
	Type                      string               `json:"type"`
	MatchStatus               string               `json:"match_status,omitempty"`
	IdentifyState             string               `json:"identify_state,omitempty"`
	Subtitles                 []Subtitle           `json:"subtitles"`
	EmbeddedSubtitles         []EmbeddedSubtitle   `json:"embeddedSubtitles"`
	EmbeddedAudioTracks       []EmbeddedAudioTrack `json:"embeddedAudioTracks"`
	TMDBID                    int                  `json:"tmdb_id"`
	TVDBID                    string               `json:"tvdb_id,omitempty"`
	Overview                  string               `json:"overview"`
	PosterPath                string               `json:"poster_path"`
	BackdropPath              string               `json:"backdrop_path"`
	ReleaseDate               string               `json:"release_date"`
	VoteAverage               float64              `json:"vote_average"`
	IMDbID                    string               `json:"imdb_id,omitempty"`
	IMDbRating                float64              `json:"imdb_rating,omitempty"`
	Artist                    string               `json:"artist,omitempty"`
	Album                     string               `json:"album,omitempty"`
	AlbumArtist               string               `json:"album_artist,omitempty"`
	DiscNumber                int                  `json:"disc_number,omitempty"`
	TrackNumber               int                  `json:"track_number,omitempty"`
	ReleaseYear               int                  `json:"release_year,omitempty"`
	MusicBrainzArtistID       string               `json:"-"`
	MusicBrainzReleaseGroupID string               `json:"-"`
	MusicBrainzReleaseID      string               `json:"-"`
	MusicBrainzRecordingID    string               `json:"-"`
	ProgressSeconds           float64              `json:"progress_seconds,omitempty"`
	ProgressPercent           float64              `json:"progress_percent,omitempty"`
	RemainingSeconds          float64              `json:"remaining_seconds,omitempty"`
	Completed                 bool                 `json:"completed,omitempty"`
	LastWatchedAt             string               `json:"last_watched_at,omitempty"`
	// Season and Episode are set for tv/anime episodes; 0 when not applicable.
	Season  int `json:"season,omitempty"`
	Episode int `json:"episode,omitempty"`
	// MetadataReviewNeeded marks an auto-picked episodic match that still needs user confirmation.
	MetadataReviewNeeded bool `json:"metadata_review_needed,omitempty"`
	// MetadataConfirmed marks episodic metadata that the user explicitly accepted.
	MetadataConfirmed bool `json:"metadata_confirmed,omitempty"`
	// ThumbnailPath is set for video items when a frame thumbnail has been generated (e.g. episode still).
	ThumbnailPath  string `json:"thumbnail_path,omitempty"`
	Missing        bool   `json:"missing,omitempty"`
	MissingSince   string `json:"missing_since,omitempty"`
	Duplicate      bool   `json:"duplicate,omitempty"`
	DuplicateCount int    `json:"duplicate_count,omitempty"`

	FileSizeBytes int64  `json:"-"`
	FileModTime   string `json:"-"`
	FileHash      string `json:"-"`
	FileHashKind  string `json:"-"`
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
	ID                        int       `json:"id"`
	UserID                    int       `json:"user_id"`
	Name                      string    `json:"name"`
	Type                      string    `json:"type"`
	Path                      string    `json:"path"`
	PreferredAudioLanguage    string    `json:"preferred_audio_language,omitempty"`
	PreferredSubtitleLanguage string    `json:"preferred_subtitle_language,omitempty"`
	SubtitlesEnabledByDefault bool      `json:"subtitles_enabled_by_default,omitempty"`
	CreatedAt                 time.Time `json:"created_at"`
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
  preferred_audio_language TEXT,
  preferred_subtitle_language TEXT,
  subtitles_enabled_by_default INTEGER,
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
  file_size_bytes INTEGER NOT NULL DEFAULT 0,
  file_mod_time TEXT,
  file_hash TEXT,
  file_hash_kind TEXT,
  last_seen_at TEXT,
  missing_since TEXT,
  match_status TEXT NOT NULL DEFAULT 'local',
  tmdb_id INTEGER,
  tvdb_id TEXT,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0,
  imdb_id TEXT,
  imdb_rating REAL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_movies_library_id ON movies(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_movies_library_path ON movies(library_id, path);

CREATE TABLE IF NOT EXISTS tv_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  file_size_bytes INTEGER NOT NULL DEFAULT 0,
  file_mod_time TEXT,
  file_hash TEXT,
  file_hash_kind TEXT,
  last_seen_at TEXT,
  missing_since TEXT,
  match_status TEXT NOT NULL DEFAULT 'local',
  tmdb_id INTEGER,
  tvdb_id TEXT,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0,
  imdb_id TEXT,
  imdb_rating REAL DEFAULT 0,
  metadata_review_needed INTEGER NOT NULL DEFAULT 0,
  metadata_confirmed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tv_episodes_library_id ON tv_episodes(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tv_episodes_library_path ON tv_episodes(library_id, path);

CREATE TABLE IF NOT EXISTS anime_episodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  file_size_bytes INTEGER NOT NULL DEFAULT 0,
  file_mod_time TEXT,
  file_hash TEXT,
  file_hash_kind TEXT,
  last_seen_at TEXT,
  missing_since TEXT,
  match_status TEXT NOT NULL DEFAULT 'local',
  tmdb_id INTEGER,
  tvdb_id TEXT,
  overview TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  release_date TEXT,
  vote_average REAL DEFAULT 0,
  imdb_id TEXT,
  imdb_rating REAL DEFAULT 0,
  metadata_review_needed INTEGER NOT NULL DEFAULT 0,
  metadata_confirmed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_anime_episodes_library_id ON anime_episodes(library_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_anime_episodes_library_path ON anime_episodes(library_id, path);

CREATE TABLE IF NOT EXISTS music_tracks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  file_size_bytes INTEGER NOT NULL DEFAULT 0,
  file_mod_time TEXT,
  file_hash TEXT,
  file_hash_kind TEXT,
  last_seen_at TEXT,
  missing_since TEXT,
  match_status TEXT NOT NULL DEFAULT 'local',
  artist TEXT,
  album TEXT,
  album_artist TEXT,
  poster_path TEXT,
  musicbrainz_artist_id TEXT,
  musicbrainz_release_group_id TEXT,
  musicbrainz_release_id TEXT,
  musicbrainz_recording_id TEXT,
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
CREATE UNIQUE INDEX IF NOT EXISTS idx_subtitles_path ON subtitles(path);

CREATE TABLE IF NOT EXISTS embedded_subtitles (
  media_id INTEGER NOT NULL,
  stream_index INTEGER NOT NULL,
  language TEXT NOT NULL,
  title TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_embedded_subtitles_media_id ON embedded_subtitles(media_id);

CREATE TABLE IF NOT EXISTS embedded_audio_tracks (
  media_id INTEGER NOT NULL,
  stream_index INTEGER NOT NULL,
  language TEXT NOT NULL,
  title TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_embedded_audio_tracks_media_id ON embedded_audio_tracks(media_id);

CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS imdb_ratings (
  imdb_id TEXT PRIMARY KEY,
  rating REAL NOT NULL DEFAULT 0,
  votes INTEGER NOT NULL DEFAULT 0,
  updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_imdb_ratings_votes ON imdb_ratings(votes DESC);

CREATE TABLE IF NOT EXISTS library_job_status (
  library_id INTEGER PRIMARY KEY REFERENCES libraries(id) ON DELETE CASCADE,
  phase TEXT NOT NULL,
  enriching INTEGER NOT NULL DEFAULT 0,
  identify_phase TEXT NOT NULL DEFAULT 'idle',
  identified INTEGER NOT NULL DEFAULT 0,
  identify_failed INTEGER NOT NULL DEFAULT 0,
  processed INTEGER NOT NULL DEFAULT 0,
  added INTEGER NOT NULL DEFAULT 0,
  updated INTEGER NOT NULL DEFAULT 0,
  removed INTEGER NOT NULL DEFAULT 0,
  unmatched INTEGER NOT NULL DEFAULT 0,
  skipped INTEGER NOT NULL DEFAULT 0,
  identify_requested INTEGER NOT NULL DEFAULT 0,
  queued_at DATETIME,
  estimated_items INTEGER NOT NULL DEFAULT 0,
  error TEXT,
  started_at DATETIME,
  finished_at DATETIME,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS playback_progress (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  media_id INTEGER NOT NULL REFERENCES media_global(id) ON DELETE CASCADE,
  position_seconds REAL NOT NULL DEFAULT 0,
  duration_seconds REAL NOT NULL DEFAULT 0,
  progress_percent REAL NOT NULL DEFAULT 0,
  completed INTEGER NOT NULL DEFAULT 0,
  last_watched_at DATETIME,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY (user_id, media_id)
);
CREATE INDEX IF NOT EXISTS idx_playback_progress_user_last_watched ON playback_progress(user_id, last_watched_at DESC);

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at DATETIME NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	return applySchemaMigrations(context.Background(), db)
}

type schemaMigration struct {
	version int
	name    string
	apply   func(context.Context, *sql.Tx) error
}

var schemaMigrations = []schemaMigration{
	{
		version: 1,
		name:    "category_tvdb_id",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"movies", "tv_episodes", "anime_episodes"} {
				if err := addColumnIfMissingTx(ctx, tx, table, "tvdb_id", "TEXT"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 2,
		name:    "category_imdb_fields",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"movies", "tv_episodes", "anime_episodes"} {
				if err := addColumnIfMissingTx(ctx, tx, table, "imdb_id", "TEXT"); err != nil {
					return err
				}
				if err := addColumnIfMissingTx(ctx, tx, table, "imdb_rating", "REAL DEFAULT 0"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 3,
		name:    "match_status",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"movies", "tv_episodes", "anime_episodes", "music_tracks"} {
				if err := addColumnIfMissingTx(ctx, tx, table, "match_status", "TEXT NOT NULL DEFAULT 'local'"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 4,
		name:    "episode_numbers",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"tv_episodes", "anime_episodes"} {
				if err := addColumnIfMissingTx(ctx, tx, table, "season", "INTEGER"); err != nil {
					return err
				}
				if err := addColumnIfMissingTx(ctx, tx, table, "episode", "INTEGER"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 5,
		name:    "thumbnail_path",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"tv_episodes", "anime_episodes"} {
				if err := addColumnIfMissingTx(ctx, tx, table, "thumbnail_path", "TEXT"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 6,
		name:    "library_playback_preferences",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, column := range []struct {
				name string
				def  string
			}{
				{name: "preferred_audio_language", def: "TEXT"},
				{name: "preferred_subtitle_language", def: "TEXT"},
				{name: "subtitles_enabled_by_default", def: "INTEGER"},
			} {
				if err := addColumnIfMissingTx(ctx, tx, "libraries", column.name, column.def); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 7,
		name:    "music_metadata_columns",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, column := range []struct {
				name string
				def  string
			}{
				{name: "artist", def: "TEXT"},
				{name: "album", def: "TEXT"},
				{name: "album_artist", def: "TEXT"},
				{name: "disc_number", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "track_number", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "release_year", def: "INTEGER NOT NULL DEFAULT 0"},
			} {
				if err := addColumnIfMissingTx(ctx, tx, "music_tracks", column.name, column.def); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 8,
		name:    "library_job_status_columns",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, column := range []struct {
				name string
				def  string
			}{
				{name: "enriching", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "identify_phase", def: "TEXT NOT NULL DEFAULT 'idle'"},
				{name: "identified", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "identify_failed", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "processed", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "added", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "updated", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "removed", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "unmatched", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "skipped", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "identify_requested", def: "INTEGER NOT NULL DEFAULT 0"},
				{name: "error", def: "TEXT"},
				{name: "started_at", def: "DATETIME"},
				{name: "finished_at", def: "DATETIME"},
				{name: "updated_at", def: "DATETIME"},
			} {
				if err := addColumnIfMissingTx(ctx, tx, "library_job_status", column.name, column.def); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 9,
		name:    "episode_metadata_review_needed",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"tv_episodes", "anime_episodes"} {
				if err := addColumnIfMissingTx(ctx, tx, table, "metadata_review_needed", "INTEGER NOT NULL DEFAULT 0"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 10,
		name:    "scan_queue_indexes",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, column := range []struct {
				name string
				def  string
			}{
				{name: "queued_at", def: "DATETIME"},
				{name: "estimated_items", def: "INTEGER NOT NULL DEFAULT 0"},
			} {
				if err := addColumnIfMissingTx(ctx, tx, "library_job_status", column.name, column.def); err != nil {
					return err
				}
			}
			for _, stmt := range []string{
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_subtitles_path ON subtitles(path)`,
				`CREATE INDEX IF NOT EXISTS idx_library_job_status_phase_updated_at ON library_job_status(phase, updated_at DESC)`,
				`CREATE INDEX IF NOT EXISTS idx_movies_library_match_status ON movies(library_id, match_status)`,
				`CREATE INDEX IF NOT EXISTS idx_tv_episodes_library_match_status ON tv_episodes(library_id, match_status)`,
				`CREATE INDEX IF NOT EXISTS idx_anime_episodes_library_match_status ON anime_episodes(library_id, match_status)`,
			} {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 11,
		name:    "episode_metadata_confirmed",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"tv_episodes", "anime_episodes"} {
				if err := addColumnIfMissingTx(ctx, tx, table, "metadata_confirmed", "INTEGER NOT NULL DEFAULT 0"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 12,
		name:    "music_provider_metadata",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			for _, column := range []struct {
				name string
				def  string
			}{
				{name: "poster_path", def: "TEXT"},
				{name: "musicbrainz_artist_id", def: "TEXT"},
				{name: "musicbrainz_release_group_id", def: "TEXT"},
				{name: "musicbrainz_release_id", def: "TEXT"},
				{name: "musicbrainz_recording_id", def: "TEXT"},
			} {
				if err := addColumnIfMissingTx(ctx, tx, "music_tracks", column.name, column.def); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 13,
		name:    "media_file_state",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			tables := []string{"movies", "tv_episodes", "anime_episodes", "music_tracks"}
			for _, table := range tables {
				for _, column := range []struct {
					name string
					def  string
				}{
					{name: "file_size_bytes", def: "INTEGER NOT NULL DEFAULT 0"},
					{name: "file_mod_time", def: "TEXT"},
					{name: "file_hash", def: "TEXT"},
					{name: "file_hash_kind", def: "TEXT"},
					{name: "last_seen_at", def: "TEXT"},
					{name: "missing_since", def: "TEXT"},
				} {
					if err := addColumnIfMissingTx(ctx, tx, table, column.name, column.def); err != nil {
						return err
					}
				}
			}
			for _, stmt := range []string{
				`CREATE INDEX IF NOT EXISTS idx_movies_library_missing_since ON movies(library_id, missing_since)`,
				`CREATE INDEX IF NOT EXISTS idx_tv_episodes_library_missing_since ON tv_episodes(library_id, missing_since)`,
				`CREATE INDEX IF NOT EXISTS idx_anime_episodes_library_missing_since ON anime_episodes(library_id, missing_since)`,
				`CREATE INDEX IF NOT EXISTS idx_music_tracks_library_missing_since ON music_tracks(library_id, missing_since)`,
				`CREATE INDEX IF NOT EXISTS idx_movies_library_file_hash ON movies(library_id, file_hash) WHERE file_hash IS NOT NULL AND file_hash != ''`,
				`CREATE INDEX IF NOT EXISTS idx_tv_episodes_library_file_hash ON tv_episodes(library_id, file_hash) WHERE file_hash IS NOT NULL AND file_hash != ''`,
				`CREATE INDEX IF NOT EXISTS idx_anime_episodes_library_file_hash ON anime_episodes(library_id, file_hash) WHERE file_hash IS NOT NULL AND file_hash != ''`,
				`CREATE INDEX IF NOT EXISTS idx_music_tracks_library_file_hash ON music_tracks(library_id, file_hash) WHERE file_hash IS NOT NULL AND file_hash != ''`,
			} {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
}

func applySchemaMigrations(ctx context.Context, db *sql.DB) error {
	applied, err := listAppliedSchemaMigrations(db)
	if err != nil {
		return err
	}

	for _, migration := range schemaMigrations {
		if _, ok := applied[migration.version]; ok {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := migration.apply(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply schema migration %d (%s): %w", migration.version, migration.name, err)
		}
		if err := recordSchemaMigrationTx(ctx, tx, migration); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func listAppliedSchemaMigrations(db *sql.DB) (map[int]struct{}, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]struct{})
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = struct{}{}
	}
	return applied, rows.Err()
}

func recordSchemaMigrationTx(ctx context.Context, tx *sql.Tx, migration schemaMigration) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		migration.version,
		migration.name,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func addColumnIfMissingTx(ctx context.Context, tx *sql.Tx, table, column, definition string) error {
	exists, err := columnExistsTx(ctx, tx, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func columnExistsTx(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
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
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func GetAllMediaForUser(db *sql.DB, userID int) ([]MediaItem, error) {
	items, err := queryAllMediaByKind(db, userID, "")
	if err != nil {
		return nil, err
	}
	return attachSubtitlesBatch(db, items)
}

// queryAllMediaByKind returns media from category tables joined with media_global.
// If kind is "", queries all four categories and merges; otherwise only that kind.
// If userID > 0, filters media to only those in libraries owned by that user.
func queryAllMediaByKind(db *sql.DB, userID int, kind string) ([]MediaItem, error) {
	kinds := []string{"movie", "tv", "anime", "music"}
	if kind != "" {
		kinds = []string{kind}
	}
	var items []MediaItem
	for _, k := range kinds {
		table := mediaTableForKind(k)
		var q string
		var args []interface{}
		args = append(args, k)

		if table == "music_tracks" {
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.artist, m.album, m.album_artist, m.poster_path, COALESCE(m.disc_number, 0), COALESCE(m.track_number, 0), COALESCE(m.release_year, 0) FROM music_tracks m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id `
		} else if table == "tv_episodes" || table == "anime_episodes" {
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, m.imdb_id, m.imdb_rating, COALESCE(m.season, 0), COALESCE(m.episode, 0), COALESCE(m.metadata_review_needed, 0), COALESCE(m.metadata_confirmed, 0), m.thumbnail_path FROM ` + table + ` m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id `
		} else {
			q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, m.imdb_id, m.imdb_rating FROM ` + table + ` m JOIN media_global g ON g.kind = ? AND g.ref_id = m.id `
		}

		if userID > 0 {
			q += ` JOIN libraries l ON l.id = m.library_id AND l.user_id = ? `
			args = append(args, userID)
		}

		q += ` ORDER BY g.id`

		rows, err := db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m MediaItem
			m.Type = k
			var overview, posterPath, backdropPath, releaseDate, thumbnailPath sql.NullString
			var matchStatus, imdbID sql.NullString
			var voteAvg, imdbRating sql.NullFloat64
			var tmdbID sql.NullInt64
			var tvdbID sql.NullString
			var metadataReviewNeeded sql.NullBool
			var metadataConfirmed sql.NullBool
			var artist, album, albumArtist sql.NullString
			var musicPosterPath sql.NullString
			if table == "music_tracks" {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &m.FileSizeBytes, &m.FileModTime, &m.FileHash, &m.FileHashKind, &m.MissingSince, &matchStatus, &artist, &album, &albumArtist, &musicPosterPath, &m.DiscNumber, &m.TrackNumber, &m.ReleaseYear)
				if artist.Valid {
					m.Artist = artist.String
				}
				if album.Valid {
					m.Album = album.String
				}
				if albumArtist.Valid {
					m.AlbumArtist = albumArtist.String
				}
				if musicPosterPath.Valid {
					m.PosterPath = musicPosterPath.String
				}
			} else if table == "tv_episodes" || table == "anime_episodes" {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &m.FileSizeBytes, &m.FileModTime, &m.FileHash, &m.FileHashKind, &m.MissingSince, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &imdbID, &imdbRating, &m.Season, &m.Episode, &metadataReviewNeeded, &metadataConfirmed, &thumbnailPath)
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
				if imdbID.Valid {
					m.IMDbID = imdbID.String
				}
				if imdbRating.Valid {
					m.IMDbRating = imdbRating.Float64
				}
				if metadataReviewNeeded.Valid {
					m.MetadataReviewNeeded = metadataReviewNeeded.Bool
				}
				if metadataConfirmed.Valid {
					m.MetadataConfirmed = metadataConfirmed.Bool
				}
				if thumbnailPath.Valid {
					m.ThumbnailPath = thumbnailPath.String
				}
			} else {
				err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &m.FileSizeBytes, &m.FileModTime, &m.FileHash, &m.FileHashKind, &m.MissingSince, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &imdbID, &imdbRating)
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
				if imdbID.Valid {
					m.IMDbID = imdbID.String
				}
				if imdbRating.Valid {
					m.IMDbRating = imdbRating.Float64
				}
			}
			if matchStatus.Valid {
				m.MatchStatus = matchStatus.String
			}
			m.Missing = m.MissingSince != ""
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
	return attachDuplicateState(db, items)
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

// ListIdentifiableByLibrary returns non-music media rows that still need identification
// or metadata repair (for example, missing TMDB IDs or poster art).
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
WHERE m.library_id = ?
  AND COALESCE(m.metadata_confirmed, 0) = 0
  AND (
    COALESCE(m.match_status, '') != ? OR
    COALESCE(m.tmdb_id, 0) = 0 OR
    COALESCE(m.poster_path, '') = '' OR
    COALESCE(m.imdb_id, '') = ''
  )`
		args = []interface{}{libraryID, MatchStatusIdentified}
	} else {
		q = `SELECT m.id, m.title, m.path FROM ` + table + ` m
WHERE m.library_id = ?
  AND (
    COALESCE(m.match_status, '') != ? OR
    COALESCE(m.tmdb_id, 0) = 0 OR
    COALESCE(m.poster_path, '') = '' OR
    COALESCE(m.imdb_id, '') = ''
  )`
		args = []interface{}{libraryID, MatchStatusIdentified}
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
func UpdateMediaMetadata(db *sql.DB, table string, refID int, title string, overview, posterPath, backdropPath, releaseDate string, voteAvg float64, imdbID string, imdbRating float64, tmdbID int, tvdbID string, season, episode int) error {
	return UpdateMediaMetadataWithState(db, table, refID, title, overview, posterPath, backdropPath, releaseDate, voteAvg, imdbID, imdbRating, tmdbID, tvdbID, season, episode, false, false)
}

// UpdateMediaMetadataWithReview updates a single category row with identified metadata and review state.
func UpdateMediaMetadataWithReview(db *sql.DB, table string, refID int, title string, overview, posterPath, backdropPath, releaseDate string, voteAvg float64, imdbID string, imdbRating float64, tmdbID int, tvdbID string, season, episode int, metadataReviewNeeded bool) error {
	return UpdateMediaMetadataWithState(db, table, refID, title, overview, posterPath, backdropPath, releaseDate, voteAvg, imdbID, imdbRating, tmdbID, tvdbID, season, episode, metadataReviewNeeded, false)
}

// UpdateMediaMetadataWithState updates a single category row with identified metadata and episodic metadata state.
func UpdateMediaMetadataWithState(db *sql.DB, table string, refID int, title string, overview, posterPath, backdropPath, releaseDate string, voteAvg float64, imdbID string, imdbRating float64, tmdbID int, tvdbID string, season, episode int, metadataReviewNeeded bool, metadataConfirmed bool) error {
	if table == "tv_episodes" || table == "anime_episodes" {
		_, err := db.Exec(`UPDATE `+table+` SET title = ?, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ?, imdb_id = ?, imdb_rating = ?, season = ?, episode = ?, metadata_review_needed = ?, metadata_confirmed = ? WHERE id = ?`,
			title, MatchStatusIdentified, tmdbID, nullStr(tvdbID), nullStr(overview), nullStr(posterPath), nullStr(backdropPath), nullStr(releaseDate), nullFloat64(voteAvg), nullStr(imdbID), nullFloat64(imdbRating), season, episode, metadataReviewNeeded, metadataConfirmed, refID)
		return err
	}
	_, err := db.Exec(`UPDATE `+table+` SET title = ?, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ?, imdb_id = ?, imdb_rating = ? WHERE id = ?`,
		title, MatchStatusIdentified, tmdbID, nullStr(tvdbID), nullStr(overview), nullStr(posterPath), nullStr(backdropPath), nullStr(releaseDate), nullFloat64(voteAvg), nullStr(imdbID), nullFloat64(imdbRating), refID)
	return err
}

// UpdateShowMetadataState sets episodic metadata flags for a batch of rows.
func UpdateShowMetadataState(db *sql.DB, table string, refIDs []int, metadataReviewNeeded bool, metadataConfirmed bool) (int, error) {
	if (table != "tv_episodes" && table != "anime_episodes") || len(refIDs) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(refIDs))
	args := make([]interface{}, 0, len(refIDs)+2)
	args = append(args, metadataReviewNeeded, metadataConfirmed)
	for i, refID := range refIDs {
		placeholders[i] = "?"
		args = append(args, refID)
	}
	result, err := db.Exec(`UPDATE `+table+` SET metadata_review_needed = ?, metadata_confirmed = ? WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rowsAffected), nil
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

func normalizeShowKeyTitle(title string) string {
	title = showNameFromTitle(title)
	title = strings.ToLower(title)
	title = showKeyNonAlnumRegexp.ReplaceAllString(title, "")
	return title
}

func showNameFromTitle(title string) string {
	// "Show Name - S01E02 - Episode" -> "Show Name"
	if i := strings.Index(strings.ToLower(title), " - s"); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	if i := strings.Index(title, " - "); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	return strings.TrimSpace(title)
}

// showKeyFromItem returns the same key the frontend uses: "tmdb-{id}" when tmdb_id set, else "title-{normalizedTitle}".
func showKeyFromItem(tmdbID int, title string) string {
	if tmdbID > 0 {
		return fmt.Sprintf("tmdb-%d", tmdbID)
	}
	return "title-" + normalizeShowKeyTitle(title)
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

// attachSubtitlesBatch loads subtitle and embedded stream metadata for all items in batch queries.
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
	audioByID, err := getEmbeddedAudioTracksByMediaIDs(db, ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if subs := subsByID[items[i].ID]; subs != nil {
			items[i].Subtitles = subs
		} else {
			items[i].Subtitles = []Subtitle{}
		}
		if embedded := embByID[items[i].ID]; embedded != nil {
			items[i].EmbeddedSubtitles = embedded
		} else {
			items[i].EmbeddedSubtitles = []EmbeddedSubtitle{}
		}
		if audioTracks := audioByID[items[i].ID]; audioTracks != nil {
			items[i].EmbeddedAudioTracks = audioTracks
		} else {
			items[i].EmbeddedAudioTracks = []EmbeddedAudioTrack{}
		}
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

func getEmbeddedAudioTracksByMediaIDs(db *sql.DB, mediaIDs []int) (map[int][]EmbeddedAudioTrack, error) {
	if len(mediaIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(mediaIDs))
	args := make([]interface{}, len(mediaIDs))
	for i := range mediaIDs {
		placeholders[i] = "?"
		args[i] = mediaIDs[i]
	}
	query := `SELECT media_id, stream_index, language, title FROM embedded_audio_tracks WHERE media_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY media_id, stream_index`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int][]EmbeddedAudioTrack)
	for rows.Next() {
		var track EmbeddedAudioTrack
		if err := rows.Scan(&track.MediaID, &track.StreamIndex, &track.Language, &track.Title); err != nil {
			return nil, err
		}
		out[track.MediaID] = append(out[track.MediaID], track)
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
	var metadataReviewNeeded sql.NullBool
	var metadataConfirmed sql.NullBool
	var overview, posterPath, backdropPath, releaseDate, thumbnailPath, matchStatus, imdbID sql.NullString
	var voteAvg, imdbRating sql.NullFloat64
	var tmdbID sql.NullInt64
	var tvdbID sql.NullString
	var artist, album, albumArtist sql.NullString
	var musicPosterPath sql.NullString
	var discNumber, trackNumber, releaseYear int
	var fileSizeBytes int64
	var fileModTime, fileHash, fileHashKind, missingSince string
	if table == "music_tracks" {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.artist, m.album, m.album_artist, m.poster_path, COALESCE(m.disc_number, 0), COALESCE(m.track_number, 0), COALESCE(m.release_year, 0) FROM music_tracks m WHERE m.id = ?`, refID).
			Scan(&refID, &libID, &title, &path, &duration, &fileSizeBytes, &fileModTime, &fileHash, &fileHashKind, &missingSince, &matchStatus, &artist, &album, &albumArtist, &musicPosterPath, &discNumber, &trackNumber, &releaseYear)
	} else if table == "tv_episodes" || table == "anime_episodes" {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, m.imdb_id, m.imdb_rating, COALESCE(m.season, 0), COALESCE(m.episode, 0), COALESCE(m.metadata_review_needed, 0), COALESCE(m.metadata_confirmed, 0), m.thumbnail_path FROM `+table+` m WHERE m.id = ?`, refID).
			Scan(&refID, &libID, &title, &path, &duration, &fileSizeBytes, &fileModTime, &fileHash, &fileHashKind, &missingSince, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &imdbID, &imdbRating, &season, &episode, &metadataReviewNeeded, &metadataConfirmed, &thumbnailPath)
	} else {
		err = db.QueryRow(`SELECT m.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, m.imdb_id, m.imdb_rating FROM `+table+` m WHERE m.id = ?`, refID).
			Scan(&refID, &libID, &title, &path, &duration, &fileSizeBytes, &fileModTime, &fileHash, &fileHashKind, &missingSince, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &imdbID, &imdbRating)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m := MediaItem{
		ID:            id,
		LibraryID:     libID,
		Title:         title,
		Path:          path,
		Duration:      duration,
		Type:          kind,
		FileSizeBytes: fileSizeBytes,
		FileModTime:   fileModTime,
		FileHash:      fileHash,
		FileHashKind:  fileHashKind,
		MissingSince:  missingSince,
	}
	if matchStatus.Valid {
		m.MatchStatus = matchStatus.String
	}
	m.Missing = m.MissingSince != ""
	if table == "tv_episodes" || table == "anime_episodes" {
		m.Season = season
		m.Episode = episode
		if metadataReviewNeeded.Valid {
			m.MetadataReviewNeeded = metadataReviewNeeded.Bool
		}
		if metadataConfirmed.Valid {
			m.MetadataConfirmed = metadataConfirmed.Bool
		}
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
		if musicPosterPath.Valid {
			m.PosterPath = musicPosterPath.String
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
	if imdbID.Valid {
		m.IMDbID = imdbID.String
	}
	if imdbRating.Valid {
		m.IMDbRating = imdbRating.Float64
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
	if subs != nil {
		m.Subtitles = subs
	} else {
		m.Subtitles = []Subtitle{}
	}
	emb, err := getEmbeddedSubtitlesForMedia(db, id)
	if err != nil {
		return nil, err
	}
	if emb != nil {
		m.EmbeddedSubtitles = emb
	} else {
		m.EmbeddedSubtitles = []EmbeddedSubtitle{}
	}
	audioTracks, err := getEmbeddedAudioTracksForMedia(db, id)
	if err != nil {
		return nil, err
	}
	if audioTracks != nil {
		m.EmbeddedAudioTracks = audioTracks
	} else {
		m.EmbeddedAudioTracks = []EmbeddedAudioTrack{}
	}
	if err := attachSingleDuplicateState(db, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// GetMediaByLibraryID returns all media for a library (one category table only), no N+1.
func GetMediaByLibraryID(db *sql.DB, libraryID int) ([]MediaItem, error) {
	var typ string
	err := db.QueryRow(`SELECT type FROM libraries WHERE id = ?`, libraryID).Scan(&typ)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []MediaItem{}, nil
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
	q := `SELECT g.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, m.imdb_id, m.imdb_rating
FROM ` + table + ` m
JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ? AND COALESCE(m.missing_since, '') = ''
ORDER BY g.id`
	if table == "music_tracks" {
		q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.artist, m.album, m.album_artist, m.poster_path, COALESCE(m.disc_number, 0), COALESCE(m.track_number, 0), COALESCE(m.release_year, 0)
FROM music_tracks m
JOIN media_global g ON g.kind = 'music' AND g.ref_id = m.id
WHERE m.library_id = ? AND COALESCE(m.missing_since, '') = ''
ORDER BY g.id`
	} else if table == "tv_episodes" || table == "anime_episodes" {
		q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, m.imdb_id, m.imdb_rating, COALESCE(m.season, 0), COALESCE(m.episode, 0), COALESCE(m.metadata_review_needed, 0), COALESCE(m.metadata_confirmed, 0), m.thumbnail_path
FROM ` + table + ` m
JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ? AND COALESCE(m.missing_since, '') = ''
ORDER BY g.id`
	} else {
		q = `SELECT g.id, m.library_id, m.title, m.path, m.duration, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.missing_since, ''), m.match_status, m.tmdb_id, m.tvdb_id, m.overview, m.poster_path, m.backdrop_path, m.release_date, m.vote_average, m.imdb_id, m.imdb_rating
FROM ` + table + ` m
JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ? AND COALESCE(m.missing_since, '') = ''
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
	items := make([]MediaItem, 0)
	for rows.Next() {
		var m MediaItem
		m.Type = kind
		m.LibraryID = libraryID
		var overview, posterPath, backdropPath, releaseDate, thumbnailPath, matchStatus, imdbID sql.NullString
		var voteAvg, imdbRating sql.NullFloat64
		var tmdbID sql.NullInt64
		var tvdbID sql.NullString
		var metadataReviewNeeded sql.NullBool
		var metadataConfirmed sql.NullBool
		var artist, album, albumArtist sql.NullString
		var musicPosterPath sql.NullString
		if table == "music_tracks" {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &m.FileSizeBytes, &m.FileModTime, &m.FileHash, &m.FileHashKind, &m.MissingSince, &matchStatus, &artist, &album, &albumArtist, &musicPosterPath, &m.DiscNumber, &m.TrackNumber, &m.ReleaseYear)
			if artist.Valid {
				m.Artist = artist.String
			}
			if album.Valid {
				m.Album = album.String
			}
			if albumArtist.Valid {
				m.AlbumArtist = albumArtist.String
			}
			if musicPosterPath.Valid {
				m.PosterPath = musicPosterPath.String
			}
		} else if table == "tv_episodes" || table == "anime_episodes" {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &m.FileSizeBytes, &m.FileModTime, &m.FileHash, &m.FileHashKind, &m.MissingSince, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &imdbID, &imdbRating, &m.Season, &m.Episode, &metadataReviewNeeded, &metadataConfirmed, &thumbnailPath)
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
			if imdbID.Valid {
				m.IMDbID = imdbID.String
			}
			if imdbRating.Valid {
				m.IMDbRating = imdbRating.Float64
			}
			if metadataReviewNeeded.Valid {
				m.MetadataReviewNeeded = metadataReviewNeeded.Bool
			}
			if metadataConfirmed.Valid {
				m.MetadataConfirmed = metadataConfirmed.Bool
			}
			if thumbnailPath.Valid {
				m.ThumbnailPath = thumbnailPath.String
			}
		} else {
			err = rows.Scan(&m.ID, &m.LibraryID, &m.Title, &m.Path, &m.Duration, &m.FileSizeBytes, &m.FileModTime, &m.FileHash, &m.FileHashKind, &m.MissingSince, &matchStatus, &tmdbID, &tvdbID, &overview, &posterPath, &backdropPath, &releaseDate, &voteAvg, &imdbID, &imdbRating)
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
			if imdbID.Valid {
				m.IMDbID = imdbID.String
			}
			if imdbRating.Valid {
				m.IMDbRating = imdbRating.Float64
			}
		}
		if matchStatus.Valid {
			m.MatchStatus = matchStatus.String
		}
		m.Missing = m.MissingSince != ""
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attachDuplicateState(db, items)
}

func attachDuplicateState(db *sql.DB, items []MediaItem) ([]MediaItem, error) {
	if len(items) == 0 {
		return items, nil
	}
	for i := range items {
		if err := attachSingleDuplicateState(db, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func attachSingleDuplicateState(db *sql.DB, item *MediaItem) error {
	if item == nil {
		return nil
	}
	if item.LibraryID <= 0 || item.Missing || item.FileHash == "" {
		item.Duplicate = false
		item.DuplicateCount = 0
		return nil
	}
	table := mediaTableForKind(item.Type)
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(1) FROM `+table+` WHERE library_id = ? AND COALESCE(file_hash, '') = ? AND COALESCE(missing_since, '') = ''`,
		item.LibraryID,
		item.FileHash,
	).Scan(&count); err != nil {
		return err
	}
	if count > 1 {
		item.Duplicate = true
		item.DuplicateCount = count
	}
	return nil
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

func getEmbeddedAudioTracksForMedia(db *sql.DB, mediaID int) ([]EmbeddedAudioTrack, error) {
	rows, err := db.Query(`SELECT media_id, stream_index, language, title FROM embedded_audio_tracks WHERE media_id = ? ORDER BY stream_index`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []EmbeddedAudioTrack
	for rows.Next() {
		var track EmbeddedAudioTrack
		if err := rows.Scan(&track.MediaID, &track.StreamIndex, &track.Language, &track.Title); err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
	}
	return tracks, rows.Err()
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

func getMediaDuration(ctx context.Context, path string) (int, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "ffprobe",
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

func probeEmbeddedAudioTracks(ctx context.Context, path string) ([]EmbeddedAudioTrack, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "ffprobe",
		"-v", "error",
		"-select_streams", "a",
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

	var tracks []EmbeddedAudioTrack
	for _, stream := range parsed.Streams {
		lang := stream.Tags.Language
		if lang == "" {
			lang = "und"
		}
		title := stream.Tags.Title
		if title == "" {
			title = lang
		}
		tracks = append(tracks, EmbeddedAudioTrack{
			StreamIndex: stream.Index,
			Language:    lang,
			Title:       title,
		})
	}
	return tracks, nil
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
				lang := "und"
				parts := strings.Split(strings.TrimSuffix(name, ext), ".")
				if len(parts) > 1 {
					lastPart := parts[len(parts)-1]
					if len(lastPart) == 2 || len(lastPart) == 3 {
						lang = lastPart
					}
				}

				_, err := dbConn.ExecContext(ctx,
					`INSERT OR IGNORE INTO subtitles (media_id, title, language, format, path) VALUES (?, ?, ?, ?, ?)`,
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
	var musicIdentifier metadata.MusicIdentifier
	if detected, ok := id.(metadata.MusicIdentifier); ok {
		musicIdentifier = detected
	}
	return HandleScanLibraryWithOptions(ctx, dbConn, root, mediaType, libraryID, ScanOptions{
		Identifier:             id,
		MusicIdentifier:        musicIdentifier,
		ProbeMedia:             true,
		ProbeEmbeddedSubtitles: true,
		ScanSidecarSubtitles:   true,
	})
}

type scanCandidate struct {
	Path    string
	RelPath string
	Name    string
	Size    int64
	ModTime string
}

func EstimateLibraryFiles(ctx context.Context, root, mediaType string) (int, error) {
	count := 0
	err := iterateLibraryFiles(ctx, root, mediaType, nil, func(scanCandidate) error {
		count++
		return nil
	})
	return count, err
}

func iterateLibraryFiles(ctx context.Context, root, mediaType string, onSkip func(), visit func(scanCandidate) error) error {
	if root == "" {
		return fmt.Errorf("path is required")
	}
	if mediaType == "" {
		mediaType = LibraryTypeMovie
	}

	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("path not found: %q — when running in Docker, use the container path (e.g. /tv, /movies, /music), not the host path", root)
		}
		return fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}

	exts := allowedExtensions(mediaType)
	return filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
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
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if shouldSkipScanPath(mediaType, relPath, d.Name()) {
			if onSkip != nil {
				onSkip()
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return visit(scanCandidate{
			Path:    path,
			RelPath: relPath,
			Name:    d.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339Nano),
		})
	})
}

func HandleScanLibraryWithOptions(
	ctx context.Context,
	dbConn *sql.DB,
	root, mediaType string,
	libraryID int,
	options ScanOptions,
) (ScanResult, error) {
	result := ScanResult{}
	if mediaType == "" {
		mediaType = LibraryTypeMovie
	}
	if libraryID <= 0 {
		return result, fmt.Errorf("library id is required")
	}

	kind := mediaType
	table := mediaTableForKind(kind)
	identifier := options.Identifier
	musicIdentifier := options.MusicIdentifier
	probeMedia := options.ProbeMedia
	probeEmbeddedSubtitleStreams := options.ProbeEmbeddedSubtitles && probeMedia
	scanSidecarSubtitles := options.ScanSidecarSubtitles
	scanSubpaths, err := NormalizeScanSubpaths(options.Subpaths)
	if err != nil {
		return result, err
	}
	scanRoots, markRoots, err := resolveScanRoots(root, scanSubpaths)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	existingByPath, err := preloadExistingMediaByPath(dbConn, table, kind, libraryID)
	if err != nil {
		return result, err
	}
	seenPaths := map[string]struct{}{}
	emitProgress := func() {
		if options.Progress != nil {
			options.Progress(ScanProgress{
				Processed: result.Added + result.Updated + result.Skipped,
				Result:    result,
			})
		}
	}
	for _, scanRoot := range scanRoots {
		err = iterateLibraryFiles(ctx, scanRoot, kind, func() {
			result.Skipped++
			emitProgress()
		}, func(candidate scanCandidate) error {
			path := candidate.Path
			if _, ok := seenPaths[path]; ok {
				return nil
			}
			seenPaths[path] = struct{}{}

			relPath, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			candidate.RelPath = relPath

			existing := existingByPath[path]
			isNew := existing.RefID == 0
			isUnchanged := !isNew &&
				existing.MissingSince == "" &&
				existing.FileSizeBytes == candidate.Size &&
				existing.FileModTime == candidate.ModTime &&
				existing.FileHash != "" &&
				existing.FileHashKind != ""

			title := strings.TrimSuffix(candidate.Name, filepath.Ext(candidate.Name))
			if title == "" {
				title = candidate.Name
			}

			mItem := MediaItem{
				Title:         title,
				Path:          path,
				Type:          kind,
				MatchStatus:   MatchStatusLocal,
				FileSizeBytes: candidate.Size,
				FileModTime:   candidate.ModTime,
				FileHash:      existing.FileHash,
				FileHashKind:  existing.FileHashKind,
			}
			var fileInfo metadata.MediaInfo
			var musicInfo metadata.MusicInfo
			switch kind {
			case LibraryTypeMusic:
				pathInfo := metadata.ParsePathForMusic(candidate.RelPath, candidate.Name)
				audioMeta := metadata.MusicMetadata{}
				if probeMedia && !SkipFFprobeInScan {
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
				musicInfo = metadata.MusicInfo{
					Title:       merged.Title,
					Artist:      merged.Artist,
					Album:       merged.Album,
					AlbumArtist: merged.AlbumArtist,
					DiscNumber:  merged.DiscNumber,
					TrackNumber: merged.TrackNumber,
					ReleaseYear: merged.ReleaseYear,
				}
			case LibraryTypeMovie:
				movieInfo := metadata.ParseMovie(candidate.RelPath, candidate.Name)
				mItem.Title = metadata.MovieDisplayTitle(movieInfo, title)
				fileInfo = metadata.MovieMediaInfo(movieInfo)
			case LibraryTypeTV, LibraryTypeAnime:
				fileInfo = metadata.ParseFilename(candidate.Name)
				pathInfo := metadata.ParsePathForTV(candidate.RelPath, candidate.Name)
				merged := metadata.MergePathInfo(pathInfo, fileInfo)
				showRoot := metadata.ShowRootPath(root, path)
				metadata.ApplyShowNFO(&merged, showRoot)
				if kind == LibraryTypeAnime && merged.IsSpecial && merged.Episode > 0 {
					merged.Season = 0
				}
				mItem.Season = merged.Season
				mItem.Episode = merged.Episode
				mItem.Title = buildEpisodeDisplayTitle(pathInfo.ShowName, merged, title, fileInfo.Title)
				fileInfo = merged
			}

			identifyInfo := fileInfo
			hasMetadata := existingHasMetadata(kind, existing)
			forceRefresh := kind != LibraryTypeMusic && hasExplicitProviderID(identifyInfo) && !existing.MetadataConfirmed
			shouldIdentify := identifier != nil &&
				(kind == LibraryTypeTV || kind == LibraryTypeAnime || kind == LibraryTypeMovie) &&
				(!hasMetadata || forceRefresh)
			shouldIdentifyMusic := kind == LibraryTypeMusic && musicIdentifier != nil
			if isUnchanged && !shouldIdentify && !shouldIdentifyMusic {
				if err := markMediaPresent(ctx, dbConn, table, existing.RefID, candidate.Size, candidate.ModTime, existing.FileHash, existing.FileHashKind, now); err != nil {
					return err
				}
				result.Updated++
				emitProgress()
				return nil
			}

			if shouldIdentify {
				mItem.MetadataReviewNeeded = false
				mItem.MetadataConfirmed = false
				switch kind {
				case LibraryTypeTV:
					if res := identifier.IdentifyTV(ctx, identifyInfo); res != nil {
						applyMatchResultToMediaItem(&mItem, res)
						mItem.MatchStatus = MatchStatusIdentified
					} else {
						mItem.MatchStatus = MatchStatusUnmatched
					}
				case LibraryTypeAnime:
					if res := identifier.IdentifyAnime(ctx, identifyInfo); res != nil {
						applyMatchResultToMediaItem(&mItem, res)
						mItem.MatchStatus = MatchStatusIdentified
					} else {
						mItem.MatchStatus = MatchStatusUnmatched
					}
				case LibraryTypeMovie:
					if res := identifier.IdentifyMovie(ctx, identifyInfo); res != nil {
						applyMatchResultToMediaItem(&mItem, res)
						mItem.MatchStatus = MatchStatusIdentified
					} else {
						mItem.MatchStatus = MatchStatusUnmatched
					}
				}
			} else if shouldIdentifyMusic {
				if res := musicIdentifier.IdentifyMusic(ctx, musicInfo); res != nil {
					applyMusicMatchResultToMediaItem(&mItem, res)
					mItem.MatchStatus = MatchStatusIdentified
				} else {
					mItem.MatchStatus = MatchStatusUnmatched
				}
			} else if !isNew {
				applyExistingMetadata(&mItem, existing, kind)
			}
			if (kind == LibraryTypeTV || kind == LibraryTypeAnime) && !shouldIdentify {
				mItem.MetadataReviewNeeded = existing.MetadataReviewNeeded
				mItem.MetadataConfirmed = existing.MetadataConfirmed
			}
			if (shouldIdentify || shouldIdentifyMusic) && mItem.MatchStatus == MatchStatusUnmatched {
				result.Unmatched++
			}

			if hash, err := computeMediaHash(ctx, path); err == nil {
				mItem.FileHash = hash
				mItem.FileHashKind = fileHashKindSHA256
			} else {
				return err
			}

			globalID := existing.GlobalID
			if isNew {
				_, globalID, err = insertScannedItem(ctx, dbConn, table, kind, libraryID, mItem, now)
				if err != nil {
					return err
				}
				result.Added++
			} else {
				if err := updateScannedItem(ctx, dbConn, table, existing.RefID, mItem, now); err != nil {
					return err
				}
				result.Updated++
			}
			emitProgress()

			if kind == LibraryTypeMusic {
				return nil
			}
			if scanSidecarSubtitles {
				if err := scanForSubtitles(ctx, dbConn, globalID, path); err != nil {
					log.Printf("scan subtitles for %s: %v", path, err)
				}
			}

			var embeddedSubs []EmbeddedSubtitle
			if probeEmbeddedSubtitleStreams && !SkipFFprobeInScan {
				embeddedSubs, _ = probeEmbeddedSubtitles(ctx, path)
			}
			if _, err := dbConn.ExecContext(ctx, `DELETE FROM embedded_subtitles WHERE media_id = ?`, globalID); err != nil {
				log.Printf("clear embedded_subtitles for media %d: %v", globalID, err)
			} else {
				for _, s := range embeddedSubs {
					if _, err := dbConn.ExecContext(ctx, `INSERT INTO embedded_subtitles (media_id, stream_index, language, title) VALUES (?, ?, ?, ?)`, globalID, s.StreamIndex, s.Language, s.Title); err != nil {
						log.Printf("insert embedded_subtitles for media %d: %v", globalID, err)
					}
				}
			}

			var embeddedAudioTracks []EmbeddedAudioTrack
			if probeMedia && !SkipFFprobeInScan {
				embeddedAudioTracks, _ = probeEmbeddedAudioTracks(ctx, path)
			}
			if _, err := dbConn.ExecContext(ctx, `DELETE FROM embedded_audio_tracks WHERE media_id = ?`, globalID); err != nil {
				log.Printf("clear embedded_audio_tracks for media %d: %v", globalID, err)
			} else {
				for _, track := range embeddedAudioTracks {
					if _, err := dbConn.ExecContext(ctx, `INSERT INTO embedded_audio_tracks (media_id, stream_index, language, title) VALUES (?, ?, ?, ?)`, globalID, track.StreamIndex, track.Language, track.Title); err != nil {
						log.Printf("insert embedded_audio_tracks for media %d: %v", globalID, err)
					}
				}
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	if err := markMissingMedia(ctx, dbConn, table, kind, libraryID, markRoots, seenPaths, now); err != nil {
		return result, err
	}
	emitProgress()
	return result, nil
}

const fileHashKindSHA256 = "sha256"

func NormalizeScanSubpaths(subpaths []string) ([]string, error) {
	if len(subpaths) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(subpaths))
	for _, subpath := range subpaths {
		subpath = strings.TrimSpace(subpath)
		if subpath == "" || subpath == "." {
			return nil, nil
		}
		clean := filepath.Clean(subpath)
		if clean == "." {
			return nil, nil
		}
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("invalid scan subpath %q", subpath)
		}
		normalized = append(normalized, clean)
	}
	sort.Strings(normalized)
	out := make([]string, 0, len(normalized))
	for _, subpath := range normalized {
		if len(out) > 0 && isSubpath(out[len(out)-1], subpath) {
			continue
		}
		out = append(out, subpath)
	}
	return out, nil
}

func isSubpath(parent, child string) bool {
	if parent == child {
		return true
	}
	return strings.HasPrefix(child, parent+string(os.PathSeparator))
}

func resolveScanRoots(root string, subpaths []string) ([]string, []string, error) {
	if len(subpaths) == 0 {
		return []string{root}, []string{root}, nil
	}
	roots := make([]string, 0, len(subpaths))
	markRoots := make([]string, 0, len(subpaths))
	for _, subpath := range subpaths {
		scanRoot := filepath.Join(root, subpath)
		markRoots = append(markRoots, scanRoot)
		info, err := os.Stat(scanRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, err
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("scan subpath is not a directory: %s", subpath)
		}
		roots = append(roots, scanRoot)
	}
	return roots, markRoots, nil
}

func applyMatchResultToMediaItem(item *MediaItem, res *metadata.MatchResult) {
	if item == nil || res == nil {
		return
	}
	item.Title = res.Title
	item.Overview = res.Overview
	item.PosterPath = res.PosterURL
	item.BackdropPath = res.BackdropURL
	item.ReleaseDate = res.ReleaseDate
	item.VoteAverage = res.VoteAverage
	item.IMDbID = res.IMDbID
	item.IMDbRating = res.IMDbRating
	if res.Provider == "tmdb" {
		if id, err := parseInt(res.ExternalID); err == nil {
			item.TMDBID = id
			item.TVDBID = ""
		}
	} else if res.Provider == "tvdb" {
		item.TVDBID = res.ExternalID
	}
}

func applyMusicMatchResultToMediaItem(item *MediaItem, res *metadata.MusicMatchResult) {
	if item == nil || res == nil {
		return
	}
	if res.Title != "" {
		item.Title = res.Title
	}
	if res.Artist != "" {
		item.Artist = res.Artist
	}
	if res.Album != "" {
		item.Album = res.Album
	}
	if res.AlbumArtist != "" {
		item.AlbumArtist = res.AlbumArtist
	}
	if res.PosterURL != "" {
		item.PosterPath = res.PosterURL
	}
	if res.ReleaseYear > 0 {
		item.ReleaseYear = res.ReleaseYear
	}
	if res.DiscNumber > 0 {
		item.DiscNumber = res.DiscNumber
	}
	if res.TrackNumber > 0 {
		item.TrackNumber = res.TrackNumber
	}
	item.MusicBrainzArtistID = res.ArtistID
	item.MusicBrainzReleaseGroupID = res.ReleaseGroupID
	item.MusicBrainzReleaseID = res.ReleaseID
	item.MusicBrainzRecordingID = res.RecordingID
}

func applyExistingMetadata(item *MediaItem, existing existingMediaRow, kind string) {
	if item == nil {
		return
	}
	item.MatchStatus = existing.MatchStatus
	item.PosterPath = existing.PosterPath
	item.TMDBID = existing.TMDBID
	item.TVDBID = existing.TVDBID
	item.IMDbID = existing.IMDbID
	item.MusicBrainzArtistID = existing.MusicBrainzArtistID
	item.MusicBrainzReleaseGroupID = existing.MusicBrainzReleaseGroupID
	item.MusicBrainzReleaseID = existing.MusicBrainzReleaseID
	item.MusicBrainzRecordingID = existing.MusicBrainzRecordingID
	if kind == LibraryTypeTV || kind == LibraryTypeAnime {
		item.MetadataReviewNeeded = existing.MetadataReviewNeeded
		item.MetadataConfirmed = existing.MetadataConfirmed
	}
}

func computeFileHash(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		n, err := f.Read(buf)
		if n > 0 {
			if _, writeErr := hasher.Write(buf[:n]); writeErr != nil {
				return "", writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func markMediaPresent(ctx context.Context, dbConn *sql.DB, table string, refID int, fileSizeBytes int64, fileModTime, fileHash, fileHashKind, seenAt string) error {
	_, err := dbConn.ExecContext(
		ctx,
		`UPDATE `+table+` SET file_size_bytes = ?, file_mod_time = ?, file_hash = ?, file_hash_kind = ?, last_seen_at = ?, missing_since = NULL WHERE id = ?`,
		fileSizeBytes,
		nullStr(fileModTime),
		nullStr(fileHash),
		nullStr(fileHashKind),
		nullStr(seenAt),
		refID,
	)
	return err
}

func markMissingMedia(ctx context.Context, dbConn *sql.DB, table, kind string, libraryID int, scanRoots []string, seenPaths map[string]struct{}, missingSince string) error {
	rows, err := dbConn.Query(`SELECT id, path FROM `+table+` WHERE library_id = ?`, libraryID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var staleIDs []int
	for rows.Next() {
		var refID int
		var path string
		if err := rows.Scan(&refID, &path); err != nil {
			return err
		}
		if _, ok := seenPaths[path]; ok {
			continue
		}
		if !pathWithinAnyRoot(path, scanRoots) {
			continue
		}
		staleIDs = append(staleIDs, refID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, refID := range staleIDs {
		if _, err := dbConn.ExecContext(ctx, `UPDATE `+table+` SET missing_since = ?, last_seen_at = COALESCE(last_seen_at, '') WHERE id = ?`, missingSince, refID); err != nil {
			return err
		}
	}
	return nil
}

func pathWithinAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

type existingMediaRow struct {
	RefID                     int
	GlobalID                  int
	Path                      string
	FileSizeBytes             int64
	FileModTime               string
	FileHash                  string
	FileHashKind              string
	LastSeenAt                string
	MissingSince              string
	TMDBID                    int
	TVDBID                    string
	IMDbID                    string
	PosterPath                string
	MusicBrainzArtistID       string
	MusicBrainzReleaseGroupID string
	MusicBrainzReleaseID      string
	MusicBrainzRecordingID    string
	MatchStatus               string
	MetadataReviewNeeded      bool
	MetadataConfirmed         bool
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

func preloadExistingMediaByPath(dbConn *sql.DB, table, kind string, libraryID int) (map[string]existingMediaRow, error) {
	query := `SELECT m.path, m.id, COALESCE(g.id, 0), COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.last_seen_at, ''), COALESCE(m.missing_since, ''), COALESCE(m.match_status, 'local') FROM ` + table + ` m
LEFT JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ?`
	if table == "music_tracks" {
		query = `SELECT m.path, m.id, COALESCE(g.id, 0), COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.last_seen_at, ''), COALESCE(m.missing_since, ''), COALESCE(m.match_status, 'local'), COALESCE(m.poster_path, ''), COALESCE(m.musicbrainz_artist_id, ''), COALESCE(m.musicbrainz_release_group_id, ''), COALESCE(m.musicbrainz_release_id, ''), COALESCE(m.musicbrainz_recording_id, '') FROM music_tracks m
LEFT JOIN media_global g ON g.kind = 'music' AND g.ref_id = m.id
WHERE m.library_id = ?`
	}
	if table == "tv_episodes" || table == "anime_episodes" {
		query = `SELECT m.path, m.id, COALESCE(g.id, 0), COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.last_seen_at, ''), COALESCE(m.missing_since, ''), COALESCE(m.tmdb_id, 0), COALESCE(m.tvdb_id, ''), COALESCE(m.imdb_id, ''), COALESCE(m.match_status, 'local'), COALESCE(m.metadata_review_needed, 0), COALESCE(m.metadata_confirmed, 0)
FROM ` + table + ` m
LEFT JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ?`
	} else if table != "music_tracks" {
		query = `SELECT m.path, m.id, COALESCE(g.id, 0), COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.last_seen_at, ''), COALESCE(m.missing_since, ''), COALESCE(m.tmdb_id, 0), COALESCE(m.tvdb_id, ''), COALESCE(m.imdb_id, ''), COALESCE(m.match_status, 'local')
FROM ` + table + ` m
LEFT JOIN media_global g ON g.kind = ? AND g.ref_id = m.id
WHERE m.library_id = ?`
	}

	var (
		rows *sql.Rows
		err  error
	)
	if table == "music_tracks" {
		rows, err = dbConn.Query(query, libraryID)
	} else {
		rows, err = dbConn.Query(query, kind, libraryID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]existingMediaRow)
	for rows.Next() {
		var row existingMediaRow
		if table == "music_tracks" {
			if err := rows.Scan(&row.Path, &row.RefID, &row.GlobalID, &row.FileSizeBytes, &row.FileModTime, &row.FileHash, &row.FileHashKind, &row.LastSeenAt, &row.MissingSince, &row.MatchStatus, &row.PosterPath, &row.MusicBrainzArtistID, &row.MusicBrainzReleaseGroupID, &row.MusicBrainzReleaseID, &row.MusicBrainzRecordingID); err != nil {
				return nil, err
			}
		} else if table == "tv_episodes" || table == "anime_episodes" {
			if err := rows.Scan(&row.Path, &row.RefID, &row.GlobalID, &row.FileSizeBytes, &row.FileModTime, &row.FileHash, &row.FileHashKind, &row.LastSeenAt, &row.MissingSince, &row.TMDBID, &row.TVDBID, &row.IMDbID, &row.MatchStatus, &row.MetadataReviewNeeded, &row.MetadataConfirmed); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&row.Path, &row.RefID, &row.GlobalID, &row.FileSizeBytes, &row.FileModTime, &row.FileHash, &row.FileHashKind, &row.LastSeenAt, &row.MissingSince, &row.TMDBID, &row.TVDBID, &row.IMDbID, &row.MatchStatus); err != nil {
				return nil, err
			}
		}
		out[row.Path] = row
	}
	return out, rows.Err()
}

func lookupExistingMedia(dbConn *sql.DB, table, kind string, libraryID int, path string) (existingMediaRow, error) {
	var row existingMediaRow
	if table == "music_tracks" {
		err := dbConn.QueryRow(`SELECT m.id, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.last_seen_at, ''), COALESCE(m.missing_since, ''), COALESCE(m.match_status, 'local'), COALESCE(m.poster_path, ''), COALESCE(m.musicbrainz_artist_id, ''), COALESCE(m.musicbrainz_release_group_id, ''), COALESCE(m.musicbrainz_release_id, ''), COALESCE(m.musicbrainz_recording_id, '') FROM music_tracks m WHERE m.library_id = ? AND m.path = ?`, libraryID, path).
			Scan(&row.RefID, &row.FileSizeBytes, &row.FileModTime, &row.FileHash, &row.FileHashKind, &row.LastSeenAt, &row.MissingSince, &row.MatchStatus, &row.PosterPath, &row.MusicBrainzArtistID, &row.MusicBrainzReleaseGroupID, &row.MusicBrainzReleaseID, &row.MusicBrainzRecordingID)
		if errors.Is(err, sql.ErrNoRows) {
			return row, nil
		}
		if err != nil {
			return row, err
		}
	} else {
		var tvdbID, imdbID sql.NullString
		var err error
		if table == "tv_episodes" || table == "anime_episodes" {
			var metadataReviewNeeded sql.NullBool
			var metadataConfirmed sql.NullBool
			err = dbConn.QueryRow(`SELECT m.id, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.last_seen_at, ''), COALESCE(m.missing_since, ''), COALESCE(m.tmdb_id, 0), m.tvdb_id, m.imdb_id, COALESCE(m.match_status, 'local'), COALESCE(m.metadata_review_needed, 0), COALESCE(m.metadata_confirmed, 0) FROM `+table+` m WHERE m.library_id = ? AND m.path = ?`, libraryID, path).
				Scan(&row.RefID, &row.FileSizeBytes, &row.FileModTime, &row.FileHash, &row.FileHashKind, &row.LastSeenAt, &row.MissingSince, &row.TMDBID, &tvdbID, &imdbID, &row.MatchStatus, &metadataReviewNeeded, &metadataConfirmed)
			if metadataReviewNeeded.Valid {
				row.MetadataReviewNeeded = metadataReviewNeeded.Bool
			}
			if metadataConfirmed.Valid {
				row.MetadataConfirmed = metadataConfirmed.Bool
			}
		} else {
			err = dbConn.QueryRow(`SELECT m.id, COALESCE(m.file_size_bytes, 0), COALESCE(m.file_mod_time, ''), COALESCE(m.file_hash, ''), COALESCE(m.file_hash_kind, ''), COALESCE(m.last_seen_at, ''), COALESCE(m.missing_since, ''), COALESCE(m.tmdb_id, 0), m.tvdb_id, m.imdb_id, COALESCE(m.match_status, 'local') FROM `+table+` m WHERE m.library_id = ? AND m.path = ?`, libraryID, path).
				Scan(&row.RefID, &row.FileSizeBytes, &row.FileModTime, &row.FileHash, &row.FileHashKind, &row.LastSeenAt, &row.MissingSince, &row.TMDBID, &tvdbID, &imdbID, &row.MatchStatus)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return row, nil
		}
		if err != nil {
			return row, err
		}
		if tvdbID.Valid {
			row.TVDBID = tvdbID.String
		}
		if imdbID.Valid {
			row.IMDbID = imdbID.String
		}
	}
	row.Path = path
	_ = dbConn.QueryRow(`SELECT id FROM media_global WHERE kind = ? AND ref_id = ?`, kind, row.RefID).Scan(&row.GlobalID)
	return row, nil
}

func insertScannedItem(ctx context.Context, dbConn *sql.DB, table, kind string, libraryID int, mItem MediaItem, seenAt string) (int, int, error) {
	tx, err := dbConn.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}

	var refID int
	if table == "music_tracks" {
		err = tx.QueryRowContext(ctx, `INSERT INTO music_tracks (library_id, title, path, duration, file_size_bytes, file_mod_time, file_hash, file_hash_kind, last_seen_at, missing_since, match_status, artist, album, album_artist, poster_path, musicbrainz_artist_id, musicbrainz_release_group_id, musicbrainz_release_id, musicbrainz_recording_id, disc_number, track_number, release_year) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, mItem.Title, mItem.Path, mItem.Duration, mItem.FileSizeBytes, nullStr(mItem.FileModTime), nullStr(mItem.FileHash), nullStr(mItem.FileHashKind), nullStr(seenAt), mItem.MatchStatus, nullStr(mItem.Artist), nullStr(mItem.Album), nullStr(mItem.AlbumArtist), nullStr(mItem.PosterPath), nullStr(mItem.MusicBrainzArtistID), nullStr(mItem.MusicBrainzReleaseGroupID), nullStr(mItem.MusicBrainzReleaseID), nullStr(mItem.MusicBrainzRecordingID), mItem.DiscNumber, mItem.TrackNumber, mItem.ReleaseYear).Scan(&refID)
		if err != nil {
			_ = tx.Rollback()
			return 0, 0, err
		}
	} else if table == "tv_episodes" || table == "anime_episodes" {
		err = tx.QueryRowContext(ctx, `INSERT INTO `+table+` (library_id, title, path, duration, file_size_bytes, file_mod_time, file_hash, file_hash_kind, last_seen_at, missing_since, match_status, tmdb_id, tvdb_id, overview, poster_path, backdrop_path, release_date, vote_average, imdb_id, imdb_rating, season, episode, metadata_review_needed, metadata_confirmed) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, mItem.Title, mItem.Path, mItem.Duration, mItem.FileSizeBytes, nullStr(mItem.FileModTime), nullStr(mItem.FileHash), nullStr(mItem.FileHashKind), nullStr(seenAt), mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), nullStr(mItem.IMDbID), nullFloat64(mItem.IMDbRating), mItem.Season, mItem.Episode, mItem.MetadataReviewNeeded, mItem.MetadataConfirmed).Scan(&refID)
		if err != nil {
			_ = tx.Rollback()
			return 0, 0, err
		}
	} else {
		err = tx.QueryRowContext(ctx, `INSERT INTO `+table+` (library_id, title, path, duration, file_size_bytes, file_mod_time, file_hash, file_hash_kind, last_seen_at, missing_since, match_status, tmdb_id, tvdb_id, overview, poster_path, backdrop_path, release_date, vote_average, imdb_id, imdb_rating) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, mItem.Title, mItem.Path, mItem.Duration, mItem.FileSizeBytes, nullStr(mItem.FileModTime), nullStr(mItem.FileHash), nullStr(mItem.FileHashKind), nullStr(seenAt), mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), nullStr(mItem.IMDbID), nullFloat64(mItem.IMDbRating)).Scan(&refID)
		if err != nil {
			_ = tx.Rollback()
			return 0, 0, err
		}
	}
	var globalID int
	err = tx.QueryRowContext(ctx, `INSERT INTO media_global (kind, ref_id) VALUES (?, ?) RETURNING id`, kind, refID).Scan(&globalID)
	if err != nil {
		_ = tx.Rollback()
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return refID, globalID, nil
}

func updateScannedItem(ctx context.Context, dbConn *sql.DB, table string, refID int, mItem MediaItem, seenAt string) error {
	if table == "music_tracks" {
		_, err := dbConn.ExecContext(ctx, `UPDATE music_tracks SET title = ?, path = ?, duration = ?, file_size_bytes = ?, file_mod_time = ?, file_hash = ?, file_hash_kind = ?, last_seen_at = ?, missing_since = NULL, match_status = ?, artist = ?, album = ?, album_artist = ?, poster_path = ?, musicbrainz_artist_id = ?, musicbrainz_release_group_id = ?, musicbrainz_release_id = ?, musicbrainz_recording_id = ?, disc_number = ?, track_number = ?, release_year = ? WHERE id = ?`,
			mItem.Title, mItem.Path, mItem.Duration, mItem.FileSizeBytes, nullStr(mItem.FileModTime), nullStr(mItem.FileHash), nullStr(mItem.FileHashKind), nullStr(seenAt), mItem.MatchStatus, nullStr(mItem.Artist), nullStr(mItem.Album), nullStr(mItem.AlbumArtist), nullStr(mItem.PosterPath), nullStr(mItem.MusicBrainzArtistID), nullStr(mItem.MusicBrainzReleaseGroupID), nullStr(mItem.MusicBrainzReleaseID), nullStr(mItem.MusicBrainzRecordingID), mItem.DiscNumber, mItem.TrackNumber, mItem.ReleaseYear, refID)
		return err
	}
	if table == "tv_episodes" || table == "anime_episodes" {
		_, err := dbConn.ExecContext(ctx, `UPDATE `+table+` SET title = ?, path = ?, duration = ?, file_size_bytes = ?, file_mod_time = ?, file_hash = ?, file_hash_kind = ?, last_seen_at = ?, missing_since = NULL, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ?, imdb_id = ?, imdb_rating = ?, season = ?, episode = ?, metadata_review_needed = ?, metadata_confirmed = ? WHERE id = ?`,
			mItem.Title, mItem.Path, mItem.Duration, mItem.FileSizeBytes, nullStr(mItem.FileModTime), nullStr(mItem.FileHash), nullStr(mItem.FileHashKind), nullStr(seenAt), mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), nullStr(mItem.IMDbID), nullFloat64(mItem.IMDbRating), mItem.Season, mItem.Episode, mItem.MetadataReviewNeeded, mItem.MetadataConfirmed, refID)
		return err
	}
	_, err := dbConn.ExecContext(ctx, `UPDATE `+table+` SET title = ?, path = ?, duration = ?, file_size_bytes = ?, file_mod_time = ?, file_hash = ?, file_hash_kind = ?, last_seen_at = ?, missing_since = NULL, match_status = ?, tmdb_id = ?, tvdb_id = ?, overview = ?, poster_path = ?, backdrop_path = ?, release_date = ?, vote_average = ?, imdb_id = ?, imdb_rating = ? WHERE id = ?`,
		mItem.Title, mItem.Path, mItem.Duration, mItem.FileSizeBytes, nullStr(mItem.FileModTime), nullStr(mItem.FileHash), nullStr(mItem.FileHashKind), nullStr(seenAt), mItem.MatchStatus, mItem.TMDBID, nullStr(mItem.TVDBID), nullStr(mItem.Overview), nullStr(mItem.PosterPath), nullStr(mItem.BackdropPath), nullStr(mItem.ReleaseDate), nullFloat64(mItem.VoteAverage), nullStr(mItem.IMDbID), nullFloat64(mItem.IMDbRating), refID)
	return err
}

func updateMediaDuration(ctx context.Context, dbConn *sql.DB, table string, refID int, duration int) error {
	_, err := dbConn.ExecContext(ctx, `UPDATE `+table+` SET duration = ? WHERE id = ?`, duration, refID)
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
			if _, err := dbConn.ExecContext(ctx, `DELETE FROM embedded_audio_tracks WHERE media_id = ?`, row.globalID); err != nil {
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
	if (kind == LibraryTypeTV || kind == LibraryTypeAnime) && row.MetadataConfirmed {
		return true
	}
	hasProviderID := row.TMDBID != 0
	if kind != LibraryTypeAnime {
		hasProviderID = hasProviderID || row.TVDBID != ""
	}
	return hasProviderID && row.IMDbID != ""
}

func nullFloat64(v float64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}
