package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/daviddao/clockmail/pkg/frontier"
	"github.com/daviddao/clockmail/pkg/model"
	"github.com/daviddao/clockmail_viewer/internal/snapshot"
)

// testSnapshot creates a snapshot with test data for rendering tests.
func testSnapshot() *snapshot.DataSnapshot {
	now := time.Now()
	agents := []model.Agent{
		{ID: "alice", Clock: 10, Epoch: 1, Round: 0, Registered: now, LastSeen: now},
		{ID: "bob", Clock: 5, Epoch: 0, Round: 0, Registered: now, LastSeen: now},
	}
	events := []model.Event{
		{ID: 1, AgentID: "alice", LamportTS: 1, Kind: model.EventMsg, Target: "bob", Body: "hello", CreatedAt: now},
		{ID: 2, AgentID: "bob", LamportTS: 2, Kind: model.EventMsg, Target: "alice", Body: "hi back", CreatedAt: now},
		{ID: 3, AgentID: "alice", LamportTS: 3, Kind: model.EventLockReq, Target: "main.go", CreatedAt: now},
		{ID: 4, AgentID: "alice", LamportTS: 4, Kind: model.EventProgress, CreatedAt: now},
	}
	locks := []model.Lock{
		{Path: "main.go", AgentID: "alice", LamportTS: 3, Epoch: 1, Exclusive: true, ExpiresAt: now.Add(time.Hour)},
	}

	// Compute frontier status.
	active := []model.Pointstamp{
		{Timestamp: model.Timestamp{Epoch: 1, Round: 0}, AgentID: "alice"},
		{Timestamp: model.Timestamp{Epoch: 0, Round: 0}, AgentID: "bob"},
	}
	f := frontier.ComputeFrontier(active)
	fStatus := map[string]frontier.FrontierStatus{
		"alice": frontier.ComputeFrontierStatus("alice", model.Timestamp{Epoch: 1, Round: 0}, active),
		"bob":   frontier.ComputeFrontierStatus("bob", model.Timestamp{Epoch: 0, Round: 0}, active),
	}

	return &snapshot.DataSnapshot{
		Agents:         agents,
		Events:         events,
		Locks:          locks,
		Frontier:       f,
		FrontierStatus: fStatus,
		ActiveAgents:   2,
		StaleAgents:    0,
		TotalEvents:    4,
		ActiveLocks:    1,
		BuiltAt:        now,
	}
}

// testModel creates a uiModel with test data (no store or watcher needed for render tests).
func testModel() uiModel {
	snap := testSnapshot()
	m := uiModel{
		snap:        snap,
		width:       80,
		height:      24,
		lastRefresh: time.Now(),
	}
	m.help.Width = 80
	return m
}

