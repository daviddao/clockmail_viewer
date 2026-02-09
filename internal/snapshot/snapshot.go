// Package snapshot builds immutable data snapshots from the clockmail store.
//
// A DataSnapshot captures the full state of agents, events, locks, and
// frontier at a point in time. Snapshots are rebuilt on each DB change
// and swapped atomically into the UI model.
package snapshot

import (
	"time"

	"github.com/daviddao/clockmail/pkg/frontier"
	"github.com/daviddao/clockmail/pkg/model"
	"github.com/daviddao/clockmail/pkg/store"
)

// DataSnapshot is an immutable, self-contained view of the clockmail state.
type DataSnapshot struct {
	Agents   []model.Agent
	Events   []model.Event
	Locks    []model.Lock
	Frontier []model.Pointstamp

	// Pre-computed per-agent frontier status.
	FrontierStatus map[string]frontier.FrontierStatus

	// Counts.
	ActiveAgents int
	StaleAgents  int
	TotalEvents  int
	ActiveLocks  int

	// Timestamp of snapshot creation.
	BuiltAt time.Time
}

// Build queries the store and returns a complete snapshot.
func Build(s *store.Store) (*DataSnapshot, error) {
	agents, err := s.ListAgents()
	if err != nil {
		return nil, err
	}

	// Fetch the newest 500 events by using MaxEventID as an anchor.
	// ListEvents(0, 500) would return the 500 oldest, missing recent activity.
	const eventLimit = 500
	maxID := s.MaxEventID()
	sinceID := maxID - int64(eventLimit)
	if sinceID < 0 {
		sinceID = 0
	}
	events, err := s.ListEventsSinceID(sinceID, eventLimit)
	if err != nil {
		return nil, err
	}

	locks, err := s.ListLocks()
	if err != nil {
		return nil, err
	}

	active, err := s.GetActivePointstamps()
	if err != nil {
		return nil, err
	}

	f := frontier.ComputeFrontier(active)

	// Compute per-agent frontier status.
	fStatus := make(map[string]frontier.FrontierStatus, len(agents))
	for _, ag := range agents {
		ts := model.Timestamp{Epoch: ag.Epoch, Round: ag.Round}
		fStatus[ag.ID] = frontier.ComputeFrontierStatus(ag.ID, ts, active)
	}

	// Count active vs stale.
	var activeCount, staleCount int
	for _, ag := range agents {
		if time.Since(ag.LastSeen) > 10*time.Minute {
			staleCount++
		} else {
			activeCount++
		}
	}

	return &DataSnapshot{
		Agents:         agents,
		Events:         events,
		Locks:          locks,
		Frontier:       f,
		FrontierStatus: fStatus,
		ActiveAgents:   activeCount,
		StaleAgents:    staleCount,
		TotalEvents:    int(s.CountEvents()), // Use COUNT(*), not max(id), to handle ID gaps
		ActiveLocks:    len(locks),
		BuiltAt:        time.Now(),
	}, nil
}
