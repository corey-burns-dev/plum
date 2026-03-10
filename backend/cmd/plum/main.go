package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"plum/internal/db"
	"plum/internal/transcoder"
	"plum/internal/ws"
)

func main() {
	addr := getEnv("PLUM_ADDR", ":8080")
	dbPath := getEnv("PLUM_DB_PATH", "./plum.db")

	sqlDB, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()

	if err := db.SeedSample(sqlDB); err != nil {
		log.Fatalf("seed sample: %v", err)
	}

	hub := ws.NewHub()
	go hub.Run()

	mux := http.NewServeMux()
	registerRoutes(mux, sqlDB, hub)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("plum backend listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}

	hub.Close()
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func registerRoutes(mux *http.ServeMux, sqlDB *sql.DB, hub *ws.Hub) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/api/media", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		db.HandleListMedia(w, r, sqlDB)
	})

	mux.HandleFunc("/api/transcode/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		idStr := r.URL.Path[len("/api/transcode/"):]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		transcoder.HandleStartTranscode(w, r, sqlDB, hub, id)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(hub, w, r)
	})
}

