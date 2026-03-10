package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// newTestDB creates an in-memory SQLite DB and initializes schema.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := createSchema(db); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestHandleScanLibrary_InsertsTvMediaAndIsIdempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tvDir := filepath.Join(tmpDir, "TV")
	if err := os.MkdirAll(tvDir, 0o755); err != nil {
		t.Fatalf("mkdir tv dir: %v", err)
	}

	episodePath := filepath.Join(tvDir, "Show.S01E01.mkv")
	if err := os.WriteFile(episodePath, []byte("fake video data"), 0o644); err != nil {
		t.Fatalf("write episode file: %v", err)
	}

	// Non-media file should be ignored.
	if err := os.WriteFile(filepath.Join(tvDir, "notes.txt"), []byte("not media"), 0o644); err != nil {
		t.Fatalf("write non-media file: %v", err)
	}

	db := newTestDB(t)
	ctx := context.Background()

	added, err := HandleScanLibrary(ctx, db, tvDir, "tv")
	if err != nil {
		t.Fatalf("first scan error: %v", err)
	}
	if added != 1 {
		t.Fatalf("expected 1 item added, got %d", added)
	}

	items, err := GetAllMedia(db)
	if err != nil {
		t.Fatalf("GetAllMedia: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(items))
	}
	if items[0].Path != episodePath {
		t.Fatalf("unexpected path, got %q want %q", items[0].Path, episodePath)
	}
	if items[0].Type != "tv" {
		t.Fatalf("unexpected type, got %q want %q", items[0].Type, "tv")
	}

	// Second scan should not create duplicates.
	addedAgain, err := HandleScanLibrary(ctx, db, tvDir, "tv")
	if err != nil {
		t.Fatalf("second scan error: %v", err)
	}
	if addedAgain != 0 {
		t.Fatalf("expected 0 items added on second scan, got %d", addedAgain)
	}

	items, err = GetAllMedia(db)
	if err != nil {
		t.Fatalf("GetAllMedia after second scan: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected still 1 media item, got %d", len(items))
	}
}

