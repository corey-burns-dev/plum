package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"plum/internal/db"
	httpapi "plum/internal/http"
	"plum/internal/metadata"
	"plum/internal/transcoder"
	"plum/internal/ws"
)

func main() {
	addr := getEnv("PLUM_ADDR", ":8080")
	conn := getEnv("PLUM_DATABASE_URL", "./data/plum.db")
	tmdbKey := getEnv("TMDB_API_KEY", "")
	tvdbKey := getEnv("TVDB_API_KEY", "")
	omdbKey := getEnv("OMDB_API_KEY", "")

	sqlDB, err := db.InitDB(conn)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()

	pipeline := metadata.NewPipeline(tmdbKey, tvdbKey, omdbKey)
	pipeline.SetIMDbRatingProvider(&db.IMDbRatingStore{DB: sqlDB})

	if err := db.SeedSample(sqlDB); err != nil {
		log.Fatalf("seed sample: %v", err)
	}

	hub := ws.NewHub()
	go hub.Run()
	playbackSessions := transcoder.NewPlaybackSessionManager(filepath.Join(os.TempDir(), "plum_playback"), hub)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	db.StartIMDbRatingsSync(appCtx, sqlDB, log.Printf)

	thumbDir := getEnv("PLUM_THUMBNAILS_DIR", "")
	if thumbDir == "" {
		thumbDir = filepath.Join(filepath.Dir(conn), "thumbnails")
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      buildRouter(sqlDB, hub, playbackSessions, pipeline, thumbDir),
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
	appCancel()

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

func buildRouter(sqlDB *sql.DB, hub *ws.Hub, playbackSessions *transcoder.PlaybackSessionManager, pipeline *metadata.Pipeline, thumbDir string) http.Handler {
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
	scanJobs := httpapi.NewLibraryScanManager(sqlDB, pipeline, hub)
	libHandler := &httpapi.LibraryHandler{
		DB:       sqlDB,
		Meta:     pipeline,
		Series:   pipeline,
		Pipeline: pipeline,
		ScanJobs: scanJobs,
	}
	scanJobs.AttachHandler(libHandler)
	if err := scanJobs.Recover(); err != nil {
		log.Printf("recover scan jobs: %v", err)
	}
	transcodingSettingsHandler := &httpapi.TranscodingSettingsHandler{DB: sqlDB}

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

		protected.Group(func(admin chi.Router) {
			admin.Use(httpapi.RequireAdmin)
			admin.Get("/api/settings/transcoding", transcodingSettingsHandler.Get)
			admin.Put("/api/settings/transcoding", transcodingSettingsHandler.Put)
		})

		protected.Post("/api/libraries", libHandler.CreateLibrary)
		protected.Get("/api/libraries", libHandler.ListLibraries)
		protected.Put("/api/libraries/{id}/playback-preferences", libHandler.UpdateLibraryPlaybackPreferences)
		protected.Get("/api/home", libHandler.GetHomeDashboard)
		protected.Get("/api/libraries/{id}/scan", libHandler.GetLibraryScanStatus)
		protected.Post("/api/libraries/{id}/scan", libHandler.ScanLibrary)
		protected.Post("/api/libraries/{id}/scan/start", libHandler.StartLibraryScan)
		protected.Post("/api/libraries/{id}/identify", libHandler.IdentifyLibrary)
		protected.Get("/api/libraries/{id}/media", libHandler.ListLibraryMedia)
		protected.Post("/api/libraries/{id}/shows/refresh", libHandler.RefreshShow)
		protected.Post("/api/libraries/{id}/shows/identify", libHandler.IdentifyShow)

		protected.Get("/api/series/search", libHandler.GetSeriesSearch)
		protected.Get("/api/series/{tmdbId}", libHandler.GetSeriesDetails)

		protected.Get("/api/media", func(w http.ResponseWriter, r *http.Request) {
			db.HandleListMedia(w, r, sqlDB)
		})
		protected.Put("/api/media/{id}/progress", libHandler.UpdateMediaProgress)
		protected.Post("/api/playback/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
			idStr := chi.URLParam(r, "id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}
			media, err := db.GetMediaByID(sqlDB, id)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if media == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			settings, err := db.GetTranscodingSettings(sqlDB)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			var payload struct {
				AudioIndex int `json:"audioIndex"`
			}
			payload.AudioIndex = -1
			if r.ContentLength != 0 {
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					http.Error(w, "invalid json", http.StatusBadRequest)
					return
				}
			}
			state, err := playbackSessions.Create(*media, settings, payload.AudioIndex)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(state)
		})
		protected.Patch("/api/playback/sessions/{sessionId}/audio", func(w http.ResponseWriter, r *http.Request) {
			sessionID := chi.URLParam(r, "sessionId")
			var payload struct {
				AudioIndex int `json:"audioIndex"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			settings, err := db.GetTranscodingSettings(sqlDB)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			state, err := playbackSessions.UpdateAudio(sessionID, settings, payload.AudioIndex)
			if err != nil {
				status := http.StatusInternalServerError
				if err == db.ErrNotFound {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(state)
		})
		protected.Delete("/api/playback/sessions/{sessionId}", func(w http.ResponseWriter, r *http.Request) {
			playbackSessions.Close(chi.URLParam(r, "sessionId"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "closed"})
		})
		protected.Get("/api/playback/sessions/{sessionId}/revisions/{revision}/*", func(w http.ResponseWriter, r *http.Request) {
			revision, err := strconv.Atoi(chi.URLParam(r, "revision"))
			if err != nil {
				http.Error(w, "invalid revision", http.StatusBadRequest)
				return
			}
			if err := playbackSessions.ServeFile(w, r, chi.URLParam(r, "sessionId"), revision, chi.URLParam(r, "*")); err != nil {
				status := http.StatusInternalServerError
				if err == db.ErrNotFound {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
			}
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
		protected.Delete("/api/transcode", func(w http.ResponseWriter, r *http.Request) {
			transcoder.HandleCancelTranscode(w, r)
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
		protected.Get("/api/media/{id}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
			idStr := chi.URLParam(r, "id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}
			if err := db.HandleServeThumbnail(w, r, sqlDB, id, thumbDir); err != nil {
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
