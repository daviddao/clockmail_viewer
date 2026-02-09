# Agent Instructions — clockmail_viewer

## Project Overview

`clockmail_viewer` (binary: `cmv`) is a real-time Bubble Tea TUI that monitors the clockmail multi-agent coordination system. It is read-only — it watches the clockmail SQLite database and displays agents, locks, messages, and Naiad frontier status.

## Go Toolchain

- **Go 1.25+** (check `go.mod` for exact version)
- **Workspace**: `go.work` links this module with `../clockmail` for local development
- Build: `make build` or `go build -o cmv ./cmd/cmv`
- Test: `go test ./...`
- Vet: `go vet ./...`
- Format: `gofmt -w .`

## Architecture

The application is structured as:

```
cmd/cmv/main.go              # Full TUI application (single file)
internal/datasource/
  source.go                  # DB discovery (env var, CWD, parent walk)
  watch.go                   # fsnotify watcher with 100ms debounce
internal/snapshot/
  snapshot.go                # Immutable DataSnapshot builder
```

### Key Patterns

1. **Immutable snapshots**: `snapshot.Build()` creates a new `DataSnapshot` on each DB change. The UI swaps it in atomically — never mutate a snapshot.
2. **Elm architecture**: Bubble Tea's `Init/Update/View` pattern. All state lives in `uiModel`.
3. **fsnotify + debounce**: The watcher monitors the DB directory for writes to `clockmail.db`, `clockmail.db-wal`, and `clockmail.db-shm`. Rapid writes are debounced at 100ms.
4. **Single-file TUI**: The entire UI is in `cmd/cmv/main.go` — views, styles, key bindings, and model.

### Upstream Dependencies (from clockmail)

Imported via `go.work` workspace:

- `pkg/model` — `Agent`, `Event`, `Lock`, `Pointstamp`, `EventKind` types
- `pkg/store` — `Store` with `ListAgents()`, `ListEvents()`, `ListLocks()`, `GetActivePointstamps()`
- `pkg/frontier` — `ComputeFrontier()`, `ComputeFrontierStatus()`, `FrontierStatus`

## Views

| ID | View | Shortcut | Description |
|----|------|----------|-------------|
| 0 | Dashboard | `d` | Agent table + locks + frontier summary |
| 1 | Messages | `m` | Filtered message timeline |
| 2 | Locks | `l` | Lock ownership with TTL |
| 3 | Frontier | `f` | Naiad antichain + per-agent status |
| 4 | Timeline | `t` | All events in causal order |
| 6 | Agent Detail | `Enter` | Drill-down (not in tab cycle) |

## Working on This Project

### Adding a New View

1. Add a new `viewID` constant (before `viewCount` if it should be in the tab cycle)
2. Add a `render<ViewName>()` method on `uiModel`
3. Add the view to the `View()` switch statement
4. Add a single-key shortcut to `viewKeys` map
5. Add context help text in `contextHelp()`

### Modifying Snapshot Data

1. Add fields to `DataSnapshot` in `internal/snapshot/snapshot.go`
2. Populate them in `snapshot.Build()`
3. Use the new fields in render methods (they're available via `m.snap`)

### Styling

All styles use the Catppuccin Mocha palette via lipgloss. Style variables are defined at package level in `main.go`. Key colors:
- Green (`#A6E3A1`): active agents, SAFE status, message recipients
- Red (`#F38BA8`): stale agents, BLOCKED status
- Blue (`#89B4FA`): headers, message senders
- Orange (`#FAB387`): locks
- Purple (`#7C3AED`): title, active tab, agent detail header

### Testing

- Smoke tests in `cmd/cmv/smoke_test.go` require a live `.clockmail/` database
- Tests use `os.Chdir` to reach the adventure4/ root where the DB lives
- Tests skip (not fail) when no database is available
- Run from the `clockmail_viewer` directory: `go test ./...`

## Issue Tracking

This project uses **bd** (beads) for issue tracking. See the root [AGENTS.md](../AGENTS.md) for commands.

## Multi-Agent Coordination

This project uses **cm** (clockmail) for multi-agent coordination. See the root [AGENTS.md](../AGENTS.md) for commands.

Key rules:
- Lock files before editing: `cm lock <path>`
- Unlock when done: `cm unlock <path>`
- Check for messages: `cm recv`
- Sync before ending session: `cm sync --epoch N`
