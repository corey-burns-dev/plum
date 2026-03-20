package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"plum/internal/db"
	"plum/internal/metadata"
	"plum/internal/ws"
)

const (
	libraryScanProgressFlushInterval = 500 * time.Millisecond
	libraryScanProgressFlushEvery    = 25
)

var estimateLibraryFiles = db.EstimateLibraryFiles

const (
	libraryScanPhaseIdle      = "idle"
	libraryScanPhaseQueued    = "queued"
	libraryScanPhaseScanning  = "scanning"
	libraryScanPhaseCompleted = "completed"
	libraryScanPhaseFailed    = "failed"

	libraryIdentifyPhaseIdle        = "idle"
	libraryIdentifyPhaseQueued      = "queued"
	libraryIdentifyPhaseIdentifying = "identifying"
	libraryIdentifyPhaseCompleted   = "completed"
	libraryIdentifyPhaseFailed      = "failed"
)

type libraryScanStatus struct {
	LibraryID         int    `json:"libraryId"`
	Phase             string `json:"phase"`
	Enriching         bool   `json:"enriching"`
	IdentifyPhase     string `json:"identifyPhase"`
	Identified        int    `json:"identified"`
	IdentifyFailed    int    `json:"identifyFailed"`
	Processed         int    `json:"processed"`
	Added             int    `json:"added"`
	Updated           int    `json:"updated"`
	Removed           int    `json:"removed"`
	Unmatched         int    `json:"unmatched"`
	Skipped           int    `json:"skipped"`
	IdentifyRequested bool   `json:"identifyRequested"`
	QueuedAt          string `json:"queuedAt,omitempty"`
	EstimatedItems    int    `json:"estimatedItems"`
	QueuePosition     int    `json:"queuePosition"`
	Error             string `json:"error,omitempty"`
	StartedAt         string `json:"startedAt,omitempty"`
	FinishedAt        string `json:"finishedAt,omitempty"`
}

type LibraryScanManager struct {
	db   *sql.DB
	hub  *ws.Hub
	meta metadata.Identifier

	mu            sync.Mutex
	jobs          map[int]libraryScanStatus
	types         map[int]string
	paths         map[int]string
	enrichCancels map[int]context.CancelFunc
	enrichSem     chan struct{}
	identifySem   chan struct{}
	handler       *LibraryHandler
	lastFlushed   map[int]libraryScanFlushState
	activeScanID  int
}

type libraryScanFlushState struct {
	at        time.Time
	processed int
}

type queuedLibrary struct {
	id       int
	queuedAt string
}

func NewLibraryScanManager(sqlDB *sql.DB, meta metadata.Identifier, hub *ws.Hub) *LibraryScanManager {
	return &LibraryScanManager{
		db:            sqlDB,
		hub:           hub,
		meta:          meta,
		jobs:          make(map[int]libraryScanStatus),
		types:         make(map[int]string),
		paths:         make(map[int]string),
		enrichCancels: make(map[int]context.CancelFunc),
		enrichSem:     make(chan struct{}, 1),
		identifySem:   make(chan struct{}, 1),
		lastFlushed:   make(map[int]libraryScanFlushState),
	}
}

