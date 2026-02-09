// Package datasource provides file watching for the clockmail database.
package datasource

import (
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors the clockmail database directory for changes.
type Watcher struct {
	watcher  *fsnotify.Watcher
	dbPath   string
	debounce time.Duration
	onChange chan struct{}
	done     chan struct{}
}

// NewWatcher creates a watcher for the given database path.
// It watches the parent directory to catch WAL checkpoint writes.
func NewWatcher(dbPath string) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(dbPath)
	if err := w.Add(dir); err != nil {
		w.Close()
		return nil, err
	}

	watcher := &Watcher{
		watcher:  w,
		dbPath:   dbPath,
		debounce: 100 * time.Millisecond,
		onChange: make(chan struct{}, 1),
		done:     make(chan struct{}),
	}

	go watcher.loop()
	return watcher, nil
}

// Changes returns a channel that receives a signal when the DB changes.
func (w *Watcher) Changes() <-chan struct{} {
	return w.onChange
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	close(w.done)
	return w.watcher.Close()
}

func (w *Watcher) loop() {
	var timer *time.Timer
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// Only care about writes to DB files (main db, WAL, SHM).
			base := filepath.Base(event.Name)
			if base != filepath.Base(w.dbPath) &&
				base != filepath.Base(w.dbPath)+"-wal" &&
				base != filepath.Base(w.dbPath)+"-shm" {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			// Debounce: reset timer on each write.
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounce, func() {
				select {
				case w.onChange <- struct{}{}:
				default: // already signaled, skip
				}
			})
		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}
