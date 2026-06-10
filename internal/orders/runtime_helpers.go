package orders

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

var runtimeHelpersLogf = log.Printf

// LastRunFuncForStore returns the latest order-run bead time for one store.
func LastRunFuncForStore(store beads.Store) LastRunFunc {
	return func(name string) (time.Time, error) {
		if store == nil {
			return time.Time{}, nil
		}
		label := "order-run:" + name
		// Order-run beads land in either tier: the ephemeral tracking bead
		// (wisps) created by the dispatcher and the molecule root (issues)
		// labeled after instantiation. Both carry the order-run label.
		results, err := store.List(beads.ListQuery{
			Label:         label,
			Limit:         1,
			IncludeClosed: true,
			Sort:          beads.SortCreatedDesc,
			TierMode:      beads.TierBoth,
		})
		if err != nil {
			if len(results) == 0 {
				return time.Time{}, err
			}
			runtimeHelpersLogf("orders: last-run lookup partially failed for %s: %v", name, err)
		}
		if len(results) == 0 {
			return time.Time{}, nil
		}
		return results[0].CreatedAt, nil
	}
}

// LastRunAcrossStores returns the most recent run time across a set of stores
// for a single order name.
func LastRunAcrossStores(stores ...beads.Store) LastRunFunc {
	return func(name string) (time.Time, error) {
		var latest time.Time
		for _, store := range stores {
			if store == nil {
				continue
			}
			last, err := LastRunFuncForStore(store)(name)
			if err != nil {
				return time.Time{}, err
			}
			if last.After(latest) {
				latest = last
			}
		}
		return latest, nil
	}
}

// CursorFuncForStore returns the max order-run seq for one store.
func CursorFuncForStore(store beads.Store) CursorFunc {
	return func(name string) uint64 {
		if store == nil {
			return 0
		}
		label := "order-run:" + name
		results, err := store.List(beads.ListQuery{
			Label:         label,
			Limit:         10,
			IncludeClosed: true,
			Sort:          beads.SortCreatedDesc,
			TierMode:      beads.TierBoth,
		})
		if err != nil {
			if len(results) == 0 {
				runtimeHelpersLogf("orders: cursor lookup failed for %s: %v", name, err)
				return 0
			}
			runtimeHelpersLogf("orders: cursor lookup partially failed for %s: %v", name, err)
		}
		if len(results) == 0 {
			return 0
		}
		labelSets := make([][]string, 0, len(results))
		for _, b := range results {
			labelSets = append(labelSets, b.Labels)
		}
		return MaxSeqFromLabels(labelSets)
	}
}

// orderTrackingLabel marks the tracking bead the dispatcher creates for
// every order run. Tracking beads also carry "order-run:<scopedName>", so a
// bounded newest-first list of them doubles as a last-run index.
const orderTrackingLabel = "order-tracking"

// LastRunBatch answers repeated last-run lookups from one bounded
// order-tracking history window per store instead of one per-name
// "order-run:<name>" query per order. Callers that loop over many orders
// (doctor's firing-currency check, cold-cache reload paths) previously paid
// N serial backing-store queries — on a bd-backed store, N subprocess
// round-trips; the batch collapses the common case to a single bounded
// list per store.
//
// A window hit reflects the order's newest dispatch (its tracking bead).
// Names missing from a store's window — never dispatched recently, window
// truncated at the limit, or the window list failed — fall back to the
// exact per-name lookup, so an answer is never worse than the unbatched
// path.
//
// A batch memoizes each store's window for its own lifetime; create one per
// pass (one doctor run, one dispatch cycle), not long-lived.
type LastRunBatch struct {
	limit   int
	mu      sync.Mutex
	windows map[beads.Store]*lastRunWindow
}

type lastRunWindow struct {
	lastRun map[string]time.Time
	failed  bool
}

// NewLastRunBatch returns a batch whose per-store history window holds at
// most limit tracking beads (newest first). Pass the same bound the order
// dispatcher uses for its tracking index.
func NewLastRunBatch(limit int) *LastRunBatch {
	return &LastRunBatch{
		limit:   limit,
		windows: make(map[beads.Store]*lastRunWindow),
	}
}

// AcrossStores returns a LastRunFunc with LastRunAcrossStores semantics
// (most recent run time across the stores), resolved through the batch's
// per-store windows.
func (b *LastRunBatch) AcrossStores(stores ...beads.Store) LastRunFunc {
	return func(name string) (time.Time, error) {
		var latest time.Time
		for _, store := range stores {
			if store == nil {
				continue
			}
			last, ok := b.windowLastRun(store, name)
			if !ok {
				exact, err := LastRunFuncForStore(store)(name)
				if err != nil {
					return time.Time{}, err
				}
				last = exact
			}
			if last.After(latest) {
				latest = last
			}
		}
		return latest, nil
	}
}

func (b *LastRunBatch) windowLastRun(store beads.Store, name string) (time.Time, bool) {
	b.mu.Lock()
	window, ok := b.windows[store]
	b.mu.Unlock()
	if !ok {
		window = b.loadWindow(store)
		b.mu.Lock()
		b.windows[store] = window
		b.mu.Unlock()
	}
	if window.failed {
		return time.Time{}, false
	}
	last, ok := window.lastRun[name]
	return last, ok
}

func (b *LastRunBatch) loadWindow(store beads.Store) *lastRunWindow {
	rows, err := store.List(beads.ListQuery{
		Label:         orderTrackingLabel,
		Limit:         b.limit,
		IncludeClosed: true,
		Sort:          beads.SortCreatedDesc,
		TierMode:      beads.TierBoth,
	})
	if err != nil {
		// Partial results could under-report a name's newest run and a
		// window hit would mask it, so any error disables the window for
		// this store and every lookup takes the exact per-name path.
		runtimeHelpersLogf("orders: last-run window list failed: %v", err)
		return &lastRunWindow{failed: true}
	}
	window := &lastRunWindow{lastRun: make(map[string]time.Time, len(rows))}
	for _, row := range rows {
		for _, label := range row.Labels {
			name, ok := strings.CutPrefix(label, "order-run:")
			if !ok || strings.TrimSpace(name) == "" {
				continue
			}
			name = strings.TrimSpace(name)
			if row.CreatedAt.After(window.lastRun[name]) {
				window.lastRun[name] = row.CreatedAt
			}
		}
	}
	return window
}

// CursorAcrossStores merges seq cursors from multiple stores.
func CursorAcrossStores(stores ...beads.Store) CursorFunc {
	fns := make([]CursorFunc, 0, len(stores))
	for _, store := range stores {
		if store != nil {
			fns = append(fns, CursorFuncForStore(store))
		}
	}
	return func(name string) uint64 {
		var latest uint64
		for _, fn := range fns {
			if seq := fn(name); seq > latest {
				latest = seq
			}
		}
		return latest
	}
}
