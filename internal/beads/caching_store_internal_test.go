package beads

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCachingStoreRunReconciliationDetectsLabelContentChanges(t *testing.T) {
	t.Parallel()

	backing := NewMemStore()
	bead, err := backing.Create(Bead{Title: "Task", Labels: []string{"old"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cache := NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	if err := backing.Update(bead.ID, UpdateOpts{
		Labels:       []string{"new"},
		RemoveLabels: []string{"old"},
	}); err != nil {
		t.Fatalf("Update backing: %v", err)
	}

	cache.runReconciliation()

	got, err := cache.Get(bead.ID)
	if err != nil {
		t.Fatalf("Get after reconcile: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "new" {
		t.Fatalf("Labels = %v, want [new]", got.Labels)
	}
}

func TestCachingStoreUpdateInvalidatesStaleCacheWhenRefreshFails(t *testing.T) {
	t.Parallel()

	backing := &refreshFailingStore{Store: NewMemStore()}
	bead, err := backing.Create(Bead{Title: "before"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cache := NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	title := "after"
	backing.failNextGet = true
	if err := cache.Update(bead.ID, UpdateOpts{Title: &title}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := cache.Get(bead.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Title != "after" {
		t.Fatalf("Title = %q, want after", got.Title)
	}

	stats := cache.Stats()
	if stats.ProblemCount != 1 {
		t.Fatalf("ProblemCount = %d, want 1", stats.ProblemCount)
	}
	if !strings.Contains(stats.LastProblem, "refresh bead after update") {
		t.Fatalf("LastProblem = %q, want refresh context", stats.LastProblem)
	}
	if stats.LastProblemAt.IsZero() {
		t.Fatal("LastProblemAt should be set")
	}
}

func TestCachingStoreDepListUpFallsThroughToBackingTruth(t *testing.T) {
	t.Parallel()

	backing := NewMemStore()
	root, err := backing.Create(Bead{Title: "root"})
	if err != nil {
		t.Fatalf("Create root: %v", err)
	}
	left, err := backing.Create(Bead{Title: "left"})
	if err != nil {
		t.Fatalf("Create left: %v", err)
	}
	right, err := backing.Create(Bead{Title: "right"})
	if err != nil {
		t.Fatalf("Create right: %v", err)
	}
	if err := backing.DepAdd(left.ID, root.ID, "blocks"); err != nil {
		t.Fatalf("DepAdd left: %v", err)
	}
	if err := backing.DepAdd(right.ID, root.ID, "blocks"); err != nil {
		t.Fatalf("DepAdd right: %v", err)
	}

	cache := NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	// Populate only one downward dep entry in the cache, leaving reverse lookups
	// incomplete unless they fall through to the backing store.
	if _, err := cache.DepList(left.ID, "down"); err != nil {
		t.Fatalf("DepList left down: %v", err)
	}

	deps, err := cache.DepList(root.ID, "up")
	if err != nil {
		t.Fatalf("DepList root up: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("DepList(root, up) = %d deps, want 2", len(deps))
	}
}

func TestCachingStoreApplyEventRecordsProblemOnMalformedPayload(t *testing.T) {
	t.Parallel()

	cache := NewCachingStoreForTest(NewMemStore(), nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	cache.ApplyEvent("bead.updated", []byte(`{`))

	stats := cache.Stats()
	if stats.ProblemCount != 1 {
		t.Fatalf("ProblemCount = %d, want 1", stats.ProblemCount)
	}
	if !strings.Contains(stats.LastProblem, "apply bead.updated event") {
		t.Fatalf("LastProblem = %q, want apply-event context", stats.LastProblem)
	}
	if stats.LastProblemAt.IsZero() {
		t.Fatal("LastProblemAt should be set")
	}
}

func TestCachingStoreRunReconciliationRecordsProblemAndDegrades(t *testing.T) {
	t.Parallel()

	backing := &listFailingStore{Store: NewMemStore()}
	if _, err := backing.Create(Bead{Title: "Task"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cache := NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	backing.failList = true
	for i := 0; i < maxCacheSyncFailures; i++ {
		cache.runReconciliation()
	}

	if cache.state != cacheDegraded {
		t.Fatalf("state = %v, want degraded", cache.state)
	}

	stats := cache.Stats()
	if stats.SyncFailures != maxCacheSyncFailures {
		t.Fatalf("SyncFailures = %d, want %d", stats.SyncFailures, maxCacheSyncFailures)
	}
	if stats.ProblemCount != int64(maxCacheSyncFailures) {
		t.Fatalf("ProblemCount = %d, want %d", stats.ProblemCount, maxCacheSyncFailures)
	}
	if !strings.Contains(stats.LastProblem, "reconcile cache") {
		t.Fatalf("LastProblem = %q, want reconcile context", stats.LastProblem)
	}
}

func TestCachingStoreNextReconcileDelayUsesFreshnessWatchdog(t *testing.T) {
	t.Parallel()

	cache := NewCachingStoreForTest(NewMemStore(), nil)
	cache.state = cacheLive
	cache.lastFreshAt = time.Unix(100, 0)

	if got := cache.nextReconcileDelay(time.Unix(110, 0)); got != 20*time.Second {
		t.Fatalf("nextReconcileDelay(fresh) = %s, want 20s", got)
	}

	cache.lastFreshAt = time.Unix(70, 0)
	if got := cache.nextReconcileDelay(time.Unix(110, 0)); got != 0 {
		t.Fatalf("nextReconcileDelay(stale) = %s, want immediate reconcile", got)
	}

	cache.state = cacheDegraded
	cache.lastFreshAt = time.Unix(109, 0)
	if got := cache.nextReconcileDelay(time.Unix(110, 0)); got != 0 {
		t.Fatalf("nextReconcileDelay(degraded) = %s, want immediate reconcile", got)
	}
}

type refreshFailingStore struct {
	Store
	failNextGet bool
}

func (s *refreshFailingStore) Get(id string) (Bead, error) {
	if s.failNextGet {
		s.failNextGet = false
		return Bead{}, errors.New("transient get failure")
	}
	return s.Store.Get(id)
}

type listFailingStore struct {
	Store
	failList bool
}

func (s *listFailingStore) List(query ListQuery) ([]Bead, error) {
	if s.failList {
		return nil, errors.New("transient list failure")
	}
	return s.Store.List(query)
}
