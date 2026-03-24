package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"plum/internal/db"
	"plum/internal/metadata"
)

type LibraryHandler struct {
	DB          *sql.DB
	Meta        metadata.Identifier
	Series      metadata.SeriesDetailsProvider
	SeriesQuery metadata.SeriesSearchProvider
	Discover    metadata.DiscoverProvider
	ScanJobs    *LibraryScanManager
	identifyRun *identifyRunTracker
}

type identifyRunTracker struct {
	mu      sync.RWMutex
	byLibID map[int]map[string]string
}

func newIdentifyRunTracker() *identifyRunTracker {
	return &identifyRunTracker{
		byLibID: make(map[int]map[string]string),
	}
}

func identifyRowKey(kind, path string) string {
	return kind + ":" + path
}

func (t *identifyRunTracker) startLibrary(libraryID int, rows []db.IdentificationRow) {
	if t == nil {
		return
	}
	states := make(map[string]string, len(rows))
	for _, row := range rows {
		states[identifyRowKey(row.Kind, row.Path)] = "queued"
	}
	t.mu.Lock()
	t.byLibID[libraryID] = states
	t.mu.Unlock()
}

func (t *identifyRunTracker) setState(libraryID int, kind, path, state string) {
	if t == nil {
		return
	}
	key := identifyRowKey(kind, path)
	t.mu.Lock()
	defer t.mu.Unlock()
	states, ok := t.byLibID[libraryID]
	if !ok {
		return
	}
	if state == "" {
		delete(states, key)
		return
	}
	states[key] = state
}

func (t *identifyRunTracker) failRows(libraryID int, rows []db.IdentificationRow) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	states, ok := t.byLibID[libraryID]
	if !ok {
		return
	}
	for _, row := range rows {
		states[identifyRowKey(row.Kind, row.Path)] = "failed"
	}
}

func (t *identifyRunTracker) finishLibrary(libraryID int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	states := t.byLibID[libraryID]
	if len(states) == 0 {
		delete(t.byLibID, libraryID)
		t.mu.Unlock()
		return
	}
	failedOnly := make(map[string]string)
	for key, value := range states {
		if value == "failed" {
			failedOnly[key] = value
		}
	}
	if len(failedOnly) == 0 {
		delete(t.byLibID, libraryID)
	} else {
		t.byLibID[libraryID] = failedOnly
	}
	t.mu.Unlock()
}

func (t *identifyRunTracker) stateForLibrary(libraryID int) map[string]string {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	states := t.byLibID[libraryID]
	if len(states) == 0 {
		return nil
	}
	out := make(map[string]string, len(states))
	for key, value := range states {
		out[key] = value
	}
	return out
}

func (t *identifyRunTracker) clearLibrary(libraryID int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.byLibID, libraryID)
	t.mu.Unlock()
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

func isMissingColumnError(err error, column string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such column: "+strings.ToLower(column)) ||
		strings.Contains(text, "has no column named "+strings.ToLower(column))
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "sqlite_busy")
}

