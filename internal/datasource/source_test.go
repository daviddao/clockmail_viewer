package datasource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daviddao/clockmail/pkg/store"
)

func TestDiscoverFromEnvVar(t *testing.T) {
	// Create a temp DB.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	s.Close()

	// Set env var.
	old := os.Getenv("CLOCKMAIL_DB")
	defer os.Setenv("CLOCKMAIL_DB", old)
	os.Setenv("CLOCKMAIL_DB", dbPath)

	path, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if path != dbPath {
		t.Errorf("Discover() = %q, want %q", path, dbPath)
	}
}

func TestDiscoverEnvVarMissing(t *testing.T) {
	old := os.Getenv("CLOCKMAIL_DB")
	defer os.Setenv("CLOCKMAIL_DB", old)
	os.Setenv("CLOCKMAIL_DB", "/nonexistent/path/clockmail.db")

	_, err := Discover()
	if err == nil {
		t.Error("Discover should fail when CLOCKMAIL_DB points to nonexistent file")
	}
}

func TestDiscoverFromCWD(t *testing.T) {
	// Create a temp dir with .clockmail/clockmail.db.
	dir := t.TempDir()
	cmDir := filepath.Join(dir, ".clockmail")
	if err := os.MkdirAll(cmDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dbPath := filepath.Join(cmDir, "clockmail.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	s.Close()

	// Clear env var to test CWD discovery.
	old := os.Getenv("CLOCKMAIL_DB")
	defer os.Setenv("CLOCKMAIL_DB", old)
	os.Unsetenv("CLOCKMAIL_DB")

	// Change to the temp dir.
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(dir)

	path, err := Discover()
	if err != nil {
		t.Fatalf("Discover from CWD: %v", err)
	}
	if filepath.Base(filepath.Dir(path)) != ".clockmail" {
		t.Errorf("expected path in .clockmail/, got %q", path)
	}
}

func TestDiscoverFromParentDir(t *testing.T) {
	// Create a temp dir with .clockmail/clockmail.db.
	dir := t.TempDir()
	cmDir := filepath.Join(dir, ".clockmail")
	if err := os.MkdirAll(cmDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dbPath := filepath.Join(cmDir, "clockmail.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	s.Close()

	// Create a child directory and chdir into it.
	childDir := filepath.Join(dir, "sub", "deep")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("MkdirAll child: %v", err)
	}

	old := os.Getenv("CLOCKMAIL_DB")
	defer os.Setenv("CLOCKMAIL_DB", old)
	os.Unsetenv("CLOCKMAIL_DB")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(childDir)

	path, err := Discover()
	if err != nil {
		t.Fatalf("Discover from parent: %v", err)
	}
	// Resolve symlinks for comparison (macOS /var -> /private/var).
	resolvedPath, _ := filepath.EvalSymlinks(path)
	resolvedExpect, _ := filepath.EvalSymlinks(dbPath)
	if resolvedPath != resolvedExpect {
		t.Errorf("Discover() = %q, want %q", path, dbPath)
	}
}

func TestDiscoverNoDB(t *testing.T) {
	// Temp dir with no .clockmail.
	dir := t.TempDir()

	old := os.Getenv("CLOCKMAIL_DB")
	defer os.Setenv("CLOCKMAIL_DB", old)
	os.Unsetenv("CLOCKMAIL_DB")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(dir)

	_, err := Discover()
	if err == nil {
		t.Error("Discover should fail when no database exists")
	}
}

func TestOpenSuccess(t *testing.T) {
	// Create a temp DB.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	s.Close()

	old := os.Getenv("CLOCKMAIL_DB")
	defer os.Setenv("CLOCKMAIL_DB", old)
	os.Setenv("CLOCKMAIL_DB", dbPath)

	st, path, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if path != dbPath {
		t.Errorf("Open path = %q, want %q", path, dbPath)
	}
}

func TestOpenFail(t *testing.T) {
	old := os.Getenv("CLOCKMAIL_DB")
	defer os.Setenv("CLOCKMAIL_DB", old)
	os.Setenv("CLOCKMAIL_DB", "/nonexistent/path/clockmail.db")

	_, _, err := Open()
	if err == nil {
		t.Error("Open should fail when no database exists")
	}
}
