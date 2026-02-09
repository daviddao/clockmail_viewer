# Clockmail Viewer (cmv)

<img width="595" height="464" alt="Lamport Figure" src="https://github.com/user-attachments/assets/3429ee1c-c8e1-4fdd-bd33-7c5c85a5e6af" />

> **Real-time TUI for monitoring multi-agent [clockmail](https://github.com/daviddao/clockmail) coordination.**

A Bubble Tea terminal interface that watches the clockmail SQLite database and displays agents, Lamport clocks, locks, messages, and Naiad frontier status — all updating live as agents work.

## Install

One-liner (requires `go` and `git`):

```bash
curl -sSL https://raw.githubusercontent.com/daviddao/clockmail_viewer/main/install.sh | sh
```

Custom install directory:

```bash
curl -sSL https://raw.githubusercontent.com/daviddao/clockmail_viewer/main/install.sh | INSTALL_DIR=/usr/local/bin sh
```

Or build from source:

```bash
git clone https://github.com/daviddao/clockmail_viewer.git && cd clockmail_viewer
make build
```

## Quick Start

```bash
cmv                              # Auto-discover .clockmail/clockmail.db
cmv --db /path/to/clockmail.db   # Specific database
cmv --view messages              # Start in Messages view
cmv --agent alice                # Focus on agent "alice"
cmv --json                       # Dump state as JSON and exit (no TUI)
```

The viewer is **read-only** — it never modifies the clockmail database. It watches for changes via fsnotify and rebuilds an immutable snapshot on each update.

## Views

Navigate between views with `Tab` or single-key shortcuts:

| Key | View | Description |
|-----|------|-------------|
| `d` | Dashboard | Agent table with clocks, frontier status (SAFE/BLOCKED), lock summary |
| `m` | Messages | Filterable message timeline (newest first) |
| `l` | Locks | Lock ownership table with TTL countdown |
| `f` | Frontier | Global Naiad antichain + per-agent SAFE/BLOCKED status |
| `t` | Timeline | All events (messages, locks, heartbeats) in causal order |
| `Enter` | Agent Detail | Drill-down: stats, locks held, sent/received messages, activity log |

On wide terminals (>= 120 columns), the Dashboard view uses a split-pane layout with the agent detail panel alongside.

## Keybindings

| Key | Action |
|-----|--------|
| `Tab` | Cycle to next view |
| `d` `m` `l` `f` `t` | Jump to specific view |
| `j` / `Down` | Move cursor down / scroll |
| `k` / `Up` | Move cursor up / scroll |
| `Enter` | Open agent detail (from Dashboard) |
| `Esc` | Back to previous view |
| `r` | Force refresh snapshot |
| `?` | Toggle help |
| `q` / `Ctrl+C` | Quit |

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--db <path>` | Auto-discover | Path to clockmail.db |
| `--refresh <duration>` | `2s` | Polling fallback interval |
| `--json` | — | Dump current state as JSON and exit (no TUI) |
| `--agent <id>` | — | Highlight/focus a specific agent on startup |
| `--view <name>` | `dashboard` | Start in specific view: dashboard, messages, locks, frontier, timeline |
| `--version` | — | Print version and exit |

## Architecture

```
cmd/cmv/main.go          Full Bubble Tea TUI (single-file application)
internal/datasource/
  source.go               Database discovery and connection
  watch.go                fsnotify watcher with 100ms debounce
internal/snapshot/
  snapshot.go             Immutable DataSnapshot builder
```

### Data Flow

```
.clockmail/clockmail.db
        │
        ▼ (fsnotify: write/create on db, wal, shm)
  datasource.Watcher
        │
        ▼ (Changes() channel, debounced 100ms)
  Bubble Tea program
        │
        ▼ (dbChangedMsg -> refreshSnapshot cmd)
  snapshot.Build(store)
        │
        ▼ (snapshotReadyMsg: atomic swap)
  uiModel.View() re-renders
```

Snapshots are immutable — the UI never mutates them. On each database change, a new `DataSnapshot` is built from the store and swapped in atomically. The watcher debounces rapid SQLite WAL writes to avoid thrashing.

### Dependencies

The viewer imports three packages from `clockmail` via a Go workspace (`go.work`):

- `pkg/model` — Agent, Event, Lock, Pointstamp types
- `pkg/store` — SQLite store (ListAgents, ListEvents, ListLocks, GetActivePointstamps)
- `pkg/frontier` — ComputeFrontier, ComputeFrontierStatus

### Theme

Styled with a Catppuccin Mocha palette via lipgloss:

- Active agents in green, stale agents (>10 min) in red
- SAFE frontier status in green, BLOCKED in red
- Message senders in blue, recipients in green
- Lock entries in orange

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `CLOCKMAIL_DB` | `.clockmail/clockmail.db` | Override database path (also set by `--db` flag) |

## Related Tools

- **[clockmail](../clockmail/)** (`cm`) — The coordination CLI that agents use to send messages, acquire locks, and report progress
- **[beads](../beads/)** (`bd`) — Distributed git-backed issue tracker
- **[beads_viewer](../beads_viewer/)** (`bv`) — TUI viewer for beads issues with graph analysis

## License

MIT
