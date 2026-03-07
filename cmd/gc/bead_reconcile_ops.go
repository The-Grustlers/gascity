package main

import (
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runtime"
)

// beadReconcileOps implements reconcileOps by storing config/live hashes
// in session bead metadata instead of tmux environment variables. This
// makes beads the single source of truth for agent lifecycle state.
//
// Hash keys use the "started_" prefix to distinguish them from the
// observational hashes written by syncSessionBeads:
//   - syncSessionBeads writes config_hash / live_hash (CURRENT config)
//   - beadReconcileOps writes started_config_hash / started_live_hash
//     (what config the agent was STARTED with)
//
// After a config change, these diverge: config_hash reflects the new
// config, while started_config_hash reflects the old config. The
// reconciler detects drift by comparing started_config_hash to the
// current config's fingerprint.
//
// Read methods fall back to the provider when the bead is missing or
// the started_* key is empty. This ensures drift detection survives
// the upgrade from provider-based hashes (Phase 2) to bead-based
// hashes (Phase 3) without requiring a restart. When the store is
// degraded (Get error), reads return empty — the reconciler treats
// empty as "no hash yet" and skips drift, avoiding destructive
// restarts when state cannot be reliably read.
//
// The store is resolved dynamically via storeFunc to handle standalone
// store replacement on config reload.
//
// listRunning and runLive delegate to the underlying provider — these
// are session-level operations that beads cannot replace.
//
// Thread safety: beadReconcileOps is accessed only from the daemon's
// tick goroutine (syncBeadsAndUpdateIndex → updateIndex, then
// doReconcileAgents → configHash/storeConfigHash). The API server
// does not call rops methods. If future code adds concurrent access,
// the index field must be protected with a mutex.
type beadReconcileOps struct {
	provider  reconcileOps       // delegate for listRunning, runLive, hash fallback
	storeFunc func() beads.Store // dynamic store resolution (handles reload)
	index     map[string]string  // session_name → bead_id (open beads)
}

// newBeadReconcileOps creates a beadReconcileOps wrapping the given provider.
// The storeFunc is called on each operation to resolve the current store,
// handling standalone store replacement on config reload.
// The index is initially empty and must be populated via updateIndex before
// hash operations will succeed.
func newBeadReconcileOps(provider reconcileOps, storeFunc func() beads.Store) *beadReconcileOps {
	return &beadReconcileOps{
		provider:  provider,
		storeFunc: storeFunc,
	}
}

// updateIndex replaces the session_name → bead_id index. Called after
// syncSessionBeads to reflect the current set of open session beads.
func (o *beadReconcileOps) updateIndex(idx map[string]string) {
	o.index = idx
}

func (o *beadReconcileOps) listRunning(prefix string) ([]string, error) {
	return o.provider.listRunning(prefix)
}

func (o *beadReconcileOps) storeConfigHash(name, hash string) error {
	id, ok := o.index[name]
	if !ok {
		// No bead for this session — fall back to provider.
		return o.provider.storeConfigHash(name, hash)
	}
	return o.storeFunc().SetMetadata(id, "started_config_hash", hash)
}

func (o *beadReconcileOps) configHash(name string) (string, error) {
	id, ok := o.index[name]
	if !ok {
		// No bead — fall back to provider (preserves pre-upgrade hashes).
		return o.provider.configHash(name)
	}
	b, err := o.storeFunc().Get(id)
	if err != nil {
		// Store degraded — return empty so reconciler skips drift
		// (reconcile.go:329 treats "" as graceful-upgrade). Falling back
		// to provider here would be asymmetric with storeConfigHash
		// (which doesn't fall back on write failure), creating an
		// infinite restart loop when the store is degraded.
		return "", nil
	}
	hash := b.Metadata["started_config_hash"]
	if hash == "" {
		// Bead exists but no started hash yet (upgrade path) — check provider.
		return o.provider.configHash(name)
	}
	return hash, nil
}

func (o *beadReconcileOps) storeLiveHash(name, hash string) error {
	id, ok := o.index[name]
	if !ok {
		return o.provider.storeLiveHash(name, hash)
	}
	return o.storeFunc().SetMetadata(id, "started_live_hash", hash)
}

func (o *beadReconcileOps) liveHash(name string) (string, error) {
	id, ok := o.index[name]
	if !ok {
		return o.provider.liveHash(name)
	}
	b, err := o.storeFunc().Get(id)
	if err != nil {
		// Store degraded — return empty (symmetric with configHash).
		return "", nil
	}
	hash := b.Metadata["started_live_hash"]
	if hash == "" {
		return o.provider.liveHash(name)
	}
	return hash, nil
}

func (o *beadReconcileOps) runLive(name string, cfg runtime.Config) error {
	return o.provider.runLive(name, cfg)
}
