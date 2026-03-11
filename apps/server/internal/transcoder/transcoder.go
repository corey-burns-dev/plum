package transcoder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"database/sql"

	"plum/internal/db"
	"plum/internal/ws"
)

var (
	activeJobMu     sync.Mutex
	activeJobCancel context.CancelFunc
	activeJobID     atomic.Int64
)

func cancelActiveJob() {
	activeJobMu.Lock()
	defer activeJobMu.Unlock()
	if activeJobCancel != nil {
		log.Printf("cancelling active transcode job")
		activeJobCancel()
		activeJobCancel = nil
	}
}

// HandleCancelTranscode cancels any running transcode job.
func HandleCancelTranscode(w http.ResponseWriter, _ *http.Request) {
	cancelActiveJob()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "cancelled",
	})
}

func HandleStartTranscode(w http.ResponseWriter, r *http.Request, sqlDB *sql.DB, hub *ws.Hub, id int) {
	// Cancel any in-flight transcode before starting a new one.
	cancelActiveJob()

	media, err := db.GetMediaByID(sqlDB, id)
	if err != nil {
		log.Printf("lookup media: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if media == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	settings, err := db.GetTranscodingSettings(sqlDB)
	if err != nil {
		log.Printf("load transcoding settings: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	log.Printf("transcoding settings: VAAPIEnabled=%v HardwareEncodingEnabled=%v Device=%s PreferredFormat=%s",
		settings.VAAPIEnabled, settings.HardwareEncodingEnabled, settings.VAAPIDevicePath, settings.PreferredHardwareEncodeFormat)

	audioIndex := -1
	if audioIndexStr := r.URL.Query().Get("audio_index"); audioIndexStr != "" {
		if idx, err := strconv.Atoi(audioIndexStr); err == nil {
			audioIndex = idx
		}
	}

	go startTranscodeJob(*media, settings, hub, audioIndex)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "started",
	})
}

func startTranscodeJob(item db.MediaItem, settings db.TranscodingSettings, hub *ws.Hub, audioIndex int) {
	outPath := filepath.Join("/tmp", fmt.Sprintf("plum_transcoded_%d.mp4", item.ID))
	stream := probeVideoStream(item.Path)
	plans := buildTranscodePlans(item.Path, outPath, settings, stream, audioIndex)

	log.Printf("transcode plans for media %d: %d plan(s), preferred=%s, audioIndex=%d", item.ID, len(plans), plans[0].Mode, audioIndex)
	for i, p := range plans {
		log.Printf("  plan[%d] mode=%s format=%s args: ffmpeg %s", i, p.Mode, p.EncodeFormat, strings.Join(p.Args, " "))
	}

	// Create a cancellable context for this job.
	ctx, cancel := context.WithCancel(context.Background())
	jobID := activeJobID.Add(1)
	activeJobMu.Lock()
	activeJobCancel = cancel
	activeJobMu.Unlock()

	// Ensure we clean up on exit.
	defer func() {
		activeJobMu.Lock()
		if activeJobID.Load() == jobID {
			activeJobCancel = nil
		}
		activeJobMu.Unlock()
	}()

	startMsg, _ := json.Marshal(map[string]interface{}{
		"type":          "transcode_started",
		"id":            item.ID,
		"preferredMode": plans[0].Mode,
	})
	hub.Broadcast(startMsg)

	start := time.Now()
	finalMode := plans[0].Mode
	fallbackUsed := false
	success := false
	var finalErr string

	for index, plan := range plans {
		// Check if cancelled before starting.
		if ctx.Err() != nil {
			log.Printf("transcode for media %d cancelled before starting plan %d", item.ID, index)
			finalErr = "cancelled"
			break
		}

		cmd := exec.CommandContext(ctx, "ffmpeg", plan.Args...)
		var stderrBuf bytes.Buffer
		cmd.Stderr = &stderrBuf
		log.Printf("starting ffmpeg for media %d in %s mode: %s -> %s", item.ID, plan.Mode, item.Path, outPath)
		if err := cmd.Run(); err != nil {
			// Log last portion of stderr for diagnostics.
			if stderrBuf.Len() > 0 {
				stderr := stderrBuf.String()
				if len(stderr) > 2048 {
					stderr = stderr[len(stderr)-2048:]
				}
				log.Printf("ffmpeg stderr for media %d:\n%s", item.ID, stderr)
			}
			// Distinguish cancellation from real errors.
			if ctx.Err() != nil {
				log.Printf("transcode for media %d cancelled during %s mode", item.ID, plan.Mode)
				finalErr = "cancelled"
				break
			}
			log.Printf("ffmpeg error for media %d in %s mode: %v", item.ID, plan.Mode, err)
			finalErr = err.Error()
			if plan.Mode == "hardware" && settings.AllowSoftwareFallback && index+1 < len(plans) {
				fallbackUsed = true
				warnMsg, _ := json.Marshal(map[string]interface{}{
					"type":    "transcode_warning",
					"id":      item.ID,
					"warning": "Hardware transcode failed. Retrying with software encoding.",
					"error":   err.Error(),
				})
				hub.Broadcast(warnMsg)
				continue
			}
			break
		}
		finalMode = plan.Mode
		finalErr = ""
		success = true
		break
	}
	elapsed := time.Since(start)

	completeMsg, _ := json.Marshal(map[string]interface{}{
		"type":         "transcode_complete",
		"id":           item.ID,
		"output":       outPath,
		"elapsed":      elapsed.Seconds(),
		"mode":         finalMode,
		"fallbackUsed": fallbackUsed,
		"success":      success,
		"error":        finalErr,
	})
	hub.Broadcast(completeMsg)
}
