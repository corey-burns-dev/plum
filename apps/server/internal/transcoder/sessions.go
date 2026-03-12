package transcoder

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"plum/internal/db"
	"plum/internal/ws"
)

var ffmpegCommandContext = exec.CommandContext
var previousRevisionCancelDelay = 20 * time.Second

type PlaybackSessionState struct {
	SessionID    string `json:"sessionId"`
	MediaID      int    `json:"mediaId"`
	Revision     int    `json:"revision"`
	AudioIndex   int    `json:"audioIndex"`
	Status       string `json:"status"`
	PlaylistPath string `json:"playlistPath"`
	Error        string `json:"error,omitempty"`
}

type playbackRevision struct {
	number       int
	audioIndex   int
	dir          string
	playlistPath string
	status       string
	err          string
	cancel       context.CancelFunc
	readySent    bool
}

type playbackSession struct {
	mu              sync.Mutex
	id              string
	media           db.MediaItem
	audioIndex      int
	activeRevision  int
	desiredRevision int
	revisions       map[int]*playbackRevision
}

type PlaybackSessionManager struct {
	root string
	hub  *ws.Hub

	mu       sync.RWMutex
	sessions map[string]*playbackSession
}

func NewPlaybackSessionManager(root string, hub *ws.Hub) *PlaybackSessionManager {
	return &PlaybackSessionManager{
		root:     root,
		hub:      hub,
		sessions: make(map[string]*playbackSession),
	}
}

func (m *PlaybackSessionManager) Create(media db.MediaItem, settings db.TranscodingSettings, audioIndex int) (PlaybackSessionState, error) {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return PlaybackSessionState{}, err
	}

	sessionID, err := newPlaybackSessionID()
	if err != nil {
		return PlaybackSessionState{}, err
	}

	session := &playbackSession{
		id:              sessionID,
		media:           media,
		audioIndex:      audioIndex,
		activeRevision:  0,
		desiredRevision: 0,
		revisions:       make(map[int]*playbackRevision),
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	log.Printf(
		"playback session create session=%s media=%d audio_index=%d",
		sessionID,
		media.ID,
		audioIndex,
	)

	return m.startRevision(session, settings, audioIndex)
}

func (m *PlaybackSessionManager) UpdateAudio(sessionID string, settings db.TranscodingSettings, audioIndex int) (PlaybackSessionState, error) {
	m.mu.RLock()
	session := m.sessions[sessionID]
	m.mu.RUnlock()
	if session == nil {
		return PlaybackSessionState{}, db.ErrNotFound
	}
	return m.startRevision(session, settings, audioIndex)
}

func (m *PlaybackSessionManager) Close(sessionID string) {
	m.mu.Lock()
	session := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if session == nil {
		return
	}

	session.mu.Lock()
	revisions := make([]*playbackRevision, 0, len(session.revisions))
	for _, revision := range session.revisions {
		revisions = append(revisions, revision)
	}
	activeRevision := session.activeRevision
	audioIndex := session.audioIndex
	mediaID := session.media.ID
	session.mu.Unlock()

	for _, revision := range revisions {
		if revision.cancel != nil {
			revision.cancel()
		}
	}
	_ = os.RemoveAll(filepath.Join(m.root, sessionID))
	m.broadcast(PlaybackSessionState{
		SessionID:  sessionID,
		MediaID:    mediaID,
		Revision:   activeRevision,
		AudioIndex: audioIndex,
		Status:     "closed",
	})
}

func (m *PlaybackSessionManager) ServeFile(w http.ResponseWriter, r *http.Request, sessionID string, revisionNumber int, name string) error {
	m.mu.RLock()
	session := m.sessions[sessionID]
	m.mu.RUnlock()
	if session == nil {
		return db.ErrNotFound
	}

	session.mu.Lock()
	revision := session.revisions[revisionNumber]
	session.mu.Unlock()
	if revision == nil {
		return db.ErrNotFound
	}

	cleanName := filepath.Clean("/" + name)
	if cleanName == "/" || strings.Contains(cleanName, "..") {
		return db.ErrNotFound
	}
	target := filepath.Join(revision.dir, cleanName)
	if !strings.HasPrefix(target, revision.dir+string(filepath.Separator)) {
		return db.ErrNotFound
	}
	if err := waitForPlaybackFile(r.Context(), target); err != nil {
		if os.IsNotExist(err) {
			return db.ErrNotFound
		}
		return err
	}

	switch filepath.Ext(target) {
	case ".m3u8":
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	case ".ts":
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, target)
	return nil
}