func TestParseViewFlag(t *testing.T) {
	tests := []struct {
		input string
		want  viewID
		err   bool
	}{
		{"dashboard", viewDashboard, false},
		{"Dashboard", viewDashboard, false},
		{"d", viewDashboard, false},
		{"messages", viewMessages, false},
		{"m", viewMessages, false},
		{"locks", viewLocks, false},
		{"l", viewLocks, false},
		{"frontier", viewFrontier, false},
		{"f", viewFrontier, false},
		{"timeline", viewTimeline, false},
		{"t", viewTimeline, false},
		{"diagram", viewDiagram, false},
		{"Diagram", viewDiagram, false},
		{"s", viewDiagram, false},
		{"bogus", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseViewFlag(tt.input)
			if tt.err {
				if err == nil {
					t.Errorf("parseViewFlag(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseViewFlag(%q) unexpected error: %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("parseViewFlag(%q) = %v, want %v", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestViewIDString(t *testing.T) {
	tests := []struct {
		v    viewID
		want string
	}{
		{viewDashboard, "Dashboard"},
		{viewMessages, "Messages"},
		{viewLocks, "Locks"},
		{viewFrontier, "Frontier"},
		{viewTimeline, "Timeline"},
		{viewDiagram, "Diagram"},
		{viewAgentDetail, "Agent Detail"},
		{viewID(99), "?"},
	}

	for _, tt := range tests {
		got := tt.v.String()
		if got != tt.want {
			t.Errorf("viewID(%d).String() = %q, want %q", int(tt.v), got, tt.want)
		}
	}
}

func TestViewLoading(t *testing.T) {
	m := testModel()
	m.width = 0 // triggers "Loading..." state

	out := m.View()
	if out != "Loading..." {
		t.Errorf("expected 'Loading...' when width=0, got %q", out)
	}
}

func TestRenderDashboardContainsAgents(t *testing.T) {
	m := testModel()
	out := m.renderDashboard()

	if !strings.Contains(out, "alice") {
		t.Error("dashboard should contain agent 'alice'")
	}
	if !strings.Contains(out, "bob") {
		t.Error("dashboard should contain agent 'bob'")
	}
	if !strings.Contains(out, "Agents") {
		t.Error("dashboard should contain 'Agents' header")
	}
	if !strings.Contains(out, "Locks") {
		t.Error("dashboard should contain 'Locks' section")
	}
}

func TestRenderDashboardSelectedAgent(t *testing.T) {
	m := testModel()
	m.selectedAgent = 0

	out := m.renderDashboard()
	if !strings.Contains(out, "> ") {
		t.Error("dashboard should show cursor '> ' for selected agent")
	}
}

func TestRenderDashboardEmptyAgents(t *testing.T) {
	m := testModel()
	m.snap = &snapshot.DataSnapshot{
		FrontierStatus: map[string]frontier.FrontierStatus{},
		BuiltAt:        time.Now(),
	}

	out := m.renderDashboard()
	if !strings.Contains(out, "no agents registered") {
		t.Error("dashboard should show 'no agents registered' when empty")
	}
}

func TestRenderMessages(t *testing.T) {
	m := testModel()
	out := m.renderMessages()

	if !strings.Contains(out, "hello") {
		t.Error("messages view should contain 'hello'")
	}
	if !strings.Contains(out, "hi back") {
		t.Error("messages view should contain 'hi back'")
	}
	if !strings.Contains(out, "Messages") {
		t.Error("messages view should contain 'Messages' header")
	}
}

func TestRenderLocks(t *testing.T) {
	m := testModel()
	out := m.renderLocks()

	if !strings.Contains(out, "main.go") {
		t.Error("locks view should contain 'main.go'")
	}
	if !strings.Contains(out, "alice") {
		t.Error("locks view should show lock holder 'alice'")
	}
}

func TestRenderLocksEmpty(t *testing.T) {
	m := testModel()
	m.snap = &snapshot.DataSnapshot{
		FrontierStatus: map[string]frontier.FrontierStatus{},
		BuiltAt:        time.Now(),
	}

	out := m.renderLocks()
	if !strings.Contains(out, "no active locks") {
		t.Error("locks view should show 'no active locks' when empty")
	}
}

func TestRenderFrontier(t *testing.T) {
	m := testModel()
	out := m.renderFrontier()

	if !strings.Contains(out, "Frontier") {
		t.Error("frontier view should contain 'Frontier' header")
	}
	// Alice should be blocked by bob.
	if !strings.Contains(out, "BLOCKED") {
		t.Error("frontier view should show BLOCKED status for alice")
	}
}

func TestRenderTimeline(t *testing.T) {
	m := testModel()
	out := m.renderTimeline()

	if !strings.Contains(out, "Timeline") {
		t.Error("timeline view should contain 'Timeline' header")
	}
	// Should show events in some form.
	if !strings.Contains(out, "L:") {
		t.Error("timeline view should contain 'L:' for Lamport timestamps")
	}
	// Should show the causality legend.
	if !strings.Contains(out, "concurrent") {
		t.Error("timeline view should contain 'concurrent' in the legend")
	}
	if !strings.Contains(out, "causal") {
		t.Error("timeline view should contain 'causal' in the legend")
	}
	// Message events should have a causal arrow marker (→).
	if !strings.Contains(out, "\u2192") {
		t.Error("timeline view should contain causal arrow marker for messages")
	}
}

func TestRenderTimelineConcurrentEvents(t *testing.T) {
	now := time.Now()
	snap := testSnapshot()
	// Add concurrent events: same Lamport TS, different agents.
	snap.Events = []model.Event{
		{ID: 1, AgentID: "alice", LamportTS: 5, Kind: model.EventLockReq, Target: "file.go", CreatedAt: now},
		{ID: 2, AgentID: "bob", LamportTS: 5, Kind: model.EventProgress, Epoch: 1, Round: 0, CreatedAt: now},
		{ID: 3, AgentID: "alice", LamportTS: 6, Kind: model.EventMsg, Target: "bob", Body: "done", CreatedAt: now},
	}

	m := testModel()
	m.snap = snap
	out := m.renderTimeline()

	// Concurrent group markers should appear (╓ and ╙).
	if !strings.Contains(out, "\u2553") {
		t.Error("timeline should show ╓ (top bracket) for concurrent group")
	}
	if !strings.Contains(out, "\u2559") {
		t.Error("timeline should show ╙ (bottom bracket) for concurrent group")
	}
}

func TestRenderTimelineNoConcurrency(t *testing.T) {
	now := time.Now()
	snap := testSnapshot()
	// All events have unique Lamport TS — no concurrency markers.
	snap.Events = []model.Event{
		{ID: 1, AgentID: "alice", LamportTS: 1, Kind: model.EventProgress, CreatedAt: now},
		{ID: 2, AgentID: "bob", LamportTS: 2, Kind: model.EventProgress, CreatedAt: now},
		{ID: 3, AgentID: "alice", LamportTS: 3, Kind: model.EventProgress, CreatedAt: now},
	}

	m := testModel()
	m.snap = snap
	out := m.renderTimeline()

	// No concurrent markers should appear.
	if strings.Contains(out, "\u2553") || strings.Contains(out, "\u2559") || strings.Contains(out, "\u2551") {
		t.Error("timeline should not show concurrency markers when all events have unique timestamps")
	}
}

func TestGroupByLamport(t *testing.T) {
	now := time.Now()
	events := []model.Event{
		{ID: 1, AgentID: "a", LamportTS: 1, CreatedAt: now},
		{ID: 2, AgentID: "b", LamportTS: 1, CreatedAt: now},
		{ID: 3, AgentID: "a", LamportTS: 2, CreatedAt: now},
		{ID: 4, AgentID: "a", LamportTS: 3, CreatedAt: now},
		{ID: 5, AgentID: "b", LamportTS: 3, CreatedAt: now},
		{ID: 6, AgentID: "c", LamportTS: 3, CreatedAt: now},
	}

	groups := groupByLamport(events)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].lamportTS != 1 || len(groups[0].events) != 2 {
		t.Errorf("group 0: want ts=1 len=2, got ts=%d len=%d", groups[0].lamportTS, len(groups[0].events))
	}
	if groups[1].lamportTS != 2 || len(groups[1].events) != 1 {
		t.Errorf("group 1: want ts=2 len=1, got ts=%d len=%d", groups[1].lamportTS, len(groups[1].events))
	}
	if groups[2].lamportTS != 3 || len(groups[2].events) != 3 {
		t.Errorf("group 2: want ts=3 len=3, got ts=%d len=%d", groups[2].lamportTS, len(groups[2].events))
	}
}

func TestGroupByLamportEmpty(t *testing.T) {
	groups := groupByLamport(nil)
	if groups != nil {
		t.Errorf("expected nil for empty input, got %v", groups)
	}
}

func TestIsConcurrentGroup(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name   string
		events []model.Event
		want   bool
	}{
		{
			name:   "single event",
			events: []model.Event{{AgentID: "a", CreatedAt: now}},
			want:   false,
		},
		{
			name: "same agent",
			events: []model.Event{
				{AgentID: "a", CreatedAt: now},
				{AgentID: "a", CreatedAt: now},
			},
			want: false,
		},
		{
			name: "different agents",
			events: []model.Event{
				{AgentID: "a", CreatedAt: now},
				{AgentID: "b", CreatedAt: now},
			},
			want: true,
		},
		{
			name: "three agents",
			events: []model.Event{
				{AgentID: "a", CreatedAt: now},
				{AgentID: "b", CreatedAt: now},
				{AgentID: "c", CreatedAt: now},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := timelineGroup{events: tt.events}
			got := isConcurrentGroup(g)
			if got != tt.want {
				t.Errorf("isConcurrentGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildCausalSet(t *testing.T) {
	now := time.Now()
	events := []model.Event{
		{ID: 1, Kind: model.EventMsg, CreatedAt: now},
		{ID: 2, Kind: model.EventLockReq, CreatedAt: now},
		{ID: 3, Kind: model.EventMsg, CreatedAt: now},
		{ID: 4, Kind: model.EventProgress, CreatedAt: now},
	}

	causal := buildCausalSet(events)
	if !causal[1] {
		t.Error("event 1 (msg) should be in causal set")
	}
	if causal[2] {
		t.Error("event 2 (lock_req) should not be in causal set")
	}
	if !causal[3] {
		t.Error("event 3 (msg) should be in causal set")
	}
	if causal[4] {
		t.Error("event 4 (progress) should not be in causal set")
	}
}

func TestRenderTimelineEmpty(t *testing.T) {
	m := testModel()
	m.snap = &snapshot.DataSnapshot{
		FrontierStatus: map[string]frontier.FrontierStatus{},
		BuiltAt:        time.Now(),
	}

	out := m.renderTimeline()
	if !strings.Contains(out, "no events") {
		t.Error("empty timeline should show 'no events'")
	}
	// Should not contain the legend when there are no events.
	if strings.Contains(out, "concurrent") {
		t.Error("empty timeline should not show the legend")
	}
}

func TestRenderTimelineThreeWayConcurrent(t *testing.T) {
	now := time.Now()
	snap := testSnapshot()
	snap.Events = []model.Event{
		{ID: 1, AgentID: "alice", LamportTS: 10, Kind: model.EventProgress, CreatedAt: now},
		{ID: 2, AgentID: "bob", LamportTS: 10, Kind: model.EventProgress, CreatedAt: now},
		{ID: 3, AgentID: "charlie", LamportTS: 10, Kind: model.EventProgress, CreatedAt: now},
	}

	m := testModel()
	m.snap = snap
	out := m.renderTimeline()

	// Three-way concurrent group: top (╓), middle (║), bottom (╙).
	if !strings.Contains(out, "\u2553") {
		t.Error("3-way concurrent should show ╓")
	}
	if !strings.Contains(out, "\u2551") {
		t.Error("3-way concurrent should show ║ for middle event")
	}
	if !strings.Contains(out, "\u2559") {
		t.Error("3-way concurrent should show ╙")
	}
}

func TestRenderAgentDetail(t *testing.T) {
	m := testModel()
	out := m.renderAgentDetailFor("alice")

	if !strings.Contains(out, "alice") {
		t.Error("agent detail should contain the agent name")
	}
}

func TestRenderAgentDetailUnknown(t *testing.T) {
	m := testModel()
	out := m.renderAgentDetailFor("unknown-agent")

	// Should handle gracefully (not panic).
	if out == "" {
		t.Error("agent detail for unknown agent should not be empty (should show header at minimum)")
	}
}

func TestRenderTitleBar(t *testing.T) {
	m := testModel()
	out := m.renderTitleBar()

	if !strings.Contains(out, "clockmail viewer") {
		t.Error("title bar should contain 'clockmail viewer'")
	}
	if !strings.Contains(out, "2 agents") {
		t.Error("title bar should show agent count")
	}
	if !strings.Contains(out, "1 locks") {
		t.Error("title bar should show lock count")
	}
	if !strings.Contains(out, "4 events") {
		t.Error("title bar should show event count")
	}
}

func TestRenderTabBar(t *testing.T) {
	m := testModel()
	m.activeView = viewMessages

	out := m.renderTabBar()
	if !strings.Contains(out, "Messages") {
		t.Error("tab bar should contain 'Messages'")
	}
	if !strings.Contains(out, "Dashboard") {
		t.Error("tab bar should contain 'Dashboard'")
	}
}

func TestRenderTabBarAgentDetail(t *testing.T) {
	m := testModel()
	m.activeView = viewAgentDetail
	m.detailAgentID = "alice"

	out := m.renderTabBar()
	if !strings.Contains(out, "Agent: alice") {
		t.Error("tab bar should show 'Agent: alice' when in agent detail view")
	}
}

func TestContextHelp(t *testing.T) {
	tests := []struct {
		v    viewID
		must string
	}{
		{viewDashboard, "enter"},
		{viewAgentDetail, "esc"},
		{viewMessages, "scroll"},
	}

	for _, tt := range tests {
		got := contextHelp(tt.v)
		if !strings.Contains(got, tt.must) {
			t.Errorf("contextHelp(%v) = %q, should contain %q", tt.v, got, tt.must)
		}
	}
}

func TestFilterEvents(t *testing.T) {
	snap := testSnapshot()

	msgs := filterEvents(snap.Events, model.EventMsg)
	if len(msgs) != 2 {
		t.Errorf("expected 2 msg events, got %d", len(msgs))
	}

	lockReqs := filterEvents(snap.Events, model.EventLockReq)
	if len(lockReqs) != 1 {
		t.Errorf("expected 1 lock_req event, got %d", len(lockReqs))
	}

	progress := filterEvents(snap.Events, model.EventProgress)
	if len(progress) != 1 {
		t.Errorf("expected 1 progress event, got %d", len(progress))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."}, // truncates at n=5 chars, appends "..."
		{"abc", 3, "abc"},
		{"abcd", 3, "abc..."}, // len > n, so truncate at 3 + "..."
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

func TestShortDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m30s"},
		{65 * time.Minute, "1h5m"},
		{-1 * time.Second, "expired"},
	}

	for _, tt := range tests {
		got := shortDuration(tt.d)
		if got != tt.want {
			t.Errorf("shortDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestBuildJSONOutput(t *testing.T) {
	snap := testSnapshot()
	out := buildJSONOutput(snap)

	// Validate structure.
	if len(out.Agents) != 2 {
		t.Errorf("expected 2 agents in JSON output, got %d", len(out.Agents))
	}
	if len(out.Locks) != 1 {
		t.Errorf("expected 1 lock in JSON output, got %d", len(out.Locks))
	}
	if len(out.Messages) != 2 {
		t.Errorf("expected 2 messages in JSON output, got %d", len(out.Messages))
	}
	if out.Stats.ActiveAgents != 2 {
		t.Errorf("expected 2 active agents in stats, got %d", out.Stats.ActiveAgents)
	}
	if out.Stats.ActiveLocks != 1 {
		t.Errorf("expected 1 active lock in stats, got %d", out.Stats.ActiveLocks)
	}
	if out.Stats.TotalEvents != 4 {
		t.Errorf("expected 4 total events in stats, got %d", out.Stats.TotalEvents)
	}

	// Validate agent fields.
	if out.Agents[0].ID != "alice" {
		t.Errorf("expected first agent to be 'alice', got %q", out.Agents[0].ID)
	}
	if out.Agents[0].LamportClock != 10 {
		t.Errorf("expected alice lamport_clock=10, got %d", out.Agents[0].LamportClock)
	}

	// Validate message fields.
	if out.Messages[0].From != "alice" || out.Messages[0].To != "bob" {
		t.Errorf("expected first message from alice to bob, got from=%q to=%q",
			out.Messages[0].From, out.Messages[0].To)
	}

	// Validate it serializes to valid JSON.
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !json.Valid(data) {
		t.Error("buildJSONOutput produced invalid JSON")
	}
}

func TestBuildJSONOutputEmptySnapshot(t *testing.T) {
	snap := &snapshot.DataSnapshot{
		FrontierStatus: map[string]frontier.FrontierStatus{},
		BuiltAt:        time.Now(),
	}
	out := buildJSONOutput(snap)

	if len(out.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(out.Agents))
	}
	if len(out.Locks) != 0 {
		t.Errorf("expected 0 locks, got %d", len(out.Locks))
	}

	// Should still serialize to valid JSON.
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !json.Valid(data) {
		t.Error("empty JSON output is invalid")
	}
}

func TestViewFullRender(t *testing.T) {
	m := testModel()
	m.activeView = viewDashboard

	out := m.View()
	if out == "" {
		t.Error("full View() render should not be empty")
	}
	if !strings.Contains(out, "clockmail viewer") {
		t.Error("full View() should contain title")
	}
	if !strings.Contains(out, "Dashboard") {
		t.Error("full View() should contain active tab name")
	}
}

func TestViewFullRenderEachView(t *testing.T) {
	views := []viewID{viewDashboard, viewMessages, viewLocks, viewFrontier, viewTimeline, viewDiagram}

	for _, v := range views {
		t.Run(v.String(), func(t *testing.T) {
			m := testModel()
			m.activeView = v

			out := m.View()
			if out == "" {
				t.Errorf("View() for %s should not be empty", v)
			}
		})
	}
}

// --- Bug fix tests ---

// TestScrollPosClampedInView verifies that View() handles scrollPos beyond
// content length gracefully without panicking (adventure4-ihh).
func TestScrollPosClampedInView(t *testing.T) {
	m := testModel()
	m.activeView = viewMessages
	m.scrollPos = 9999 // way beyond content

	// Should not panic and should produce output.
	out := m.View()
	if out == "" {
		t.Error("View() with excessive scrollPos should not be empty")
	}
}

// TestScrollPosBoundedOnDown verifies that pressing Down repeatedly doesn't
// allow scrollPos to grow unbounded (adventure4-ik4).
func TestScrollPosBoundedOnDown(t *testing.T) {
	m := testModel()
	m.activeView = viewTimeline
	m.width = 80
	m.height = 24

	// Press Down many more times than there are content lines.
	for i := 0; i < 500; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(uiModel)
	}

	// scrollPos should be bounded, not 500.
	maxReasonable := m.snap.TotalEvents + len(m.snap.Agents) + len(m.snap.Locks) + 20
	if m.scrollPos > maxReasonable {
		t.Errorf("scrollPos = %d after 500 Down presses, expected <= %d", m.scrollPos, maxReasonable)
	}
}

// TestSelectedAgentClampedOnSnapshotRefresh verifies that selectedAgent is
// clamped when a new snapshot has fewer agents (adventure4-cah).
func TestSelectedAgentClampedOnSnapshotRefresh(t *testing.T) {
	m := testModel()
	m.selectedAgent = 1 // selecting "bob" (index 1 of 2 agents)

	// Simulate a snapshot refresh that removes an agent (only alice remains).
	now := time.Now()
	newSnap := &snapshot.DataSnapshot{
		Agents: []model.Agent{
			{ID: "alice", Clock: 10, Epoch: 1, Round: 0, LastSeen: now},
		},
		FrontierStatus: map[string]frontier.FrontierStatus{},
		ActiveAgents:   1,
		TotalEvents:    4,
		BuiltAt:        now,
	}

	updated, _ := m.Update(snapshotReadyMsg{snap: newSnap})
	m = updated.(uiModel)

	if m.selectedAgent >= len(m.snap.Agents) {
		t.Errorf("selectedAgent = %d but only %d agents exist (should be clamped)",
			m.selectedAgent, len(m.snap.Agents))
	}
	if m.selectedAgent != 0 {
		t.Errorf("selectedAgent should be 0 after clamping, got %d", m.selectedAgent)
	}
}

// TestSelectedAgentClampedToZeroOnEmptySnapshot verifies no panic when all
// agents disappear (adventure4-cah edge case).
func TestSelectedAgentClampedToZeroOnEmptySnapshot(t *testing.T) {
	m := testModel()
	m.selectedAgent = 1

	emptySnap := &snapshot.DataSnapshot{
		FrontierStatus: map[string]frontier.FrontierStatus{},
		BuiltAt:        time.Now(),
	}

	updated, _ := m.Update(snapshotReadyMsg{snap: emptySnap})
	m = updated.(uiModel)

	if m.selectedAgent != 0 {
		t.Errorf("selectedAgent should be 0 on empty snapshot, got %d", m.selectedAgent)
	}

	// View() should not panic either.
	m.width = 80
	m.height = 24
	out := m.View()
	if out == "" {
		t.Error("View() with empty snapshot should not be empty")
	}
}

// TestViewScrollDoesNotMutateModel verifies that View() doesn't rely on
// mutating the model (value receiver) for scroll behavior (adventure4-ihh).
func TestViewScrollDoesNotMutateModel(t *testing.T) {
	m := testModel()
	m.activeView = viewTimeline
	m.scrollPos = 2

	// Call View() — should not change m.scrollPos.
	_ = m.View()

	if m.scrollPos != 2 {
		t.Errorf("View() mutated scrollPos from 2 to %d (value receiver should prevent this)", m.scrollPos)
	}
}

// --- Stale agent and lock expiration tests (adventure4-8rr) ---

// TestRenderDashboardStaleAgent verifies that agents with LastSeen > 10 min
// ago are rendered differently from active agents.
func TestRenderDashboardStaleAgent(t *testing.T) {
	now := time.Now()
	snap := testSnapshot()
	// Make bob stale (last seen 20 minutes ago).
	snap.Agents[1].LastSeen = now.Add(-20 * time.Minute)
	snap.StaleAgents = 1
	snap.ActiveAgents = 1

	m := testModel()
	m.snap = snap
	out := m.renderDashboard()

	// Both agents should still appear.
	if !strings.Contains(out, "alice") {
		t.Error("dashboard should contain active agent 'alice'")
	}
	if !strings.Contains(out, "bob") {
		t.Error("dashboard should contain stale agent 'bob'")
	}
}

// TestRenderLocksExpired verifies that expired locks show "EXPIRED" label.
func TestRenderLocksExpired(t *testing.T) {
	now := time.Now()
	snap := testSnapshot()
	// Make the lock expired (ExpiresAt in the past).
	snap.Locks[0].ExpiresAt = now.Add(-5 * time.Minute)

	m := testModel()
	m.snap = snap
	out := m.renderLocks()

	if !strings.Contains(out, "EXPIRED") {
		t.Error("locks view should show 'EXPIRED' for expired locks")
	}
}

// TestRenderAgentDetailStale verifies that a stale agent shows "STALE" badge.
func TestRenderAgentDetailStale(t *testing.T) {
	now := time.Now()
	snap := testSnapshot()
	snap.Agents[0].LastSeen = now.Add(-15 * time.Minute) // alice is stale

	m := testModel()
	m.snap = snap
	out := m.renderAgentDetailFor("alice")

	if !strings.Contains(out, "STALE") {
		t.Error("agent detail should show 'STALE' badge for stale agent")
	}
}

// TestRenderAgentDetailActive verifies that an active agent shows "ACTIVE" badge.
func TestRenderAgentDetailActive(t *testing.T) {
	m := testModel()
	out := m.renderAgentDetailFor("alice")

	if !strings.Contains(out, "ACTIVE") {
		t.Error("agent detail should show 'ACTIVE' badge for active agent")
	}
}

// --- Keyboard navigation (Update) tests (adventure4-8rr) ---

// TestUpdateTabCyclesViews verifies that pressing Tab cycles through views.
func TestUpdateTabCyclesViews(t *testing.T) {
	m := testModel()
	m.activeView = viewDashboard

	// Press Tab — should go to Messages.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(uiModel)
	if m.activeView != viewMessages {
		t.Errorf("after Tab from Dashboard, expected Messages, got %s", m.activeView)
	}

	// Press Tab again — should go to Locks.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(uiModel)
	if m.activeView != viewLocks {
		t.Errorf("after Tab from Messages, expected Locks, got %s", m.activeView)
	}
}

// TestUpdateTabWrapsAround verifies that Tab wraps from the last view to Dashboard.
func TestUpdateTabWrapsAround(t *testing.T) {
	m := testModel()
	m.activeView = viewDiagram // last view before sentinel

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(uiModel)
	if m.activeView != viewDashboard {
		t.Errorf("Tab from Timeline should wrap to Dashboard, got %s", m.activeView)
	}
}

// TestUpdateTabFromAgentDetailGoesToDashboard verifies that Tab from
// Agent Detail returns to Dashboard.
func TestUpdateTabFromAgentDetailGoesToDashboard(t *testing.T) {
	m := testModel()
	m.activeView = viewAgentDetail
	m.detailAgentID = "alice"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(uiModel)
	if m.activeView != viewDashboard {
		t.Errorf("Tab from AgentDetail should go to Dashboard, got %s", m.activeView)
	}
	if m.detailAgentID != "" {
		t.Error("detailAgentID should be cleared after Tab from AgentDetail")
	}
}

// TestUpdateUpDownDashboard verifies Up/Down changes selectedAgent on Dashboard.
func TestUpdateUpDownDashboard(t *testing.T) {
	m := testModel()
	m.activeView = viewDashboard
	m.selectedAgent = 0

	// Down — select second agent.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(uiModel)
	if m.selectedAgent != 1 {
		t.Errorf("Down should select agent 1, got %d", m.selectedAgent)
	}

	// Down again — should stay at 1 (only 2 agents).
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(uiModel)
	if m.selectedAgent != 1 {
		t.Errorf("Down at last agent should stay at 1, got %d", m.selectedAgent)
	}

	// Up — back to first agent.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(uiModel)
	if m.selectedAgent != 0 {
		t.Errorf("Up should select agent 0, got %d", m.selectedAgent)
	}

	// Up again — should stay at 0.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(uiModel)
	if m.selectedAgent != 0 {
		t.Errorf("Up at first agent should stay at 0, got %d", m.selectedAgent)
	}
}

// TestUpdateEnterDrillsIntoAgentDetail verifies Enter drills into Agent Detail.
func TestUpdateEnterDrillsIntoAgentDetail(t *testing.T) {
	m := testModel()
	m.activeView = viewDashboard
	m.selectedAgent = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(uiModel)
	if m.activeView != viewAgentDetail {
		t.Errorf("Enter should go to AgentDetail, got %s", m.activeView)
	}
	if m.detailAgentID != "alice" {
		t.Errorf("Enter should set detailAgentID to 'alice', got %q", m.detailAgentID)
	}
}

// TestUpdateEscReturnsFromAgentDetail verifies Esc returns to Dashboard.
func TestUpdateEscReturnsFromAgentDetail(t *testing.T) {
	m := testModel()
	m.activeView = viewAgentDetail
	m.detailAgentID = "alice"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(uiModel)
	if m.activeView != viewDashboard {
		t.Errorf("Esc from AgentDetail should go to Dashboard, got %s", m.activeView)
	}
	if m.detailAgentID != "" {
		t.Error("Esc should clear detailAgentID")
	}
}

// TestUpdateViewShortcuts verifies single-key shortcuts switch views.
func TestUpdateViewShortcuts(t *testing.T) {
	tests := []struct {
		key  string
		want viewID
	}{
		{"d", viewDashboard},
		{"m", viewMessages},
		{"l", viewLocks},
		{"f", viewFrontier},
		{"t", viewTimeline},
		{"s", viewDiagram},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := testModel()
			m.activeView = viewDashboard // start from dashboard

			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			m = updated.(uiModel)
			if m.activeView != tt.want {
				t.Errorf("key %q should switch to %s, got %s", tt.key, tt.want, m.activeView)
			}
		})
	}
}

// TestUpdateScrollResetsOnViewChange verifies scrollPos resets when switching views.
func TestUpdateScrollResetsOnViewChange(t *testing.T) {
	m := testModel()
	m.activeView = viewTimeline
	m.scrollPos = 10

	// Switch view via shortcut.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(uiModel)
	if m.scrollPos != 0 {
		t.Errorf("scrollPos should reset to 0 on view change, got %d", m.scrollPos)
	}
}

// TestUpdateHelpToggle verifies ? toggles help display.
func TestUpdateHelpToggle(t *testing.T) {
	m := testModel()
	if m.showHelp {
		t.Error("showHelp should start as false")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(uiModel)
	if !m.showHelp {
		t.Error("? should toggle showHelp to true")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(uiModel)
	if m.showHelp {
		t.Error("? again should toggle showHelp back to false")
	}
}

// TestUpdateWindowSizeMsg verifies width/height are captured.
func TestUpdateWindowSizeMsg(t *testing.T) {
	m := testModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(uiModel)
	if m.width != 120 {
		t.Errorf("width should be 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("height should be 40, got %d", m.height)
	}
}

// TestUpdateUpDownScroll verifies Up/Down scrolls in non-dashboard views.
func TestUpdateUpDownScroll(t *testing.T) {
	m := testModel()
	m.activeView = viewTimeline
	m.scrollPos = 0

	// Down increments scroll.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(uiModel)
	if m.scrollPos != 1 {
		t.Errorf("Down should increment scrollPos to 1, got %d", m.scrollPos)
	}

	// Up decrements scroll.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(uiModel)
	if m.scrollPos != 0 {
		t.Errorf("Up should decrement scrollPos to 0, got %d", m.scrollPos)
	}

	// Up at 0 stays at 0.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(uiModel)
	if m.scrollPos != 0 {
		t.Errorf("Up at 0 should stay at 0, got %d", m.scrollPos)
	}
}

// --- Diagram view tests ---

func TestRenderDiagram(t *testing.T) {
	m := testModel()
	out := m.renderDiagram()

	// Should contain the header.
	if !strings.Contains(out, "Lamport Space-Time Diagram") {
		t.Error("diagram should contain header 'Lamport Space-Time Diagram'")
	}
	// Should contain agent names as column headers.
	if !strings.Contains(out, "alice") {
		t.Error("diagram should contain agent 'alice'")
	}
	if !strings.Contains(out, "bob") {
		t.Error("diagram should contain agent 'bob'")
	}
	// Should contain Lamport timestamp labels.
	// Test data has events at L=1, 2, 3, 4.
	if !strings.Contains(out, "1") {
		t.Error("diagram should contain Lamport timestamp 1")
	}
	// Should contain event markers.
	if !strings.Contains(out, ">") {
		t.Error("diagram should contain '>' message marker")
	}
	// Should contain the legend.
	if !strings.Contains(out, "msg") {
		t.Error("diagram should contain 'msg' in legend")
	}
	if !strings.Contains(out, "heartbeat") {
		t.Error("diagram should contain 'heartbeat' in legend")
	}
}

func TestRenderDiagramEmpty(t *testing.T) {
	m := testModel()
	m.snap = &snapshot.DataSnapshot{
		FrontierStatus: map[string]frontier.FrontierStatus{},
		BuiltAt:        time.Now(),
	}

	out := m.renderDiagram()
	if !strings.Contains(out, "no events") {
		t.Error("empty diagram should show 'no events'")
	}
	// Should still have the header.
	if !strings.Contains(out, "Lamport Space-Time Diagram") {
		t.Error("empty diagram should still have header")
	}
}

func TestBuildDiagramData(t *testing.T) {
	now := time.Now()
	agents := []model.Agent{
		{ID: "alice", Clock: 10, LastSeen: now},
		{ID: "bob", Clock: 5, LastSeen: now},
	}
	events := []model.Event{
		{ID: 1, AgentID: "alice", LamportTS: 1, Kind: model.EventMsg, Target: "bob", Body: "hello", CreatedAt: now},
		{ID: 2, AgentID: "bob", LamportTS: 2, Kind: model.EventMsg, Target: "alice", Body: "reply", CreatedAt: now},
		{ID: 3, AgentID: "alice", LamportTS: 5, Kind: model.EventProgress, CreatedAt: now},
		{ID: 4, AgentID: "bob", LamportTS: 5, Kind: model.EventLockReq, Target: "file.go", CreatedAt: now},
	}

	agentOrder, rows := buildDiagramData(agents, events)

	// Agent order should match registration order.
	if len(agentOrder) != 2 || agentOrder[0] != "alice" || agentOrder[1] != "bob" {
		t.Errorf("agentOrder = %v, want [alice bob]", agentOrder)
	}

	// Should have 3 distinct Lamport timestamps: 1, 2, 5.
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// Rows should be sorted ascending by Lamport timestamp.
	if rows[0].lamportTS != 1 {
		t.Errorf("row 0 ts = %d, want 1", rows[0].lamportTS)
	}
	if rows[1].lamportTS != 2 {
		t.Errorf("row 1 ts = %d, want 2", rows[1].lamportTS)
	}
	if rows[2].lamportTS != 5 {
		t.Errorf("row 2 ts = %d, want 5", rows[2].lamportTS)
	}

	// Row at ts=1: alice has a message event.
	cell, ok := rows[0].cells["alice"]
	if !ok {
		t.Error("row ts=1 should have alice cell")
	}
	if cell.label != ">" {
		t.Errorf("alice at ts=1 label = %q, want '>'", cell.label)
	}

	// Row at ts=1 should have a message from alice to bob.
	if len(rows[0].messages) != 1 {
		t.Fatalf("row ts=1 messages len = %d, want 1", len(rows[0].messages))
	}
	if rows[0].messages[0].fromAgent != "alice" || rows[0].messages[0].toAgent != "bob" {
		t.Errorf("row ts=1 message: from=%q to=%q, want alice->bob",
			rows[0].messages[0].fromAgent, rows[0].messages[0].toAgent)
	}

	// Row at ts=5: both alice and bob have events (concurrent).
	if _, ok := rows[2].cells["alice"]; !ok {
		t.Error("row ts=5 should have alice cell")
	}
	if _, ok := rows[2].cells["bob"]; !ok {
		t.Error("row ts=5 should have bob cell")
	}
	if rows[2].cells["alice"].label != "*" {
		t.Errorf("alice at ts=5 label = %q, want '*' (progress)", rows[2].cells["alice"].label)
	}
	if rows[2].cells["bob"].label != "L" {
		t.Errorf("bob at ts=5 label = %q, want 'L' (lock)", rows[2].cells["bob"].label)
	}
}

func TestBuildDiagramDataEmpty(t *testing.T) {
	agentOrder, rows := buildDiagramData(nil, nil)
	if len(agentOrder) != 0 {
		t.Errorf("expected empty agentOrder, got %v", agentOrder)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestSortInt64s(t *testing.T) {
	tests := []struct {
		name  string
		input []int64
		want  []int64
	}{
		{"already sorted", []int64{1, 2, 3}, []int64{1, 2, 3}},
		{"reverse", []int64{3, 2, 1}, []int64{1, 2, 3}},
		{"mixed", []int64{5, 1, 3, 2, 4}, []int64{1, 2, 3, 4, 5}},
		{"single", []int64{42}, []int64{42}},
		{"empty", []int64{}, []int64{}},
		{"duplicates", []int64{3, 1, 3, 2, 1}, []int64{1, 1, 2, 3, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := make([]int64, len(tt.input))
			copy(s, tt.input)
			sortInt64s(s)
			for i, v := range s {
				if v != tt.want[i] {
					t.Errorf("sortInt64s(%v) = %v, want %v", tt.input, s, tt.want)
					break
				}
			}
		})
	}
}

func TestAgentIndex(t *testing.T) {
	agents := []string{"alice", "bob", "charlie"}

	if got := agentIndex(agents, "alice"); got != 0 {
		t.Errorf("agentIndex(alice) = %d, want 0", got)
	}
	if got := agentIndex(agents, "bob"); got != 1 {
		t.Errorf("agentIndex(bob) = %d, want 1", got)
	}
	if got := agentIndex(agents, "charlie"); got != 2 {
		t.Errorf("agentIndex(charlie) = %d, want 2", got)
	}
	if got := agentIndex(agents, "unknown"); got != -1 {
		t.Errorf("agentIndex(unknown) = %d, want -1", got)
	}
	if got := agentIndex(nil, "alice"); got != -1 {
		t.Errorf("agentIndex on nil = %d, want -1", got)
	}
}

func TestRenderDiagramWithMessages(t *testing.T) {
	// Verify message arrows appear in diagram output.
	m := testModel()
	out := m.renderDiagram()

	// Test data has alice->bob message at ts=1, bob->alice at ts=2.
	// The diagram should contain arrow characters.
	// Right arrow: ▶ (U+25B6)
	// Left arrow: ◀ (U+25C0)
	hasRightArrow := strings.Contains(out, "\u25B6")
	hasLeftArrow := strings.Contains(out, "\u25C0")
	if !hasRightArrow && !hasLeftArrow {
		t.Error("diagram with messages should contain arrow characters (▶ or ◀)")
	}
}

func TestRenderDiagramProcessLines(t *testing.T) {
	// Verify process lines (│) appear for agents with no event at a given timestamp.
	now := time.Now()
	snap := testSnapshot()
	// Events only for alice at multiple timestamps; bob should show │.
	snap.Events = []model.Event{
		{ID: 1, AgentID: "alice", LamportTS: 1, Kind: model.EventProgress, CreatedAt: now},
		{ID: 2, AgentID: "alice", LamportTS: 3, Kind: model.EventProgress, CreatedAt: now},
		{ID: 3, AgentID: "alice", LamportTS: 5, Kind: model.EventProgress, CreatedAt: now},
	}

	m := testModel()
	m.snap = snap
	out := m.renderDiagram()

	// Process line character: │ (U+2502)
	if !strings.Contains(out, "\u2502") {
		t.Error("diagram should show process lines (│) for agents with no event at a timestamp")
	}
}
