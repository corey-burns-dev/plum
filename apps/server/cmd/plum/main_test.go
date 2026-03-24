package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"plum/internal/auth"
	"plum/internal/db"
	"plum/internal/metadata"
	"plum/internal/transcoder"
	"plum/internal/ws"
)

func TestNewHTTPServer_DisablesGlobalWriteTimeout(t *testing.T) {
	srv := newHTTPServer(":8080", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if srv.WriteTimeout != 0 {
		t.Fatalf("expected WriteTimeout to be disabled, got %s", srv.WriteTimeout)
	}
	if srv.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout to remain 15s, got %s", srv.ReadTimeout)
	}
}

func TestBuildRouter_WebSocketRequiresAuthentication(t *testing.T) {
	serverURL, _, cleanup := testServer(t)
	defer cleanup()

	resp, err := dialWebSocket(serverURL, "http://allowed.example", "")
	if err == nil {
		t.Fatal("expected websocket dial to fail without authentication")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 response, got %+v", resp)
	}
}

func TestBuildRouter_WebSocketRejectsDisallowedOrigin(t *testing.T) {
	serverURL, dbConn, cleanup := testServer(t)
	defer cleanup()

	sessionID := createSession(t, dbConn)
	resp, err := dialWebSocket(serverURL, "http://blocked.example", "plum_session="+sessionID)
	if err == nil {
		t.Fatal("expected websocket dial to fail for disallowed origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 response, got %+v", resp)
	}
}

func TestBuildRouter_WebSocketAllowsAuthenticatedAllowedOrigin(t *testing.T) {
	serverURL, dbConn, cleanup := testServer(t)
	defer cleanup()

	sessionID := createSession(t, dbConn)
	resp, err := dialWebSocket(serverURL, "http://allowed.example", "plum_session="+sessionID)
	if err != nil {
		t.Fatalf("expected websocket dial to succeed, got err=%v resp=%+v", err, resp)
	}
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 response, got %+v", resp)
	}
}

func TestBuildRouter_DiscoverRequiresAuthentication(t *testing.T) {
	serverURL, _, cleanup := testServer(t)
	defer cleanup()

	resp, err := http.Get(serverURL + "/api/discover")
	if err != nil {
		t.Fatalf("get discover: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func testServer(t *testing.T) (string, *sql.DB, func()) {
	t.Helper()
	t.Setenv("PLUM_ALLOWED_ORIGINS", "http://allowed.example")

	dbConn, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	hub := ws.NewHub()
	go hub.Run()

	playbackSessions := transcoder.NewPlaybackSessionManager(filepath.Join(t.TempDir(), "playback"), hub)
	router := buildRouter(dbConn, hub, playbackSessions, metadata.NewPipeline("", "", "", ""), t.TempDir())
	server := httptest.NewServer(router)

	return server.URL, dbConn, func() {
		server.Close()
		hub.Close()
		_ = dbConn.Close()
	}
}

func createSession(t *testing.T, dbConn *sql.DB) string {
	t.Helper()

	passwordHash, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Now().UTC()
	var userID int
	if err := dbConn.QueryRow(
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, 1, ?) RETURNING id`,
		"user@example.com",
		passwordHash,
		now,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	sessionID, err := auth.NewSessionID()
	if err != nil {
		t.Fatalf("new session id: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		sessionID,
		userID,
		now,
		now.Add(auth.SessionLifetime()),
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	return sessionID
}

func dialWebSocket(serverURL, origin, cookie string) (*http.Response, error) {
	wsURL, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}
	wsURL.Scheme = "ws"
	wsURL.Path = "/ws"

	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	if cookie != "" {
		header.Set("Cookie", cookie)
	}

	conn, resp, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL.String(), header)
	if err == nil {
		_ = conn.Close()
	}
	return resp, err
}
