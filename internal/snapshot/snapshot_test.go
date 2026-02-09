package snapshot

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/daviddao/clockmail/pkg/model"
	"github.com/daviddao/clockmail/pkg/store"
)

// newTestStore creates a temporary clockmail store for testing.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clockmail.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// makeEvent creates a model.Event for test insertion.
func makeEvent(agentID string, kind model.EventKind, target, body string, ts int64) *model.Event {
	return &model.Event{
		AgentID:   agentID,
		LamportTS: ts,
		Kind:      kind,
		Target:    target,
		Body:      body,
		CreatedAt: time.Now(),
	}
}

func TestBuildEmptyStore(t *testing.T) {
	s := newTestStore(t)

	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snap.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(snap.Agents))
	}
	if len(snap.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(snap.Events))
	}
	if len(snap.Locks) != 0 {
		t.Errorf("expected 0 locks, got %d", len(snap.Locks))
	}
	if len(snap.Frontier) != 0 {
		t.Errorf("expected 0 frontier points, got %d", len(snap.Frontier))
	}
	if snap.ActiveAgents != 0 {
		t.Errorf("expected 0 active agents, got %d", snap.ActiveAgents)
	}
	if snap.StaleAgents != 0 {
		t.Errorf("expected 0 stale agents, got %d", snap.StaleAgents)
	}
	if snap.TotalEvents != 0 {
		t.Errorf("expected 0 total events, got %d", snap.TotalEvents)
	}
	if snap.ActiveLocks != 0 {
		t.Errorf("expected 0 active locks, got %d", snap.ActiveLocks)
	}
	if snap.BuiltAt.IsZero() {
		t.Error("BuiltAt should not be zero")
	}
}

func TestBuildWithAgents(t *testing.T) {
	s := newTestStore(t)

	// Register two agents (they will be "active" since LastSeen is now).
	if _, err := s.RegisterAgent("alice"); err != nil {
		t.Fatalf("RegisterAgent alice: %v", err)
	}
	if _, err := s.RegisterAgent("bob"); err != nil {
		t.Fatalf("RegisterAgent bob: %v", err)
	}

	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snap.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(snap.Agents))
	}
	if snap.ActiveAgents != 2 {
		t.Errorf("expected 2 active agents, got %d", snap.ActiveAgents)
	}
	if snap.StaleAgents != 0 {
		t.Errorf("expected 0 stale agents, got %d", snap.StaleAgents)
	}

	// Both agents should have frontier status entries.
	if len(snap.FrontierStatus) != 2 {
		t.Errorf("expected 2 frontier status entries, got %d", len(snap.FrontierStatus))
	}
	for _, agID := range []string{"alice", "bob"} {
		if _, ok := snap.FrontierStatus[agID]; !ok {
			t.Errorf("missing frontier status for %s", agID)
		}
	}
}

func TestBuildWithEvents(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.RegisterAgent("alice"); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Insert a message event.
	e := makeEvent("alice", model.EventMsg, "bob", "hello", 1)
	if _, err := s.InsertEvent(e); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if snap.TotalEvents != 1 {
		t.Errorf("expected 1 event, got %d", snap.TotalEvents)
	}
	if len(snap.Events) != 1 {
		t.Fatalf("expected 1 event in slice, got %d", len(snap.Events))
	}
	if snap.Events[0].Kind != model.EventMsg {
		t.Errorf("expected kind 'msg', got %q", snap.Events[0].Kind)
	}
}

