package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"plum/internal/db"
)

func TestTranscodingSettingsHandler_GetDefaults(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	handler := &TranscodingSettingsHandler{DB: dbConn}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/transcoding", nil)
	req = req.WithContext(withUser(req.Context(), &db.User{ID: 1, IsAdmin: true}))
	rec := httptest.NewRecorder()

	handler.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Settings db.TranscodingSettings          `json:"settings"`
		Warnings []db.TranscodingSettingsWarning `json:"warnings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Settings.VAAPIDevicePath != "/dev/dri/renderD128" {
		t.Fatalf("device path = %q", payload.Settings.VAAPIDevicePath)
	}
	if !payload.Settings.DecodeCodecs.HEVC10Bit || !payload.Settings.DecodeCodecs.VP910Bit {
		t.Fatalf("expected 10-bit decode defaults to be enabled: %+v", payload.Settings.DecodeCodecs)
	}
	if payload.Settings.HardwareEncodingEnabled {
		t.Fatalf("expected hardware encoding to default off")
	}
}

func TestTranscodingSettingsHandler_PutRequiresAdmin(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	handler := &TranscodingSettingsHandler{DB: dbConn}
	router := chi.NewRouter()
	router.Use(RequireAuth)
	router.Group(func(admin chi.Router) {
		admin.Use(RequireAdmin)
		admin.Put("/api/settings/transcoding", handler.Put)
	})

	body := bytes.NewBufferString(`{"vaapiEnabled":true,"vaapiDevicePath":"/dev/dri/renderD128","decodeCodecs":{"h264":true,"hevc":true,"mpeg2":true,"vc1":true,"vp8":true,"vp9":true,"av1":true,"hevc10bit":true,"vp910bit":true},"hardwareEncodingEnabled":true,"encodeFormats":{"h264":true,"hevc":false,"av1":false},"preferredHardwareEncodeFormat":"h264","allowSoftwareFallback":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/transcoding", body)
	req = req.WithContext(withUser(req.Context(), &db.User{ID: 2, IsAdmin: false}))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTranscodingSettingsHandler_PutPersistsSettings(t *testing.T) {
	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`, "admin@test.com", "hash", now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	handler := &TranscodingSettingsHandler{DB: dbConn}
	payload := db.DefaultTranscodingSettings()
	payload.VAAPIEnabled = true
	payload.HardwareEncodingEnabled = true
	payload.EncodeFormats.H264 = false
	payload.EncodeFormats.HEVC = true
	payload.PreferredHardwareEncodeFormat = "hevc"

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/settings/transcoding", bytes.NewReader(raw))
	req = req.WithContext(context.WithValue(withUser(req.Context(), &db.User{ID: userID, IsAdmin: true}), chi.RouteCtxKey, chi.NewRouteContext()))
	rec := httptest.NewRecorder()

	handler.Put(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	saved, err := db.GetTranscodingSettings(dbConn)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if !saved.VAAPIEnabled || !saved.HardwareEncodingEnabled {
		t.Fatalf("expected saved hardware settings: %+v", saved)
	}
	if saved.PreferredHardwareEncodeFormat != "hevc" {
		t.Fatalf("preferred format = %q", saved.PreferredHardwareEncodeFormat)
	}
	if saved.EncodeFormats.H264 {
		t.Fatalf("expected h264 encode to remain disabled: %+v", saved.EncodeFormats)
	}
}
