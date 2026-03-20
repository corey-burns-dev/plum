package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

var ErrNotFound = errors.New("not found")

func HandleListMediaForUser(w http.ResponseWriter, r *http.Request, dbConn *sql.DB, userID int) {
	items, err := GetAllMediaForUser(dbConn, userID)
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

// HandleStreamMedia looks up a media item and serves the original file contents.
// Transcoded playback is served exclusively through playback sessions.
func HandleStreamMedia(w http.ResponseWriter, r *http.Request, dbConn *sql.DB, id int) error {
	item, err := GetMediaByID(dbConn, id)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrNotFound
	}
	sourcePath, err := ResolveMediaSourcePath(dbConn, *item)
	if err != nil {
		return err
	}

	http.ServeFile(w, r, sourcePath)
	return nil
}