func (m *LibraryScanManager) AttachHandler(handler *LibraryHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

func (m *LibraryScanManager) Recover() error {
	statuses, err := db.ListLibraryJobStatuses(m.db)
	if err != nil {
		return err
	}

	type pendingEstimate struct {
		libraryID int
		path      string
		kind      string
	}

	var (
		scansToEstimate     []pendingEstimate
		enrichmentsToResume []int
		identifiesToResume  []int
	)

	m.mu.Lock()
	for _, persisted := range statuses {
		status := persistedLibraryStatusToRuntime(persisted)
		m.jobs[status.LibraryID] = status
		m.types[status.LibraryID] = persisted.Type
		m.paths[status.LibraryID] = persisted.Path
		delete(m.lastFlushed, status.LibraryID)

		switch {
		case status.Phase == libraryScanPhaseQueued || status.Phase == libraryScanPhaseScanning:
			status.Phase = libraryScanPhaseQueued
			status.Enriching = false
			if status.IdentifyPhase == libraryIdentifyPhaseQueued || status.IdentifyPhase == libraryIdentifyPhaseIdentifying {
				status.IdentifyPhase = libraryIdentifyPhaseIdle
				status.Identified = 0
				status.IdentifyFailed = 0
			}
			m.jobs[status.LibraryID] = status
			scansToEstimate = append(scansToEstimate, pendingEstimate{
				libraryID: status.LibraryID,
				path:      persisted.Path,
				kind:      persisted.Type,
			})
		case status.Enriching:
			enrichmentsToResume = append(enrichmentsToResume, status.LibraryID)
		}
		if status.IdentifyRequested &&
			(status.IdentifyPhase == libraryIdentifyPhaseQueued || status.IdentifyPhase == libraryIdentifyPhaseIdentifying) &&
			status.Phase == libraryScanPhaseCompleted {
			identifiesToResume = append(identifiesToResume, status.LibraryID)
		}
	}
	m.mu.Unlock()

	for _, pending := range scansToEstimate {
		m.mu.Lock()
		status := m.jobs[pending.libraryID]
		if status.QueuedAt == "" {
			status.QueuedAt = time.Now().UTC().Format(time.RFC3339)
		}
		m.jobs[pending.libraryID] = status
		m.mu.Unlock()
		m.queueEstimate(pending.libraryID, pending.path, pending.kind)
	}
	m.flushAllStatuses(true)
	m.scheduleNext()

	for _, libraryID := range enrichmentsToResume {
		m.startEnrichment(libraryID, m.types[libraryID], m.paths[libraryID])
	}
	for _, libraryID := range identifiesToResume {
		m.startIdentify(libraryID)
	}

	return nil
}

func (m *LibraryScanManager) start(libraryID int, path, libraryType string, identify bool) libraryScanStatus {
	if !m.canIdentifyLibrary(libraryType) {
		identify = false
	}

	m.mu.Lock()
	if cancel, ok := m.enrichCancels[libraryID]; ok {
		cancel()
		delete(m.enrichCancels, libraryID)
	}

	status, ok := m.jobs[libraryID]
	if ok && (status.Phase == libraryScanPhaseQueued || status.Phase == libraryScanPhaseScanning) {
		status.IdentifyRequested = status.IdentifyRequested || identify
		m.jobs[libraryID] = status
		m.types[libraryID] = libraryType
		m.paths[libraryID] = path
		result := m.statusLocked(libraryID)
		m.mu.Unlock()
		m.flushAllStatuses(true)
		return result
	}

	now := time.Now().UTC().Format(time.RFC3339)
	status = libraryScanStatus{
		LibraryID:         libraryID,
		Phase:             libraryScanPhaseQueued,
		IdentifyPhase:     libraryIdentifyPhaseIdle,
		IdentifyRequested: identify,
		QueuedAt:          now,
		StartedAt:         now,
	}
	m.jobs[libraryID] = status
	m.types[libraryID] = libraryType
	m.paths[libraryID] = path
	delete(m.lastFlushed, libraryID)
	result := m.statusLocked(libraryID)
	m.mu.Unlock()

	m.flushAllStatuses(true)
	m.queueEstimate(libraryID, path, libraryType)
	m.scheduleNext()
	return result
}

func (m *LibraryScanManager) queueEstimate(libraryID int, path, libraryType string) {
	if path == "" {
		return
	}
	go func() {
		estimatedItems, err := estimateLibraryFiles(context.Background(), path, libraryType)
		if err != nil {
			return
		}

		m.mu.Lock()
		status, ok := m.jobs[libraryID]
		if !ok || m.paths[libraryID] != path || m.types[libraryID] != libraryType {
			m.mu.Unlock()
			return
		}
		if status.EstimatedItems == estimatedItems {
			m.mu.Unlock()
			return
		}
		status.EstimatedItems = estimatedItems
		m.jobs[libraryID] = status
		m.mu.Unlock()

		m.flushStatus(libraryID, true)
	}()
}

func (m *LibraryScanManager) status(libraryID int) libraryScanStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(libraryID)
}

