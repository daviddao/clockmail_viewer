// Package datasource discovers and connects to the clockmail SQLite database.
package datasource

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/daviddao/clockmail/pkg/store"
)

const (
	defaultDir = ".clockmail"
	defaultDB  = ".clockmail/clockmail.db"
)

// Discover finds the clockmail database path.
// Priority: CLOCKMAIL_DB env var > .clockmail/clockmail.db in CWD > walk up parents.
func Discover() (string, error) {
	if env := os.Getenv("CLOCKMAIL_DB"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
		return "", fmt.Errorf("CLOCKMAIL_DB=%q: %w", env, os.ErrNotExist)
	}

	// Check CWD first.
	if _, err := os.Stat(defaultDB); err == nil {
		abs, err := filepath.Abs(defaultDB)
		if err != nil {
			return "", fmt.Errorf("resolve absolute path for %s: %w", defaultDB, err)
		}
		return abs, nil
	}

	// Walk up parent directories.
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, defaultDB)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("no clockmail database found (looked for %s)", defaultDB)
}

// Open discovers and opens the clockmail store.
func Open() (*store.Store, string, error) {
	path, err := Discover()
	if err != nil {
		return nil, "", err
	}
	s, err := store.New(path)
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", path, err)
	}
	return s, path, nil
}
