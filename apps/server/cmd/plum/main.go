package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	conn := getEnv("PLUM_DATABASE_URL", getEnv("PLUM_DB_PATH", "./data/plum.db"))
	tmdbKey := getEnv("TMDB_API_KEY", "")
	tvdbKey := getEnv("TVDB_API_KEY", "")
	omdbKey := getEnv("OMDB_API_KEY", "")
	musicBrainzContact := getEnv("MUSICBRAINZ_CONTACT_URL", "")

	if err := ensureDatabaseDir(conn); err != nil {
		log.Fatalf("prepare db dir: %v", err)
	}

	sqlDB, err := db.InitDB(conn)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()

	pipeline := metadata.NewPipeline(tmdbKey, tvdbKey, omdbKey, musicBrainzContact)
	pipeline.SetIMDbRatingProvider(&db.IMDbRatingStore{DB: sqlDB})
	pipeline.SetProviderCache(db.NewMetadataProviderCacheStore(sqlDB))

	hub := ws.NewHub()
	go hub.Run()
	playbackSessions := transcoder.NewPlaybackSessionManager(filepath.Join(os.TempDir(), "plum_playback"), hub)
	if err := transcoder.CleanupLegacyTranscodes(os.TempDir()); err != nil {
		log.Printf("cleanup legacy transcodes: %v", err)
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	db.StartIMDbRatingsSync(appCtx, sqlDB, log.Printf)
	db.StartSessionCleanup(appCtx, sqlDB, log.Printf)

	thumbDir := getEnv("PLUM_THUMBNAILS_DIR", "")
	if thumbDir == "" {
		thumbDir = filepath.Join(filepath.Dir(conn), "thumbnails")
	}

	srv := newHTTPServer(addr, buildRouter(sqlDB, hub, playbackSessions, pipeline, thumbDir))

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

func ensureDatabaseDir(conn string) error {
	if conn == "" || conn == ":memory:" || strings.HasPrefix(conn, "file:") {
		return nil
	}
	path := strings.SplitN(conn, "?", 2)[0]
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func buildRouter(sqlDB *sql.DB, hub *ws.Hub, playbackSessions *transcoder.PlaybackSessionManager, pipeline *metadata.Pipeline, thumbDir string) http.Handler {
	r := chi.NewRouter()
	allowedOrigins := httpapi.AllowedOriginsFromEnv(os.Getenv("PLUM_ALLOWED_ORIGINS"))
	r.Use(httpapi.CORSMiddleware(allowedOrigins))
	r.Use(httpapi.RequestBodyLimitMiddleware(httpapi.RequestBodyLimitBytes()))

	r.Use(httpapi.AuthMiddleware(sqlDB))

	authHandler := &httpapi.AuthHandler{DB: sqlDB}
	scanJobs := httpapi.NewLibraryScanManager(sqlDB, pipeline, hub)
	playbackHandler := &httpapi.PlaybackHandler{
		DB:       sqlDB,
		Sessions: playbackSessions,
		ThumbDir: thumbDir,
	}
	libHandler := &httpapi.LibraryHandler{
		DB:          sqlDB,
		Meta:        pipeline,
		Series:      pipeline,
		SeriesQuery: pipeline,
		Discover:    pipeline,
		ScanJobs:    scanJobs,
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
		protected.Get("/api/discover", libHandler.GetDiscover)
		protected.Get("/api/discover/search", libHandler.SearchDiscover)
		protected.Get("/api/discover/{mediaType}/{tmdbId}", libHandler.GetDiscoverTitleDetails)
		protected.Get("/api/libraries/{id}/scan", libHandler.GetLibraryScanStatus)
		protected.Post("/api/libraries/{id}/scan", libHandler.ScanLibrary)
		protected.Post("/api/libraries/{id}/scan/start", libHandler.StartLibraryScan)
		protected.Post("/api/libraries/{id}/identify", libHandler.IdentifyLibrary)
		protected.Get("/api/libraries/{id}/media", libHandler.ListLibraryMedia)
		protected.Post("/api/libraries/{id}/shows/refresh", libHandler.RefreshShow)
		protected.Post("/api/libraries/{id}/shows/identify", libHandler.IdentifyShow)
		protected.Post("/api/libraries/{id}/shows/confirm", libHandler.ConfirmShow)

		protected.Get("/api/series/search", libHandler.GetSeriesSearch)
		protected.Get("/api/series/{tmdbId}", libHandler.GetSeriesDetails)

		protected.Get("/api/media", playbackHandler.ListMedia)
		protected.Put("/api/media/{id}/progress", libHandler.UpdateMediaProgress)
		protected.Post("/api/playback/sessions/{id}", playbackHandler.CreateSession)
		protected.Patch("/api/playback/sessions/{sessionId}/audio", playbackHandler.UpdateSessionAudio)
		protected.Delete("/api/playback/sessions/{sessionId}", playbackHandler.CloseSession)
		protected.Get("/api/playback/sessions/{sessionId}/revisions/{revision}/*", playbackHandler.ServeSessionRevision)
		protected.Get("/api/stream/{id}", playbackHandler.StreamMedia)
		protected.Get("/api/media/{id}/subtitles/embedded/{index}", playbackHandler.StreamEmbeddedSubtitle)
		protected.Get("/api/subtitles/{id}", playbackHandler.StreamSubtitle)
		protected.Get("/api/media/{id}/thumbnail", playbackHandler.ServeThumbnail)
	})

	r.Get("/ws", httpapi.ServeWebSocket(hub, playbackSessions, allowedOrigins))

	return r
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
	}
}