func (m *PlaybackSessionManager) startRevision(session *playbackSession, settings db.TranscodingSettings, audioIndex int) (PlaybackSessionState, error) {
	session.mu.Lock()
	session.desiredRevision += 1
	revisionNumber := session.desiredRevision
	session.audioIndex = audioIndex

	for _, revision := range session.revisions {
		if revision.number > session.activeRevision && revision.number != revisionNumber && revision.cancel != nil {
			revision.cancel()
		}
	}

	dir := filepath.Join(m.root, session.id, fmt.Sprintf("revision_%d", revisionNumber))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return PlaybackSessionState{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	revision := &playbackRevision{
		number:       revisionNumber,
		audioIndex:   audioIndex,
		dir:          dir,
		playlistPath: fmt.Sprintf("/api/playback/sessions/%s/revisions/%d/index.m3u8", session.id, revisionNumber),
		status:       "starting",
		cancel:       cancel,
	}
	session.revisions[revisionNumber] = revision
	session.mu.Unlock()

	log.Printf(
		"playback revision start session=%s media=%d revision=%d audio_index=%d",
		session.id,
		session.media.ID,
		revision.number,
		audioIndex,
	)

	go m.runRevision(ctx, session, revision, settings)

	return PlaybackSessionState{
		SessionID:    session.id,
		MediaID:      session.media.ID,
		Revision:     revision.number,
		AudioIndex:   audioIndex,
		Status:       revision.status,
		PlaylistPath: revision.playlistPath,
	}, nil
}

func (m *PlaybackSessionManager) runRevision(ctx context.Context, session *playbackSession, revision *playbackRevision, settings db.TranscodingSettings) {
	stream := probeVideoStream(session.media.Path)
	plans := buildHLSPlans(session.media.Path, revision.dir, settings, stream, revision.audioIndex)
	finalState := PlaybackSessionState{
		SessionID:    session.id,
		MediaID:      session.media.ID,
		Revision:     revision.number,
		AudioIndex:   revision.audioIndex,
		Status:       "error",
		PlaylistPath: revision.playlistPath,
	}

	for index, plan := range plans {
		if ctx.Err() != nil {
			return
		}

		log.Printf(
			"playback revision ffmpeg start session=%s media=%d revision=%d mode=%s attempt=%d/%d",
			session.id,
			session.media.ID,
			revision.number,
			plan.Mode,
			index+1,
			len(plans),
		)

		if err := os.RemoveAll(revision.dir); err == nil {
			_ = os.MkdirAll(revision.dir, 0o755)
		}

		cmd := ffmpegCommandContext(ctx, "ffmpeg", plan.Args...)
		var stderrBuf bytes.Buffer
		cmd.Stderr = io.MultiWriter(&stderrBuf, os.Stderr)
		if err := cmd.Start(); err != nil {
			finalState.Error = err.Error()
			log.Printf(
				"playback revision ffmpeg start failed session=%s media=%d revision=%d mode=%s error=%q",
				session.id,
				session.media.ID,
				revision.number,
				plan.Mode,
				finalState.Error,
			)
			continue
		}

		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()

		ticker := time.NewTicker(250 * time.Millisecond)
		ready := false
	loop:
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case err := <-waitCh:
				ticker.Stop()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					finalState.Error = compactFFmpegError(stderrBuf.String(), err)
					log.Printf(
						"playback revision ffmpeg failed session=%s media=%d revision=%d mode=%s ready=%t error=%q",
						session.id,
						session.media.ID,
						revision.number,
						plan.Mode,
						ready,
						finalState.Error,
					)
					break loop
				}
				if !ready && revisionReady(revision.dir) {
					ready = true
					m.markRevisionReady(session, revision)
				}
				log.Printf(
					"playback revision ffmpeg exited session=%s media=%d revision=%d mode=%s ready=%t",
					session.id,
					session.media.ID,
					revision.number,
					plan.Mode,
					ready,
				)
				return
			case <-ticker.C:
				if !ready && revisionReady(revision.dir) {
					ready = true
					m.markRevisionReady(session, revision)
				}
			}
		}

		if plan.Mode == "hardware" && settings.AllowSoftwareFallback && index+1 < len(plans) && !ready {
			log.Printf(
				"playback revision fallback session=%s media=%d revision=%d from=%s to=%s",
				session.id,
				session.media.ID,
				revision.number,
				plan.Mode,
				plans[index+1].Mode,
			)
			continue
		}
		break
	}

	if finalState.Error == "" {
		finalState.Error = "transcode failed"
	}
	revision.status = "error"
	revision.err = finalState.Error
	log.Printf(
		"playback revision error session=%s media=%d revision=%d error=%q",
		session.id,
		session.media.ID,
		revision.number,
		finalState.Error,
	)
	m.broadcast(finalState)
}