func (m *LibraryScanManager) statusLocked(libraryID int) libraryScanStatus {
	if status, ok := m.jobs[libraryID]; ok {
		status.QueuePosition = m.queuePositionLocked(libraryID)
		if status.Error == "" {
			status.Error = scanStatusWarning(status, m.paths[libraryID])
		}
		return status
	}
	return libraryScanStatus{
		LibraryID:      libraryID,
		Phase:          libraryScanPhaseIdle,
		Enriching:      false,
		IdentifyPhase:  libraryIdentifyPhaseIdle,
		EstimatedItems: 0,
		QueuePosition:  0,
	}
}

func scanStatusWarning(status libraryScanStatus, path string) string {
	if status.Phase == libraryScanPhaseCompleted && status.Processed == 0 && path != "" {
		return fmt.Sprintf(
			"No media files were found under %s. Verify the mounted library path contains media and that PLUM_MEDIA_*_PATH in .env points to the correct host folder.",
			path,
		)
	}
	return ""
}

func (m *LibraryScanManager) scheduleNext() {
	m.mu.Lock()
	if m.activeScanID != 0 {
		m.mu.Unlock()
		return
	}
	nextID, status, libraryType, path := m.nextQueuedLocked()
	if nextID == 0 {
		m.mu.Unlock()
		return
	}
	status.Phase = libraryScanPhaseScanning
	status.QueuePosition = 0
	status.FinishedAt = ""
	status.Error = ""
	m.jobs[nextID] = status
	m.activeScanID = nextID
	m.mu.Unlock()

	m.flushAllStatuses(true)
	go m.run(nextID, status, libraryType, path)
}

func (m *LibraryScanManager) nextQueuedLocked() (int, libraryScanStatus, string, string) {
	queued := m.queuedLibrariesLocked()
	if len(queued) == 0 {
		return 0, libraryScanStatus{}, "", ""
	}
	nextID := queued[0].id
	status := m.jobs[nextID]
	return nextID, status, m.types[nextID], m.paths[nextID]
}

func (m *LibraryScanManager) queuedLibrariesLocked() []queuedLibrary {
	queued := make([]queuedLibrary, 0, len(m.jobs))
	for libraryID, status := range m.jobs {
		if status.Phase != libraryScanPhaseQueued {
			continue
		}
		queued = append(queued, queuedLibrary{
			id:       libraryID,
			queuedAt: status.QueuedAt,
		})
	}
	sort.Slice(queued, func(i, j int) bool {
		if queued[i].queuedAt != queued[j].queuedAt {
			if queued[i].queuedAt == "" {
				return false
			}
			if queued[j].queuedAt == "" {
				return true
			}
			return queued[i].queuedAt < queued[j].queuedAt
		}
		return queued[i].id < queued[j].id
	})
	return queued
}

func (m *LibraryScanManager) queuePositionLocked(libraryID int) int {
	queued := m.queuedLibrariesLocked()
	for idx, item := range queued {
		if item.id == libraryID {
			return idx + 1
		}
	}
	return 0
}

func (m *LibraryScanManager) run(libraryID int, status libraryScanStatus, libraryType, path string) {
	result, err := db.HandleScanLibraryWithOptions(context.Background(), m.db, path, libraryType, libraryID, db.ScanOptions{
		ProbeMedia:             false,
		ProbeEmbeddedSubtitles: false,
		ScanSidecarSubtitles:   false,
		Progress: func(progress db.ScanProgress) {
			m.updateProgress(libraryID, progress)
		},
	})
	if err != nil {
		m.finish(libraryID, libraryScanPhaseFailed, db.ScanResult{}, err.Error())
		return
	}
	m.finish(libraryID, libraryScanPhaseCompleted, result, "")
	if status.IdentifyRequested && libraryType != db.LibraryTypeMusic {
		m.startIdentify(libraryID)
	}
	m.startEnrichment(libraryID, libraryType, path)
}

