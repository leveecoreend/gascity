package beads

import "context"

// GraphApplyStore is an optional store capability for atomically creating a
// precomputed graph of beads, dependency edges, and post-create assignments.
type GraphApplyStore interface {
	ApplyGraphPlan(ctx context.Context, plan *GraphApplyPlan) (*GraphApplyResult, error)
}

// GraphApplyPlan describes a symbolic bead graph to create atomically.
// Keys are caller-defined stable identifiers (for example recipe step IDs).
type GraphApplyPlan struct {
	CommitMessage string           `json:"commit_message,omitempty"`
	Nodes         []GraphApplyNode `json:"nodes"`
	Edges         []GraphApplyEdge `json:"edges,omitempty"`
}

// GraphApplyNode describes a single bead to create.
type GraphApplyNode struct {
	Key               string            `json:"key"`
	Title             string            `json:"title"`
	Type              string            `json:"type,omitempty"`
	Description       string            `json:"description,omitempty"`
	Assignee          string            `json:"assignee,omitempty"`
	AssignAfterCreate bool              `json:"assign_after_create,omitempty"`
	From              string            `json:"from,omitempty"`
	Labels            []string          `json:"labels,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	MetadataRefs      map[string]string `json:"metadata_refs,omitempty"`
	ParentKey         string            `json:"parent_key,omitempty"`
	ParentID          string            `json:"parent_id,omitempty"`
}

// GraphApplyEdge describes a dependency edge. At least one of FromKey/FromID
// and one of ToKey/ToID must be set.
type GraphApplyEdge struct {
	FromKey  string `json:"from_key,omitempty"`
	FromID   string `json:"from_id,omitempty"`
	ToKey    string `json:"to_key,omitempty"`
	ToID     string `json:"to_id,omitempty"`
	Type     string `json:"type,omitempty"`
	Metadata string `json:"metadata,omitempty"`
}

// GraphApplyResult returns the concrete bead IDs assigned to each symbolic key.
type GraphApplyResult struct {
	IDs map[string]string `json:"ids"`
}
