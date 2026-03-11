package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"plum/internal/db"
	"plum/internal/metadata"
)

type LibraryHandler struct {
	DB       *sql.DB
	Meta     metadata.Identifier
	Series   metadata.SeriesDetailsProvider
	Pipeline *metadata.Pipeline
	ScanJobs *LibraryScanManager
}

type createLibraryRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path"`
}

type updateLibraryPlaybackPreferencesRequest struct {
	PreferredAudioLanguage    string `json:"preferred_audio_language"`
	PreferredSubtitleLanguage string `json:"preferred_subtitle_language"`
	SubtitlesEnabledByDefault bool   `json:"subtitles_enabled_by_default"`
}

type libraryResponse struct {
	ID                        int    `json:"id"`
	Name                      string `json:"name"`
	Type                      string `json:"type"`
	Path                      string `json:"path"`
	UserID                    int    `json:"user_id"`
	PreferredAudioLanguage    string `json:"preferred_audio_language,omitempty"`
	PreferredSubtitleLanguage string `json:"preferred_subtitle_language,omitempty"`
	SubtitlesEnabledByDefault bool   `json:"subtitles_enabled_by_default"`
}

func defaultLibraryPlaybackPreferences(libraryType string) (preferredAudio string, preferredSubtitle string, subtitlesEnabled bool) {
	switch libraryType {
	case db.LibraryTypeAnime:
		return "ja", "en", true
	case db.LibraryTypeMovie, db.LibraryTypeTV:
		return "en", "en", true
	default:
		return "", "", false
	}
}

func buildLibraryResponse(
	id int,
	name string,
	libraryType string,
	path string,
	userID int,
	preferredAudio sql.NullString,
	preferredSubtitle sql.NullString,
	subtitlesEnabled sql.NullBool,
) libraryResponse {
	defaultAudio, defaultSubtitle, defaultSubtitlesEnabled := defaultLibraryPlaybackPreferences(libraryType)
	return libraryResponse{
		ID:                        id,
		Name:                      name,
		Type:                      libraryType,
		Path:                      path,
		UserID:                    userID,
		PreferredAudioLanguage:    strings.TrimSpace(coalesceNullableString(preferredAudio, defaultAudio)),
		PreferredSubtitleLanguage: strings.TrimSpace(coalesceNullableString(preferredSubtitle, defaultSubtitle)),
		SubtitlesEnabledByDefault: coalesceNullableBool(subtitlesEnabled, defaultSubtitlesEnabled),
	}
}

func coalesceNullableString(value sql.NullString, fallback string) string {
	if value.Valid {
		return value.String
	}
	return fallback
}

func coalesceNullableBool(value sql.NullBool, fallback bool) bool {
	if value.Valid {
		return value.Bool
	}
	return fallback
}

