package beads

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// ApplyEvent updates the cache from a bd hook event. Call this when the
// event bus delivers a bead.created, bead.updated, or bead.closed event
// with the full bead JSON payload. This keeps the cache fresh without
// waiting for reconciliation.
func (c *CachingStore) ApplyEvent(eventType string, payload json.RawMessage) {
	if len(payload) == 0 {
		return
	}

	b, err := decodeCacheEvent(payload)
	if err != nil {
		c.recordProblem(fmt.Sprintf("apply %s event", eventType), err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != cacheLive {
		return
	}

	switch eventType {
	case "bead.created":
		if _, exists := c.beads[b.ID]; !exists {
			c.beads[b.ID] = cloneBead(b)
			c.updateStatsLocked()
		}
	case "bead.updated":
		c.beads[b.ID] = cloneBead(b)
	case "bead.closed":
		if _, exists := c.beads[b.ID]; !exists {
			c.updateStatsLocked()
		}
		c.beads[b.ID] = cloneBead(b)
	default:
		return
	}

	c.markFreshLocked(time.Now())
}

// ApplyDepEvent updates the dep cache for a bead. Call after dep
// mutations are detected via events or write-through.
func (c *CachingStore) ApplyDepEvent(beadID string, deps []Dep) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != cacheLive {
		return
	}
	c.deps[beadID] = cloneDeps(deps)
	c.markFreshLocked(time.Now())
	c.updateStatsLocked()
}

func decodeCacheEvent(payload json.RawMessage) (Bead, error) {
	var b Bead
	if err := json.Unmarshal(payload, &b); err != nil {
		return Bead{}, err
	}
	if b.ID == "" {
		return Bead{}, fmt.Errorf("missing bead id")
	}
	// bd hook payloads use "issue_type" while Bead uses "type". If Type
	// wasn't populated from "type", check for the bd field name.
	if b.Type == "" {
		var bdCompat struct {
			IssueType string `json:"issue_type"`
		}
		if err := json.Unmarshal(payload, &bdCompat); err == nil && bdCompat.IssueType != "" {
			b.Type = bdCompat.IssueType
		}
	}
	return b, nil
}

func (c *CachingStore) notifyChange(eventType string, b Bead) {
	if c.onChange == nil {
		return
	}
	payload, err := json.Marshal(b)
	if err != nil {
		c.recordProblem(fmt.Sprintf("marshal %s notification", eventType), err)
		return
	}
	c.onChange(eventType, b.ID, payload)
}

func (c *CachingStore) notifyChangeLocked(eventType string, b Bead) {
	if c.onChange == nil {
		return
	}
	payload, err := json.Marshal(b)
	if err != nil {
		c.recordProblemLocked(fmt.Sprintf("marshal %s notification", eventType), err)
		return
	}
	// Unlock before callback to avoid holding the lock during event recording.
	c.mu.Unlock()
	c.onChange(eventType, b.ID, payload)
	c.mu.Lock()
}

func beadChanged(old, fresh Bead) bool {
	if old.Status != fresh.Status {
		return true
	}
	if old.Title != fresh.Title {
		return true
	}
	if old.Assignee != fresh.Assignee {
		return true
	}
	if old.Description != fresh.Description {
		return true
	}
	if len(old.Metadata) != len(fresh.Metadata) {
		return true
	}
	for k, v := range old.Metadata {
		if fresh.Metadata[k] != v {
			return true
		}
	}
	return !slices.Equal(old.Labels, fresh.Labels)
}