func discoverHTTPStatus(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	if errors.Is(err, metadata.ErrTMDBNotConfigured) {
		return http.StatusServiceUnavailable, err.Error()
	}
	return http.StatusInternalServerError, "discover failed: " + err.Error()
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
	err := retryCreateLibraryInsert(h.DB, u.ID, payload, defaultAudio, defaultSubtitle, subtitlesEnabled, now, &libID)
	if err != nil {
		log.Printf("create library: %v", err)
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

func retryCreateLibraryInsert(
	dbConn *sql.DB,
	userID int,
	payload createLibraryRequest,
	defaultAudio string,
	defaultSubtitle string,
	subtitlesEnabled bool,
	now time.Time,
	libID *int,
) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		err = dbConn.QueryRow(
			`INSERT INTO libraries (user_id, name, type, path, preferred_audio_language, preferred_subtitle_language, subtitles_enabled_by_default, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			userID, payload.Name, payload.Type, payload.Path, defaultAudio, defaultSubtitle, subtitlesEnabled, now,
		).Scan(libID)
		if isMissingColumnError(err, "preferred_audio_language") {
			err = dbConn.QueryRow(
				`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
				userID, payload.Name, payload.Type, payload.Path, now,
			).Scan(libID)
		}
		if err == nil || !isSQLiteBusyError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	return err
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
	legacyColumns := false
	if err != nil && isMissingColumnError(err, "preferred_audio_language") {
		legacyColumns = true
		rows, err = h.DB.Query(
			`SELECT id, name, type, path, user_id FROM libraries WHERE user_id = ? ORDER BY id`,
			u.ID,
		)
	}
	if err != nil {
		log.Printf("list libraries: %v", err)
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
		if legacyColumns {
			err = rows.Scan(&id, &name, &libraryType, &path, &userID)
		} else {
			err = rows.Scan(
				&id,
				&name,
				&libraryType,
				&path,
				&userID,
				&preferredAudio,
				&preferredSubtitle,
				&subtitlesEnabled,
			)
		}
		if err != nil {
			log.Printf("scan libraries row: %v", err)
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
		if !isMissingColumnError(err, "preferred_audio_language") {
			log.Printf("update library playback preferences: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
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
	status           identifyJobStatus
	job              identifyJob
	fallbackEligible bool
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
		h.identifyRun.clearLibrary(libraryID)
		return identifyResult{Identified: 0, Failed: 0}, nil
	}
	if h.identifyRun == nil {
		h.identifyRun = newIdentifyRunTracker()
	}
	h.identifyRun.startLibrary(libraryID, rows)
	defer h.identifyRun.finishLibrary(libraryID)

	var libraryPath string
	_ = h.DB.QueryRow(`SELECT path FROM libraries WHERE id = ?`, libraryID).Scan(&libraryPath)
	identified, failed := 0, 0
	initialJobs := make([]identifyJob, 0, len(rows))
	for _, row := range rows {
		initialJobs = append(initialJobs, identifyJob{row: row})
	}
	sortIdentifyJobs(initialJobs, libraryPath)
	initialIdentified, retryJobs, initialFailed := h.runIdentifyJobs(ctx, libraryID, libraryPath, initialJobs)
	retryIdentified, _, retryFailed := h.runIdentifyJobs(ctx, libraryID, libraryPath, retryJobs)
	identified += initialIdentified + retryIdentified

	fallbackIdentified, fallbackFailed := h.identifyShowFallbacks(ctx, libraryID, libraryPath, append(initialFailed, retryFailed...))
	identified += fallbackIdentified
	failed += fallbackFailed

	return identifyResult{Identified: identified, Failed: failed}, nil
}

func (h *LibraryHandler) runIdentifyJobs(
	ctx context.Context,
	libraryID int,
	libraryPath string,
	jobsToRun []identifyJob,
) (identified int, retryJobs []identifyJob, failed []identifyJobResult) {
	if len(jobsToRun) == 0 {
		return 0, nil, nil
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
					results <- h.identifyLibraryJob(ctx, libraryID, job, libraryPath, rateLimiter)
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
			failed = append(failed, result)
		}
	}

	return identified, retryJobs, failed
}

type showFallbackGroup struct {
	queries []string
	rows    []db.IdentificationRow
}

func (h *LibraryHandler) identifyShowFallbacks(
	ctx context.Context,
	libraryID int,
	libraryPath string,
	failedResults []identifyJobResult,
) (identified int, failed int) {
	if len(failedResults) == 0 {
		return 0, 0
	}

	groups := make(map[string]*showFallbackGroup)
	for _, result := range failedResults {
		if !result.fallbackEligible {
			h.identifyRun.setState(libraryID, result.job.row.Kind, result.job.row.Path, "failed")
			failed++
			continue
		}
		queries := showFallbackQueries(result.job.row, libraryPath)
		if len(queries) == 0 {
			h.identifyRun.setState(libraryID, result.job.row.Kind, result.job.row.Path, "failed")
			failed++
			continue
		}
		groupKey := strings.ToLower(queries[0])
		group, ok := groups[groupKey]
		if !ok {
			group = &showFallbackGroup{queries: queries}
			groups[groupKey] = group
		}
		group.rows = append(group.rows, result.job.row)
	}

	for _, group := range groups {
		updated, err := h.identifyShowFallbackGroup(ctx, group.queries, group.rows)
		if err != nil || updated != len(group.rows) {
			h.identifyRun.failRows(libraryID, group.rows[updated:])
			identified += updated
			failed += len(group.rows) - updated
			continue
		}
		for _, row := range group.rows {
			h.identifyRun.setState(libraryID, row.Kind, row.Path, "")
		}
		identified += updated
	}

	return identified, failed
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
	libraryID int,
	job identifyJob,
	libraryPath string,
	rateLimiter <-chan struct{},
) identifyJobResult {
	h.identifyRun.setState(libraryID, job.row.Kind, job.row.Path, "identifying")
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
			h.identifyRun.setState(libraryID, row.Kind, row.Path, "queued")
			return identifyJobResult{status: identifyJobRetry, job: job}
		}
		return identifyJobResult{
			status:           identifyJobFailed,
			job:              job,
			fallbackEligible: (row.Kind == db.LibraryTypeAnime || row.Kind == db.LibraryTypeTV) && itemCtx.Err() == nil,
		}
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
	h.identifyRun.setState(libraryID, row.Kind, row.Path, "")
	return identifyJobResult{status: identifyJobSucceeded, job: job}
}

func (h *LibraryHandler) identifyShowFallbackGroup(
	ctx context.Context,
	queries []string,
	rows []db.IdentificationRow,
) (int, error) {
	if h.SeriesQuery == nil || len(rows) == 0 {
		return 0, nil
	}
	var (
		results []metadata.MatchResult
		err     error
	)
	for _, query := range queries {
		results, err = h.SeriesQuery.SearchTV(ctx, query)
		if err != nil {
			return 0, err
		}
		if len(results) > 0 {
			break
		}
	}
	if len(results) == 0 {
		return 0, nil
	}
	tmdbID, err := strconv.Atoi(results[0].ExternalID)
	if err != nil || tmdbID <= 0 {
		return 0, err
	}
	refs := make([]db.ShowEpisodeRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, db.ShowEpisodeRef{
			RefID:   row.RefID,
			Kind:    row.Kind,
			Season:  row.Season,
			Episode: row.Episode,
		})
	}
	return h.applyTMDBSeriesToRefs(ctx, tmdbID, refs, true, false)
}