func (m *LibraryScanManager) updateProgress(libraryID int, progress db.ScanProgress) {
	m.mu.Lock()
	status, ok := m.jobs[libraryID]
	if !ok {
		m.mu.Unlock()
		return
	}
	status.Processed = progress.Processed
	status.Added = progress.Result.Added
	status.Updated = progress.Result.Updated
	status.Removed = progress.Result.Removed
	status.Unmatched = progress.Result.Unmatched
	status.Skipped = progress.Result.Skipped
	m.jobs[libraryID] = status
	m.mu.Unlock()
	m.flushStatus(libraryID, false)
}

func (m *LibraryScanManager) startIdentify(libraryID int) {
	m.mu.Lock()
	status, ok := m.jobs[libraryID]
	if !ok {
		m.mu.Unlock()
		return
	}
	status.IdentifyPhase = libraryIdentifyPhaseQueued
	m.jobs[libraryID] = status
	m.mu.Unlock()
	m.flushStatus(libraryID, true)

	go func() {
		m.identifySem <- struct{}{}
		defer func() { <-m.identifySem }()

		m.mu.Lock()
		status, ok := m.jobs[libraryID]
		if !ok {
			m.mu.Unlock()
			return
		}
		status.IdentifyPhase = libraryIdentifyPhaseIdentifying
		m.jobs[libraryID] = status
		m.mu.Unlock()
		m.flushStatus(libraryID, true)

		handler := m.handler
		if handler == nil {
			handler = &LibraryHandler{DB: m.db, Meta: m.meta}
		}
		result, err := handler.identifyLibrary(context.Background(), libraryID)

		m.mu.Lock()
		status, ok = m.jobs[libraryID]
		if !ok {
			m.mu.Unlock()
			return
		}
		status.Identified = result.Identified
		status.IdentifyFailed = result.Failed
		if err != nil {
			status.IdentifyPhase = libraryIdentifyPhaseFailed
		} else {
			status.IdentifyPhase = libraryIdentifyPhaseCompleted
		}
		m.jobs[libraryID] = status
		m.mu.Unlock()
		m.flushStatus(libraryID, true)
	}()
}

func (m *LibraryScanManager) finish(libraryID int, phase string, result db.ScanResult, errText string) {
	m.mu.Lock()
	status := m.jobs[libraryID]
	status.Phase = phase
	status.Processed = result.Added + result.Updated + result.Skipped
	status.Added = result.Added
	status.Updated = result.Updated
	status.Removed = result.Removed
	status.Unmatched = result.Unmatched
	status.Skipped = result.Skipped
	status.Error = errText
	status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if phase == libraryScanPhaseFailed {
		status.Enriching = false
	}
	m.jobs[libraryID] = status
	if m.activeScanID == libraryID {
		m.activeScanID = 0
	}
	m.mu.Unlock()
	m.flushAllStatuses(true)
	m.scheduleNext()
}

func (m *LibraryScanManager) startEnrichment(libraryID int, libraryType, path string) {
	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	status, ok := m.jobs[libraryID]
	if !ok {
		m.mu.Unlock()
		cancel()
		return
	}
	status.Enriching = true
	m.jobs[libraryID] = status
	m.enrichCancels[libraryID] = cancel
	m.mu.Unlock()
	m.flushStatus(libraryID, true)

	go func() {
		select {
		case m.enrichSem <- struct{}{}:
		case <-ctx.Done():
			m.finishEnrichment(libraryID)
			return
		}
		defer func() { <-m.enrichSem }()

		options := db.ScanOptions{
			ProbeMedia:             true,
			ProbeEmbeddedSubtitles: true,
			ScanSidecarSubtitles:   true,
		}
		if libraryType == db.LibraryTypeMusic && status.IdentifyRequested {
			if musicIdentifier, ok := m.meta.(metadata.MusicIdentifier); ok {
				options.MusicIdentifier = musicIdentifier
			}
		}
		_, err := db.HandleScanLibraryWithOptions(ctx, m.db, path, libraryType, libraryID, options)
		if err != nil && ctx.Err() == nil {
			// Enrichment is best-effort; preserve browseability and just stop tracking.
		}
		m.finishEnrichment(libraryID)
	}()
}

func (m *LibraryScanManager) canIdentifyLibrary(libraryType string) bool {
	if m.meta == nil {
		return false
	}
	if libraryType != db.LibraryTypeMusic {
		return true
	}
	_, ok := m.meta.(metadata.MusicIdentifier)
	return ok
}

