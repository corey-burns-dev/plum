package db

import (
	"database/sql"
	"time"
)

type LibraryJobStatus struct {
	LibraryID         int
	Path              string
	Type              string
	Phase             string
	Enriching         bool
	IdentifyPhase     string
	Identified        int
	IdentifyFailed    int
	Processed         int
	Added             int
	Updated           int
	Removed           int
	Unmatched         int
	Skipped           int
	IdentifyRequested bool
	Error             string
	StartedAt         string
	FinishedAt        string
}

func UpsertLibraryJobStatus(dbConn *sql.DB, status LibraryJobStatus) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := dbConn.Exec(
		`INSERT INTO library_job_status (
			library_id, phase, enriching, identify_phase, identified, identify_failed,
			processed, added, updated, removed, unmatched, skipped,
			identify_requested, error, started_at, finished_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(library_id) DO UPDATE SET
			phase = excluded.phase,
			enriching = excluded.enriching,
			identify_phase = excluded.identify_phase,
			identified = excluded.identified,
			identify_failed = excluded.identify_failed,
			processed = excluded.processed,
			added = excluded.added,
			updated = excluded.updated,
			removed = excluded.removed,
			unmatched = excluded.unmatched,
			skipped = excluded.skipped,
			identify_requested = excluded.identify_requested,
			error = excluded.error,
			started_at = excluded.started_at,
			finished_at = excluded.finished_at,
			updated_at = excluded.updated_at`,
		status.LibraryID,
		status.Phase,
		boolToInt(status.Enriching),
		status.IdentifyPhase,
		status.Identified,
		status.IdentifyFailed,
		status.Processed,
		status.Added,
		status.Updated,
		status.Removed,
		status.Unmatched,
		status.Skipped,
		boolToInt(status.IdentifyRequested),
		nullStr(status.Error),
		nullStr(status.StartedAt),
		nullStr(status.FinishedAt),
		now,
	)
	return err
}

func ListLibraryJobStatuses(dbConn *sql.DB) ([]LibraryJobStatus, error) {
	rows, err := dbConn.Query(
		`SELECT
			s.library_id,
			l.path,
			l.type,
			s.phase,
			COALESCE(s.enriching, 0),
			COALESCE(s.identify_phase, 'idle'),
			COALESCE(s.identified, 0),
			COALESCE(s.identify_failed, 0),
			COALESCE(s.processed, 0),
			COALESCE(s.added, 0),
			COALESCE(s.updated, 0),
			COALESCE(s.removed, 0),
			COALESCE(s.unmatched, 0),
			COALESCE(s.skipped, 0),
			COALESCE(s.identify_requested, 0),
			COALESCE(s.error, ''),
			COALESCE(s.started_at, ''),
			COALESCE(s.finished_at, '')
		FROM library_job_status s
		JOIN libraries l ON l.id = s.library_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LibraryJobStatus
	for rows.Next() {
		var status LibraryJobStatus
		var enriching int
		var identifyRequested int
		if err := rows.Scan(
			&status.LibraryID,
			&status.Path,
			&status.Type,
			&status.Phase,
			&enriching,
			&status.IdentifyPhase,
			&status.Identified,
			&status.IdentifyFailed,
			&status.Processed,
			&status.Added,
			&status.Updated,
			&status.Removed,
			&status.Unmatched,
			&status.Skipped,
			&identifyRequested,
			&status.Error,
			&status.StartedAt,
			&status.FinishedAt,
		); err != nil {
			return nil, err
		}
		status.Enriching = enriching != 0
		status.IdentifyRequested = identifyRequested != 0
		out = append(out, status)
	}
	return out, rows.Err()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
