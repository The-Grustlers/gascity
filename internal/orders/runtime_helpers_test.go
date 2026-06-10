package orders

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

type rowsErrorStore struct {
	*beads.MemStore
	rows []beads.Bead
	err  error
}

func (s *rowsErrorStore) List(_ beads.ListQuery) ([]beads.Bead, error) {
	return s.rows, s.err
}

func TestLastRunFuncForStoreReturnsLatestRun(t *testing.T) {
	store := beads.NewMemStore()

	first, err := store.Create(beads.Bead{
		Title:  "order:digest",
		Status: "closed",
		Labels: []string{"order-run:digest"},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Millisecond)

	second, err := store.Create(beads.Bead{
		Title:  "order:digest",
		Status: "closed",
		Labels: []string{"order-run:digest", "wisp-failed"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := LastRunFuncForStore(store)("digest")
	if err != nil {
		t.Fatalf("LastRunFuncForStore(): %v", err)
	}
	if !got.Equal(second.CreatedAt) {
		t.Fatalf("LastRunFuncForStore() = %s, want %s (latest run should remain authoritative)", got, second.CreatedAt)
	}
	if !second.CreatedAt.After(first.CreatedAt) {
		t.Fatalf("test setup invalid: second.CreatedAt=%s, first.CreatedAt=%s", second.CreatedAt, first.CreatedAt)
	}
}

func TestLastRunFuncForStoreReturnsZeroWhenNoRunsExist(t *testing.T) {
	store := beads.NewMemStore()

	got, err := LastRunFuncForStore(store)("digest")
	if err != nil {
		t.Fatalf("LastRunFuncForStore(): %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("LastRunFuncForStore() = %s, want zero time", got)
	}
}

func TestLastRunFuncForStoreUsesRowsFromPartialTierError(t *testing.T) {
	want := time.Date(2026, 5, 15, 7, 0, 0, 0, time.UTC)
	store := &rowsErrorStore{
		MemStore: beads.NewMemStore(),
		rows: []beads.Bead{{
			ID:        "run-1",
			Title:     "digest",
			CreatedAt: want,
			Labels:    []string{"order-run:digest"},
		}},
		err: errors.New("wisps tier unavailable"),
	}

	got, err := LastRunFuncForStore(store)("digest")
	if err != nil {
		t.Fatalf("LastRunFuncForStore(): %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("LastRunFuncForStore() = %s, want %s from surviving rows", got, want)
	}
}

func TestCursorFuncForStoreUsesRowsAndLogsPartialTierError(t *testing.T) {
	oldLogf := runtimeHelpersLogf
	var logs []string
	runtimeHelpersLogf = func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() {
		runtimeHelpersLogf = oldLogf
	})
	store := &rowsErrorStore{
		MemStore: beads.NewMemStore(),
		rows: []beads.Bead{{
			ID:     "run-1",
			Labels: []string{"order-run:digest", "seq:42"},
		}},
		err: errors.New("wisps tier unavailable"),
	}

	got := CursorFuncForStore(store)("digest")
	if got != 42 {
		t.Fatalf("CursorFuncForStore() = %d, want 42 from surviving rows", got)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "partially failed") {
		t.Fatalf("logs = %#v, want partial failure log", logs)
	}
}

type queryRecordingStore struct {
	beads.Store
	queries []beads.ListQuery
}

func (s *queryRecordingStore) List(q beads.ListQuery) ([]beads.Bead, error) {
	s.queries = append(s.queries, q)
	return s.Store.List(q)
}

func (s *queryRecordingStore) listCounts() (window, perName int) {
	for _, q := range s.queries {
		switch {
		case q.Label == orderTrackingLabel:
			window++
		case strings.HasPrefix(q.Label, "order-run:"):
			perName++
		}
	}
	return window, perName
}

func TestLastRunBatchServesLookupsFromOneWindowList(t *testing.T) {
	mem := beads.NewMemStore()
	created := make(map[string]time.Time, 3)
	for _, name := range []string{"digest", "sweep", "lint"} {
		bead, err := mem.Create(beads.Bead{
			Title:  "order:" + name,
			Status: "closed",
			Labels: []string{orderTrackingLabel, "order-run:" + name},
		})
		if err != nil {
			t.Fatal(err)
		}
		created[name] = bead.CreatedAt
		time.Sleep(time.Millisecond)
	}

	store := &queryRecordingStore{Store: mem}
	fn := NewLastRunBatch(100).AcrossStores(store)
	for name, want := range created {
		got, err := fn(name)
		if err != nil {
			t.Fatalf("batched last run %s: %v", name, err)
		}
		if !got.Equal(want) {
			t.Fatalf("batched last run %s = %s, want %s", name, got, want)
		}
	}

	window, perName := store.listCounts()
	if window != 1 {
		t.Fatalf("window lists = %d, want exactly 1", window)
	}
	if perName != 0 {
		t.Fatalf("per-name lists = %d, want 0 (all lookups served from the window)", perName)
	}
}

func TestLastRunBatchFallsBackOnWindowMiss(t *testing.T) {
	mem := beads.NewMemStore()
	// A run bead without a tracking bead (e.g. an untracked manual run)
	// never enters the window; the lookup must fall back to the exact
	// per-name query and still find it.
	run, err := mem.Create(beads.Bead{
		Title:  "order:digest",
		Status: "closed",
		Labels: []string{"order-run:digest"},
	})
	if err != nil {
		t.Fatal(err)
	}

	store := &queryRecordingStore{Store: mem}
	got, err := NewLastRunBatch(100).AcrossStores(store)("digest")
	if err != nil {
		t.Fatalf("batched last run: %v", err)
	}
	if !got.Equal(run.CreatedAt) {
		t.Fatalf("batched last run = %s, want %s from per-name fallback", got, run.CreatedAt)
	}
	window, perName := store.listCounts()
	if window != 1 || perName != 1 {
		t.Fatalf("lists = window %d / per-name %d, want 1 / 1 (miss falls back)", window, perName)
	}
}

func TestLastRunBatchFallsBackWhenWindowListFails(t *testing.T) {
	var logs []string
	oldLogf := runtimeHelpersLogf
	runtimeHelpersLogf = func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() {
		runtimeHelpersLogf = oldLogf
	})

	mem := beads.NewMemStore()
	run, err := mem.Create(beads.Bead{
		Title:  "order:digest",
		Status: "closed",
		Labels: []string{orderTrackingLabel, "order-run:digest"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &windowFailStore{Store: mem}

	got, err := NewLastRunBatch(100).AcrossStores(store)("digest")
	if err != nil {
		t.Fatalf("batched last run: %v", err)
	}
	if !got.Equal(run.CreatedAt) {
		t.Fatalf("batched last run = %s, want %s from per-name fallback", got, run.CreatedAt)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "last-run window list failed") {
		t.Fatalf("logs = %#v, want window failure log", logs)
	}
}

func TestLastRunBatchMergesNewestAcrossStores(t *testing.T) {
	older := beads.NewMemStore()
	if _, err := older.Create(beads.Bead{
		Title:  "order:digest",
		Status: "closed",
		Labels: []string{orderTrackingLabel, "order-run:digest"},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	newer := beads.NewMemStore()
	want, err := newer.Create(beads.Bead{
		Title:  "order:digest",
		Status: "closed",
		Labels: []string{orderTrackingLabel, "order-run:digest"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := NewLastRunBatch(100).AcrossStores(older, newer)("digest")
	if err != nil {
		t.Fatalf("batched last run: %v", err)
	}
	if !got.Equal(want.CreatedAt) {
		t.Fatalf("batched last run = %s, want newest across stores %s", got, want.CreatedAt)
	}
}

type windowFailStore struct {
	beads.Store
}

func (s *windowFailStore) List(q beads.ListQuery) ([]beads.Bead, error) {
	if q.Label == orderTrackingLabel {
		return nil, errors.New("window list failed")
	}
	return s.Store.List(q)
}
