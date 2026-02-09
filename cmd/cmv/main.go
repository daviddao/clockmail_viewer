// cmv is a real-time TUI viewer for the clockmail multi-agent coordination system.
//
// It watches the clockmail SQLite database for changes and displays agents,
// Lamport clocks, locks, messages, and Naiad frontier status in real time.
//
// Usage:
//
//	cmv                         # Auto-discover .clockmail/clockmail.db
//	cmv --db <path>             # Use specific database path
//	cmv --json                  # Dump current state as JSON and exit
//	cmv --agent <id>            # Focus on a specific agent on startup
//	cmv --view dashboard        # Start in a specific view
//	cmv --refresh 5s            # Set polling fallback interval
//	cmv --version               # Print version and exit
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/daviddao/clockmail/pkg/model"
	"github.com/daviddao/clockmail/pkg/store"
	"github.com/daviddao/clockmail_viewer/internal/datasource"
	"github.com/daviddao/clockmail_viewer/internal/snapshot"
)

// Version is set via ldflags at build time (e.g. -X main.Version=v0.1.0).
var Version = "dev"

// parseViewFlag maps a --view flag string to a viewID.
func parseViewFlag(s string) (viewID, error) {
	switch strings.ToLower(s) {
	case "dashboard", "d":
		return viewDashboard, nil
	case "messages", "m":
		return viewMessages, nil
	case "locks", "l":
		return viewLocks, nil
	case "frontier", "f":
		return viewFrontier, nil
	case "timeline", "t":
		return viewTimeline, nil
	case "diagram", "s":
		return viewDiagram, nil
	default:
		return 0, fmt.Errorf("unknown view %q (valid: dashboard, messages, locks, frontier, timeline, diagram)", s)
	}
}

// jsonOutput is the structure for --json mode, matching cm status --json format.
type jsonOutput struct {
	Agents   []jsonAgent   `json:"agents"`
	Locks    []jsonLock    `json:"locks"`
	Frontier []jsonPoint   `json:"frontier"`
	Messages []jsonMessage `json:"messages"`
	Stats    jsonStats     `json:"stats"`
}

type jsonAgent struct {
	ID           string `json:"id"`
	LamportClock int64  `json:"lamport_clock"`
	Epoch        int64  `json:"epoch"`
	Round        int64  `json:"round"`
	LastSeen     string `json:"last_seen"`
	Safe         bool   `json:"safe_to_finalize"`
}

type jsonLock struct {
	Path      string `json:"path"`
	AgentID   string `json:"agent_id"`
	LamportTS int64  `json:"lamport_ts"`
	ExpiresAt string `json:"expires_at"`
}

type jsonPoint struct {
	AgentID string `json:"agent_id"`
	Epoch   int64  `json:"epoch"`
	Round   int64  `json:"round"`
}

type jsonMessage struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Body      string `json:"body"`
	LamportTS int64  `json:"lamport_ts"`
}

type jsonStats struct {
	ActiveAgents int `json:"active_agents"`
	StaleAgents  int `json:"stale_agents"`
	TotalEvents  int `json:"total_events"`
	ActiveLocks  int `json:"active_locks"`
}

func main() {
	dbPath := flag.String("db", "", "path to clockmail.db (default: auto-discover)")
	refreshDur := flag.Duration("refresh", 2*time.Second, "polling fallback interval")
	jsonMode := flag.Bool("json", false, "dump current state as JSON and exit (no TUI)")
	agentFlag := flag.String("agent", "", "highlight/focus a specific agent on startup")
	viewFlag := flag.String("view", "", "start in specific view (dashboard|messages|locks|frontier|timeline)")
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("cmv %s\n", Version)
		os.Exit(0)
	}

	if *dbPath != "" {
		os.Setenv("CLOCKMAIL_DB", *dbPath)
	}

	s, path, err := datasource.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmv: %v\n", err)
		os.Exit(1)
	}

	// --json mode: build snapshot, print JSON, exit.
	if *jsonMode {
		snap, err := snapshot.Build(s)
		if err != nil {
			s.Close()
			fmt.Fprintf(os.Stderr, "cmv: snapshot: %v\n", err)
			os.Exit(1)
		}
		s.Close()
		out := buildJSONOutput(snap)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "cmv: json: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	w, err := datasource.NewWatcher(path)
	if err != nil {
		s.Close()
		fmt.Fprintf(os.Stderr, "cmv: watch: %v\n", err)
		os.Exit(1)
	}

	snap, err := snapshot.Build(s)
	if err != nil {
		w.Close()
		s.Close()
		fmt.Fprintf(os.Stderr, "cmv: snapshot: %v\n", err)
		os.Exit(1)
	}

	m := newModel(s, w, snap, path)
	m.refreshInterval = *refreshDur

	// Apply --view flag.
	if *viewFlag != "" {
		v, err := parseViewFlag(*viewFlag)
		if err != nil {
			w.Close()
			s.Close()
			fmt.Fprintf(os.Stderr, "cmv: %v\n", err)
			os.Exit(1)
		}
		m.activeView = v
	}

	// Apply --agent flag: focus on the specified agent.
	if *agentFlag != "" {
		for i, ag := range snap.Agents {
			if ag.ID == *agentFlag {
				m.selectedAgent = i
				m.detailAgentID = ag.ID
				m.activeView = viewAgentDetail
				break
			}
		}
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Feed DB change events into the TUI.
	go func() {
		for range w.Changes() {
			p.Send(dbChangedMsg{})
		}
	}()

	// Polling fallback: refresh at --refresh interval even if fsnotify misses events.
	go func() {
		ticker := time.NewTicker(*refreshDur)
		defer ticker.Stop()
		for range ticker.C {
			p.Send(dbChangedMsg{})
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cmv: %v\n", err)
		os.Exit(1)
	}
}

// buildJSONOutput converts a snapshot into the JSON output structure.
func buildJSONOutput(snap *snapshot.DataSnapshot) jsonOutput {
	agents := make([]jsonAgent, len(snap.Agents))
	for i, ag := range snap.Agents {
		safe := false
		if fs, ok := snap.FrontierStatus[ag.ID]; ok {
			safe = fs.SafeToFinalize
		}
		agents[i] = jsonAgent{
			ID:           ag.ID,
			LamportClock: ag.Clock,
			Epoch:        ag.Epoch,
			Round:        ag.Round,
			LastSeen:     ag.LastSeen.Format(time.RFC3339),
			Safe:         safe,
		}
	}

	locks := make([]jsonLock, len(snap.Locks))
	for i, l := range snap.Locks {
		locks[i] = jsonLock{
			Path:      l.Path,
			AgentID:   l.AgentID,
			LamportTS: l.LamportTS,
			ExpiresAt: l.ExpiresAt.Format(time.RFC3339),
		}
	}

	points := make([]jsonPoint, len(snap.Frontier))
	for i, p := range snap.Frontier {
		points[i] = jsonPoint{
			AgentID: p.AgentID,
			Epoch:   p.Timestamp.Epoch,
			Round:   p.Timestamp.Round,
		}
	}

	msgs := filterEvents(snap.Events, model.EventMsg)
	messages := make([]jsonMessage, len(msgs))
	for i, e := range msgs {
		messages[i] = jsonMessage{
			From:      e.AgentID,
			To:        e.Target,
			Body:      e.Body,
			LamportTS: e.LamportTS,
		}
	}

	return jsonOutput{
		Agents:   agents,
		Locks:    locks,
		Frontier: points,
		Messages: messages,
		Stats: jsonStats{
			ActiveAgents: snap.ActiveAgents,
			StaleAgents:  snap.StaleAgents,
			TotalEvents:  snap.TotalEvents,
			ActiveLocks:  snap.ActiveLocks,
		},
	}
}

// --- Messages ---

type dbChangedMsg struct{}

type snapshotReadyMsg struct {
	snap *snapshot.DataSnapshot
	err  error
}

type tickMsg struct{}

// --- Key bindings ---

type keyMap struct {
	Quit    key.Binding
	Tab     key.Binding
	Refresh key.Binding
	Up      key.Binding
	Down    key.Binding
	Help    key.Binding
	Enter   key.Binding
	Esc     key.Binding
	Filter  key.Binding
}

var keys = keyMap{
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next view")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "down")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select agent")),
	Esc:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter agent")),
}

