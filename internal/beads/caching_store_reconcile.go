package beads

import (
	"context"
	"time"
)

func (c *CachingStore) reconcileLoop(ctx context.Context) {
	timer := time.NewTimer(cacheReconcilePollInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if c.nextReconcileDelay(time.Now()) == 0 && c.reconciling.CompareAndSwap(false, true) {
			c.runReconciliation()
			c.reconciling.Store(false)
		}

		next := c.nextReconcileDelay(time.Now())
		if next <= 0 || next > cacheReconcilePollInterval {
			next = cacheReconcilePollInterval
		}
		timer.Reset(next)
	}
}

func (c *CachingStore) adaptiveInterval() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.adaptiveIntervalLocked()
}

func (c *CachingStore) adaptiveIntervalLocked() time.Duration {
	total := len(c.beads)
	switch {
	case total >= 5000:
		return cacheReconcileIntervalLarge
	case total >= 1000:
		return cacheReconcileIntervalMedium
	default:
		return cacheReconcileIntervalSmall
	}
}

func (c *CachingStore) nextReconcileDelay(now time.Time) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.state == cacheDegraded || c.lastFreshAt.IsZero() {
		return 0
	}

	dueAt := c.lastFreshAt.Add(c.adaptiveIntervalLocked())
	if !now.Before(dueAt) {
		return 0
	}
	return dueAt.Sub(now)
}

func (c *CachingStore) runReconciliation() {
	start := time.Now()
	fresh, err := c.backing.List(ListQuery{AllowScan: true})
	if err != nil {
		c.mu.Lock()
		c.syncFailures++
		if c.syncFailures >= maxCacheSyncFailures && c.state == cacheLive {
			c.state = cacheDegraded
		}
		c.recordProblemLocked("reconcile cache", err)
		c.updateStatsLocked()
		c.mu.Unlock()
		return
	}

	freshByID := make(map[string]Bead, len(fresh))
	for _, b := range fresh {
		freshByID[b.ID] = b
	}

	c.mu.Lock()

	var adds, removes, updates int64
	var changedIDs []string

	// Detect new and updated.
	for id, fb := range freshByID {
		old, exists := c.beads[id]
		if !exists {
			c.beads[id] = cloneBead(fb)
			adds++
			changedIDs = append(changedIDs, id)
			c.notifyChangeLocked("bead.created", fb)
		} else if beadChanged(old, fb) {
			c.beads[id] = cloneBead(fb)
			updates++
			changedIDs = append(changedIDs, id)
			c.notifyChangeLocked("bead.updated", fb)
		}
	}

	// Detect removed.
	for id, old := range c.beads {
		if _, exists := freshByID[id]; !exists {
			delete(c.beads, id)
			delete(c.deps, id)
			removes++
			c.notifyChangeLocked("bead.closed", old)
		}
	}

	c.syncFailures = 0
	if c.state == cacheDegraded {
		c.state = cacheLive
	}

	now := time.Now()
	durMs := float64(time.Since(start).Microseconds()) / 1000.0
	c.stats.LastReconcileAt = now
	c.stats.LastReconcileMs = durMs
	c.stats.Adds += adds
	c.stats.Removes += removes
	c.stats.Updates += updates
	c.markFreshLocked(now)
	c.updateStatsLocked()
	c.mu.Unlock()

	if len(changedIDs) == 0 {
		return
	}

	depMap, depErr := c.fetchDepsForIDs(changedIDs)
	if depErr != nil {
		c.recordProblem("refresh dep cache after reconcile", depErr)
		return
	}

	c.mu.Lock()
	for id, deps := range depMap {
		c.deps[id] = cloneDeps(deps)
	}
	c.updateStatsLocked()
	c.mu.Unlock()
}
