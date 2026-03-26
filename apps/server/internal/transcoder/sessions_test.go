package transcoder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"plum/internal/db"
)

func TestRunRevisionFallsBackToSoftwareBeforeReady(t *testing.T) {
	root := t.TempDir()
	manager := NewPlaybackSessionManager(root, nil)
	session := &playbackSession{
		id: "session-fallback",
		media: db.MediaItem{
			ID:   42,
			Path: filepath.Join(root, "media.mkv"),
		},
		revisions: make(map[int]*playbackRevision),
	}

	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = true
	settings.HardwareEncodingEnabled = true
	settings.AllowSoftwareFallback = true

	previousCommandContext := ffmpegCommandContext
	ffmpegCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		exitCode := "0"
		if hlsArgsUseHardware(args) {
			exitCode = "1"
		}
		return fakeHLSCommand(ctx, args, exitCode)
	}
	t.Cleanup(func() {
		ffmpegCommandContext = previousCommandContext
	})

	if _, err := manager.startRevision(session, settings, -1, playbackDecision{Delivery: "transcode"}); err != nil {
		t.Fatalf("startRevision: %v", err)
	}

	revision := waitForRevisionStatus(t, session, 1, "ready")
	if revision.err != "" {
		t.Fatalf("expected empty revision error, got %q", revision.err)
	}

	session.mu.Lock()
	activeRevision := session.activeRevision
	session.mu.Unlock()
	if activeRevision != 1 {
		t.Fatalf("activeRevision = %d, want 1", activeRevision)
	}
}

func TestRunRevisionMarksErrorAfterAllPlansFail(t *testing.T) {
	root := t.TempDir()
	manager := NewPlaybackSessionManager(root, nil)
	session := &playbackSession{
		id: "session-error",
		media: db.MediaItem{
			ID:   7,
			Path: filepath.Join(root, "media.mkv"),
		},
		revisions: make(map[int]*playbackRevision),
	}

	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = true
	settings.HardwareEncodingEnabled = true
	settings.AllowSoftwareFallback = true

	previousCommandContext := ffmpegCommandContext
	ffmpegCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return fakeHLSCommand(ctx, args, "1")
	}
	t.Cleanup(func() {
		ffmpegCommandContext = previousCommandContext
	})

	if _, err := manager.startRevision(session, settings, -1, playbackDecision{Delivery: "transcode"}); err != nil {
		t.Fatalf("startRevision: %v", err)
	}

	revision := waitForRevisionStatus(t, session, 1, "error")
	if revision.err == "" {
		t.Fatal("expected revision error to be populated")
	}

	session.mu.Lock()
	activeRevision := session.activeRevision
	session.mu.Unlock()
	if activeRevision != 0 {
		t.Fatalf("activeRevision = %d, want 0", activeRevision)
	}
}