// viewKeys maps single keys to views for fast navigation.
var viewKeys = map[string]viewID{
	"d": viewDashboard,
	"m": viewMessages,
	"l": viewLocks,
	"f": viewFrontier,
	"t": viewTimeline,
	"s": viewDiagram,
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Refresh, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Tab, k.Refresh, k.Up, k.Down},
		{k.Enter, k.Esc, k.Help, k.Quit},
	}
}

// contextHelp returns help text appropriate for the current view.
func contextHelp(v viewID) string {
	switch v {
	case viewDashboard:
		return "j/k: select agent | enter: drill down | d/m/l/f/t/s: views | tab: next | ?: help | q: quit"
	case viewAgentDetail:
		return "j/k: scroll | esc: back to dashboard | d/m/l/f/t/s: views | ?: help | q: quit"
	case viewMessages, viewTimeline:
		return "j/k: scroll | /: filter agent | d/m/l/f/t/s: views | tab: next | ?: help | q: quit"
	default:
		return "j/k: scroll | d/m/l/f/t/s: views | tab: next | ?: help | q: quit"
	}
}

// --- Views ---

type viewID int

const (
	viewDashboard viewID = iota
	viewMessages
	viewLocks
	viewFrontier
	viewTimeline
	viewDiagram
	viewCount // sentinel — views below here are not in the tab bar
	viewAgentDetail
)

func (v viewID) String() string {
	switch v {
	case viewDashboard:
		return "Dashboard"
	case viewMessages:
		return "Messages"
	case viewLocks:
		return "Locks"
	case viewFrontier:
		return "Frontier"
	case viewTimeline:
		return "Timeline"
	case viewDiagram:
		return "Diagram"
	case viewAgentDetail:
		return "Agent Detail"
	}
	return "?"
}

// --- Model ---

type uiModel struct {
	store   *store.Store
	watcher *datasource.Watcher
	snap    *snapshot.DataSnapshot
	dbPath  string

	activeView      viewID
	prevView        viewID // for Esc navigation
	width           int
	height          int
	scrollPos       int
	selectedAgent   int
	detailAgentID   string // agent ID for detail view
	filterAgent     string // agent filter for Messages/Timeline ("" = all)
	refreshInterval time.Duration

	help     help.Model
	showHelp bool

	lastRefresh time.Time
}

func newModel(s *store.Store, w *datasource.Watcher, snap *snapshot.DataSnapshot, dbPath string) uiModel {
	h := help.New()
	return uiModel{
		store:       s,
		watcher:     w,
		snap:        snap,
		dbPath:      dbPath,
		help:        h,
		lastRefresh: time.Now(),
	}
}

func (m uiModel) Init() tea.Cmd {
	return tea.Batch(
		tickEvery(),
	)
}

