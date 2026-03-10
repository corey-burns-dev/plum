package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
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

