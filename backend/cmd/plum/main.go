package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"plum/internal/db"
	"plum/internal/transcoder"
	"plum/internal/ws"
)

func main() {
	addr := getEnv("PLUM_ADDR", ":8080")
	dbPath := getEnv("PLUM_DB_PATH", "./plum.db")
	tvPath := getEnv("PLUM_TV_PATH", "/home/cburns/Videos")

	sqlDB, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()

	if err := db.SeedSample(sqlDB); err != nil {
		log.Fatalf("seed sample: %v", err)
	}

	// Best-effort scan of the default TV library path so the UI is useful
	// without any manual configuration. Errors are logged but do not prevent
	// the server from starting.
	if tvPath != "" {
		added, err := db.HandleScanLibrary(context.Background(), sqlDB, tvPath, "tv")
		if err != nil {
			log.Printf("tv scan (%s): %v", tvPath, err)
		} else if added > 0 {
			log.Printf("tv scan (%s): added %d items", tvPath, added)
		}
	}

	hub := ws.NewHub()
	go hub.Run()

	srv := &http.Server{
		Addr:         addr,
		Handler:      buildRouter(sqlDB, hub),
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

func buildRouter(sqlDB *sql.DB, hub *ws.Hub) http.Handler {
	r := chi.NewRouter()

	// simple CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Get("/api/media", func(w http.ResponseWriter, r *http.Request) {
		db.HandleListMedia(w, r, sqlDB)
	})

	r.Post("/api/scan", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		added, err := db.HandleScanLibrary(r.Context(), sqlDB, payload.Path, payload.Type)
		if err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"added": added,
		})
	})

	r.Post("/api/transcode/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		transcoder.HandleStartTranscode(w, r, sqlDB, hub, id)
	})

	r.Get("/api/stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := db.HandleStreamMedia(w, r, sqlDB, id); err != nil {
			status := http.StatusInternalServerError
			if err == db.ErrNotFound {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
		}
	})

	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(hub, w, r)
	})

	return r
}

