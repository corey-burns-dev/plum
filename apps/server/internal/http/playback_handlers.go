package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"plum/internal/db"
	"plum/internal/transcoder"
)

type PlaybackHandler struct {
	DB       *sql.DB
	Sessions *transcoder.PlaybackSessionManager
	ThumbDir string
}

func (h *PlaybackHandler) ListMedia(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	db.HandleListMediaForUser(w, r, h.DB, u.ID)
}

func (h *PlaybackHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathInt(w, chi.URLParam(r, "id"), "invalid id")
	if !ok {
		return
	}
	media, err := db.GetMediaByID(h.DB, id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if media == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	sourcePath, err := db.ResolveMediaSourcePath(h.DB, *media)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	media.Path = sourcePath
	settings, err := db.GetTranscodingSettings(h.DB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload struct {
		AudioIndex int `json:"audioIndex"`
	}
	payload.AudioIndex = -1
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
	}

	state, err := h.Sessions.Create(*media, settings, payload.AudioIndex, user.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

func (h *PlaybackHandler) UpdateSessionAudio(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	var payload struct {
		AudioIndex int `json:"audioIndex"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	settings, err := db.GetTranscodingSettings(h.DB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	state, err := h.Sessions.UpdateAudio(sessionID, settings, payload.AudioIndex)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

func (h *PlaybackHandler) CloseSession(w http.ResponseWriter, r *http.Request) {
	h.Sessions.Close(chi.URLParam(r, "sessionId"))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "closed"})
}

func (h *PlaybackHandler) ServeSessionRevision(w http.ResponseWriter, r *http.Request) {
	revision, ok := parsePathInt(w, chi.URLParam(r, "revision"), "invalid revision")
	if !ok {
		return
	}
	if err := h.Sessions.ServeFile(w, r, chi.URLParam(r, "sessionId"), revision, chi.URLParam(r, "*")); err != nil {
		writePlaybackError(w, err)
	}
}

func (h *PlaybackHandler) StreamMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathInt(w, chi.URLParam(r, "id"), "invalid id")
	if !ok {
		return
	}
	if err := db.HandleStreamMedia(w, r, h.DB, id); err != nil {
		writePlaybackError(w, err)
	}
}

func (h *PlaybackHandler) StreamEmbeddedSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathInt(w, chi.URLParam(r, "id"), "invalid id")
	if !ok {
		return
	}
	streamIndex, ok := parsePathInt(w, chi.URLParam(r, "index"), "invalid index")
	if !ok {
		return
	}
	if err := db.HandleStreamEmbeddedSubtitle(w, r, h.DB, id, streamIndex); err != nil {
		writePlaybackError(w, err)
	}
}

func (h *PlaybackHandler) StreamSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathInt(w, chi.URLParam(r, "id"), "invalid id")
	if !ok {
		return
	}
	if err := db.HandleStreamSubtitle(w, r, h.DB, id); err != nil {
		writePlaybackError(w, err)
	}
}

func (h *PlaybackHandler) ServeThumbnail(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathInt(w, chi.URLParam(r, "id"), "invalid id")
	if !ok {
		return
	}
	if err := db.HandleServeThumbnail(w, r, h.DB, id, h.ThumbDir); err != nil {
		writePlaybackError(w, err)
	}
}

func parsePathInt(w http.ResponseWriter, raw string, message string) (int, bool) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		http.Error(w, message, http.StatusBadRequest)
		return 0, false
	}
	return value, true
}

func writePlaybackError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, db.ErrNotFound) {
		status = http.StatusNotFound
	}
	http.Error(w, err.Error(), status)
}
