package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
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
}

type libraryScanFlushState struct {
	at        time.Time
	processed int
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

	var scansToResume []int
	var enrichmentsToResume []int
	var identifiesToResume []int

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
			scansToResume = append(scansToResume, status.LibraryID)
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

	for _, libraryID := range scansToResume {
		m.flushStatus(libraryID, true)
		go m.run(libraryID)
	}
	for _, libraryID := range enrichmentsToResume {
		m.startEnrichment(libraryID, m.types[libraryID], m.paths[libraryID])
	}
	for _, libraryID := range identifiesToResume {
		m.startIdentify(libraryID)
	}

	return nil
}

func (m *LibraryScanManager) start(libraryID int, path, libraryType string, identify bool) libraryScanStatus {
	if libraryType == db.LibraryTypeMusic || m.meta == nil {
		identify = false
	}

	m.mu.Lock()
	if cancel, ok := m.enrichCancels[libraryID]; ok {
		cancel()
		delete(m.enrichCancels, libraryID)
	}
	active, ok := m.jobs[libraryID]
	if ok && (active.Phase == libraryScanPhaseQueued || active.Phase == libraryScanPhaseScanning) {
		m.mu.Unlock()
		return active
	}

	status := libraryScanStatus{
		LibraryID:         libraryID,
		Phase:             libraryScanPhaseQueued,
		IdentifyPhase:     libraryIdentifyPhaseIdle,
		IdentifyRequested: identify,
		StartedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	m.jobs[libraryID] = status
	m.types[libraryID] = libraryType
	m.paths[libraryID] = path
	delete(m.lastFlushed, libraryID)
	m.mu.Unlock()

	m.flushStatus(libraryID, true)

	go m.run(libraryID)

	return status
}

func (m *LibraryScanManager) status(libraryID int) libraryScanStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status, ok := m.jobs[libraryID]; ok {
		return status
	}
	return libraryScanStatus{
		LibraryID:     libraryID,
		Phase:         libraryScanPhaseIdle,
		Enriching:     false,
		IdentifyPhase: libraryIdentifyPhaseIdle,
	}
}

func (m *LibraryScanManager) run(libraryID int) {
	status, libraryType, path := m.markScanning(libraryID)
	if status.LibraryID == 0 {
		return
	}

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
	if status.IdentifyRequested {
		m.startIdentify(libraryID)
	}
	m.startEnrichment(libraryID, libraryType, path)
}

func (m *LibraryScanManager) markScanning(libraryID int) (libraryScanStatus, string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status, ok := m.jobs[libraryID]
	if !ok {
		return libraryScanStatus{}, "", ""
	}
	status.Phase = libraryScanPhaseScanning
	m.jobs[libraryID] = status
	go m.flushStatus(libraryID, true)
	return status, m.types[libraryID], m.paths[libraryID]
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
	m.mu.Unlock()
	m.flushStatus(libraryID, true)
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

		_, err := db.HandleScanLibraryWithOptions(ctx, m.db, path, libraryType, libraryID, db.ScanOptions{
			ProbeMedia:             true,
			ProbeEmbeddedSubtitles: true,
			ScanSidecarSubtitles:   true,
		})
		if err != nil && ctx.Err() == nil {
			// Enrichment is best-effort; preserve browseability and just stop tracking.
		}
		m.finishEnrichment(libraryID)
	}()
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

func (m *LibraryScanManager) flushStatus(libraryID int, force bool) {
	m.mu.Lock()
	status, ok := m.jobs[libraryID]
	if !ok {
		m.mu.Unlock()
		return
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
