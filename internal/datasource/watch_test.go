package datasource

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewWatcherSuccess(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clockmail.db")
	if err := os.WriteFile(dbPath, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewWatcher(dbPath)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	if w.Changes() == nil {
		t.Error("Changes() returned nil channel")
	}
}

func TestNewWatcherBadPath(t *testing.T) {
	_, err := NewWatcher("/nonexistent/dir/clockmail.db")
	if err == nil {
		t.Error("NewWatcher should fail for nonexistent directory")
	}
}

func TestWatcherDetectsWrite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clockmail.db")
	if err := os.WriteFile(dbPath, []byte("initial"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewWatcher(dbPath)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	// Give fsnotify time to start watching.
	time.Sleep(50 * time.Millisecond)

	// Write to the DB file.
	if err := os.WriteFile(dbPath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should receive a change signal within debounce + margin.
	select {
	case <-w.Changes():
		// Success.
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for change signal on DB write")
	}
}

func TestWatcherDetectsWALWrite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clockmail.db")
	if err := os.WriteFile(dbPath, []byte("db"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewWatcher(dbPath)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	time.Sleep(50 * time.Millisecond)

	// Write to the WAL file — watcher should detect this too.
	walPath := dbPath + "-wal"
	if err := os.WriteFile(walPath, []byte("wal data"), 0o644); err != nil {
		t.Fatalf("WriteFile WAL: %v", err)
	}

	select {
	case <-w.Changes():
		// Success.
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for change signal on WAL write")
	}
}

func TestWatcherIgnoresUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clockmail.db")
	if err := os.WriteFile(dbPath, []byte("db"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewWatcher(dbPath)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	time.Sleep(50 * time.Millisecond)

	// Write to an unrelated file in the same directory.
	unrelated := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(unrelated, []byte("noise"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should NOT receive a signal.
	select {
	case <-w.Changes():
		t.Error("unexpected change signal from unrelated file write")
	case <-time.After(300 * time.Millisecond):
		// Correct — no signal.
	}
}

func TestWatcherClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clockmail.db")
	if err := os.WriteFile(dbPath, []byte("db"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewWatcher(dbPath)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	// Close should not panic.
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