func (m *LibraryScanManager) finishEnrichment(libraryID int) {
	m.mu.Lock()
	status, ok := m.jobs[libraryID]
	if !ok {
		m.mu.Unlock()
		return
	}
	status.Enriching = false
	m.jobs[libraryID] = status
	if cancel, ok := m.enrichCancels[libraryID]; ok {
		cancel()
		delete(m.enrichCancels, libraryID)
	}
	m.mu.Unlock()
	m.flushStatus(libraryID, true)
}

func (m *LibraryScanManager) flushAllStatuses(force bool) {
	m.mu.Lock()
	ids := make([]int, 0, len(m.jobs))
	for libraryID := range m.jobs {
		ids = append(ids, libraryID)
	}
	m.mu.Unlock()
	for _, libraryID := range ids {
		m.flushStatus(libraryID, force)
	}
}

func (m *LibraryScanManager) flushStatus(libraryID int, force bool) {
	m.mu.Lock()
	status, ok := m.jobs[libraryID]
	if !ok {
		m.mu.Unlock()
		return
	}
	status.QueuePosition = m.queuePositionLocked(libraryID)
	if status.Error == "" {
		status.Error = scanStatusWarning(status, m.paths[libraryID])
	}
	last := m.lastFlushed[libraryID]
	now := time.Now()
	shouldFlush := force ||
		last.at.IsZero() ||
		status.Processed-last.processed >= libraryScanProgressFlushEvery ||
		now.Sub(last.at) >= libraryScanProgressFlushInterval
	if !shouldFlush {
		m.mu.Unlock()
		return
	}
	m.lastFlushed[libraryID] = libraryScanFlushState{
		at:        now,
		processed: status.Processed,
	}
	path := m.paths[libraryID]
	libraryType := m.types[libraryID]
	m.mu.Unlock()

	_ = db.UpsertLibraryJobStatus(m.db, runtimeLibraryStatusToPersistent(status, path, libraryType))
	m.broadcast(status)
}

func runtimeLibraryStatusToPersistent(status libraryScanStatus, path, libraryType string) db.LibraryJobStatus {
	return db.LibraryJobStatus{
		LibraryID:         status.LibraryID,
		Path:              path,
		Type:              libraryType,
		Phase:             status.Phase,
		Enriching:         status.Enriching,
		IdentifyPhase:     status.IdentifyPhase,
		Identified:        status.Identified,
		IdentifyFailed:    status.IdentifyFailed,
		Processed:         status.Processed,
		Added:             status.Added,
		Updated:           status.Updated,
		Removed:           status.Removed,
		Unmatched:         status.Unmatched,
		Skipped:           status.Skipped,
		IdentifyRequested: status.IdentifyRequested,
		QueuedAt:          status.QueuedAt,
		EstimatedItems:    status.EstimatedItems,
		Error:             status.Error,
		StartedAt:         status.StartedAt,
		FinishedAt:        status.FinishedAt,
	}
}

func persistedLibraryStatusToRuntime(status db.LibraryJobStatus) libraryScanStatus {
	return libraryScanStatus{
		LibraryID:         status.LibraryID,
		Phase:             status.Phase,
		Enriching:         status.Enriching,
		IdentifyPhase:     status.IdentifyPhase,
		Identified:        status.Identified,
		IdentifyFailed:    status.IdentifyFailed,
		Processed:         status.Processed,
		Added:             status.Added,
		Updated:           status.Updated,
		Removed:           status.Removed,
		Unmatched:         status.Unmatched,
		Skipped:           status.Skipped,
		IdentifyRequested: status.IdentifyRequested,
		QueuedAt:          status.QueuedAt,
		EstimatedItems:    status.EstimatedItems,
		Error:             status.Error,
		StartedAt:         status.StartedAt,
		FinishedAt:        status.FinishedAt,
	}
}

func (m *LibraryScanManager) broadcast(status libraryScanStatus) {
	if m.hub == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type": "library_scan_update",
		"scan": status,
	})
	if err != nil {
		return
	}
	m.hub.Broadcast(payload)
}