func showFallbackQueries(row db.IdentificationRow, libraryPath string) []string {
	info := identifyMediaInfo(row, libraryPath)
	candidates := []string{
		showTitleFromEpisodeTitle(row.Title),
		strings.TrimSpace(info.Title),
	}
	seen := make(map[string]struct{}, len(candidates))
	queries := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		queries = append(queries, candidate)
	}
	return queries
}

func showTitleFromEpisodeTitle(title string) string {
	title = strings.TrimSpace(title)
	if i := strings.Index(strings.ToLower(title), " - s"); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	if i := strings.Index(title, " - "); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	return title
}

func (h *LibraryHandler) applyTMDBSeriesToRefs(
	ctx context.Context,
	seriesTMDBID int,
	refs []db.ShowEpisodeRef,
	metadataReviewNeeded bool,
	metadataConfirmed bool,
) (int, error) {
	if h.SeriesQuery == nil || seriesTMDBID <= 0 || len(refs) == 0 {
		return 0, nil
	}
	table := db.MediaTableForKind(refs[0].Kind)
	seriesID := strconv.Itoa(seriesTMDBID)
	var canonical db.CanonicalMetadata
	if h.Series != nil {
		if details, err := h.Series.GetSeriesDetails(ctx, seriesTMDBID); err == nil && details != nil {
			canonical = db.CanonicalMetadata{
				Title:        details.Name,
				Overview:     details.Overview,
				PosterPath:   details.PosterPath,
				BackdropPath: details.BackdropPath,
				ReleaseDate:  details.FirstAirDate,
				IMDbID:       details.IMDbID,
				IMDbRating:   details.IMDbRating,
			}
		}
	}
	updated := 0
	for _, ref := range refs {
		ep, err := h.SeriesQuery.GetEpisode(ctx, "tmdb", seriesID, ref.Season, ref.Episode)
		if err != nil || ep == nil {
			continue
		}
		tvdbID := ""
		if ep.Provider == "tvdb" {
			tvdbID = ep.ExternalID
		}
		if err := db.UpdateMediaMetadataWithCanonicalState(h.DB, table, ref.RefID, ep.Title, ep.Overview, ep.PosterURL, ep.BackdropURL, ep.ReleaseDate, ep.VoteAverage, ep.IMDbID, ep.IMDbRating, seriesTMDBID, tvdbID, ref.Season, ref.Episode, canonical, metadataReviewNeeded, metadataConfirmed); err != nil {
			continue
		}
		updated++
		time.Sleep(identifyRateLimitMs * time.Millisecond)
	}
	return updated, nil
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

func sortIdentifyJobs(jobs []identifyJob, libraryPath string) {
	sort.SliceStable(jobs, func(i, j int) bool {
		a := identifyJobPriority(jobs[i], libraryPath)
		b := identifyJobPriority(jobs[j], libraryPath)
		if a != b {
			return a < b
		}
		if jobs[i].row.Kind != jobs[j].row.Kind {
			return jobs[i].row.Kind < jobs[j].row.Kind
		}
		if jobs[i].row.Season != jobs[j].row.Season {
			return jobs[i].row.Season < jobs[j].row.Season
		}
		if jobs[i].row.Episode != jobs[j].row.Episode {
			return jobs[i].row.Episode < jobs[j].row.Episode
		}
		return jobs[i].row.Path < jobs[j].row.Path
	})
}

func identifyJobPriority(job identifyJob, libraryPath string) int {
	info := identifyMediaInfo(job.row, libraryPath)
	switch job.row.Kind {
	case db.LibraryTypeMovie:
		if info.TMDBID > 0 || info.TVDBID != "" {
			return 0
		}
		if info.Year > 0 {
			return 1
		}
		return 2
	case db.LibraryTypeTV, db.LibraryTypeAnime:
		season := info.Season
		if season == 0 {
			season = job.row.Season
		}
		episode := info.Episode
		if episode == 0 {
			episode = job.row.Episode
		}
		if (season == 1 || season == 0) && episode == 1 {
			return 0
		}
		if episode > 0 && episode <= 3 {
			return 1
		}
		return 2
	default:
		return 3
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
	subpaths, err := requestedScanSubpaths(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id metadata.Identifier
	if identify {
		id = h.Meta
	}

	var musicIdentifier metadata.MusicIdentifier
	if detected, ok := id.(metadata.MusicIdentifier); ok {
		musicIdentifier = detected
	}
	added, err := db.HandleScanLibraryWithOptions(r.Context(), h.DB, path, typ, libraryID, db.ScanOptions{
		Identifier:             id,
		MusicIdentifier:        musicIdentifier,
		ProbeMedia:             true,
		ProbeEmbeddedSubtitles: true,
		ScanSidecarSubtitles:   true,
		Subpaths:               subpaths,
	})
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
	subpaths, err := requestedScanSubpaths(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	status := h.ScanJobs.start(libraryID, path, typ, r.URL.Query().Get("identify") != "false", subpaths)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func requestedScanSubpaths(r *http.Request) ([]string, error) {
	subpath := strings.TrimSpace(r.URL.Query().Get("subpath"))
	if subpath == "" {
		return nil, nil
	}
	return db.NormalizeScanSubpaths([]string{subpath})
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
	if identifyStates := h.identifyRun.stateForLibrary(libraryID); len(identifyStates) > 0 {
		for i := range items {
			if state, ok := identifyStates[identifyRowKey(items[i].Type, items[i].Path)]; ok {
				items[i].IdentifyState = state
			}
		}
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

func (h *LibraryHandler) GetDiscover(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.Discover == nil {
		http.Error(w, metadata.ErrTMDBNotConfigured.Error(), http.StatusServiceUnavailable)
		return
	}
	payload, err := h.Discover.GetDiscover(r.Context())
	if err != nil {
		status, message := discoverHTTPStatus(err)
		http.Error(w, message, status)
		return
	}
	if payload == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	for i := range payload.Shelves {
		if err := db.AttachDiscoverLibraryMatches(h.DB, u.ID, payload.Shelves[i].Items); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *LibraryHandler) SearchDiscover(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&metadata.DiscoverSearchResponse{
			Movies: []metadata.DiscoverItem{},
			TV:     []metadata.DiscoverItem{},
		})
		return
	}
	if h.Discover == nil {
		http.Error(w, metadata.ErrTMDBNotConfigured.Error(), http.StatusServiceUnavailable)
		return
	}
	payload, err := h.Discover.SearchDiscover(r.Context(), query)
	if err != nil {
		status, message := discoverHTTPStatus(err)
		http.Error(w, message, status)
		return
	}
	if payload == nil {
		payload = &metadata.DiscoverSearchResponse{
			Movies: []metadata.DiscoverItem{},
			TV:     []metadata.DiscoverItem{},
		}
	}
	if err := db.AttachDiscoverLibraryMatches(h.DB, u.ID, payload.Movies); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := db.AttachDiscoverLibraryMatches(h.DB, u.ID, payload.TV); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *LibraryHandler) GetDiscoverTitleDetails(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.Discover == nil {
		http.Error(w, metadata.ErrTMDBNotConfigured.Error(), http.StatusServiceUnavailable)
		return
	}

	mediaType := metadata.DiscoverMediaType(strings.TrimSpace(chi.URLParam(r, "mediaType")))
	if mediaType != metadata.DiscoverMediaTypeMovie && mediaType != metadata.DiscoverMediaTypeTV {
		http.Error(w, "invalid media type", http.StatusBadRequest)
		return
	}
	tmdbID, err := strconv.Atoi(chi.URLParam(r, "tmdbId"))
	if err != nil || tmdbID <= 0 {
		http.Error(w, "invalid tmdb id", http.StatusBadRequest)
		return
	}

	details, err := h.Discover.GetDiscoverTitleDetails(r.Context(), mediaType, tmdbID)
	if err != nil {
		status, message := discoverHTTPStatus(err)
		http.Error(w, message, status)
		return
	}
	if details == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := db.AttachDiscoverTitleLibraryMatches(h.DB, u.ID, details); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(details)
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
	if h.SeriesQuery == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
		return
	}
	results, err := h.SeriesQuery.SearchTV(r.Context(), q)
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

type confirmShowRequest struct {
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
	// Use first episode's TMDB ID (series id) for the show.
	seriesTMDBID := refs[0].TMDBID
	if h.SeriesQuery == nil || seriesTMDBID <= 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(showActionResult{Updated: 0})
		return
	}
	updated, _ := h.applyTMDBSeriesToRefs(r.Context(), seriesTMDBID, refs, false, true)
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
	if h.SeriesQuery == nil {
		http.Error(w, "metadata not configured", http.StatusServiceUnavailable)
		return
	}
	updated, _ := h.applyTMDBSeriesToRefs(r.Context(), payload.TmdbID, refs, false, true)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(showActionResult{Updated: updated})
}

func (h *LibraryHandler) ConfirmShow(w http.ResponseWriter, r *http.Request) {
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
	var payload confirmShowRequest
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
	refIDs := make([]int, 0, len(refs))
	for _, ref := range refs {
		refIDs = append(refIDs, ref.RefID)
	}
	updated, err := db.UpdateShowMetadataState(h.DB, db.MediaTableForKind(refs[0].Kind), refIDs, false, true)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(showActionResult{Updated: updated})
}
