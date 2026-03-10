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
)

type MediaItem struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	Duration int    `json:"duration"`
	Type     string `json:"type"`
}

func InitDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if err := createSchema(db); err != nil {
		return nil, err
	}
	return db, nil
}

func createSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS media (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  path TEXT NOT NULL,
  duration INTEGER NOT NULL DEFAULT 0,
  type TEXT NOT NULL DEFAULT 'video'
);`
	_, err := db.Exec(schema)
	return err
}

func SeedSample(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	samples := []MediaItem{
		{Title: "Sample Video 1", Path: "/tmp/sample1.mp4", Duration: 60, Type: "video"},
		{Title: "Sample Video 2", Path: "/tmp/sample2.mp4", Duration: 120, Type: "video"},
	}

	for _, m := range samples {
		if _, err := db.Exec(
			`INSERT INTO media (title, path, duration, type) VALUES (?, ?, ?, ?)`,
			m.Title, m.Path, m.Duration, m.Type,
		); err != nil {
			return err
		}
	}
	return nil
}

func GetAllMedia(db *sql.DB) ([]MediaItem, error) {
	rows, err := db.Query(`SELECT id, title, path, duration, type FROM media ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []MediaItem
	for rows.Next() {
		var m MediaItem
		if err := rows.Scan(&m.ID, &m.Title, &m.Path, &m.Duration, &m.Type); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func GetMediaByID(db *sql.DB, id int) (*MediaItem, error) {
	var m MediaItem
	err := db.QueryRow(`SELECT id, title, path, duration, type FROM media WHERE id = ?`, id).
		Scan(&m.ID, &m.Title, &m.Path, &m.Duration, &m.Type)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
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

// HandleScanLibrary walks the given filesystem path, inserting any supported
// media files into the media table. It returns the number of new records added.
func HandleScanLibrary(ctx context.Context, dbConn *sql.DB, root, mediaType string) (int, error) {
	if root == "" {
		return 0, fmt.Errorf("path is required")
	}
	if mediaType == "" {
		mediaType = "video"
	}
	info, err := os.Stat(root)
	if err != nil {
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

		var existing int
		if err := dbConn.QueryRow(`SELECT COUNT(1) FROM media WHERE path = ?`, path).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}

		title := strings.TrimSuffix(d.Name(), ext)
		if title == "" {
			title = d.Name()
		}

		if _, err := dbConn.ExecContext(ctx,
			`INSERT INTO media (title, path, duration, type) VALUES (?, ?, ?, ?)`,
			title, path, 0, mediaType,
		); err != nil {
			return err
		}
		added++
		return nil
	})
	if err != nil {
		return added, err
	}
	return added, nil
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