func TestServeFileWaitsForDelayedSegment(t *testing.T) {
	root := t.TempDir()
	manager := NewPlaybackSessionManager(root, nil)
	revisionDir := filepath.Join(root, "session-serve", "revision_1")
	if err := os.MkdirAll(revisionDir, 0o755); err != nil {
		t.Fatalf("mkdir revision dir: %v", err)
	}

	manager.sessions["session-serve"] = &playbackSession{
		id: "session-serve",
		media: db.MediaItem{
			ID: 9,
		},
		revisions: map[int]*playbackRevision{
			1: {
				number: 1,
				dir:    revisionDir,
				status: "ready",
			},
		},
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(revisionDir, "segment_00001.ts"), []byte("segment"), 0o644)
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/playback/sessions/session-serve/revisions/1/segment_00001.ts", nil)
	rec := httptest.NewRecorder()

	if err := manager.ServeFile(rec, req, "session-serve", 1, "segment_00001.ts"); err != nil {
		t.Fatalf("ServeFile: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "segment" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestMarkRevisionReadyDefersPreviousRevisionCancellation(t *testing.T) {
	manager := NewPlaybackSessionManager(t.TempDir(), nil)
	canceled := make(chan struct{}, 1)
	session := &playbackSession{
		id:              "session-ready",
		media:           db.MediaItem{ID: 11},
		activeRevision:  1,
		desiredRevision: 2,
		revisions: map[int]*playbackRevision{
			1: {
				number: 1,
				cancel: func() {
					canceled <- struct{}{}
				},
			},
			2: {
				number:     2,
				audioIndex: 2,
			},
		},
	}

	previousDelay := previousRevisionCancelDelay
	previousRevisionCancelDelay = 25 * time.Millisecond
	t.Cleanup(func() {
		previousRevisionCancelDelay = previousDelay
	})

	manager.markRevisionReady(session, session.revisions[2])

	select {
	case <-canceled:
		t.Fatal("previous revision canceled immediately")
	case <-time.After(10 * time.Millisecond):
	}

	select {
	case <-canceled:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("previous revision was not canceled after delay")
	}
}

func TestHandleDisconnectClosesSessionAfterGracePeriod(t *testing.T) {
	manager := NewPlaybackSessionManager(t.TempDir(), nil)
	session := &playbackSession{
		id:        "session-disconnect",
		userID:    7,
		media:     db.MediaItem{ID: 13},
		revisions: make(map[int]*playbackRevision),
	}

	manager.mu.Lock()
	manager.sessions[session.id] = session
	manager.mu.Unlock()

	previousGrace := playbackDisconnectGracePeriod
	playbackDisconnectGracePeriod = 25 * time.Millisecond
	t.Cleanup(func() {
		playbackDisconnectGracePeriod = previousGrace
	})

	if err := manager.Attach(session.id, session.userID, "client-a"); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	manager.HandleDisconnect(session.userID, "client-a")

	waitForSessionClosed(t, manager, session.id)
}

func TestAttachCancelsPendingDisconnectClose(t *testing.T) {
	manager := NewPlaybackSessionManager(t.TempDir(), nil)
	session := &playbackSession{
		id:        "session-reattach",
		userID:    8,
		media:     db.MediaItem{ID: 14},
		revisions: make(map[int]*playbackRevision),
	}

	manager.mu.Lock()
	manager.sessions[session.id] = session
	manager.mu.Unlock()

	previousGrace := playbackDisconnectGracePeriod
	playbackDisconnectGracePeriod = 80 * time.Millisecond
	t.Cleanup(func() {
		playbackDisconnectGracePeriod = previousGrace
	})

	if err := manager.Attach(session.id, session.userID, "client-a"); err != nil {
		t.Fatalf("Attach initial: %v", err)
	}

	manager.HandleDisconnect(session.userID, "client-a")

	time.Sleep(25 * time.Millisecond)

	if err := manager.Attach(session.id, session.userID, "client-b"); err != nil {
		t.Fatalf("Attach reconnect: %v", err)
	}

	time.Sleep(90 * time.Millisecond)

	manager.mu.RLock()
	remaining := manager.sessions[session.id]
	ownedBy := manager.clients["client-b"]
	manager.mu.RUnlock()

	if remaining == nil {
		t.Fatal("expected session to remain after reattach")
	}
	if ownedBy != session.id {
		t.Fatalf("client-b owner = %q, want %q", ownedBy, session.id)
	}
}

func TestAttachTransfersOwnershipFromPreviousClient(t *testing.T) {
	manager := NewPlaybackSessionManager(t.TempDir(), nil)
	session := &playbackSession{
		id:        "session-transfer",
		userID:    9,
		media:     db.MediaItem{ID: 15},
		revisions: make(map[int]*playbackRevision),
	}

	manager.mu.Lock()
	manager.sessions[session.id] = session
	manager.mu.Unlock()

	previousGrace := playbackDisconnectGracePeriod
	playbackDisconnectGracePeriod = 25 * time.Millisecond
	t.Cleanup(func() {
		playbackDisconnectGracePeriod = previousGrace
	})

	if err := manager.Attach(session.id, session.userID, "client-a"); err != nil {
		t.Fatalf("Attach initial: %v", err)
	}
	if err := manager.Attach(session.id, session.userID, "client-b"); err != nil {
		t.Fatalf("Attach transfer: %v", err)
	}

	manager.HandleDisconnect(session.userID, "client-a")
	time.Sleep(40 * time.Millisecond)

	manager.mu.RLock()
	remaining := manager.sessions[session.id]
	ownedBy := manager.clients["client-b"]
	manager.mu.RUnlock()

	if remaining == nil {
		t.Fatal("expected stale client disconnect not to close session")
	}
	if ownedBy != session.id {
		t.Fatalf("client-b owner = %q, want %q", ownedBy, session.id)
	}
}

func waitForRevisionStatus(t *testing.T, session *playbackSession, revisionNumber int, status string) *playbackRevision {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		revision := session.revisions[revisionNumber]
		if revision != nil && revision.status == status {
			session.mu.Unlock()
			return revision
		}
		session.mu.Unlock()
		time.Sleep(25 * time.Millisecond)
	}

	session.mu.Lock()
	revision := session.revisions[revisionNumber]
	session.mu.Unlock()
	if revision == nil {
		t.Fatalf("revision %d was never created", revisionNumber)
	}
	t.Fatalf("revision %d status = %q, want %q", revisionNumber, revision.status, status)
	return nil
}

func waitForSessionClosed(t *testing.T, manager *PlaybackSessionManager, sessionID string) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		manager.mu.RLock()
		session := manager.sessions[sessionID]
		manager.mu.RUnlock()
		if session == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	manager.mu.RLock()
	_, ok := manager.sessions[sessionID]
	manager.mu.RUnlock()
	if ok {
		t.Fatalf("session %q was not closed", sessionID)
	}
}

func hlsArgsUseHardware(args []string) bool {
	for _, arg := range args {
		if arg == "-vaapi_device" || strings.HasSuffix(arg, "_vaapi") {
			return true
		}
	}
	return false
}

func fakeHLSCommand(ctx context.Context, args []string, exitCode string) *exec.Cmd {
	playlistPath := args[len(args)-1]
	segmentTemplate := ""
	masterPlaylistName := ""
	for index := 0; index < len(args)-1; index += 1 {
		if args[index] == "-hls_segment_filename" && index+1 < len(args) {
			segmentTemplate = args[index+1]
		}
		if args[index] == "-master_pl_name" && index+1 < len(args) {
			masterPlaylistName = args[index+1]
		}
	}

	script := `
playlist_path="$1"
segment_template="$2"
master_playlist_name="$3"
exit_code="$4"
resolved_playlist_path="${playlist_path//%v/0}"
resolved_segment_template="${segment_template//%v/0}"
mkdir -p "$(dirname "$resolved_playlist_path")"
printf '#EXTM3U\n' > "$resolved_playlist_path"
if [ -n "$master_playlist_name" ]; then
  out_dir="$(dirname "$(dirname "$resolved_playlist_path")")"
  printf '#EXTM3U\n' > "$out_dir/$master_playlist_name"
fi
segment_path="${resolved_segment_template//%05d/00000}"
mkdir -p "$(dirname "$segment_path")"
printf 'segment' > "$segment_path"
exit "$exit_code"
`

	return exec.CommandContext(
		ctx,
		"bash",
		"-lc",
		script,
		"bash",
		playlistPath,
		segmentTemplate,
		masterPlaylistName,
		exitCode,
	)
}
