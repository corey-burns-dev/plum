package db

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	imdbRatingsDatasetURL          = "https://datasets.imdbws.com/title.ratings.tsv.gz"
	appSettingsKeyIMDbRatingsSync  = "imdb_ratings_last_sync"
	imdbRatingsRefreshInterval     = 24 * time.Hour
	imdbRatingsDownloadTimeout     = 10 * time.Minute
	imdbRatingsApplyBatchUpdatedAt = "imdb_ratings_apply"
)

type IMDbRatingStore struct {
	DB *sql.DB
}

func (s *IMDbRatingStore) GetIMDbRatingByID(_ context.Context, imdbID string) (float64, error) {
	if s == nil || s.DB == nil || imdbID == "" {
		return 0, nil
	}
	var rating float64
	err := s.DB.QueryRow(`SELECT rating FROM imdb_ratings WHERE imdb_id = ?`, imdbID).Scan(&rating)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return rating, err
}

func StartIMDbRatingsSync(ctx context.Context, dbConn *sql.DB, logger func(string, ...any)) {
	if dbConn == nil {
		return
	}
	run := func() {
		if err := syncIMDbRatingsIfStale(ctx, dbConn); err != nil && logger != nil && ctx.Err() == nil {
			logger("sync imdb ratings: %v", err)
		}
	}
	go run()
	go func() {
		ticker := time.NewTicker(imdbRatingsRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func syncIMDbRatingsIfStale(ctx context.Context, dbConn *sql.DB) error {
	lastSync, err := getAppSettingTime(dbConn, appSettingsKeyIMDbRatingsSync)
	if err != nil {
		return err
	}
	if !lastSync.IsZero() && time.Since(lastSync) < imdbRatingsRefreshInterval {
		return nil
	}
	return SyncIMDbRatings(ctx, dbConn)
}

func SyncIMDbRatings(ctx context.Context, dbConn *sql.DB) error {
	downloadCtx, cancel := context.WithTimeout(ctx, imdbRatingsDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, imdbRatingsDatasetURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download imdb ratings: status %d", resp.StatusCode)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tx, err := dbConn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
CREATE TEMP TABLE IF NOT EXISTS imdb_ratings_import (
  imdb_id TEXT PRIMARY KEY,
  rating REAL NOT NULL,
  votes INTEGER NOT NULL
)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM imdb_ratings_import`); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO imdb_ratings_import (imdb_id, rating, votes) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	scanner := bufio.NewScanner(gzr)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		rating, parseErr := strconv.ParseFloat(fields[1], 64)
		if parseErr != nil {
			continue
		}
		votes, parseErr := strconv.Atoi(fields[2])
		if parseErr != nil {
			continue
		}
		if _, err = stmt.ExecContext(ctx, fields[0], rating, votes); err != nil {
			return err
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err = tx.ExecContext(ctx, `DELETE FROM imdb_ratings`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO imdb_ratings (imdb_id, rating, votes, updated_at)
SELECT imdb_id, rating, votes, ? FROM imdb_ratings_import
`, now); err != nil {
		return err
	}
	if err = saveAppSettingValueTx(ctx, tx, appSettingsKeyIMDbRatingsSync, now); err != nil {
		return err
	}
	if err = applyIMDbRatingsTx(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func applyIMDbRatingsTx(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, table := range []string{"movies", "tv_episodes", "anime_episodes"} {
		if _, err := tx.ExecContext(ctx, `
UPDATE `+table+`
SET imdb_rating = COALESCE((SELECT r.rating FROM imdb_ratings r WHERE r.imdb_id = `+table+`.imdb_id), imdb_rating)
WHERE COALESCE(imdb_id, '') != ''
`); err != nil {
			return err
		}
	}
	return saveAppSettingValueTx(ctx, tx, imdbRatingsApplyBatchUpdatedAt, now)
}

func getAppSettingTime(dbConn *sql.DB, key string) (time.Time, error) {
	var raw string
	err := dbConn.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, nil
	}
	return parsed, nil
}

func saveAppSettingValueTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.ExecContext(ctx, `
INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, key, value, now)
	return err
}