func TestBuildWithLocks(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.RegisterAgent("alice"); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	lock, conflict, err := s.AcquireLock("main.go", "alice", 1, 0, true, time.Hour)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if conflict != nil {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
	_ = lock

	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if snap.ActiveLocks != 1 {
		t.Errorf("expected 1 active lock, got %d", snap.ActiveLocks)
	}
	if len(snap.Locks) != 1 {
		t.Fatalf("expected 1 lock, got %d", len(snap.Locks))
	}
	if snap.Locks[0].Path != "main.go" {
		t.Errorf("expected lock on 'main.go', got %q", snap.Locks[0].Path)
	}
	if snap.Locks[0].AgentID != "alice" {
		t.Errorf("expected lock by 'alice', got %q", snap.Locks[0].AgentID)
	}
}

func TestBuildFrontierTwoAgents(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.RegisterAgent("alice"); err != nil {
		t.Fatalf("RegisterAgent alice: %v", err)
	}
	if _, err := s.RegisterAgent("bob"); err != nil {
		t.Fatalf("RegisterAgent bob: %v", err)
	}

	// Advance alice to epoch=1, bob stays at epoch=0.
	if err := s.UpdateAgentClock("alice", 5, 1, 0); err != nil {
		t.Fatalf("UpdateAgentClock alice: %v", err)
	}

	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Alice should be blocked by bob (bob is still at epoch=0 <= alice's epoch=1).
	aliceStatus, ok := snap.FrontierStatus["alice"]
	if !ok {
		t.Fatal("missing frontier status for alice")
	}
	if aliceStatus.SafeToFinalize {
		t.Error("alice should NOT be safe to finalize (bob is behind)")
	}

	// Bob at epoch=0: alice is at epoch=1 which is NOT <= epoch=0, so bob IS safe.
	bobStatus, ok := snap.FrontierStatus["bob"]
	if !ok {
		t.Fatal("missing frontier status for bob")
	}
	if !bobStatus.SafeToFinalize {
		t.Error("bob should be safe to finalize (alice is ahead)")
	}
}

func TestBuildSnapshotIsImmutable(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.RegisterAgent("alice"); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	snap1, err := Build(s)
	if err != nil {
		t.Fatalf("Build 1: %v", err)
	}

	// Register a second agent and build again.
	if _, err := s.RegisterAgent("bob"); err != nil {
		t.Fatalf("RegisterAgent bob: %v", err)
	}

	snap2, err := Build(s)
	if err != nil {
		t.Fatalf("Build 2: %v", err)
	}

	// snap1 should still show 1 agent (immutable).
	if len(snap1.Agents) != 1 {
		t.Errorf("snap1 should have 1 agent (immutable), got %d", len(snap1.Agents))
	}
	if len(snap2.Agents) != 2 {
		t.Errorf("snap2 should have 2 agents, got %d", len(snap2.Agents))
	}
}

func TestBuildBuiltAtIsRecent(t *testing.T) {
	s := newTestStore(t)

	before := time.Now()
	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	after := time.Now()

	if snap.BuiltAt.Before(before) || snap.BuiltAt.After(after) {
		t.Errorf("BuiltAt %v not between %v and %v", snap.BuiltAt, before, after)
	}
}

func TestBuildMultipleEventTypes(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.RegisterAgent("alice"); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	events := []*model.Event{
		makeEvent("alice", model.EventMsg, "bob", "hello", 1),
		makeEvent("alice", model.EventLockReq, "main.go", "", 2),
		makeEvent("alice", model.EventProgress, "", "", 3),
		makeEvent("alice", model.EventLockRel, "main.go", "", 4),
	}
	for _, e := range events {
		if _, err := s.InsertEvent(e); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if snap.TotalEvents != 4 {
		t.Errorf("expected 4 events, got %d", snap.TotalEvents)
	}
}

func TestBuildWithReopenedStore(t *testing.T) {
	// Create a temp DB, close it, reopen, and verify Build still works.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	s.Close()

	s2, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New re-open: %v", err)
	}
	defer s2.Close()

	snap, err := Build(s2)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if snap == nil {
		t.Fatal("snapshot should not be nil")
	}
}

func TestBuildFetchesNewestEvents(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.RegisterAgent("alice"); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Insert 10 events with ascending timestamps.
	for i := 1; i <= 10; i++ {
		e := makeEvent("alice", model.EventMsg, "bob", fmt.Sprintf("msg-%d", i), int64(i))
		if _, err := s.InsertEvent(e); err != nil {
			t.Fatalf("InsertEvent %d: %v", i, err)
		}
	}

	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// TotalEvents should reflect the real DB count (via MaxEventID).
	if snap.TotalEvents != 10 {
		t.Errorf("expected TotalEvents=10, got %d", snap.TotalEvents)
	}

	// All 10 events should be present (well under the 500 limit).
	if len(snap.Events) != 10 {
		t.Errorf("expected 10 events in snapshot, got %d", len(snap.Events))
	}

	// Last event should have the highest Lamport TS.
	if len(snap.Events) > 0 {
		lastEvent := snap.Events[len(snap.Events)-1]
		if lastEvent.LamportTS != 10 {
			t.Errorf("expected last event LamportTS=10, got %d", lastEvent.LamportTS)
		}
	}
}

func TestBuildClosedStoreReturnsError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clockmail.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	s.Close() // Close before Build

	_, err = Build(s)
	if err == nil {
		t.Error("Build on closed store should return an error")
	}
}

func TestBuildFrontierStatusMap(t *testing.T) {
	s := newTestStore(t)

	// Register three agents.
	for _, id := range []string{"alice", "bob", "carol"} {
		if _, err := s.RegisterAgent(id); err != nil {
			t.Fatalf("RegisterAgent %s: %v", id, err)
		}
	}

	// All at same epoch — everyone should be safe (no one is behind anyone else).
	snap, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, id := range []string{"alice", "bob", "carol"} {
		fs, ok := snap.FrontierStatus[id]
		if !ok {
			t.Errorf("missing frontier status for %s", id)
			continue
		}
		// All agents at epoch=0, round=0 — they are all at the same position.
		// Each agent's position is LessEq to the others, so NOT safe.
		// Actually: ComputeFrontierStatus skips self, and other agents at (0,0) LessEq (0,0) = true,
		// so no one is safe when all are at the same position with 3+ agents.
		if fs.SafeToFinalize {
			t.Errorf("%s should NOT be safe (other agents at same position block it)", id)
		}
	}
}
