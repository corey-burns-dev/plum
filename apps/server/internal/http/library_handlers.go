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
	Movies      metadata.MovieDetailsProvider
	Series      metadata.SeriesDetailsProvider
	SeriesQuery metadata.SeriesSearchProvider
	Discover    metadata.DiscoverProvider
	ScanJobs    *LibraryScanManager
	SearchIndex *SearchIndexManager
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
	Name                string `json:"name"`
	Type                string `json:"type"`
	Path                string `json:"path"`
	WatcherEnabled      *bool  `json:"watcher_enabled,omitempty"`
	WatcherMode         string `json:"watcher_mode,omitempty"`
	ScanIntervalMinutes *int   `json:"scan_interval_minutes,omitempty"`
}

type updateLibraryPlaybackPreferencesRequest struct {
	PreferredAudioLanguage    string `json:"preferred_audio_language"`
	PreferredSubtitleLanguage string `json:"preferred_subtitle_language"`
	SubtitlesEnabledByDefault bool   `json:"subtitles_enabled_by_default"`
	WatcherEnabled            *bool  `json:"watcher_enabled,omitempty"`
	WatcherMode               string `json:"watcher_mode,omitempty"`
	ScanIntervalMinutes       *int   `json:"scan_interval_minutes,omitempty"`
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
	WatcherEnabled            bool   `json:"watcher_enabled"`
	WatcherMode               string `json:"watcher_mode,omitempty"`
	ScanIntervalMinutes       int    `json:"scan_interval_minutes"`
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

func defaultLibraryAutomation() (watcherEnabled bool, watcherMode string, scanIntervalMinutes int) {
	return false, db.LibraryWatcherModeAuto, 0
}

func normalizeLibraryAutomationWithDefaults(
	defaultEnabled bool,
	defaultMode string,
	defaultInterval int,
	watcherEnabled *bool,
	watcherMode string,
	scanIntervalMinutes *int,
) (bool, string, int) {
	enabled := defaultEnabled
	if watcherEnabled != nil {
		enabled = *watcherEnabled
	}
	mode := strings.TrimSpace(strings.ToLower(watcherMode))
	if mode == "" {
		mode = defaultMode
	}
	if mode != db.LibraryWatcherModeAuto && mode != db.LibraryWatcherModePoll {
		mode = defaultMode
	}
	interval := defaultInterval
	if scanIntervalMinutes != nil && *scanIntervalMinutes > 0 {
		interval = *scanIntervalMinutes
	}
	return enabled, mode, interval
}

func normalizeLibraryAutomation(
	watcherEnabled *bool,
	watcherMode string,
	scanIntervalMinutes *int,
) (bool, string, int) {
	defaultEnabled, defaultMode, defaultInterval := defaultLibraryAutomation()
	return normalizeLibraryAutomationWithDefaults(defaultEnabled, defaultMode, defaultInterval, watcherEnabled, watcherMode, scanIntervalMinutes)
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
	watcherEnabled sql.NullBool,
	watcherMode sql.NullString,
	scanIntervalMinutes sql.NullInt64,
) libraryResponse {
	defaultAudio, defaultSubtitle, defaultSubtitlesEnabled := defaultLibraryPlaybackPreferences(libraryType)
	defaultWatcherEnabled, defaultWatcherMode, defaultScanIntervalMinutes := defaultLibraryAutomation()
	return libraryResponse{
		ID:                        id,
		Name:                      name,
		Type:                      libraryType,
		Path:                      path,
		UserID:                    userID,
		PreferredAudioLanguage:    strings.TrimSpace(coalesceNullableString(preferredAudio, defaultAudio)),
		PreferredSubtitleLanguage: strings.TrimSpace(coalesceNullableString(preferredSubtitle, defaultSubtitle)),
		SubtitlesEnabledByDefault: coalesceNullableBool(subtitlesEnabled, defaultSubtitlesEnabled),
		WatcherEnabled:            coalesceNullableBool(watcherEnabled, defaultWatcherEnabled),
		WatcherMode:               strings.TrimSpace(coalesceNullableString(watcherMode, defaultWatcherMode)),
		ScanIntervalMinutes:       coalesceNullableInt(scanIntervalMinutes, defaultScanIntervalMinutes),
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

func coalesceNullableInt(value sql.NullInt64, fallback int) int {
	if value.Valid {
		return int(value.Int64)
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
	watcherEnabled, watcherMode, scanIntervalMinutes := normalizeLibraryAutomation(
		payload.WatcherEnabled,
		payload.WatcherMode,
		payload.ScanIntervalMinutes,
	)
	err := retryCreateLibraryInsert(
		h.DB,
		u.ID,
		payload,
		defaultAudio,
		defaultSubtitle,
		subtitlesEnabled,
		watcherEnabled,
		watcherMode,
		scanIntervalMinutes,
		now,
		&libID,
	)
	if err != nil {
		log.Printf("create library: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if h.ScanJobs != nil {
		h.ScanJobs.ConfigureLibraryAutomation(libID, payload.Path, payload.Type, watcherEnabled, watcherMode, scanIntervalMinutes)
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
		WatcherEnabled:            watcherEnabled,
		WatcherMode:               watcherMode,
		ScanIntervalMinutes:       scanIntervalMinutes,
	})
}

func retryCreateLibraryInsert(
	dbConn *sql.DB,
	userID int,
	payload createLibraryRequest,
	defaultAudio string,
	defaultSubtitle string,
	subtitlesEnabled bool,
	watcherEnabled bool,
	watcherMode string,
	scanIntervalMinutes int,
	now time.Time,
	libID *int,
) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		err = dbConn.QueryRow(
			`INSERT INTO libraries (
				user_id, name, type, path, preferred_audio_language, preferred_subtitle_language,
				subtitles_enabled_by_default, watcher_enabled, watcher_mode, scan_interval_minutes, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			userID, payload.Name, payload.Type, payload.Path, defaultAudio, defaultSubtitle, subtitlesEnabled, watcherEnabled, watcherMode, scanIntervalMinutes, now,
		).Scan(libID)
		if isMissingColumnError(err, "preferred_audio_language") {
			err = dbConn.QueryRow(
				`INSERT INTO libraries (user_id, name, type, path, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
				userID, payload.Name, payload.Type, payload.Path, now,
			).Scan(libID)
		} else if isMissingColumnError(err, "watcher_enabled") {
			err = dbConn.QueryRow(
				`INSERT INTO libraries (
					user_id, name, type, path, preferred_audio_language, preferred_subtitle_language,
					subtitles_enabled_by_default, created_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
				userID, payload.Name, payload.Type, payload.Path, defaultAudio, defaultSubtitle, subtitlesEnabled, now,
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
		`SELECT id, name, type, path, user_id, preferred_audio_language, preferred_subtitle_language,
		        subtitles_enabled_by_default, watcher_enabled, watcher_mode, scan_interval_minutes
		   FROM libraries WHERE user_id = ? ORDER BY id`,
		u.ID,
	)
	legacyColumns := false
	legacyAutomationColumns := false
	if err != nil && isMissingColumnError(err, "preferred_audio_language") {
		legacyColumns = true
		rows, err = h.DB.Query(
			`SELECT id, name, type, path, user_id FROM libraries WHERE user_id = ? ORDER BY id`,
			u.ID,
		)
	} else if err != nil && isMissingColumnError(err, "watcher_enabled") {
		legacyAutomationColumns = true
		rows, err = h.DB.Query(
			`SELECT id, name, type, path, user_id, preferred_audio_language, preferred_subtitle_language, subtitles_enabled_by_default
			   FROM libraries WHERE user_id = ? ORDER BY id`,
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
			watcherEnabled    sql.NullBool
			watcherMode       sql.NullString
			scanInterval      sql.NullInt64
		)
		if legacyColumns {
			err = rows.Scan(&id, &name, &libraryType, &path, &userID)
		} else if legacyAutomationColumns {
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
				&watcherEnabled,
				&watcherMode,
				&scanInterval,
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
			watcherEnabled,
			watcherMode,
			scanInterval,
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
		libraryID             int
		ownerID               int
		name                  string
		libraryType           string
		path                  string
		currentWatcherEnabled sql.NullBool
		currentWatcherMode    sql.NullString
		currentScanInterval   sql.NullInt64
	)
	err := h.DB.QueryRow(
		`SELECT id, user_id, name, type, path, watcher_enabled, watcher_mode, scan_interval_minutes FROM libraries WHERE id = ?`,
		idStr,
	).Scan(&libraryID, &ownerID, &name, &libraryType, &path, &currentWatcherEnabled, &currentWatcherMode, &currentScanInterval)
	legacyAutomationColumns := false
	if err != nil && isMissingColumnError(err, "watcher_enabled") {
		legacyAutomationColumns = true
		err = h.DB.QueryRow(
			`SELECT id, user_id, name, type, path FROM libraries WHERE id = ?`,
			idStr,
		).Scan(&libraryID, &ownerID, &name, &libraryType, &path)
	}
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if ownerID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if legacyAutomationColumns {
		currentWatcherEnabled = sql.NullBool{}
		currentWatcherMode = sql.NullString{}
		currentScanInterval = sql.NullInt64{}
	}
	defaultWatcherEnabled, defaultWatcherMode, defaultScanInterval := defaultLibraryAutomation()
	watcherEnabled, watcherMode, scanIntervalMinutes := normalizeLibraryAutomationWithDefaults(
		coalesceNullableBool(currentWatcherEnabled, defaultWatcherEnabled),
		coalesceNullableString(currentWatcherMode, defaultWatcherMode),
		coalesceNullableInt(currentScanInterval, defaultScanInterval),
		payload.WatcherEnabled,
		payload.WatcherMode,
		payload.ScanIntervalMinutes,
	)

	if _, err := h.DB.Exec(
		`UPDATE libraries
		    SET preferred_audio_language = ?, preferred_subtitle_language = ?, subtitles_enabled_by_default = ?,
		        watcher_enabled = ?, watcher_mode = ?, scan_interval_minutes = ?
		  WHERE id = ?`,
		payload.PreferredAudioLanguage,
		payload.PreferredSubtitleLanguage,
		payload.SubtitlesEnabledByDefault,
		watcherEnabled,
		watcherMode,
		scanIntervalMinutes,
		libraryID,
	); err != nil {
		if isMissingColumnError(err, "watcher_enabled") {
			if _, err := h.DB.Exec(
				`UPDATE libraries SET preferred_audio_language = ?, preferred_subtitle_language = ?, subtitles_enabled_by_default = ? WHERE id = ?`,
				payload.PreferredAudioLanguage,
				payload.PreferredSubtitleLanguage,
				payload.SubtitlesEnabledByDefault,
				libraryID,
			); err == nil {
				goto encodeLibraryResponse
			}
		}
		if !isMissingColumnError(err, "preferred_audio_language") {
			log.Printf("update library playback preferences: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if h.ScanJobs != nil {
		h.ScanJobs.ConfigureLibraryAutomation(libraryID, path, libraryType, watcherEnabled, watcherMode, scanIntervalMinutes)
	}

encodeLibraryResponse:
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
		WatcherEnabled:            watcherEnabled,
		WatcherMode:               watcherMode,
		ScanIntervalMinutes:       scanIntervalMinutes,
	})
}

type scanResult = db.ScanResult

type identifyResult struct {
	Identified int `json:"identified"`
	Failed     int `json:"failed"`
}

var (
	identifyInitialTimeout   = 8 * time.Second
	identifyRetryTimeout     = 45 * time.Second
	identifyMovieWorkers     = 6
	identifyMovieRateLimit   = 100 * time.Millisecond
	identifyMovieRateBurst   = 6
	identifyEpisodeWorkers   = 4
	identifyEpisodeRateLimit = 100 * time.Millisecond
	identifyEpisodeRateBurst = 4
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

type identifyRunConfig struct {
	workers      int
	rateInterval time.Duration
	rateBurst    int
}

type episodeIdentifyGroup struct {
	key             string
	kind            string
	groupQuery      string
	fallbackQueries []string
	explicitTMDBID  int
	explicitTVDBID  string
	attempt         int
	representative  db.EpisodeIdentifyRow
	rows            []db.EpisodeIdentifyRow
}

type episodeGroupJob struct {
	group   episodeIdentifyGroup
	attempt int
}

type episodeGroupResult struct {
	group      episodeIdentifyGroup
	identified int
	retry      bool
	failed     []identifyJobResult
}

type episodeSearchCache struct {
	mu    sync.Mutex
	calls map[string]*episodeSearchCall
}

type episodeSearchCall struct {
	done    chan struct{}
	results []metadata.MatchResult
	err     error
}

type episodeSeriesDetailsCache struct {
	mu    sync.Mutex
	calls map[int]*episodeSeriesDetailsCall
}

type episodeSeriesDetailsCall struct {
	done    chan struct{}
	details *metadata.SeriesDetails
	err     error
}

type episodeLookupCache struct {
	mu    sync.Mutex
	calls map[string]*episodeLookupCall
}

type episodeLookupCall struct {
	done  chan struct{}
	match *metadata.MatchResult
	err   error
}

type episodicIdentifyCache struct {
	handler      *LibraryHandler
	searchCache  *episodeSearchCache
	detailsCache *episodeSeriesDetailsCache
	episodeCache *episodeLookupCache
}

type movieIdentifyCall struct {
	done chan struct{}
	res  *metadata.MatchResult
}

type movieIdentifyCache struct {
	mu    sync.Mutex
	calls map[string]*movieIdentifyCall
}

func newMovieIdentifyCache() *movieIdentifyCache {
	return &movieIdentifyCache{calls: make(map[string]*movieIdentifyCall)}
}

func (c *movieIdentifyCache) lookupOrRun(key string, fn func() *metadata.MatchResult) *metadata.MatchResult {
	if c == nil || key == "" {
		return fn()
	}

	c.mu.Lock()
	if call, ok := c.calls[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.res
	}
	call := &movieIdentifyCall{done: make(chan struct{})}
	c.calls[key] = call
	c.mu.Unlock()

	call.res = fn()
	close(call.done)

	c.mu.Lock()
	if call.res == nil {
		delete(c.calls, key)
	}
	c.mu.Unlock()
	return call.res
}

func newEpisodicIdentifyCache(handler *LibraryHandler) *episodicIdentifyCache {
	return &episodicIdentifyCache{
		handler: handler,
		searchCache: &episodeSearchCache{
			calls: make(map[string]*episodeSearchCall),
		},
		detailsCache: &episodeSeriesDetailsCache{
			calls: make(map[int]*episodeSeriesDetailsCall),
		},
		episodeCache: &episodeLookupCache{
			calls: make(map[string]*episodeLookupCall),
		},
	}
}

func (c *episodeSearchCache) lookupOrRun(key string, fn func() ([]metadata.MatchResult, error)) ([]metadata.MatchResult, error) {
	if c == nil || key == "" {
		return fn()
	}
	c.mu.Lock()
	if call, ok := c.calls[key]; ok {
		c.mu.Unlock()
		<-call.done
		return append([]metadata.MatchResult(nil), call.results...), call.err
	}
	call := &episodeSearchCall{done: make(chan struct{})}
	c.calls[key] = call
	c.mu.Unlock()

	call.results, call.err = fn()
	close(call.done)

	c.mu.Lock()
	if call.err != nil || len(call.results) == 0 {
		delete(c.calls, key)
	}
	c.mu.Unlock()

	return append([]metadata.MatchResult(nil), call.results...), call.err
}

func (c *episodeSeriesDetailsCache) lookupOrRun(key int, fn func() (*metadata.SeriesDetails, error)) (*metadata.SeriesDetails, error) {
	if c == nil || key <= 0 {
		return fn()
	}
	c.mu.Lock()
	if call, ok := c.calls[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.details, call.err
	}
	call := &episodeSeriesDetailsCall{done: make(chan struct{})}
	c.calls[key] = call
	c.mu.Unlock()

	call.details, call.err = fn()
	close(call.done)

	c.mu.Lock()
	if call.err != nil || call.details == nil {
		delete(c.calls, key)
	}
	c.mu.Unlock()

	return call.details, call.err
}

func (c *episodeLookupCache) lookupOrRun(key string, fn func() (*metadata.MatchResult, error)) (*metadata.MatchResult, error) {
	if c == nil || key == "" {
		return fn()
	}
	c.mu.Lock()
	if call, ok := c.calls[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.match, call.err
	}
	call := &episodeLookupCall{done: make(chan struct{})}
	c.calls[key] = call
	c.mu.Unlock()

	call.match, call.err = fn()
	close(call.done)

	c.mu.Lock()
	if call.err != nil || call.match == nil {
		delete(c.calls, key)
	}
	c.mu.Unlock()

	return call.match, call.err
}

func (c *episodicIdentifyCache) SearchTV(ctx context.Context, query string) ([]metadata.MatchResult, error) {
	if c == nil || c.handler == nil || c.handler.SeriesQuery == nil {
		return nil, nil
	}
	return c.searchCache.lookupOrRun(strings.ToLower(strings.TrimSpace(query)), func() ([]metadata.MatchResult, error) {
		return c.handler.SeriesQuery.SearchTV(ctx, query)
	})
}

func (c *episodicIdentifyCache) GetSeriesDetails(ctx context.Context, tmdbID int) (*metadata.SeriesDetails, error) {
	if c == nil || c.handler == nil || c.handler.Series == nil {
		return nil, nil
	}
	return c.detailsCache.lookupOrRun(tmdbID, func() (*metadata.SeriesDetails, error) {
		return c.handler.Series.GetSeriesDetails(ctx, tmdbID)
	})
}

func (c *episodicIdentifyCache) GetEpisode(ctx context.Context, provider, seriesID string, season, episode int) (*metadata.MatchResult, error) {
	if c == nil || c.handler == nil || c.handler.SeriesQuery == nil {
		return nil, nil
	}
	key := provider + ":" + seriesID + ":" + strconv.Itoa(season) + ":" + strconv.Itoa(episode)
	return c.episodeCache.lookupOrRun(key, func() (*metadata.MatchResult, error) {
		return c.handler.SeriesQuery.GetEpisode(ctx, provider, seriesID, season, episode)
	})
}

func identifyConfigForKind(kind string) identifyRunConfig {
	if kind == db.LibraryTypeMovie {
		return identifyRunConfig{
			workers:      identifyMovieWorkers,
			rateInterval: identifyMovieRateLimit,
			rateBurst:    identifyMovieRateBurst,
		}
	}
	return identifyRunConfig{
		workers:      identifyEpisodeWorkers,
		rateInterval: identifyEpisodeRateLimit,
		rateBurst:    identifyEpisodeRateBurst,
	}
}

func identificationRowsFromEpisodeRows(rows []db.EpisodeIdentifyRow) []db.IdentificationRow {
	out := make([]db.IdentificationRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.IdentificationRow)
	}
	return out
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
	var libraryPath string
	_ = h.DB.QueryRow(`SELECT path FROM libraries WHERE id = ?`, libraryID).Scan(&libraryPath)
	var libraryType string
	if err := h.DB.QueryRow(`SELECT type FROM libraries WHERE id = ?`, libraryID).Scan(&libraryType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.identifyRun.clearLibrary(libraryID)
			return identifyResult{Identified: 0, Failed: 0}, nil
		}
		return identifyResult{}, err
	}

	if libraryType == db.LibraryTypeTV || libraryType == db.LibraryTypeAnime {
		rows, err := db.ListEpisodeIdentifyRowsByLibrary(h.DB, libraryID)
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
		h.identifyRun.startLibrary(libraryID, identificationRowsFromEpisodeRows(rows))
		defer h.identifyRun.finishLibrary(libraryID)
		return h.identifyEpisodesByGroup(ctx, libraryID, libraryPath, libraryType, rows)
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

	identified, failed := 0, 0
	initialJobs := make([]identifyJob, 0, len(rows))
	for _, row := range rows {
		initialJobs = append(initialJobs, identifyJob{row: row})
	}
	sortIdentifyJobs(initialJobs, libraryPath)
	var movieCache *movieIdentifyCache
	if rows[0].Kind == db.LibraryTypeMovie {
		movieCache = newMovieIdentifyCache()
	}
	initialIdentified, retryJobs, initialFailed := h.runIdentifyJobs(ctx, libraryID, libraryPath, initialJobs, movieCache, true)
	retryIdentified, _, retryFailed := h.runIdentifyJobs(ctx, libraryID, libraryPath, retryJobs, movieCache, true)
	identified += initialIdentified + retryIdentified

	fallbackIdentified, fallbackFailed := h.identifyShowFallbacks(ctx, libraryID, libraryPath, append(initialFailed, retryFailed...), nil, true)
	identified += fallbackIdentified
	failed += fallbackFailed

	return identifyResult{Identified: identified, Failed: failed}, nil
}

func (h *LibraryHandler) runIdentifyJobs(
	ctx context.Context,
	libraryID int,
	libraryPath string,
	jobsToRun []identifyJob,
	movieCache *movieIdentifyCache,
	queueSearch bool,
) (identified int, retryJobs []identifyJob, failed []identifyJobResult) {
	if len(jobsToRun) == 0 {
		return 0, nil, nil
	}

	results := make(chan identifyJobResult, len(jobsToRun))
	jobs := make(chan identifyJob)
	config := identifyConfigForKind(jobsToRun[0].row.Kind)
	workerCount := config.workers
	if workerCount > len(jobsToRun) {
		workerCount = len(jobsToRun)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	rateLimiter := newIdentifyRateLimiter(ctx, config.rateInterval, config.rateBurst)

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
					results <- h.identifyLibraryJob(ctx, libraryID, job, libraryPath, rateLimiter, movieCache, queueSearch)
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
	cache *episodicIdentifyCache,
	queueSearch bool,
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
		updated, err := h.identifyShowFallbackGroup(ctx, libraryPath, group.queries, group.rows, cache, queueSearch)
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

func episodeGroupKey(row db.EpisodeIdentifyRow, libraryPath string) (string, string, []string) {
	if row.Season <= 0 || row.Episode <= 0 {
		return "", "", nil
	}
	info := identifyMediaInfo(row.IdentificationRow, libraryPath)
	title := strings.TrimSpace(info.Title)
	queries := showFallbackQueries(row.IdentificationRow, libraryPath)
	if row.TMDBID > 0 {
		return "tmdb:" + strconv.Itoa(row.TMDBID), "", nil
	}
	if row.TVDBID != "" {
		return "tvdb:" + row.TVDBID, title, queries
	}
	if title != "" {
		return "title:" + metadata.NormalizeSeriesTitle(title), title, queries
	}
	if len(queries) > 0 {
		return "fallback:" + strings.ToLower(queries[0]), queries[0], queries
	}
	return "", "", nil
}

func buildEpisodeIdentifyGroups(rows []db.EpisodeIdentifyRow, libraryPath string) ([]episodeIdentifyGroup, []identifyJob) {
	groupsByKey := make(map[string]*episodeIdentifyGroup)
	order := make([]string, 0, len(rows))
	residual := make([]identifyJob, 0)
	for _, row := range rows {
		key, query, fallbackQueries := episodeGroupKey(row, libraryPath)
		if key == "" {
			residual = append(residual, identifyJob{row: row.IdentificationRow})
			continue
		}
		group, ok := groupsByKey[key]
		if !ok {
			group = &episodeIdentifyGroup{
				key:             key,
				kind:            row.Kind,
				groupQuery:      strings.TrimSpace(query),
				fallbackQueries: append([]string(nil), fallbackQueries...),
				explicitTMDBID:  row.TMDBID,
				explicitTVDBID:  row.TVDBID,
				representative:  row,
			}
			groupsByKey[key] = group
			order = append(order, key)
		}
		group.rows = append(group.rows, row)
		if row.TMDBID > 0 && group.explicitTMDBID == 0 {
			group.explicitTMDBID = row.TMDBID
		}
		if row.TVDBID != "" && group.explicitTVDBID == "" {
			group.explicitTVDBID = row.TVDBID
		}
		if group.groupQuery == "" && query != "" {
			group.groupQuery = query
		}
		if len(group.fallbackQueries) == 0 && len(fallbackQueries) > 0 {
			group.fallbackQueries = append([]string(nil), fallbackQueries...)
		}
	}

	groups := make([]episodeIdentifyGroup, 0, len(order))
	for _, key := range order {
		group := groupsByKey[key]
		if len(group.rows) < 2 && group.explicitTMDBID == 0 && group.explicitTVDBID == "" {
			residual = append(residual, identifyJob{row: group.rows[0].IdentificationRow})
			continue
		}
		sort.SliceStable(group.rows, func(i, j int) bool {
			if group.rows[i].Season != group.rows[j].Season {
				return group.rows[i].Season < group.rows[j].Season
			}
			if group.rows[i].Episode != group.rows[j].Episode {
				return group.rows[i].Episode < group.rows[j].Episode
			}
			return group.rows[i].Path < group.rows[j].Path
		})
		group.representative = group.rows[0]
		groups = append(groups, *group)
	}
	sortIdentifyJobs(residual, libraryPath)
	return groups, residual
}

func identifyGroupRowsAsQueued(tracker *identifyRunTracker, libraryID int, rows []db.EpisodeIdentifyRow) {
	if tracker == nil {
		return
	}
	for _, row := range rows {
		tracker.setState(libraryID, row.Kind, row.Path, "queued")
	}
}

func identifyGroupRowsAsIdentifying(tracker *identifyRunTracker, libraryID int, rows []db.EpisodeIdentifyRow) {
	if tracker == nil {
		return
	}
	for _, row := range rows {
		tracker.setState(libraryID, row.Kind, row.Path, "identifying")
	}
}

func identifyGroupRowsClear(tracker *identifyRunTracker, libraryID int, rows []db.EpisodeIdentifyRow) {
	if tracker == nil {
		return
	}
	for _, row := range rows {
		tracker.setState(libraryID, row.Kind, row.Path, "")
	}
}

func identifyGroupRowsFail(tracker *identifyRunTracker, libraryID int, rows []db.EpisodeIdentifyRow) {
	if tracker == nil {
		return
	}
	for _, row := range rows {
		tracker.setState(libraryID, row.Kind, row.Path, "failed")
	}
}

func episodeIdentifyFailedResults(group episodeIdentifyGroup) []identifyJobResult {
	out := make([]identifyJobResult, 0, len(group.rows))
	for _, row := range group.rows {
		out = append(out, identifyJobResult{
			status:           identifyJobFailed,
			job:              identifyJob{row: row.IdentificationRow},
			fallbackEligible: true,
		})
	}
	return out
}

func episodeIdentifyFallbackResultsFromJobs(results []identifyJobResult) []identifyJobResult {
	out := make([]identifyJobResult, 0, len(results))
	for _, result := range results {
		if result.status != identifyJobFailed {
			continue
		}
		out = append(out, result)
	}
	return out
}

func (h *LibraryHandler) identifyEpisodesByGroup(
	ctx context.Context,
	libraryID int,
	libraryPath string,
	libraryType string,
	rows []db.EpisodeIdentifyRow,
) (identifyResult, error) {
	groups, residualJobs := buildEpisodeIdentifyGroups(rows, libraryPath)
	cache := newEpisodicIdentifyCache(h)

	identified := 0
	failedResults := make([]identifyJobResult, 0)

	groupIdentified, retryGroups, groupFailed := h.runEpisodeIdentifyGroups(ctx, libraryID, libraryPath, groups, cache)
	identified += groupIdentified
	failedResults = append(failedResults, groupFailed...)

	retryIdentified, unresolvedGroups, retryFailed := h.runEpisodeIdentifyGroups(ctx, libraryID, libraryPath, retryGroups, cache)
	identified += retryIdentified
	failedResults = append(failedResults, retryFailed...)
	for _, group := range unresolvedGroups {
		failedResults = append(failedResults, episodeIdentifyFailedResults(group)...)
	}

	residualIdentified, residualRetryJobs, residualInitialFailed := h.runIdentifyJobs(ctx, libraryID, libraryPath, residualJobs, nil, false)
	identified += residualIdentified
	residualRetryIdentified, _, residualRetryFailed := h.runIdentifyJobs(ctx, libraryID, libraryPath, residualRetryJobs, nil, false)
	identified += residualRetryIdentified
	failedResults = append(failedResults, residualInitialFailed...)
	failedResults = append(failedResults, residualRetryFailed...)

	fallbackIdentified, fallbackFailed := h.identifyShowFallbacks(ctx, libraryID, libraryPath, failedResults, cache, false)
	identified += fallbackIdentified
	if identified > 0 && h.SearchIndex != nil {
		h.SearchIndex.Queue(libraryID, false)
	}
	return identifyResult{Identified: identified, Failed: fallbackFailed}, nil
}

func (h *LibraryHandler) runEpisodeIdentifyGroups(
	ctx context.Context,
	libraryID int,
	libraryPath string,
	groups []episodeIdentifyGroup,
	cache *episodicIdentifyCache,
) (identified int, retryGroups []episodeIdentifyGroup, failed []identifyJobResult) {
	if len(groups) == 0 {
		return 0, nil, nil
	}

	results := make(chan episodeGroupResult, len(groups))
	groupJobs := make(chan episodeGroupJob)
	config := identifyConfigForKind(groups[0].kind)
	workerCount := config.workers
	if workerCount > len(groups) {
		workerCount = len(groups)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	rateLimiter := newIdentifyRateLimiter(ctx, config.rateInterval, config.rateBurst)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-groupJobs:
					if !ok {
						return
					}
					groupIdentified, retry, groupFailed := h.identifyEpisodeGroup(ctx, libraryID, libraryPath, job, cache, rateLimiter)
					results <- episodeGroupResult{
						group:      job.group,
						identified: groupIdentified,
						retry:      retry,
						failed:     groupFailed,
					}
				}
			}
		}()
	}

	go func() {
		defer close(results)
		wg.Wait()
	}()

enqueueLoop:
	for _, group := range groups {
		select {
		case <-ctx.Done():
			break enqueueLoop
		case groupJobs <- episodeGroupJob{group: group, attempt: group.attempt}:
		}
	}
	close(groupJobs)

	for result := range results {
		identified += result.identified
		if result.retry {
			next := result.group
			next.attempt++
			retryGroups = append(retryGroups, next)
		}
		failed = append(failed, result.failed...)
	}

	return identified, retryGroups, failed
}

type tmdbSeriesSelection struct {
	tmdbID               int
	metadataReviewNeeded bool
}

func fallbackIdentifyInfo(row db.IdentificationRow, libraryPath string) metadata.MediaInfo {
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
	return info
}

func scoredTMDBSeriesMatch(
	candidates []metadata.MatchResult,
	info metadata.MediaInfo,
) (best *metadata.MatchResult, topScore int, secondScore int, hasSecond bool) {
	type scored struct {
		match *metadata.MatchResult
		score int
	}
	scores := make([]scored, 0, len(candidates))
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Provider != "tmdb" {
			continue
		}
		if tmdbID, err := strconv.Atoi(candidate.ExternalID); err != nil || tmdbID <= 0 {
			continue
		}
		scores = append(scores, scored{
			match: candidate,
			score: metadata.ScoreTV(candidate, info),
		})
	}
	if len(scores) == 0 {
		return nil, 0, 0, false
	}
	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})
	best = scores[0].match
	topScore = scores[0].score
	if len(scores) > 1 {
		secondScore = scores[1].score
		hasSecond = true
	}
	return best, topScore, secondScore, hasSecond
}

func (h *LibraryHandler) selectTMDBSeriesFallback(
	ctx context.Context,
	libraryPath string,
	representative db.IdentificationRow,
	queries []string,
	cache *episodicIdentifyCache,
) (tmdbSeriesSelection, error) {
	info := fallbackIdentifyInfo(representative, libraryPath)
	seenQueries := make(map[string]struct{}, len(queries))
	bestTentative := tmdbSeriesSelection{}
	bestTentativeScore := 0
	hasTentative := false

	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		key := strings.ToLower(query)
		if _, ok := seenQueries[key]; ok {
			continue
		}
		seenQueries[key] = struct{}{}

		var (
			results []metadata.MatchResult
			err     error
		)
		if cache != nil {
			results, err = cache.SearchTV(ctx, query)
		} else {
			results, err = h.SeriesQuery.SearchTV(ctx, query)
		}
		if err != nil {
			return tmdbSeriesSelection{}, err
		}

		best, topScore, secondScore, hasSecond := scoredTMDBSeriesMatch(results, info)
		if best == nil {
			continue
		}
		tmdbID, err := strconv.Atoi(best.ExternalID)
		if err != nil || tmdbID <= 0 {
			continue
		}
		if topScore >= metadata.ScoreAutoMatch &&
			(!hasSecond || (topScore-secondScore) >= metadata.ScoreMargin) {
			return tmdbSeriesSelection{tmdbID: tmdbID, metadataReviewNeeded: false}, nil
		}
		if !hasTentative || topScore > bestTentativeScore {
			bestTentative = tmdbSeriesSelection{
				tmdbID:               tmdbID,
				metadataReviewNeeded: true,
			}
			bestTentativeScore = topScore
			hasTentative = true
		}
	}

	if hasTentative {
		return bestTentative, nil
	}
	return tmdbSeriesSelection{}, nil
}

func (h *LibraryHandler) resolveTMDBSeriesSelectionForGroup(
	ctx context.Context,
	libraryPath string,
	group episodeIdentifyGroup,
	cache *episodicIdentifyCache,
) (tmdbSeriesSelection, error) {
	if group.explicitTMDBID > 0 {
		return tmdbSeriesSelection{tmdbID: group.explicitTMDBID}, nil
	}
	queries := make([]string, 0, len(group.fallbackQueries)+1)
	if group.groupQuery != "" {
		queries = append(queries, group.groupQuery)
	}
	for _, query := range group.fallbackQueries {
		if query == "" {
			continue
		}
		seen := false
		for _, existing := range queries {
			if strings.EqualFold(existing, query) {
				seen = true
				break
			}
		}
		if !seen {
			queries = append(queries, query)
		}
	}
	return h.selectTMDBSeriesFallback(ctx, libraryPath, group.representative.IdentificationRow, queries, cache)
}

func (h *LibraryHandler) identifyEpisodeGroup(
	ctx context.Context,
	libraryID int,
	libraryPath string,
	job episodeGroupJob,
	cache *episodicIdentifyCache,
	rateLimiter <-chan struct{},
) (identified int, retry bool, failed []identifyJobResult) {
	identifyGroupRowsAsIdentifying(h.identifyRun, libraryID, job.group.rows)
	select {
	case <-ctx.Done():
		return 0, false, episodeIdentifyFailedResults(job.group)
	case <-rateLimiter:
	}

	itemCtx, cancel := context.WithTimeout(ctx, identifyTimeoutForAttempt(job.attempt))
	defer cancel()

	selection, err := h.resolveTMDBSeriesSelectionForGroup(itemCtx, libraryPath, job.group, cache)
	if err != nil || selection.tmdbID <= 0 {
		if err == nil && job.attempt == 0 {
			identifyGroupRowsAsQueued(h.identifyRun, libraryID, job.group.rows)
			return 0, true, nil
		}
		identifyGroupRowsFail(h.identifyRun, libraryID, job.group.rows)
		return 0, false, episodeIdentifyFailedResults(job.group)
	}

	refs := make([]db.ShowEpisodeRef, 0, len(job.group.rows))
	for _, row := range job.group.rows {
		refs = append(refs, db.ShowEpisodeRef{
			RefID:   row.RefID,
			Kind:    row.Kind,
			Season:  row.Season,
			Episode: row.Episode,
		})
	}
	updatedRefIDs, err := h.applySeriesToRefs(
		itemCtx,
		selection.tmdbID,
		refs,
		selection.metadataReviewNeeded,
		false,
		cache,
		false,
	)
	if err != nil || len(updatedRefIDs) == 0 {
		if err == nil && job.attempt == 0 {
			identifyGroupRowsAsQueued(h.identifyRun, libraryID, job.group.rows)
			return 0, true, nil
		}
		identifyGroupRowsFail(h.identifyRun, libraryID, job.group.rows)
		return 0, false, episodeIdentifyFailedResults(job.group)
	}

	updatedSet := make(map[int]struct{}, len(updatedRefIDs))
	for _, refID := range updatedRefIDs {
		updatedSet[refID] = struct{}{}
	}
	updatedRows := make([]db.EpisodeIdentifyRow, 0, len(updatedSet))
	unresolved := make([]db.EpisodeIdentifyRow, 0)
	for _, row := range job.group.rows {
		if _, ok := updatedSet[row.RefID]; ok {
			updatedRows = append(updatedRows, row)
			continue
		}
		unresolved = append(unresolved, row)
	}
	identifyGroupRowsClear(h.identifyRun, libraryID, updatedRows)
	if len(unresolved) > 0 {
		identifyGroupRowsFail(h.identifyRun, libraryID, unresolved)
		failed = append(failed, episodeIdentifyFailedResults(episodeIdentifyGroup{
			key:             job.group.key,
			kind:            job.group.kind,
			groupQuery:      job.group.groupQuery,
			fallbackQueries: job.group.fallbackQueries,
			rows:            unresolved,
		})...)
	}
	return len(updatedRows), false, failed
}

func newIdentifyRateLimiter(ctx context.Context, interval time.Duration, burst int) <-chan struct{} {
	if burst < 1 {
		burst = 1
	}
	ch := make(chan struct{}, burst)
	for i := 0; i < burst; i++ {
		ch <- struct{}{}
	}
	go func() {
		ticker := time.NewTicker(interval)
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

func movieIdentifyKey(info metadata.MediaInfo) string {
	title := metadata.NormalizeTitle(info.Title)
	if title == "" {
		return ""
	}
	if info.Year > 0 {
		return title + ":" + strconv.Itoa(info.Year)
	}
	return title
}

func updateMetadataWithRetry(
	dbConn *sql.DB,
	table string,
	refID int,
	title string,
	overview string,
	posterPath string,
	backdropPath string,
	releaseDate string,
	voteAvg float64,
	imdbID string,
	imdbRating float64,
	tmdbID int,
	tvdbID string,
	season int,
	episode int,
	canonical db.CanonicalMetadata,
	metadataReviewNeeded bool,
	metadataConfirmed bool,
) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		lastErr = db.UpdateMediaMetadataWithCanonicalState(
			dbConn,
			table,
			refID,
			title,
			overview,
			posterPath,
			backdropPath,
			releaseDate,
			voteAvg,
			imdbID,
			imdbRating,
			tmdbID,
			tvdbID,
			season,
			episode,
			canonical,
			metadataReviewNeeded,
			metadataConfirmed,
		)
		if lastErr == nil || !isSQLiteBusyError(lastErr) {
			return lastErr
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	return lastErr
}

func (h *LibraryHandler) identifyLibraryJob(
	ctx context.Context,
	libraryID int,
	job identifyJob,
	libraryPath string,
	rateLimiter <-chan struct{},
	movieCache *movieIdentifyCache,
	queueSearch bool,
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
		if movieCache != nil {
			res = movieCache.lookupOrRun(movieIdentifyKey(info), func() *metadata.MatchResult {
				return h.Meta.IdentifyMovie(itemCtx, info)
			})
		} else {
			res = h.Meta.IdentifyMovie(itemCtx, info)
		}
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
	cast := make([]db.CastCredit, 0, len(res.Cast))
	for _, member := range res.Cast {
		cast = append(cast, db.CastCredit{
			Name:        member.Name,
			Character:   member.Character,
			Order:       member.Order,
			ProfilePath: member.ProfilePath,
			Provider:    member.Provider,
			ProviderID:  member.ProviderID,
		})
	}
	if err := updateMetadataWithRetry(h.DB, tbl, row.RefID, res.Title, res.Overview, res.PosterURL, res.BackdropURL, res.ReleaseDate, res.VoteAverage, res.IMDbID, res.IMDbRating, tmdbID, tvdbID, row.Season, row.Episode, db.CanonicalMetadata{
		Title:        res.Title,
		Overview:     res.Overview,
		PosterPath:   res.PosterURL,
		BackdropPath: res.BackdropURL,
		ReleaseDate:  res.ReleaseDate,
		IMDbID:       res.IMDbID,
		IMDbRating:   res.IMDbRating,
		Genres:       res.Genres,
		Cast:         cast,
		Runtime:      res.Runtime,
	}, false, false); err != nil {
		return identifyJobResult{status: identifyJobFailed, job: job}
	}
	h.identifyRun.setState(libraryID, row.Kind, row.Path, "")
	if queueSearch && h.SearchIndex != nil {
		h.SearchIndex.Queue(libraryID, false)
	}
	return identifyJobResult{status: identifyJobSucceeded, job: job}
}

func (h *LibraryHandler) identifyShowFallbackGroup(
	ctx context.Context,
	libraryPath string,
	queries []string,
	rows []db.IdentificationRow,
	cache *episodicIdentifyCache,
	queueSearch bool,
) (int, error) {
	if h.SeriesQuery == nil || len(rows) == 0 {
		return 0, nil
	}
	selection, err := h.selectTMDBSeriesFallback(ctx, libraryPath, rows[0], queries, cache)
	if err != nil {
		return 0, err
	}
	if selection.tmdbID <= 0 {
		return 0, nil
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
	updatedRefIDs, err := h.applySeriesToRefs(
		ctx,
		selection.tmdbID,
		refs,
		selection.metadataReviewNeeded,
		false,
		cache,
		queueSearch,
	)
	return len(updatedRefIDs), err
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

func (h *LibraryHandler) applySeriesToRefs(
	ctx context.Context,
	seriesTMDBID int,
	refs []db.ShowEpisodeRef,
	metadataReviewNeeded bool,
	metadataConfirmed bool,
	cache *episodicIdentifyCache,
	queueSearch bool,
) ([]int, error) {
	if h.SeriesQuery == nil || seriesTMDBID <= 0 || len(refs) == 0 {
		return nil, nil
	}
	table := db.MediaTableForKind(refs[0].Kind)
	seriesID := strconv.Itoa(seriesTMDBID)
	var canonical db.CanonicalMetadata
	if h.Series != nil {
		var details *metadata.SeriesDetails
		var err error
		if cache != nil {
			details, err = cache.GetSeriesDetails(ctx, seriesTMDBID)
		} else {
			details, err = h.Series.GetSeriesDetails(ctx, seriesTMDBID)
		}
		if err == nil && details != nil {
			cast := make([]db.CastCredit, 0, len(details.Cast))
			for _, member := range details.Cast {
				cast = append(cast, db.CastCredit{
					Name:        member.Name,
					Character:   member.Character,
					Order:       member.Order,
					ProfilePath: member.ProfilePath,
					Provider:    member.Provider,
					ProviderID:  member.ProviderID,
				})
			}
			canonical = db.CanonicalMetadata{
				Title:        details.Name,
				Overview:     details.Overview,
				PosterPath:   details.PosterPath,
				BackdropPath: details.BackdropPath,
				ReleaseDate:  details.FirstAirDate,
				IMDbID:       details.IMDbID,
				IMDbRating:   details.IMDbRating,
				Genres:       details.Genres,
				Cast:         cast,
				Runtime:      details.Runtime,
			}
		}
	}
	updatedRefIDs := make([]int, 0, len(refs))
	for _, ref := range refs {
		var (
			ep  *metadata.MatchResult
			err error
		)
		if cache != nil {
			ep, err = cache.GetEpisode(ctx, "tmdb", seriesID, ref.Season, ref.Episode)
		} else {
			ep, err = h.SeriesQuery.GetEpisode(ctx, "tmdb", seriesID, ref.Season, ref.Episode)
		}
		if err != nil || ep == nil {
			continue
		}
		tvdbID := ""
		if ep.Provider == "tvdb" {
			tvdbID = ep.ExternalID
		}
		if err := updateMetadataWithRetry(h.DB, table, ref.RefID, ep.Title, ep.Overview, ep.PosterURL, ep.BackdropURL, ep.ReleaseDate, ep.VoteAverage, ep.IMDbID, ep.IMDbRating, seriesTMDBID, tvdbID, ref.Season, ref.Episode, canonical, metadataReviewNeeded, metadataConfirmed); err != nil {
			continue
		}
		updatedRefIDs = append(updatedRefIDs, ref.RefID)
		if cache == nil {
			time.Sleep(identifyEpisodeRateLimit)
		}
	}
	if len(updatedRefIDs) > 0 && queueSearch && len(refs) > 0 && h.SearchIndex != nil {
		var libraryID int
		if err := h.DB.QueryRow(`SELECT library_id FROM `+table+` WHERE id = ?`, refs[0].RefID).Scan(&libraryID); err == nil {
			h.SearchIndex.Queue(libraryID, false)
		}
	}
	return updatedRefIDs, nil
}

func (h *LibraryHandler) applyTMDBSeriesToRefs(
	ctx context.Context,
	seriesTMDBID int,
	refs []db.ShowEpisodeRef,
	metadataReviewNeeded bool,
	metadataConfirmed bool,
) (int, error) {
	updatedRefIDs, err := h.applySeriesToRefs(ctx, seriesTMDBID, refs, metadataReviewNeeded, metadataConfirmed, nil, true)
	return len(updatedRefIDs), err
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

func (h *LibraryHandler) SearchLibraryMedia(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	libraryID := 0
	if rawLibraryID := strings.TrimSpace(r.URL.Query().Get("library_id")); rawLibraryID != "" {
		parsed, err := strconv.Atoi(rawLibraryID)
		if err != nil || parsed <= 0 {
			http.Error(w, "invalid library_id", http.StatusBadRequest)
			return
		}
		libraryID = parsed
	}
	limit := 30
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	searchType := strings.TrimSpace(r.URL.Query().Get("type"))
	if searchType != "" && searchType != "movie" && searchType != "show" {
		http.Error(w, "invalid type", http.StatusBadRequest)
		return
	}
	results, err := db.SearchLibraryMedia(h.DB, db.SearchQuery{
		UserID:    u.ID,
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		LibraryID: libraryID,
		Type:      searchType,
		Genre:     strings.TrimSpace(r.URL.Query().Get("genre")),
		Limit:     limit,
	})
	if err != nil {
		http.Error(w, "search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

func (h *LibraryHandler) GetLibraryMovieDetails(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	libraryID, _, _, ok := h.authorizeLibraryRequest(w, r, u.ID)
	if !ok {
		return
	}
	mediaID, err := strconv.Atoi(chi.URLParam(r, "mediaId"))
	if err != nil || mediaID <= 0 {
		http.Error(w, "invalid media id", http.StatusBadRequest)
		return
	}
	details, err := db.GetLibraryMovieDetails(h.DB, libraryID, mediaID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if details == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(details)
}

func (h *LibraryHandler) GetLibraryShowDetails(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	libraryID, _, _, ok := h.authorizeLibraryRequest(w, r, u.ID)
	if !ok {
		return
	}
	showKey := strings.TrimSpace(chi.URLParam(r, "showKey"))
	if showKey == "" {
		http.Error(w, "invalid show key", http.StatusBadRequest)
		return
	}
	details, err := db.GetLibraryShowDetails(h.DB, libraryID, showKey)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if details == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(details)
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
