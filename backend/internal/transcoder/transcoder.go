package transcoder

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"

	"database/sql"

	"plum/internal/db"
	"plum/internal/ws"
)

func HandleStartTranscode(w http.ResponseWriter, r *http.Request, sqlDB *sql.DB, hub *ws.Hub, id int) {
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

	go startTranscodeJob(*media, hub)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "started",
	})
}

func startTranscodeJob(item db.MediaItem, hub *ws.Hub) {
	startMsg, _ := json.Marshal(map[string]interface{}{
		"type": "transcode_started",
		"id":   item.ID,
	})
	hub.Broadcast(startMsg)

	outPath := filepath.Join("/tmp", fmt.Sprintf("plum_transcoded_%d.mp4", item.ID))
	cmd := exec.CommandContext(
		context.Background(),
		"ffmpeg",
		"-y",
		"-i", item.Path,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-f", "mp4",
		outPath,
	)

	log.Printf("starting ffmpeg for media %d: %s -> %s", item.ID, item.Path, outPath)
	start := time.Now()
	if err := cmd.Run(); err != nil {
		log.Printf("ffmpeg error for media %d: %v", item.ID, err)
	}
	elapsed := time.Since(start)

	completeMsg, _ := json.Marshal(map[string]interface{}{
		"type":    "transcode_complete",
		"id":      item.ID,
		"output":  outPath,
		"elapsed": elapsed.Seconds(),
	})
	hub.Broadcast(completeMsg)
}