func tickEvery() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Check single-key view shortcuts first (always available).
		if v, ok := viewKeys[msg.String()]; ok {
			m.activeView = v
			m.scrollPos = 0
			m.detailAgentID = ""
			// Clear agent filter when leaving filterable views.
			if v != viewMessages && v != viewTimeline {
				m.filterAgent = ""
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			m.watcher.Close()
			m.store.Close()
			return m, tea.Quit

		case key.Matches(msg, keys.Esc):
			// Back navigation from agent detail.
			if m.activeView == viewAgentDetail {
				m.activeView = viewDashboard
				m.detailAgentID = ""
				m.scrollPos = 0
			}

		case key.Matches(msg, keys.Enter):
			// Drill into agent detail from dashboard.
			if m.activeView == viewDashboard && len(m.snap.Agents) > 0 {
				if m.selectedAgent >= 0 && m.selectedAgent < len(m.snap.Agents) {
					m.detailAgentID = m.snap.Agents[m.selectedAgent].ID
					m.prevView = m.activeView
					m.activeView = viewAgentDetail
					m.scrollPos = 0
				}
			}

		case key.Matches(msg, keys.Tab):
			if m.activeView == viewAgentDetail {
				// Tab from agent detail goes back to dashboard
				m.activeView = viewDashboard
				m.detailAgentID = ""
			} else {
				m.activeView = (m.activeView + 1) % viewCount
			}
			// Clear filter when leaving filterable views.
			if m.activeView != viewMessages && m.activeView != viewTimeline {
				m.filterAgent = ""
			}
			m.scrollPos = 0

		case key.Matches(msg, keys.Refresh):
			return m, m.refreshSnapshot()

		case key.Matches(msg, keys.Up):
			if m.activeView == viewDashboard {
				if m.selectedAgent > 0 {
					m.selectedAgent--
				}
			} else {
				if m.scrollPos > 0 {
					m.scrollPos--
				}
			}

		case key.Matches(msg, keys.Down):
			if m.activeView == viewDashboard {
				if m.selectedAgent < len(m.snap.Agents)-1 {
					m.selectedAgent++
				}
			} else {
				// Estimate max scroll generously. Each event may produce
				// multiple lines after message body wrapping, so multiply
				// by a factor. View() clamps if we overshoot (adventure4-ik4).
				maxScroll := (m.snap.TotalEvents+len(m.snap.Agents)+len(m.snap.Locks))*8 + 20
				if m.scrollPos < maxScroll {
					m.scrollPos++
				}
			}

		case key.Matches(msg, keys.Filter):
			// Cycle agent filter: "" -> agent1 -> agent2 -> ... -> "".
			// Only applies in Messages and Timeline views.
			if m.activeView == viewMessages || m.activeView == viewTimeline {
				agents := m.snap.Agents
				if m.filterAgent == "" && len(agents) > 0 {
					m.filterAgent = agents[0].ID
				} else {
					// Find current agent index and advance.
					found := false
					for i, ag := range agents {
						if ag.ID == m.filterAgent {
							if i+1 < len(agents) {
								m.filterAgent = agents[i+1].ID
							} else {
								m.filterAgent = "" // wrap to "all"
							}
							found = true
							break
						}
					}
					if !found {
						m.filterAgent = ""
					}
				}
				m.scrollPos = 0
			}

		case key.Matches(msg, keys.Help):
			m.showHelp = !m.showHelp
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width

	case dbChangedMsg:
		return m, m.refreshSnapshot()

	case snapshotReadyMsg:
		if msg.err == nil && msg.snap != nil {
			m.snap = msg.snap
			m.lastRefresh = time.Now()
			// Clamp selectedAgent to avoid index-out-of-bounds after agent
			// count changes between snapshots (adventure4-cah).
			if len(m.snap.Agents) == 0 {
				m.selectedAgent = 0
			} else if m.selectedAgent >= len(m.snap.Agents) {
				m.selectedAgent = len(m.snap.Agents) - 1
			}
		}

	case tickMsg:
		return m, tickEvery()
	}

	return m, nil
}

func (m uiModel) refreshSnapshot() tea.Cmd {
	s := m.store
	return func() tea.Msg {
		snap, err := snapshot.Build(s)
		return snapshotReadyMsg{snap: snap, err: err}
	}
}

// --- Styles ---

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7C3AED")).
			Background(lipgloss.Color("#1E1E2E")).
			Padding(0, 1)

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#CDD6F4")).
			Background(lipgloss.Color("#7C3AED")).
			Padding(0, 1)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6C7086")).
				Background(lipgloss.Color("#313244")).
				Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#89B4FA"))

	agentActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#A6E3A1"))

	agentStaleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F38BA8"))

	lockStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAB387"))

	safeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A6E3A1")).
			Bold(true)

	unsafeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F38BA8")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C7086"))

	msgFromStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#89B4FA")).
			Bold(true)

	msgToStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A6E3A1"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CDD6F4")).
			Background(lipgloss.Color("#1E1E2E"))
)

// --- View rendering ---

func (m uiModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Title bar.
	b.WriteString(m.renderTitleBar())
	b.WriteRune('\n')

	// Tab bar.
	b.WriteString(m.renderTabBar())
	b.WriteRune('\n')
	b.WriteRune('\n')

	// Content area.
	contentHeight := m.height - 5 // title + tabs + status + padding
	if m.showHelp {
		contentHeight -= 3
	}

	var content string

	// Split-pane: Dashboard + Agent Detail side by side on wide terminals.
	if m.activeView == viewDashboard && m.width >= 120 && m.detailAgentID == "" &&
		len(m.snap.Agents) > 0 && m.selectedAgent < len(m.snap.Agents) {
		// Auto-split: show dashboard left, selected agent detail right.
		leftWidth := m.width/2 - 1
		rightWidth := m.width - leftWidth - 3 // 3 for separator

		left := m.renderDashboard()
		agentID := m.snap.Agents[m.selectedAgent].ID
		right := m.renderAgentDetailFor(agentID)

		content = renderSplitPane(left, right, leftWidth, rightWidth, contentHeight)
	} else {
		switch m.activeView {
		case viewDashboard:
			content = m.renderDashboard()
		case viewMessages:
			content = m.renderMessages()
		case viewLocks:
			content = m.renderLocks()
		case viewFrontier:
			content = m.renderFrontier()
		case viewTimeline:
			content = m.renderTimeline()
		case viewDiagram:
			content = m.renderDiagram()
		case viewAgentDetail:
			content = m.renderAgentDetailFor(m.detailAgentID)
		}

		// Apply scroll using a local variable. View() is a value receiver
		// so mutating m.scrollPos here would be dead code (adventure4-ihh).
		lines := strings.Split(content, "\n")
		scrollPos := m.scrollPos
		if scrollPos >= len(lines) {
			scrollPos = max(0, len(lines)-1)
		}
		if scrollPos > 0 && scrollPos < len(lines) {
			lines = lines[scrollPos:]
		}
		if len(lines) > contentHeight {
			lines = lines[:contentHeight]
		}
		content = strings.Join(lines, "\n")
	}

	// Truncate each line to terminal width so content doesn't wrap
	// on resize. Uses ANSI-aware width measurement.
	content = truncateLines(content, m.width)

	b.WriteString(content)

	// Pad to fill screen.
	rendered := strings.Count(b.String(), "\n")
	for rendered < m.height-2 {
		b.WriteRune('\n')
		rendered++
	}

	// Help / status bar.
	if m.showHelp {
		b.WriteString(m.help.View(keys))
	} else {
		b.WriteString(m.renderStatusBar())
	}

	return b.String()
}

func (m uiModel) renderTitleBar() string {
	title := titleStyle.Render("clockmail viewer")
	stats := dimStyle.Render(fmt.Sprintf(
		"%d agents | %d locks | %d events",
		m.snap.ActiveAgents+m.snap.StaleAgents,
		m.snap.ActiveLocks,
		m.snap.TotalEvents,
	))
	gap := strings.Repeat(" ", max(0, m.width-lipgloss.Width(title)-lipgloss.Width(stats)-2))
	return title + gap + stats
}