func (h *LibraryHandler) CreateLibrary(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload createLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if payload.Name == "" || payload.Path == "" || payload.Type == "" {
		http.Error(w, "name, path and type are required", http.StatusBadRequest)
		return
	}
	switch payload.Type {
	case db.LibraryTypeTV, db.LibraryTypeMovie, db.LibraryTypeMusic, db.LibraryTypeAnime:
		// allowed
	default:
		http.Error(w, "type must be tv, movie, music, or anime", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	var libID int
	defaultAudio, defaultSubtitle, subtitlesEnabled := defaultLibraryPlaybackPreferences(payload.Type)
	err := h.DB.QueryRow(
		`INSERT INTO libraries (user_id, name, type, path, preferred_audio_language, preferred_subtitle_language, subtitles_enabled_by_default, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		u.ID, payload.Name, payload.Type, payload.Path, defaultAudio, defaultSubtitle, subtitlesEnabled, now,
	).Scan(&libID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(libraryResponse{
		ID:                        libID,
		Name:                      payload.Name,
		Type:                      payload.Type,
		Path:                      payload.Path,
		UserID:                    u.ID,
		PreferredAudioLanguage:    defaultAudio,
		PreferredSubtitleLanguage: defaultSubtitle,
		SubtitlesEnabledByDefault: subtitlesEnabled,
	})
}

func (h *LibraryHandler) ListLibraries(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := h.DB.Query(
		`SELECT id, name, type, path, user_id, preferred_audio_language, preferred_subtitle_language, subtitles_enabled_by_default FROM libraries WHERE user_id = ? ORDER BY id`,
		u.ID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var libs []libraryResponse
	for rows.Next() {
		var (
			id                int
			name              string
			libraryType       string
			path              string
			userID            int
			preferredAudio    sql.NullString
			preferredSubtitle sql.NullString
			subtitlesEnabled  sql.NullBool
		)
		if err := rows.Scan(
			&id,
			&name,
			&libraryType,
			&path,
			&userID,
			&preferredAudio,
			&preferredSubtitle,
			&subtitlesEnabled,
		); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		libs = append(libs, buildLibraryResponse(
			id,
			name,
			libraryType,
			path,
			userID,
			preferredAudio,
			preferredSubtitle,
			subtitlesEnabled,
		))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(libs)
}

func (h *LibraryHandler) UpdateLibraryPlaybackPreferences(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	var payload updateLibraryPlaybackPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	payload.PreferredAudioLanguage = strings.TrimSpace(strings.ToLower(payload.PreferredAudioLanguage))
	payload.PreferredSubtitleLanguage = strings.TrimSpace(strings.ToLower(payload.PreferredSubtitleLanguage))

	var (
		libraryID   int
		ownerID     int
		name        string
		libraryType string
		path        string
	)
	err := h.DB.QueryRow(
		`SELECT id, user_id, name, type, path FROM libraries WHERE id = ?`,
		idStr,
	).Scan(&libraryID, &ownerID, &name, &libraryType, &path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if ownerID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := h.DB.Exec(
		`UPDATE libraries SET preferred_audio_language = ?, preferred_subtitle_language = ?, subtitles_enabled_by_default = ? WHERE id = ?`,
		payload.PreferredAudioLanguage,
		payload.PreferredSubtitleLanguage,
		payload.SubtitlesEnabledByDefault,
		libraryID,
	); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(libraryResponse{
		ID:                        libraryID,
		Name:                      name,
		Type:                      libraryType,
		Path:                      path,
		UserID:                    ownerID,
		PreferredAudioLanguage:    payload.PreferredAudioLanguage,
		PreferredSubtitleLanguage: payload.PreferredSubtitleLanguage,
		SubtitlesEnabledByDefault: payload.SubtitlesEnabledByDefault,
	})
}

type scanResult = db.ScanResult

type identifyResult struct {
	Identified int `json:"identified"`
	Failed     int `json:"failed"`
}

const identifyRateLimitMs = 200

var (
	identifyInitialTimeout    = 8 * time.Second
	identifyRetryTimeout      = 45 * time.Second
	identifyLibraryWorkers    = 3
	identifyRateLimitInterval = identifyRateLimitMs * time.Millisecond
)

type identifyJob struct {
	row     db.IdentificationRow
	attempt int
}

type identifyJobStatus string

const (
	identifyJobSucceeded identifyJobStatus = "succeeded"
	identifyJobRetry     identifyJobStatus = "retry"
	identifyJobFailed    identifyJobStatus = "failed"
)

type identifyJobResult struct {
	status identifyJobStatus
	job    identifyJob
}

func (h *LibraryHandler) IdentifyLibrary(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	var libraryID, ownerID int
	err := h.DB.QueryRow(`SELECT id, user_id FROM libraries WHERE id = ?`, idStr).Scan(&libraryID, &ownerID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if ownerID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	result, err := h.identifyLibrary(r.Context(), libraryID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *LibraryHandler) identifyLibrary(ctx context.Context, libraryID int) (identifyResult, error) {
	if h.Meta == nil {
		return identifyResult{Identified: 0, Failed: 0}, nil
	}
	rows, err := db.ListIdentifiableByLibrary(h.DB, libraryID)
	if err != nil {
		return identifyResult{}, err
	}
	if len(rows) == 0 {
		return identifyResult{Identified: 0, Failed: 0}, nil
	}
	var libraryPath string
	_ = h.DB.QueryRow(`SELECT path FROM libraries WHERE id = ?`, libraryID).Scan(&libraryPath)
	identified, failed := 0, 0
	initialJobs := make([]identifyJob, 0, len(rows))
	for _, row := range rows {
		initialJobs = append(initialJobs, identifyJob{row: row})
	}
	initialIdentified, retryJobs, initialFailed := h.runIdentifyJobs(ctx, libraryPath, initialJobs)
	retryIdentified, _, retryFailed := h.runIdentifyJobs(ctx, libraryPath, retryJobs)
	identified += initialIdentified + retryIdentified
	failed += initialFailed + retryFailed

	return identifyResult{Identified: identified, Failed: failed}, nil
}

func (h *LibraryHandler) runIdentifyJobs(
	ctx context.Context,
	libraryPath string,
	jobsToRun []identifyJob,
) (identified int, retryJobs []identifyJob, failed int) {
	if len(jobsToRun) == 0 {
		return 0, nil, 0
	}

	results := make(chan identifyJobResult, len(jobsToRun))
	jobs := make(chan identifyJob)
	workerCount := identifyLibraryWorkers
	if workerCount > len(jobsToRun) {
		workerCount = len(jobsToRun)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	rateLimiter := newIdentifyRateLimiter(ctx)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					results <- h.identifyLibraryJob(ctx, job, libraryPath, rateLimiter)
				}
			}
		}()
	}

	go func() {
		defer close(results)
		wg.Wait()
	}()

enqueueLoop:
	for _, job := range jobsToRun {
		select {
		case <-ctx.Done():
			break enqueueLoop
		case jobs <- job:
		}
	}
	close(jobs)

	for result := range results {
		switch result.status {
		case identifyJobSucceeded:
			identified++
		case identifyJobRetry:
			retryJobs = append(retryJobs, identifyJob{
				row:     result.job.row,
				attempt: result.job.attempt + 1,
			})
		case identifyJobFailed:
			failed++
		}
	}

	return identified, retryJobs, failed
}

func newIdentifyRateLimiter(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	go func() {
		ticker := time.NewTicker(identifyRateLimitInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}()
	return ch
}

func identifyTimeoutForAttempt(attempt int) time.Duration {
	if attempt <= 0 {
		return identifyInitialTimeout
	}
	return identifyRetryTimeout
}

func (h *LibraryHandler) identifyLibraryJob(
	ctx context.Context,
	job identifyJob,
	libraryPath string,
	rateLimiter <-chan struct{},
) identifyJobResult {
	select {
	case <-ctx.Done():
		return identifyJobResult{job: job}
	case <-rateLimiter:
	}

	row := job.row
	info := identifyMediaInfo(row, libraryPath)
	if info.Season == 0 {
		info.Season = row.Season
	}
	if info.Episode == 0 {
		info.Episode = row.Episode
	}
	if info.Title == "" {
		info.Title = row.Title
	}

	itemCtx, cancel := context.WithTimeout(ctx, identifyTimeoutForAttempt(job.attempt))
	defer cancel()

	var res *metadata.MatchResult
	switch row.Kind {
	case db.LibraryTypeTV:
		res = h.Meta.IdentifyTV(itemCtx, info)
	case db.LibraryTypeAnime:
		res = h.Meta.IdentifyAnime(itemCtx, info)
	case db.LibraryTypeMovie:
		res = h.Meta.IdentifyMovie(itemCtx, info)
	default:
		return identifyJobResult{status: identifyJobFailed, job: job}
	}
	if res == nil {
		if job.attempt == 0 {
			return identifyJobResult{status: identifyJobRetry, job: job}
		}
		return identifyJobResult{status: identifyJobFailed, job: job}
	}

	tmdbID, tvdbID := 0, ""
	if res.Provider == "tmdb" {
		if id, err := strconv.Atoi(res.ExternalID); err == nil {
			tmdbID = id
		}
	} else if res.Provider == "tvdb" {
		tvdbID = res.ExternalID
	}
	tbl := db.MediaTableForKind(row.Kind)
	if err := db.UpdateMediaMetadata(h.DB, tbl, row.RefID, res.Title, res.Overview, res.PosterURL, res.BackdropURL, res.ReleaseDate, res.VoteAverage, res.IMDbID, res.IMDbRating, tmdbID, tvdbID, row.Season, row.Episode); err != nil {
		return identifyJobResult{status: identifyJobFailed, job: job}
	}
	return identifyJobResult{status: identifyJobSucceeded, job: job}
}

func identifyMediaInfo(row db.IdentificationRow, libraryPath string) metadata.MediaInfo {
	base := filepath.Base(row.Path)
	relPath, _ := filepath.Rel(libraryPath, row.Path)
	switch row.Kind {
	case db.LibraryTypeMovie:
		return metadata.MovieMediaInfo(metadata.ParseMovie(relPath, base))
	case db.LibraryTypeTV, db.LibraryTypeAnime:
		info := metadata.ParseFilename(base)
		pathInfo := metadata.ParsePathForTV(relPath, base)
		info = metadata.MergePathInfo(pathInfo, info)
		showRoot := metadata.ShowRootPath(libraryPath, row.Path)
		metadata.ApplyShowNFO(&info, showRoot)
		if row.Kind == db.LibraryTypeAnime && info.IsSpecial && info.Episode > 0 {
			info.Season = 0
		}
		return info
	default:
		return metadata.ParseFilename(base)
	}
}

func (h *LibraryHandler) ScanLibrary(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	libraryID, path, typ, ok := h.authorizeLibraryRequest(w, r, u.ID)
	if !ok {
		return
	}

	identify := r.URL.Query().Get("identify") != "false"
	var id metadata.Identifier
	if identify {
		id = h.Meta
	}

	added, err := db.HandleScanLibrary(r.Context(), h.DB, path, typ, libraryID, id)
	if err != nil {
		http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(added)
}

func (h *LibraryHandler) StartLibraryScan(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.ScanJobs == nil {
		http.Error(w, "scan queue unavailable", http.StatusServiceUnavailable)
		return
	}

	libraryID, path, typ, ok := h.authorizeLibraryRequest(w, r, u.ID)
	if !ok {
		return
	}

	status := h.ScanJobs.start(libraryID, path, typ, r.URL.Query().Get("identify") != "false")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (h *LibraryHandler) GetLibraryScanStatus(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.ScanJobs == nil {
		http.Error(w, "scan queue unavailable", http.StatusServiceUnavailable)
		return
	}

	libraryID, _, _, ok := h.authorizeLibraryRequest(w, r, u.ID)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.ScanJobs.status(libraryID))
}

func (h *LibraryHandler) authorizeLibraryRequest(
	w http.ResponseWriter,
	r *http.Request,
	userID int,
) (libraryID int, path string, typ string, ok bool) {
	idStr := chi.URLParam(r, "id")
	var ownerID int
	err := h.DB.QueryRow(
		`SELECT id, user_id, path, type FROM libraries WHERE id = ?`,
		idStr,
	).Scan(&libraryID, &ownerID, &path, &typ)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, "", "", false
	}
	if ownerID != userID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return 0, "", "", false
	}
	return libraryID, path, typ, true
}

func (h *LibraryHandler) GetSeriesDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tmdbIDStr := chi.URLParam(r, "tmdbId")
	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil || tmdbID <= 0 {
		http.Error(w, "invalid tmdb id", http.StatusBadRequest)
		return
	}
	if h.Series == nil {
		http.Error(w, "series metadata not configured", http.StatusServiceUnavailable)
		return
	}
	details, err := h.Series.GetSeriesDetails(r.Context(), tmdbID)
	if err != nil {
		http.Error(w, "failed to fetch series: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if details == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(details)
}

func (h *LibraryHandler) ListLibraryMedia(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	var libraryID, ownerID int
	err := h.DB.QueryRow(`SELECT id, user_id FROM libraries WHERE id = ?`, idStr).Scan(&libraryID, &ownerID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if ownerID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	items, err := db.GetMediaByLibraryIDForUser(h.DB, libraryID, u.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (h *LibraryHandler) GetHomeDashboard(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	dashboard, err := db.GetHomeDashboardForUser(h.DB, u.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dashboard)
}

func (h *LibraryHandler) UpdateMediaProgress(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	mediaID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || mediaID <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	item, err := db.GetMediaByID(h.DB, mediaID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if item == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var ownerID int
	if err := h.DB.QueryRow(`SELECT user_id FROM libraries WHERE id = ?`, item.LibraryID).Scan(&ownerID); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if ownerID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload updateMediaProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := db.UpsertPlaybackProgress(h.DB, u.ID, mediaID, payload.PositionSeconds, payload.DurationSeconds, payload.Completed); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *LibraryHandler) GetSeriesSearch(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
		return
	}
	if h.Pipeline == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
		return
	}
	results, err := h.Pipeline.SearchTV(r.Context(), q)
	if err != nil {
		http.Error(w, "search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []metadata.MatchResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

type refreshShowRequest struct {
	ShowKey string `json:"showKey"`
}

type identifyShowRequest struct {
	ShowKey string `json:"showKey"`
	TmdbID  int    `json:"tmdbId"`
}

type showActionResult struct {
	Updated int `json:"updated"`
}

type updateMediaProgressRequest struct {
	PositionSeconds float64 `json:"position_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
	Completed       bool    `json:"completed"`
}

func (h *LibraryHandler) RefreshShow(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	var libraryID, ownerID int
	err := h.DB.QueryRow(`SELECT id, user_id FROM libraries WHERE id = ?`, idStr).Scan(&libraryID, &ownerID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if ownerID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload refreshShowRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if payload.ShowKey == "" {
		http.Error(w, "showKey is required", http.StatusBadRequest)
		return
	}
	refs, err := db.ListShowEpisodeRefs(h.DB, libraryID, payload.ShowKey)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(refs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(showActionResult{Updated: 0})
		return
	}
	// Use first episode's TMDB ID (series id) for the show
	seriesTMDBID := refs[0].TMDBID
	if h.Pipeline == nil || seriesTMDBID <= 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(showActionResult{Updated: 0})
		return
	}
	table := db.MediaTableForKind(refs[0].Kind)
	updated := 0
	seriesID := strconv.Itoa(seriesTMDBID)
	for _, ref := range refs {
		ep, err := h.Pipeline.GetEpisode(r.Context(), "tmdb", seriesID, ref.Season, ref.Episode)
		if err != nil || ep == nil {
			continue
		}
		tvdbID := ""
		if ep.Provider == "tvdb" {
			tvdbID = ep.ExternalID
		}
		if err := db.UpdateMediaMetadata(h.DB, table, ref.RefID, ep.Title, ep.Overview, ep.PosterURL, ep.BackdropURL, ep.ReleaseDate, ep.VoteAverage, ep.IMDbID, ep.IMDbRating, seriesTMDBID, tvdbID, ref.Season, ref.Episode); err != nil {
			continue
		}
		updated++
		time.Sleep(identifyRateLimitMs * time.Millisecond)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(showActionResult{Updated: updated})
}

func (h *LibraryHandler) IdentifyShow(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "id")
	var libraryID, ownerID int
	err := h.DB.QueryRow(`SELECT id, user_id FROM libraries WHERE id = ?`, idStr).Scan(&libraryID, &ownerID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if ownerID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload identifyShowRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if payload.ShowKey == "" || payload.TmdbID <= 0 {
		http.Error(w, "showKey and tmdbId are required", http.StatusBadRequest)
		return
	}
	refs, err := db.ListShowEpisodeRefs(h.DB, libraryID, payload.ShowKey)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(refs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(showActionResult{Updated: 0})
		return
	}
	if h.Pipeline == nil {
		http.Error(w, "metadata not configured", http.StatusServiceUnavailable)
		return
	}
	table := db.MediaTableForKind(refs[0].Kind)
	seriesID := strconv.Itoa(payload.TmdbID)
	updated := 0
	for _, ref := range refs {
		ep, err := h.Pipeline.GetEpisode(r.Context(), "tmdb", seriesID, ref.Season, ref.Episode)
		if err != nil || ep == nil {
			continue
		}
		tvdbID := ""
		if ep.Provider == "tvdb" {
			tvdbID = ep.ExternalID
		}
		if err := db.UpdateMediaMetadata(h.DB, table, ref.RefID, ep.Title, ep.Overview, ep.PosterURL, ep.BackdropURL, ep.ReleaseDate, ep.VoteAverage, ep.IMDbID, ep.IMDbRating, payload.TmdbID, tvdbID, ref.Season, ref.Episode); err != nil {
			continue
		}
		updated++
		time.Sleep(identifyRateLimitMs * time.Millisecond)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(showActionResult{Updated: updated})
}
