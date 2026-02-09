package main

import (
	"os"
	"testing"

	"github.com/daviddao/clockmail_viewer/internal/datasource"
	"github.com/daviddao/clockmail_viewer/internal/snapshot"
)

func TestSmokeDBConnection(t *testing.T) {
	// Try to find the project DB.
	origDir, _ := os.Getwd()
	os.Chdir("../../..") // up to adventure4/
	defer os.Chdir(origDir)

	s, path, err := datasource.Open()
	if err != nil {
		t.Skipf("no clockmail DB available: %v", err)
	}
	defer s.Close()

	t.Logf("connected to %s", path)

	snap, err := snapshot.Build(s)
	if err != nil {
		t.Fatalf("snapshot build failed: %v", err)
	}

	t.Logf("snapshot: %d agents, %d events, %d locks, built at %s",
		snap.ActiveAgents+snap.StaleAgents,
		snap.TotalEvents,
		snap.ActiveLocks,
		snap.BuiltAt)

	if snap.ActiveAgents+snap.StaleAgents == 0 {
		t.Log("warning: no agents found (expected for fresh DB)")
	}
}

func TestSmokeWatcher(t *testing.T) {
	origDir, _ := os.Getwd()
	os.Chdir("../../..") // up to adventure4/
	defer os.Chdir(origDir)

	path, err := datasource.Discover()
	if err != nil {
		t.Skipf("no clockmail DB available: %v", err)
	}

	w, err := datasource.NewWatcher(path)
	if err != nil {
		t.Fatalf("watcher creation failed: %v", err)
	}
	defer w.Close()

	t.Logf("watching %s", path)
	// Just verify it doesn't crash on creation/close.
}