func (m *PlaybackSessionManager) markRevisionReady(session *playbackSession, revision *playbackRevision) {
	session.mu.Lock()
	if revision.readySent {
		session.mu.Unlock()
		return
	}
	revision.readySent = true
	revision.status = "ready"

	previousActive := session.activeRevision
	if revision.number == session.desiredRevision {
		session.activeRevision = revision.number
		session.audioIndex = revision.audioIndex
	}
	activeRevision := session.activeRevision
	audioIndex := session.audioIndex
	mediaID := session.media.ID
	sessionID := session.id
	session.mu.Unlock()

	m.broadcast(PlaybackSessionState{
		SessionID:    sessionID,
		MediaID:      mediaID,
		Revision:     revision.number,
		AudioIndex:   audioIndex,
		Status:       "ready",
		PlaylistPath: revision.playlistPath,
	})

	if previousActive > 0 && previousActive != activeRevision {
		session.mu.Lock()
		previous := session.revisions[previousActive]
		session.mu.Unlock()
		if previous != nil && previous.cancel != nil {
			delay := previousRevisionCancelDelay
			log.Printf(
				"playback revision ready session=%s media=%d revision=%d previous_revision=%d cancel_delay=%s",
				sessionID,
				mediaID,
				revision.number,
				previousActive,
				delay,
			)
			time.AfterFunc(delay, func() {
				previous.cancel()
			})
		}
	}
}

func (m *PlaybackSessionManager) broadcast(state PlaybackSessionState) {
	if m.hub == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":         "playback_session_update",
		"sessionId":    state.SessionID,
		"mediaId":      state.MediaID,
		"revision":     state.Revision,
		"audioIndex":   state.AudioIndex,
		"status":       state.Status,
		"playlistPath": state.PlaylistPath,
		"error":        state.Error,
	})
	if err != nil {
		log.Printf("marshal playback session update: %v", err)
		return
	}
	m.hub.Broadcast(payload)
}

func revisionReady(dir string) bool {
	playlistPath := filepath.Join(dir, "index.m3u8")
	playlistInfo, err := os.Stat(playlistPath)
	if err != nil || playlistInfo.Size() == 0 {
		return false
	}

	matches, err := filepath.Glob(filepath.Join(dir, "segment_*.ts"))
	if err != nil {
		return false
	}
	return len(matches) > 0
}

func waitForPlaybackFile(ctx context.Context, target string) error {
	_, err := os.Stat(target)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) || !isPlaybackArtifact(target) {
		return err
	}

	deadline := time.NewTimer(1500 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if _, statErr := os.Stat(target); statErr == nil {
				return nil
			}
			return ctx.Err()
		case <-deadline.C:
			_, statErr := os.Stat(target)
			return statErr
		case <-ticker.C:
			if _, statErr := os.Stat(target); statErr == nil {
				return nil
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
		}
	}
}

func isPlaybackArtifact(target string) bool {
	switch filepath.Ext(target) {
	case ".m3u8", ".ts":
		return true
	default:
		return false
	}
}

func compactFFmpegError(stderr string, err error) string {
	stderr = strings.TrimSpace(stderr)
	if len(stderr) > 512 {
		stderr = stderr[len(stderr)-512:]
	}
	if stderr == "" {
		return err.Error()
	}
	return stderr
}

func newPlaybackSessionID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