func (m uiModel) renderTabBar() string {
	var tabs []string
	for i := viewID(0); i < viewCount; i++ {
		if i == m.activeView {
			tabs = append(tabs, tabActiveStyle.Render(i.String()))
		} else {
			tabs = append(tabs, tabInactiveStyle.Render(i.String()))
		}
	}
	// Show Agent Detail as active tab when drilled in.
	if m.activeView == viewAgentDetail {
		tabs = append(tabs, tabActiveStyle.Render("Agent: "+m.detailAgentID))
	}
	return strings.Join(tabs, " ")
}

func (m uiModel) renderStatusBar() string {
	ago := time.Since(m.lastRefresh).Truncate(time.Second)
	left := fmt.Sprintf(" %s", contextHelp(m.activeView))
	right := fmt.Sprintf("refreshed %s ago ", ago)
	gap := strings.Repeat(" ", max(0, m.width-len(left)-len(right)))
	return statusBarStyle.Render(left + gap + right)
}

// --- Dashboard view ---

func (m uiModel) renderDashboard() string {
	var b strings.Builder

	// Agents table.
	b.WriteString(headerStyle.Render("Agents"))
	b.WriteRune('\n')
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %-16s %-10s %-14s %-12s %s",
		"ID", "Lamport", "Progress", "Last Seen", "Frontier")))
	b.WriteRune('\n')

	for i, ag := range m.snap.Agents {
		stale := time.Since(ag.LastSeen) > 10*time.Minute
		style := agentActiveStyle
		if stale {
			style = agentStaleStyle
		}

		// Frontier status for this agent.
		fStr := ""
		if fs, ok := m.snap.FrontierStatus[ag.ID]; ok {
			if fs.SafeToFinalize {
				fStr = safeStyle.Render("SAFE")
			} else {
				blockers := make([]string, 0, len(fs.BlockedBy))
				for _, bl := range fs.BlockedBy {
					blockers = append(blockers, bl.AgentID)
				}
				fStr = unsafeStyle.Render("BLOCKED by " + strings.Join(blockers, ","))
			}
		}

		seenAgo := shortDuration(time.Since(ag.LastSeen))
		cursor := "  "
		if i == m.selectedAgent {
			cursor = "> "
		}
		progress := fmt.Sprintf("e%d/r%d", ag.Epoch, ag.Round)
		line := fmt.Sprintf("%s%-16s %-10d %-14s %-12s %s",
			cursor, ag.ID, ag.Clock, progress, seenAgo, fStr)
		if i == m.selectedAgent {
			b.WriteString(style.Bold(true).Render(line))
		} else {
			b.WriteString(style.Render(line))
		}
		b.WriteRune('\n')
	}

	if len(m.snap.Agents) == 0 {
		b.WriteString(dimStyle.Render("  (no agents registered)"))
		b.WriteRune('\n')
	}

	b.WriteRune('\n')

	// Lock summary.
	b.WriteString(headerStyle.Render("Locks"))
	b.WriteRune('\n')
	if len(m.snap.Locks) > 0 {
		for _, l := range m.snap.Locks {
			remaining := shortDuration(time.Until(l.ExpiresAt))
			line := fmt.Sprintf("  %-30s held by %-12s L:%-4d expires in %s",
				l.Path, l.AgentID, l.LamportTS, remaining)
			b.WriteString(lockStyle.Render(line))
			b.WriteRune('\n')
		}
	} else {
		b.WriteString(dimStyle.Render("  (no active locks)"))
		b.WriteRune('\n')
	}

	b.WriteRune('\n')

	// Frontier summary.
	b.WriteString(headerStyle.Render("Frontier"))
	b.WriteRune('\n')
	if len(m.snap.Frontier) > 0 {
		for _, p := range m.snap.Frontier {
			line := fmt.Sprintf("  %s @ epoch=%d round=%d", p.AgentID, p.Timestamp.Epoch, p.Timestamp.Round)
			b.WriteString(dimStyle.Render(line))
			b.WriteRune('\n')
		}
	} else {
		b.WriteString(dimStyle.Render("  (no active pointstamps)"))
		b.WriteRune('\n')
	}

	return b.String()
}

// --- Messages view ---

func (m uiModel) renderMessages() string {
	var b strings.Builder
	if m.filterAgent != "" {
		b.WriteString(headerStyle.Render("Messages"))
		b.WriteString(dimStyle.Render(" "))
		b.WriteString(msgFromStyle.Render(fmt.Sprintf("[filter: %s]", m.filterAgent)))
	} else {
		b.WriteString(headerStyle.Render("Messages"))
	}
	b.WriteRune('\n')

	msgs := filterEvents(m.snap.Events, model.EventMsg)
	// Apply agent filter.
	if m.filterAgent != "" {
		var filtered []model.Event
		for _, e := range msgs {
			if eventMatchesAgent(e, m.filterAgent) {
				filtered = append(filtered, e)
			}
		}
		msgs = filtered
	}
	if len(msgs) == 0 {
		if m.filterAgent != "" {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  (no messages involving %s)", m.filterAgent)))
		} else {
			b.WriteString(dimStyle.Render("  (no messages)"))
		}
		b.WriteRune('\n')
		return b.String()
	}

	// Available width for message body wrapping.
	bodyIndent := "        " // 8 spaces
	bodyWidth := m.width - len(bodyIndent) - 1
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	for i := len(msgs) - 1; i >= 0; i-- {
		e := msgs[i]
		from := msgFromStyle.Render(e.AgentID)
		to := msgToStyle.Render(e.Target)
		ts := dimStyle.Render(fmt.Sprintf("[L:%d]", e.LamportTS))
		b.WriteString(fmt.Sprintf("  %s %s -> %s\n", ts, from, to))
		// Wrap message body to terminal width.
		for _, line := range wrapText(e.Body, bodyWidth) {
			b.WriteString(bodyIndent)
			b.WriteString(line)
			b.WriteRune('\n')
		}
	}

	return b.String()
}

// --- Locks view ---

func (m uiModel) renderLocks() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Active Locks"))
	b.WriteRune('\n')

	if len(m.snap.Locks) == 0 {
		b.WriteString(dimStyle.Render("  (no active locks)"))
		b.WriteRune('\n')
		return b.String()
	}

	b.WriteString(dimStyle.Render(fmt.Sprintf("  %-32s %-14s %-8s %-8s %s",
		"Path", "Agent", "Lamport", "Epoch", "TTL Remaining")))
	b.WriteRune('\n')

	for _, l := range m.snap.Locks {
		remaining := time.Until(l.ExpiresAt)
		ttlStr := shortDuration(remaining)
		if remaining < 0 {
			ttlStr = unsafeStyle.Render("EXPIRED")
		}
		line := fmt.Sprintf("  %-32s %-14s %-8d %-8d %s",
			l.Path, l.AgentID, l.LamportTS, l.Epoch, ttlStr)
		b.WriteString(lockStyle.Render(line))
		b.WriteRune('\n')
	}

	return b.String()
}

