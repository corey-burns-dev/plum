package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"plum/internal/db"
	"plum/internal/transcoder"
)

type TranscodingSettingsHandler struct {
	DB *sql.DB
}

type transcodingSettingsResponse struct {
	Settings db.TranscodingSettings          `json:"settings"`
	Warnings []db.TranscodingSettingsWarning `json:"warnings"`
}

func (h *TranscodingSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := db.GetTranscodingSettings(h.DB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(transcodingSettingsResponse{
		Settings: settings,
		Warnings: transcoder.GetSettingsWarnings(settings),
	})
}

func (h *TranscodingSettingsHandler) Put(w http.ResponseWriter, r *http.Request) {
	var payload db.TranscodingSettings
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	settings, err := db.SaveTranscodingSettings(h.DB, payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrNoHardwareEncodeFormats) || errors.Is(err, db.ErrInvalidPreferredFormat) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(transcodingSettingsResponse{
		Settings: settings,
		Warnings: transcoder.GetSettingsWarnings(settings),
	})
}
