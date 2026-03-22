package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const metadataRefreshPolicyKey = "metadata_refresh_policy"

type MetadataRefreshPolicy struct {
	ScanRefreshMinAgeHours int `json:"scan_refresh_min_age_hours"`
	BackgroundRefreshAge   int `json:"background_refresh_age_days"`
	MaxItemsPerRun         int `json:"max_items_per_run"`
}

func defaultMetadataRefreshPolicy() MetadataRefreshPolicy {
	return MetadataRefreshPolicy{
		ScanRefreshMinAgeHours: 24,
		BackgroundRefreshAge:   14,
		MaxItemsPerRun:         500,
	}
}

func ensureMetadataRefreshPolicyDefaults(db *sql.DB) error {
	policy := defaultMetadataRefreshPolicy()
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO app_settings (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO NOTHING`,
		metadataRefreshPolicyKey,
		string(raw),
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func GetMetadataRefreshPolicy(db *sql.DB) MetadataRefreshPolicy {
	policy := defaultMetadataRefreshPolicy()
	var raw string
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, metadataRefreshPolicyKey).Scan(&raw); err != nil {
		return policy
	}
	_ = json.Unmarshal([]byte(raw), &policy)
	if policy.ScanRefreshMinAgeHours <= 0 {
		policy.ScanRefreshMinAgeHours = 24
	}
	if policy.BackgroundRefreshAge <= 0 {
		policy.BackgroundRefreshAge = 14
	}
	if policy.MaxItemsPerRun <= 0 {
		policy.MaxItemsPerRun = 500
	}
	return policy
}

func metadataHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

type CanonicalMetadata struct {
	Title        string
	Overview     string
	PosterPath   string
	BackdropPath string
	ReleaseDate  string
	IMDbID       string
	IMDbRating   float64
}

func showKindForTable(table string) string {
	switch table {
	case "tv_episodes":
		return LibraryTypeTV
	case "anime_episodes":
		return LibraryTypeAnime
	default:
		return ""
	}
}

func upsertShowAndSeasonForEpisodeTx(
	ctx context.Context,
	tx *sql.Tx,
	libraryID int,
	table string,
	tmdbID int,
	tvdbID string,
	title string,
	overview string,
	posterPath string,
	backdropPath string,
	releaseDate string,
	imdbID string,
	imdbRating float64,
	seasonNumber int,
) (int, int, error) {
	return upsertShowAndSeasonTx(ctx, tx, libraryID, table, tmdbID, tvdbID, CanonicalMetadata{
		Title:        title,
		Overview:     overview,
		PosterPath:   posterPath,
		BackdropPath: backdropPath,
		ReleaseDate:  releaseDate,
		IMDbID:       imdbID,
		IMDbRating:   imdbRating,
	}, seasonNumber)
}

func upsertShowAndSeasonTx(
	ctx context.Context,
	tx *sql.Tx,
	libraryID int,
	table string,
	tmdbID int,
	tvdbID string,
	canonical CanonicalMetadata,
	seasonNumber int,
) (int, int, error) {
	kind := showKindForTable(table)
	if kind == "" {
		return 0, 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	showTitle := strings.TrimSpace(showNameFromTitle(canonical.Title))
	if showTitle == "" {
		showTitle = strings.TrimSpace(canonical.Title)
	}
	titleKey := normalizeShowKeyTitle(showTitle)
	if titleKey == "" {
		titleKey = "unknown"
	}

	showHash := metadataHash(
		strconvInt(tmdbID),
		tvdbID,
		showTitle,
		canonical.Overview,
		canonical.PosterPath,
		canonical.BackdropPath,
		canonical.ReleaseDate,
		canonical.IMDbID,
		fmt.Sprintf("%.3f", canonical.IMDbRating),
	)

	showID, err := findShowIDTx(ctx, tx, libraryID, kind, tmdbID, titleKey)
	if err != nil {
		return 0, 0, err
	}
	if showID == 0 {
		if err := tx.QueryRowContext(ctx, `INSERT INTO shows (
library_id, kind, tmdb_id, tvdb_id, title, title_key, overview, poster_path, backdrop_path, first_air_date, imdb_id, imdb_rating, metadata_version, metadata_hash, last_refreshed_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?) RETURNING id`,
			libraryID,
			kind,
			nullInt(tmdbID),
			nullStr(tvdbID),
			showTitle,
			titleKey,
			nullStr(canonical.Overview),
			nullStr(canonical.PosterPath),
			nullStr(canonical.BackdropPath),
			nullStr(canonical.ReleaseDate),
			nullStr(canonical.IMDbID),
			nullFloat64(canonical.IMDbRating),
			showHash,
			now,
			now,
			now,
		).Scan(&showID); err != nil {
			return 0, 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE shows SET
tmdb_id = ?,
tvdb_id = ?,
title = ?,
title_key = ?,
overview = ?,
poster_path = ?,
backdrop_path = ?,
first_air_date = ?,
imdb_id = ?,
imdb_rating = ?,
metadata_version = CASE WHEN COALESCE(metadata_hash, '') != ? THEN COALESCE(metadata_version, 1) + 1 ELSE COALESCE(metadata_version, 1) END,
metadata_hash = ?,
last_refreshed_at = ?,
updated_at = ?
WHERE id = ?`,
			nullInt(tmdbID),
			nullStr(tvdbID),
			showTitle,
			titleKey,
			nullStr(canonical.Overview),
			nullStr(canonical.PosterPath),
			nullStr(canonical.BackdropPath),
			nullStr(canonical.ReleaseDate),
			nullStr(canonical.IMDbID),
			nullFloat64(canonical.IMDbRating),
			showHash,
			showHash,
			now,
			now,
			showID,
		); err != nil {
			return 0, 0, err
		}
	}

	seasonHash := metadataHash(
		strconvInt(seasonNumber),
		canonical.Overview,
		canonical.PosterPath,
		canonical.ReleaseDate,
	)
	seasonID, err := findSeasonIDTx(ctx, tx, showID, seasonNumber)
	if err != nil {
		return 0, 0, err
	}
	if seasonID == 0 {
		seasonTitle := seasonDisplayTitle(seasonNumber)
		if err := tx.QueryRowContext(ctx, `INSERT INTO seasons (
show_id, season_number, title, overview, poster_path, air_date, metadata_version, metadata_hash, last_refreshed_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?) RETURNING id`,
			showID,
			seasonNumber,
			seasonTitle,
			nullStr(canonical.Overview),
			nullStr(canonical.PosterPath),
			nullStr(canonical.ReleaseDate),
			seasonHash,
			now,
			now,
			now,
		).Scan(&seasonID); err != nil {
			return 0, 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE seasons SET
title = ?,
overview = ?,
poster_path = ?,
air_date = ?,
metadata_version = CASE WHEN COALESCE(metadata_hash, '') != ? THEN COALESCE(metadata_version, 1) + 1 ELSE COALESCE(metadata_version, 1) END,
metadata_hash = ?,
last_refreshed_at = ?,
updated_at = ?
WHERE id = ?`,
			seasonDisplayTitle(seasonNumber),
			nullStr(canonical.Overview),
			nullStr(canonical.PosterPath),
			nullStr(canonical.ReleaseDate),
			seasonHash,
			seasonHash,
			now,
			now,
			seasonID,
		); err != nil {
			return 0, 0, err
		}
	}

	return showID, seasonID, nil
}

func findShowIDTx(ctx context.Context, tx *sql.Tx, libraryID int, kind string, tmdbID int, titleKey string) (int, error) {
	var showID int
	if tmdbID > 0 {
		err := tx.QueryRowContext(ctx, `SELECT id FROM shows WHERE library_id = ? AND kind = ? AND tmdb_id = ? LIMIT 1`, libraryID, kind, tmdbID).Scan(&showID)
		if err == nil {
			return showID, nil
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}
	var existingTMDBID int
	err := tx.QueryRowContext(ctx, `SELECT id, COALESCE(tmdb_id, 0) FROM shows WHERE library_id = ? AND kind = ? AND title_key = ? LIMIT 1`, libraryID, kind, titleKey).Scan(&showID, &existingTMDBID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	if tmdbID > 0 && existingTMDBID > 0 && existingTMDBID != tmdbID {
		return 0, nil
	}
	return showID, nil
}

func findSeasonIDTx(ctx context.Context, tx *sql.Tx, showID int, seasonNumber int) (int, error) {
	var seasonID int
	err := tx.QueryRowContext(ctx, `SELECT id FROM seasons WHERE show_id = ? AND season_number = ?`, showID, seasonNumber).Scan(&seasonID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return seasonID, nil
}

func seasonDisplayTitle(number int) string {
	if number <= 0 {
		return "Specials"
	}
	return fmt.Sprintf("Season %d", number)
}

func backfillShowsAndSeasonsTx(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, spec := range []struct {
		kind  string
		table string
	}{
		{kind: LibraryTypeTV, table: "tv_episodes"},
		{kind: LibraryTypeAnime, table: "anime_episodes"},
	} {
		rows, err := tx.QueryContext(ctx, `SELECT id, library_id, title, COALESCE(tmdb_id, 0), COALESCE(tvdb_id, ''), COALESCE(overview, ''), COALESCE(poster_path, ''), COALESCE(backdrop_path, ''), COALESCE(release_date, ''), COALESCE(imdb_id, ''), COALESCE(imdb_rating, 0), COALESCE(season, 0)
FROM `+spec.table+`
ORDER BY id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var (
				refID       int
				libraryID   int
				title       string
				tmdbID      int
				tvdbID      string
				overview    string
				posterPath  string
				backdrop    string
				releaseDate string
				imdbID      string
				imdbRating  float64
				season      int
			)
			if err := rows.Scan(&refID, &libraryID, &title, &tmdbID, &tvdbID, &overview, &posterPath, &backdrop, &releaseDate, &imdbID, &imdbRating, &season); err != nil {
				rows.Close()
				return err
			}
			showID, seasonID, err := upsertShowAndSeasonForEpisodeTx(ctx, tx, libraryID, spec.table, tmdbID, tvdbID, title, overview, posterPath, backdrop, releaseDate, imdbID, imdbRating, season)
			if err != nil {
				rows.Close()
				return err
			}
			if showID == 0 || seasonID == 0 {
				continue
			}
			contentHash := metadataHash(title, overview, posterPath, backdrop, releaseDate, imdbID, fmt.Sprintf("%.3f", imdbRating), strconvInt(tmdbID), tvdbID, strconvInt(season))
			if _, err := tx.ExecContext(ctx, `UPDATE `+spec.table+` SET
show_id = ?,
season_id = ?,
metadata_content_hash = COALESCE(NULLIF(metadata_content_hash, ''), ?),
last_metadata_refresh_at = COALESCE(NULLIF(last_metadata_refresh_at, ''), ?)
WHERE id = ?`, showID, seasonID, contentHash, now, refID); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func strconvInt(v int) string {
	return fmt.Sprintf("%d", v)
}

func nullInt(v int) interface{} {
	if v <= 0 {
		return nil
	}
	return v
}