// --- Frontier view ---

func (m uiModel) renderFrontier() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Naiad Frontier"))
	b.WriteRune('\n')
	b.WriteRune('\n')

	// Global frontier.
	b.WriteString(headerStyle.Render("  Global Antichain"))
	b.WriteRune('\n')
	if len(m.snap.Frontier) > 0 {
		for _, p := range m.snap.Frontier {
			line := fmt.Sprintf("    %s @ epoch=%d round=%d",
				p.AgentID, p.Timestamp.Epoch, p.Timestamp.Round)
			b.WriteString(dimStyle.Render(line))
			b.WriteRune('\n')
		}
	} else {
		b.WriteString(dimStyle.Render("    (empty)"))
		b.WriteRune('\n')
	}

	b.WriteRune('\n')

	// Per-agent status.
	b.WriteString(headerStyle.Render("  Per-Agent Status"))
	b.WriteRune('\n')
	for _, ag := range m.snap.Agents {
		fs, ok := m.snap.FrontierStatus[ag.ID]
		if !ok {
			continue
		}
		if fs.SafeToFinalize {
			b.WriteString(fmt.Sprintf("    %s: %s (epoch=%d round=%d)\n",
				agentActiveStyle.Render(ag.ID),
				safeStyle.Render("SAFE"),
				ag.Epoch, ag.Round))
		} else {
			blockers := make([]string, 0, len(fs.BlockedBy))
			for _, bl := range fs.BlockedBy {
				blockers = append(blockers, fmt.Sprintf("%s@e%d/r%d",
					bl.AgentID, bl.Timestamp.Epoch, bl.Timestamp.Round))
			}
			b.WriteString(fmt.Sprintf("    %s: %s by %s\n",
				agentStaleStyle.Render(ag.ID),
				unsafeStyle.Render("BLOCKED"),
				strings.Join(blockers, ", ")))
		}
	}

	return b.String()
}

// --- Timeline view ---

// concurrentStyle highlights concurrent event markers.
var concurrentStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#F9E2AF")).
	Bold(true)

// causalStyle highlights causal link markers.
var causalStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#A6E3A1"))

// timelineGroup holds events sharing the same Lamport timestamp.
type timelineGroup struct {
	lamportTS int64
	events    []model.Event
}

// groupByLamport groups events by their Lamport timestamp.
// Input must be sorted by LamportTS ascending (as stored).
func groupByLamport(events []model.Event) []timelineGroup {
	if len(events) == 0 {
		return nil
	}
	var groups []timelineGroup
	current := timelineGroup{lamportTS: events[0].LamportTS}
	for _, e := range events {
		if e.LamportTS != current.lamportTS {
			groups = append(groups, current)
			current = timelineGroup{lamportTS: e.LamportTS}
		}
		current.events = append(current.events, e)
	}
	groups = append(groups, current)
	return groups
}

// isConcurrentGroup returns true if a group has events from multiple agents.
func isConcurrentGroup(g timelineGroup) bool {
	if len(g.events) <= 1 {
		return false
	}
	first := g.events[0].AgentID
	for _, e := range g.events[1:] {
		if e.AgentID != first {
			return true
		}
	}
	return false
}

// buildCausalSet returns the set of event IDs that are message sends.
// Message sends establish causal ordering: if A sends to B at TS=N,
// then B's receipt (and all subsequent events) must have TS > N.
func buildCausalSet(events []model.Event) map[int64]bool {
	causal := make(map[int64]bool)
	for _, e := range events {
		if e.Kind == model.EventMsg {
			causal[e.ID] = true
		}
	}
	return causal
}

