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

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"plum/internal/db"
	httpapi "plum/internal/http"
	"plum/internal/transcoder"
	"plum/internal/ws"
)

func main() {
	addr := getEnv("PLUM_ADDR", ":8080")
	conn := getEnv("PLUM_DATABASE_URL", "./data/plum.db")
	tmdbKey := getEnv("TMDB_API_KEY", "")

	sqlDB, err := db.InitDB(conn)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()

	if err := db.SeedSample(sqlDB); err != nil {
		log.Fatalf("seed sample: %v", err)
	}

	hub := ws.NewHub()
	go hub.Run()

	srv := &http.Server{
		Addr:         addr,
		Handler:      buildRouter(sqlDB, hub, tmdbKey),
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

func buildRouter(sqlDB *sql.DB, hub *ws.Hub, tmdbKey string) http.Handler {
	r := chi.NewRouter()

	// CORS: allow credentials (cookies) by reflecting Origin when set
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Use(httpapi.AuthMiddleware(sqlDB))

	authHandler := &httpapi.AuthHandler{DB: sqlDB}
	libHandler := &httpapi.LibraryHandler{DB: sqlDB, TMDBKey: tmdbKey}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Get("/api/setup/status", authHandler.SetupStatus)
	r.Post("/api/auth/admin-setup", authHandler.AdminSetup)
	r.Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/logout", authHandler.Logout)
	r.Get("/api/auth/me", authHandler.Me)

	r.Group(func(protected chi.Router) {
		protected.Use(httpapi.RequireAuth)

		protected.Post("/api/libraries", libHandler.CreateLibrary)
		protected.Get("/api/libraries", libHandler.ListLibraries)
		protected.Post("/api/libraries/{id}/scan", libHandler.ScanLibrary)
		protected.Get("/api/libraries/{id}/media", libHandler.ListLibraryMedia)

		protected.Get("/api/media", func(w http.ResponseWriter, r *http.Request) {
			db.HandleListMedia(w, r, sqlDB)
		})
		protected.Post("/api/transcode/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
			transcoder.HandleStartTranscode(w, r, sqlDB, hub, id)
		})
		protected.Get("/api/stream/{id}", func(w http.ResponseWriter, r *http.Request) {
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
		protected.Get("/api/media/{id}/subtitles/embedded/{index}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		indexStr := chi.URLParam(r, "index")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		streamIndex, err := strconv.Atoi(indexStr)
		if err != nil {
			http.Error(w, "invalid index", http.StatusBadRequest)
			return
		}
			if err := db.HandleStreamEmbeddedSubtitle(w, r, sqlDB, id, streamIndex); err != nil {
				status := http.StatusInternalServerError
				if err == db.ErrNotFound {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
			}
		})
		protected.Get("/api/subtitles/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
			if err := db.HandleStreamSubtitle(w, r, sqlDB, id); err != nil {
				status := http.StatusInternalServerError
				if err == db.ErrNotFound {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
			}
		})
	})

	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(hub, w, r)
	})

	return r
}