func (m uiModel) renderTimeline() string {
	var b strings.Builder
	if m.filterAgent != "" {
		b.WriteString(headerStyle.Render("Event Timeline"))
		b.WriteString(dimStyle.Render(" "))
		b.WriteString(msgFromStyle.Render(fmt.Sprintf("[filter: %s]", m.filterAgent)))
	} else {
		b.WriteString(headerStyle.Render("Event Timeline"))
	}
	b.WriteRune('\n')

	// Apply agent filter.
	events := m.snap.Events
	if m.filterAgent != "" {
		var filtered []model.Event
		for _, e := range events {
			if eventMatchesAgent(e, m.filterAgent) {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}

	if len(events) == 0 {
		if m.filterAgent != "" {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  (no events involving %s)", m.filterAgent)))
		} else {
			b.WriteString(dimStyle.Render("  (no events)"))
		}
		b.WriteRune('\n')
		return b.String()
	}

	// Legend explaining Lamport ordering vs causality.
	b.WriteString(dimStyle.Render("  Sorted by Lamport clock (L). Only messages ("))
	b.WriteString(causalStyle.Render("\u2192"))
	b.WriteString(dimStyle.Render(") prove causal ordering."))
	b.WriteRune('\n')
	b.WriteString(dimStyle.Render("  "))
	b.WriteString(concurrentStyle.Render("Bracketed"))
	b.WriteString(dimStyle.Render(" events share a clock value and are definitely concurrent."))
	b.WriteRune('\n')
	b.WriteString(dimStyle.Render("  Other cross-agent events may also be concurrent (L order \u2260 causal order)."))
	b.WriteRune('\n')
	b.WriteRune('\n')

	// Group events by Lamport timestamp.
	groups := groupByLamport(events)
	causalIDs := buildCausalSet(events)

	// Body lines use a modest indent to show they belong to the message above
	// without wasting horizontal space on deep alignment.
	bodyIndent := "          " // 10 spaces
	bodyWidth := m.width - len(bodyIndent) - 1
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	// Show most recent first.
	for gi := len(groups) - 1; gi >= 0; gi-- {
		g := groups[gi]
		concurrent := isConcurrentGroup(g)

		for ei, e := range g.events {
			ts := dimStyle.Render(fmt.Sprintf("[L:%-4d]", e.LamportTS))
			agent := msgFromStyle.Render(e.AgentID)

			// Concurrency marker: show bracket for multi-agent groups.
			marker := "   "
			if concurrent {
				if ei == 0 {
					marker = concurrentStyle.Render(" \u2553 ") // ╓ top
				} else if ei == len(g.events)-1 {
					marker = concurrentStyle.Render(" \u2559 ") // ╙ bottom
				} else {
					marker = concurrentStyle.Render(" \u2551 ") // ║ middle
				}
			}

			// Causal indicator for message sends.
			causalMark := "  "
			if causalIDs[e.ID] {
				causalMark = causalStyle.Render("\u2192 ") // →
			}

			switch e.Kind {
			case model.EventMsg:
				// Header line: timestamp, markers, agent, and target.
				b.WriteString(fmt.Sprintf("  %s%s%s%s -> %s\n",
					ts, marker, causalMark, agent, msgToStyle.Render(e.Target)))
				// Body wrapped below with indent.
				for _, line := range wrapText(e.Body, bodyWidth) {
					b.WriteString(bodyIndent)
					b.WriteString(line)
					b.WriteRune('\n')
				}
			case model.EventLockReq:
				b.WriteString(fmt.Sprintf("  %s%s%s%s %s\n",
					ts, marker, causalMark, agent, lockStyle.Render("lock "+e.Target)))
			case model.EventLockRel:
				b.WriteString(fmt.Sprintf("  %s%s%s%s %s\n",
					ts, marker, causalMark, agent, dimStyle.Render("unlock "+e.Target)))
			case model.EventProgress:
				b.WriteString(fmt.Sprintf("  %s%s%s%s %s\n",
					ts, marker, causalMark, agent, dimStyle.Render(fmt.Sprintf("heartbeat e%d/r%d", e.Epoch, e.Round))))
			default:
				b.WriteString(fmt.Sprintf("  %s%s%s%s %s %s %s\n",
					ts, marker, causalMark, agent, string(e.Kind), e.Target, e.Body))
			}
		}
	}

	return b.String()
}

// --- Lamport Diagram view ---
//
// Renders a space-time diagram inspired by Fig 1 of Lamport's 1978 paper
// "Time, Clocks, and the Ordering of Events in a Distributed System".
//
// Layout: horizontal axis = agents (process columns), vertical axis = Lamport
// time (increasing downward). Each agent gets a vertical swimlane. Events are
// plotted as markers on the agent's column at the event's Lamport timestamp.
// Messages between agents are shown as arrows from sender to receiver.

// diagramStyle for the process column lines.
var diagramLineStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#6C7086"))

// diagramEventStyle for event dots.
var diagramEventStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#CDD6F4")).
	Bold(true)

// diagramMsgStyle for message arrows.
var diagramMsgStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#F9E2AF"))

// diagramRow represents one Lamport timestamp row in the diagram.
type diagramRow struct {
	lamportTS int64
	cells     map[string]diagramCell // agentID -> cell content
	messages  []diagramMsg           // messages sent at this timestamp
}

// diagramCell is what happened for one agent at one Lamport timestamp.
type diagramCell struct {
	event model.Event
	label string // short label for the cell
}

// diagramMsg represents a message arrow in the diagram.
type diagramMsg struct {
	fromAgent string
	toAgent   string
	body      string
}

// buildDiagramData constructs the row/column data for the space-time diagram.
func buildDiagramData(agents []model.Agent, events []model.Event) ([]string, []diagramRow) {
	// Collect unique agent IDs in registration order.
	agentOrder := make([]string, 0, len(agents))
	for _, ag := range agents {
		agentOrder = append(agentOrder, ag.ID)
	}

	// Build rows indexed by Lamport timestamp.
	rowMap := make(map[int64]*diagramRow)
	var timestamps []int64

	for _, e := range events {
		ts := e.LamportTS
		row, ok := rowMap[ts]
		if !ok {
			row = &diagramRow{
				lamportTS: ts,
				cells:     make(map[string]diagramCell),
			}
			rowMap[ts] = row
			timestamps = append(timestamps, ts)
		}

		// Determine cell label.
		var label string
		switch e.Kind {
		case model.EventMsg:
			label = ">"
		case model.EventLockReq:
			label = "L"
		case model.EventLockRel:
			label = "U"
		case model.EventProgress:
			label = "*"
		default:
			label = "?"
		}

		row.cells[e.AgentID] = diagramCell{event: e, label: label}

		// Track messages for arrows.
		if e.Kind == model.EventMsg && e.Target != "" {
			row.messages = append(row.messages, diagramMsg{
				fromAgent: e.AgentID,
				toAgent:   e.Target,
				body:      e.Body,
			})
		}
	}

	// Sort timestamps ascending.
	sortInt64s(timestamps)

	rows := make([]diagramRow, len(timestamps))
	for i, ts := range timestamps {
		rows[i] = *rowMap[ts]
	}

	return agentOrder, rows
}

// sortInt64s sorts a slice of int64 in ascending order.
func sortInt64s(s []int64) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// agentIndex returns the column index of an agent, or -1 if not found.
func agentIndex(agents []string, id string) int {
	for i, a := range agents {
		if a == id {
			return i
		}
	}
	return -1
}

func (m uiModel) renderDiagram() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Lamport Space-Time Diagram"))
	b.WriteRune('\n')

	if len(m.snap.Events) == 0 {
		b.WriteString(dimStyle.Render("  (no events)"))
		b.WriteRune('\n')
		return b.String()
	}

	// Legend.
	b.WriteString(dimStyle.Render("  Processes as columns, Lamport time increasing downward (cf. Lamport 1978, Fig 1)."))
	b.WriteRune('\n')
	b.WriteString(dimStyle.Render("  "))
	b.WriteString(diagramEventStyle.Render(">"))
	b.WriteString(dimStyle.Render("=msg "))
	b.WriteString(diagramEventStyle.Render("*"))
	b.WriteString(dimStyle.Render("=heartbeat "))
	b.WriteString(diagramEventStyle.Render("L"))
	b.WriteString(dimStyle.Render("=lock "))
	b.WriteString(diagramEventStyle.Render("U"))
	b.WriteString(dimStyle.Render("=unlock "))
	b.WriteString(diagramMsgStyle.Render("~~~>"))
	b.WriteString(dimStyle.Render("=message arrow"))
	b.WriteRune('\n')
	b.WriteRune('\n')

	agentOrder, rows := buildDiagramData(m.snap.Agents, m.snap.Events)
	if len(agentOrder) == 0 || len(rows) == 0 {
		b.WriteString(dimStyle.Render("  (no data)"))
		b.WriteRune('\n')
		return b.String()
	}

	// Compute column widths.
	colWidth := 14  // width per agent column
	tsColWidth := 7 // width for the timestamp label

	// Header row: agent names.
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %-*s", tsColWidth, "L")))
	for _, ag := range agentOrder {
		name := ag
		if len(name) > colWidth-2 {
			name = name[:colWidth-2]
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("%-*s", colWidth, name)))
	}
	b.WriteRune('\n')

	// Separator line.
	b.WriteString(dimStyle.Render("  " + strings.Repeat("\u2500", tsColWidth)))
	for range agentOrder {
		b.WriteString(dimStyle.Render(strings.Repeat("\u2500", colWidth)))
	}
	b.WriteRune('\n')

	// Render rows with time increasing downward (Lamport 1978, Fig 1).
	for ri := 0; ri < len(rows); ri++ {
		row := rows[ri]

		// Timestamp label.
		b.WriteString(dimStyle.Render(fmt.Sprintf("  %-*d", tsColWidth, row.lamportTS)))

		// Agent columns.
		for _, ag := range agentOrder {
			cell, hasEvent := row.cells[ag]
			if hasEvent {
				// Show event marker with agent-colored style.
				stale := false
				for _, a := range m.snap.Agents {
					if a.ID == ag {
						stale = time.Since(a.LastSeen) > 10*time.Minute
						break
					}
				}
				style := agentActiveStyle
				if stale {
					style = agentStaleStyle
				}

				marker := style.Bold(true).Render(cell.label)
				// Pad to column width (marker is 1 visible char).
				b.WriteString(fmt.Sprintf("%s%-*s", marker, colWidth-1, ""))
			} else {
				// Empty column — show the process line.
				b.WriteString(diagramLineStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u2502")))
			}
		}
		b.WriteRune('\n')

		// Render message arrows below the event row.
		for _, msg := range row.messages {
			fromIdx := agentIndex(agentOrder, msg.fromAgent)
			toIdx := agentIndex(agentOrder, msg.toAgent)
			if fromIdx < 0 || toIdx < 0 {
				continue
			}

			// Build the arrow line.
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %-*s", tsColWidth, "")))

			if fromIdx < toIdx {
				// Arrow going right: from -> to.
				for ci := range agentOrder {
					if ci < fromIdx {
						b.WriteString(diagramLineStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u2502")))
					} else if ci == fromIdx {
						b.WriteString(diagramMsgStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u2570"+strings.Repeat("\u2500", colWidth-2))))
					} else if ci > fromIdx && ci < toIdx {
						b.WriteString(diagramMsgStyle.Render(fmt.Sprintf("%-*s", colWidth, strings.Repeat("\u2500", colWidth))))
					} else if ci == toIdx {
						b.WriteString(diagramMsgStyle.Render("\u25B6"))
						b.WriteString(fmt.Sprintf("%-*s", colWidth-1, ""))
					} else {
						b.WriteString(diagramLineStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u2502")))
					}
				}
			} else if fromIdx > toIdx {
				// Arrow going left: from -> to.
				for ci := range agentOrder {
					if ci < toIdx {
						b.WriteString(diagramLineStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u2502")))
					} else if ci == toIdx {
						b.WriteString(diagramMsgStyle.Render("\u25C0"))
						b.WriteString(diagramMsgStyle.Render(fmt.Sprintf("%-*s", colWidth-1, strings.Repeat("\u2500", colWidth-1))))
					} else if ci > toIdx && ci < fromIdx {
						b.WriteString(diagramMsgStyle.Render(fmt.Sprintf("%-*s", colWidth, strings.Repeat("\u2500", colWidth))))
					} else if ci == fromIdx {
						b.WriteString(diagramMsgStyle.Render("\u256F"))
						b.WriteString(fmt.Sprintf("%-*s", colWidth-1, ""))
					} else {
						b.WriteString(diagramLineStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u2502")))
					}
				}
			}
			b.WriteRune('\n')
		}
	}

	return b.String()
}

// --- Agent Detail view ---

var (
	detailHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#CBA6F7"))

	detailSectionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#89B4FA")).
				MarginTop(1)
)

func (m uiModel) renderAgentDetailFor(agentID string) string {
	var b strings.Builder

	// Find agent.
	var agent *model.Agent
	for i := range m.snap.Agents {
		if m.snap.Agents[i].ID == agentID {
			agent = &m.snap.Agents[i]
			break
		}
	}

	if agent == nil {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  Agent %q not found", agentID)))
		return b.String()
	}

	// Header.
	stale := time.Since(agent.LastSeen) > 10*time.Minute
	statusBadge := safeStyle.Render("ACTIVE")
	if stale {
		statusBadge = unsafeStyle.Render("STALE")
	}

	b.WriteString(detailHeaderStyle.Render(fmt.Sprintf("Agent: %s", agent.ID)))
	b.WriteString("  ")
	b.WriteString(statusBadge)
	b.WriteRune('\n')
	b.WriteString(dimStyle.Render(fmt.Sprintf("  Lamport clock: %d | Progress: e%d/r%d | Last seen: %s ago",
		agent.Clock, agent.Epoch, agent.Round, shortDuration(time.Since(agent.LastSeen)))))
	b.WriteRune('\n')

	// Frontier status.
	if fs, ok := m.snap.FrontierStatus[agentID]; ok {
		if fs.SafeToFinalize {
			b.WriteString(fmt.Sprintf("  Frontier: %s (epoch=%d round=%d)\n",
				safeStyle.Render("SAFE"), agent.Epoch, agent.Round))
		} else {
			blockers := make([]string, 0, len(fs.BlockedBy))
			for _, bl := range fs.BlockedBy {
				blockers = append(blockers, fmt.Sprintf("%s@e%d/r%d",
					bl.AgentID, bl.Timestamp.Epoch, bl.Timestamp.Round))
			}
			b.WriteString(fmt.Sprintf("  Frontier: %s by %s\n",
				unsafeStyle.Render("BLOCKED"), strings.Join(blockers, ", ")))
		}
	}

	b.WriteRune('\n')

	// Locks held by this agent.
	b.WriteString(detailSectionStyle.Render("Locks Held"))
	b.WriteRune('\n')
	var agentLocks int
	for _, l := range m.snap.Locks {
		if l.AgentID == agentID {
			remaining := shortDuration(time.Until(l.ExpiresAt))
			b.WriteString(lockStyle.Render(fmt.Sprintf("  %s  (L:%d, expires in %s)",
				l.Path, l.LamportTS, remaining)))
			b.WriteRune('\n')
			agentLocks++
		}
	}
	if agentLocks == 0 {
		b.WriteString(dimStyle.Render("  (none)"))
		b.WriteRune('\n')
	}

	b.WriteRune('\n')

	// Messages sent by this agent.
	b.WriteString(detailSectionStyle.Render("Messages Sent"))
	b.WriteRune('\n')
	var sentCount int
	for i := len(m.snap.Events) - 1; i >= 0 && sentCount < 15; i-- {
		e := m.snap.Events[i]
		if e.Kind == model.EventMsg && e.AgentID == agentID {
			body := e.Body
			if len(body) > 80 {
				body = body[:80] + "..."
			}
			b.WriteString(fmt.Sprintf("  %s -> %s: %s\n",
				dimStyle.Render(fmt.Sprintf("[L:%d]", e.LamportTS)),
				msgToStyle.Render(e.Target),
				body))
			sentCount++
		}
	}
	if sentCount == 0 {
		b.WriteString(dimStyle.Render("  (none)"))
		b.WriteRune('\n')
	}

	b.WriteRune('\n')

	// Messages received by this agent.
	b.WriteString(detailSectionStyle.Render("Messages Received"))
	b.WriteRune('\n')
	var recvCount int
	for i := len(m.snap.Events) - 1; i >= 0 && recvCount < 15; i-- {
		e := m.snap.Events[i]
		if e.Kind == model.EventMsg && e.Target == agentID {
			body := e.Body
			if len(body) > 80 {
				body = body[:80] + "..."
			}
			b.WriteString(fmt.Sprintf("  %s %s: %s\n",
				dimStyle.Render(fmt.Sprintf("[L:%d]", e.LamportTS)),
				msgFromStyle.Render(e.AgentID),
				body))
			recvCount++
		}
	}
	if recvCount == 0 {
		b.WriteString(dimStyle.Render("  (none)"))
		b.WriteRune('\n')
	}

	b.WriteRune('\n')

	// Recent events (all kinds) by this agent.
	b.WriteString(detailSectionStyle.Render("Recent Activity"))
	b.WriteRune('\n')
	var actCount int
	for i := len(m.snap.Events) - 1; i >= 0 && actCount < 20; i-- {
		e := m.snap.Events[i]
		if e.AgentID != agentID {
			continue
		}
		ts := dimStyle.Render(fmt.Sprintf("[L:%-4d]", e.LamportTS))
		var detail string
		switch e.Kind {
		case model.EventMsg:
			detail = fmt.Sprintf("-> %s: %s", e.Target, truncate(e.Body, 60))
		case model.EventLockReq:
			detail = lockStyle.Render("lock " + e.Target)
		case model.EventLockRel:
			detail = dimStyle.Render("unlock " + e.Target)
		case model.EventProgress:
			detail = dimStyle.Render(fmt.Sprintf("heartbeat e=%d r=%d", e.Epoch, e.Round))
		default:
			detail = fmt.Sprintf("%s %s", e.Kind, e.Target)
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", ts, detail))
		actCount++
	}
	if actCount == 0 {
		b.WriteString(dimStyle.Render("  (none)"))
		b.WriteRune('\n')
	}

	return b.String()
}

// --- Split-pane rendering ---

// renderSplitPane renders two content panes side by side with a vertical separator.
func renderSplitPane(left, right string, leftWidth, rightWidth, maxHeight int) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	// Pad to equal height.
	maxLines := max(len(leftLines), len(rightLines))
	if maxLines > maxHeight {
		maxLines = maxHeight
	}
	for len(leftLines) < maxLines {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, "")
	}

	sep := dimStyle.Render("│")
	var b strings.Builder
	for i := 0; i < maxLines; i++ {
		l := padOrTruncate(stripAnsi(leftLines[i]), leftLines[i], leftWidth)
		r := rightLines[i]
		b.WriteString(l)
		b.WriteString(" ")
		b.WriteString(sep)
		b.WriteString(" ")
		b.WriteString(r)
		b.WriteRune('\n')
	}
	return b.String()
}

// padOrTruncate pads or truncates a line to the target visible width.
// raw is the string without ANSI codes (for width calculation),
// styled is the actual string with ANSI codes.
func padOrTruncate(raw, styled string, width int) string {
	visWidth := len(raw)
	if visWidth >= width {
		// Truncate: just use raw truncated (lose styling on overflow).
		if len(raw) > width {
			return raw[:width]
		}
		return styled
	}
	// Pad with spaces.
	return styled + strings.Repeat(" ", width-visWidth)
}

// stripAnsi removes ANSI escape sequences for width calculations.
func stripAnsi(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// --- Helpers ---

// eventMatchesAgent returns true if the event involves the given agent as
// sender (AgentID) or receiver (Target). Empty filter matches everything.
func eventMatchesAgent(e model.Event, agent string) bool {
	if agent == "" {
		return true
	}
	return e.AgentID == agent || e.Target == agent
}

func filterEvents(events []model.Event, kind model.EventKind) []model.Event {
	var out []model.Event
	for _, e := range events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// truncateLines truncates each line in content to at most width visible
// characters, preserving ANSI escape codes. This prevents terminal line
// wrapping when the window is resized narrower.
func truncateLines(content string, width int) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

// wrapText breaks s into lines of at most width characters, splitting on word
// boundaries where possible. If a single word exceeds width it is hard-split.
// Embedded newlines are respected — each paragraph is wrapped independently.
func wrapText(s string, width int) []string {
	if width <= 0 {
		width = 80
	}

	// Split on existing newlines first so embedded \n is respected.
	paragraphs := strings.Split(s, "\n")
	var lines []string
	for _, para := range paragraphs {
		lines = append(lines, wrapParagraph(para, width)...)
	}
	return lines
}

// wrapParagraph wraps a single paragraph (no embedded newlines) to width.
func wrapParagraph(s string, width int) []string {
	if len(s) <= width {
		return []string{s}
	}

	var lines []string
	for len(s) > 0 {
		if len(s) <= width {
			lines = append(lines, s)
			break
		}
		// Try to break at a space at or before position width.
		cut := -1
		for i := width; i > 0; i-- {
			if s[i] == ' ' {
				cut = i
				break
			}
		}
		if cut <= 0 {
			// No space found — hard-split at width.
			cut = width
			lines = append(lines, s[:cut])
			s = s[cut:]
		} else {
			lines = append(lines, s[:cut])
			s = s[cut+1:] // skip the space
		}
	}
	return lines
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func shortDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
