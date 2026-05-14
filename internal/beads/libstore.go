package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	bdlib "github.com/steveyegge/beads"
)

const (
	beadsLibMaxOpenConns  = 4
	beadsLibMaxIdleConns  = 2
	beadsLibRemoteTimeout = 5 * time.Second
)

var beadsLibOpenMu sync.Mutex

// BeadsLibStore implements Store using the public beads Go storage API.
//
//nolint:revive // Name matches the operator-facing backend driver.
type BeadsLibStore struct {
	dir        string
	storage    bdlib.Storage
	idPrefix   string
	actor      string
	createdMu  sync.Mutex
	createdSeq map[string]uint64
	nextSeq    uint64
}

// NewBeadsLibStore opens a beads library store for scopeRoot.
func NewBeadsLibStore(scopeRoot, idPrefix string) (*BeadsLibStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bdCommandTimeout)
	defer cancel()

	beadsDir := filepath.Join(scopeRoot, ".beads")
	storage, err := openBeadsLibStorage(ctx, beadsDir)
	if err != nil {
		return nil, fmt.Errorf("open beads lib store %s: %w", beadsDir, err)
	}
	configureBeadsLibPool(storage)
	return NewBeadsLibStoreWithStorage(scopeRoot, storage, idPrefix), nil
}

func openBeadsLibStorage(ctx context.Context, beadsDir string) (bdlib.Storage, error) {
	beadsLibOpenMu.Lock()
	defer beadsLibOpenMu.Unlock()

	restore, err := installBeadsLibDoltWrapper()
	if err != nil {
		return nil, err
	}
	defer restore()

	return bdlib.OpenFromConfig(ctx, beadsDir)
}

func installBeadsLibDoltWrapper() (func(), error) {
	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		return func() {}, nil
	}
	timeoutPath, err := exec.LookPath("timeout")
	if err != nil {
		return func() {}, nil
	}
	tmpDir, err := os.MkdirTemp("", "gascity-beadslib-dolt-")
	if err != nil {
		return nil, fmt.Errorf("create beadslib dolt wrapper dir: %w", err)
	}
	wrapperPath := filepath.Join(tmpDir, "dolt")
	// beadslib runs `dolt remote -v` during OpenFromConfig without a
	// context-aware exec; bound that best-effort probe while opening.
	content := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "remote" ] && [ "$2" = "-v" ]; then
  exec %q %s %q "$@"
fi
exec %q "$@"
`, timeoutPath, beadsLibRemoteTimeout.String(), doltPath, doltPath)
	if err := os.WriteFile(wrapperPath, []byte(content), 0o755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("write beadslib dolt wrapper: %w", err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", tmpDir+string(os.PathListSeparator)+oldPath); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("install beadslib dolt wrapper: %w", err)
	}
	return func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.RemoveAll(tmpDir)
	}, nil
}

// NewBeadsLibStoreWithStorage wraps an existing beads storage implementation.
func NewBeadsLibStoreWithStorage(scopeRoot string, storage bdlib.Storage, idPrefix string) *BeadsLibStore {
	return &BeadsLibStore{
		dir:        scopeRoot,
		storage:    storage,
		idPrefix:   normalizeIDPrefix(idPrefix),
		actor:      beadsLibActor(),
		createdSeq: make(map[string]uint64),
	}
}

// IDPrefix returns the bead ID prefix owned by this store, without trailing "-".
func (s *BeadsLibStore) IDPrefix() string {
	if s == nil {
		return ""
	}
	return s.idPrefix
}

// Shutdown releases the underlying beads storage handle.
func (s *BeadsLibStore) Shutdown() error {
	if s == nil || s.storage == nil {
		return nil
	}
	return s.storage.Close()
}

// Create persists a new bead through the beads library.
func (s *BeadsLibStore) Create(b Bead) (Bead, error) {
	ctx, cancel := s.writeContext()
	defer cancel()

	issue, metadata, err := s.beadToIssue(b)
	if err != nil {
		return Bead{}, err
	}
	if err := s.storage.CreateIssue(ctx, issue, s.actor); err != nil {
		if isInvalidIssueType(err) {
			if customErr := s.ensureIssueType(ctx, string(issue.IssueType)); customErr != nil {
				return Bead{}, fmt.Errorf("beads lib create: %w", errors.Join(err, customErr))
			}
			retryErr := s.storage.CreateIssue(ctx, issue, s.actor)
			if retryErr == nil {
				goto created
			}
			return Bead{}, fmt.Errorf("beads lib create: %w", retryErr)
		}
		return Bead{}, fmt.Errorf("beads lib create: %w", err)
	}
created:
	s.rememberCreated(issue.ID)
	if b.ParentID != "" {
		if err := s.addDep(ctx, issue.ID, b.ParentID, "parent-child"); err != nil {
			return Bead{}, fmt.Errorf("beads lib create parent dep: %w", err)
		}
	}
	for _, depID := range b.Needs {
		if strings.TrimSpace(depID) == "" {
			continue
		}
		if err := s.addDep(ctx, issue.ID, depID, "blocks"); err != nil {
			return Bead{}, fmt.Errorf("beads lib create needs dep: %w", err)
		}
	}
	created, err := s.Get(issue.ID)
	if err != nil {
		return Bead{}, err
	}
	if created.Assignee == "" {
		created.Assignee = b.Assignee
	}
	if created.From == "" {
		created.From = b.From
	}
	if created.Ref == "" {
		created.Ref = b.Ref
	}
	if len(metadata) > 0 && created.Metadata == nil {
		created.Metadata = metadata
	}
	return created, nil
}

// Get retrieves a bead by ID through the beads library.
func (s *BeadsLibStore) Get(id string) (Bead, error) {
	ctx, cancel := s.readContext()
	defer cancel()

	issue, err := s.storage.GetIssue(ctx, id)
	if err != nil {
		if isBeadsLibNotFound(err) {
			return Bead{}, fmt.Errorf("getting bead %q: %w", id, ErrNotFound)
		}
		return Bead{}, fmt.Errorf("getting bead %q: %w", id, err)
	}
	deps, _ := s.depList(ctx, id, "down")
	return issueToBead(issue, deps), nil
}

// Update modifies fields of an existing bead.
func (s *BeadsLibStore) Update(id string, opts UpdateOpts) error {
	updates := make(map[string]interface{})
	if opts.Title != nil {
		updates["title"] = *opts.Title
	}
	if opts.Status != nil {
		updates["status"] = *opts.Status
	}
	if opts.Type != nil {
		updates["issue_type"] = *opts.Type
	}
	if opts.Priority != nil {
		updates["priority"] = *opts.Priority
	}
	if opts.Description != nil {
		updates["description"] = *opts.Description
	}
	if opts.Assignee != nil {
		updates["assignee"] = *opts.Assignee
	}

	ctx, cancel := s.writeContext()
	defer cancel()

	if len(opts.Metadata) > 0 {
		current, err := s.Get(id)
		if err != nil {
			return err
		}
		metadata := maps.Clone(current.Metadata)
		if metadata == nil {
			metadata = make(map[string]string, len(opts.Metadata))
		}
		for k, v := range opts.Metadata {
			metadata[k] = v
		}
		raw, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("updating bead %q metadata: %w", id, err)
		}
		updates["metadata"] = json.RawMessage(raw)
	}
	if len(updates) > 0 {
		if err := s.storage.UpdateIssue(ctx, id, updates, s.actor); err != nil {
			if isInvalidIssueType(err) && opts.Type != nil {
				if customErr := s.ensureIssueType(ctx, *opts.Type); customErr != nil {
					return fmt.Errorf("updating bead %q: %w", id, errors.Join(err, customErr))
				}
				retryErr := s.storage.UpdateIssue(ctx, id, updates, s.actor)
				if retryErr == nil {
					goto updated
				}
				if isBeadsLibNotFound(retryErr) {
					return fmt.Errorf("updating bead %q: %w", id, ErrNotFound)
				}
				return fmt.Errorf("updating bead %q: %w", id, retryErr)
			}
			if isBeadsLibNotFound(err) {
				return fmt.Errorf("updating bead %q: %w", id, ErrNotFound)
			}
			return fmt.Errorf("updating bead %q: %w", id, err)
		}
	}
updated:
	if opts.ParentID != nil {
		if err := s.replaceParent(ctx, id, *opts.ParentID); err != nil {
			return fmt.Errorf("updating bead %q parent: %w", id, err)
		}
	}
	for _, label := range opts.Labels {
		if err := s.storage.AddLabel(ctx, id, label, s.actor); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			if isBeadsLibNotFound(err) {
				return fmt.Errorf("updating bead %q labels: %w", id, ErrNotFound)
			}
			return fmt.Errorf("updating bead %q labels: %w", id, err)
		}
	}
	for _, label := range opts.RemoveLabels {
		if err := s.storage.RemoveLabel(ctx, id, label, s.actor); err != nil && !isBeadsLibNotFound(err) {
			return fmt.Errorf("updating bead %q labels: %w", id, err)
		}
	}
	return nil
}

// Close sets a bead's status to closed.
func (s *BeadsLibStore) Close(id string) error {
	reason := ""
	if b, err := s.Get(id); err == nil {
		reason = strings.TrimSpace(b.Metadata["close_reason"])
	}
	ctx, cancel := s.writeContext()
	defer cancel()
	if err := s.storage.CloseIssue(ctx, id, reason, s.actor, ""); err != nil {
		if b, getErr := s.Get(id); getErr == nil && b.Status == "closed" {
			return nil
		}
		if isBeadsLibNotFound(err) {
			return fmt.Errorf("closing bead %q: %w", id, ErrNotFound)
		}
		return fmt.Errorf("closing bead %q: %w", id, err)
	}
	return nil
}

// Reopen sets a closed bead's status back to open.
func (s *BeadsLibStore) Reopen(id string) error {
	ctx, cancel := s.writeContext()
	defer cancel()
	if err := s.storage.ReopenIssue(ctx, id, "", s.actor); err != nil {
		if isBeadsLibNotFound(err) {
			return fmt.Errorf("reopening bead %q: %w", id, ErrNotFound)
		}
		return fmt.Errorf("reopening bead %q: %w", id, err)
	}
	return nil
}

// CloseAll closes multiple beads and applies metadata to each first.
func (s *BeadsLibStore) CloseAll(ids []string, metadata map[string]string) (int, error) {
	closed := 0
	var closeErr error
	for _, id := range ids {
		if len(metadata) > 0 {
			if err := s.SetMetadataBatch(id, metadata); err != nil {
				closeErr = errors.Join(closeErr, err)
				continue
			}
		}
		if err := s.Close(id); err != nil {
			closeErr = errors.Join(closeErr, err)
			continue
		}
		closed++
	}
	return closed, closeErr
}

// List returns beads matching the query.
func (s *BeadsLibStore) List(query ListQuery) ([]Bead, error) {
	if !query.HasFilter() && !query.AllowScan {
		return nil, fmt.Errorf("beads lib list: %w", ErrQueryRequiresScan)
	}
	ctx, cancel := s.readContext()
	defer cancel()

	filter := issueFilterFromListQuery(query)
	issues, err := s.storage.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("beads lib list: %w", err)
	}
	result := make([]Bead, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issueToBead(issue, depsFromLibIssue(issue)))
	}
	return s.applyListQuery(result, query), nil
}

// ListOpen returns non-closed beads by default.
func (s *BeadsLibStore) ListOpen(status ...string) ([]Bead, error) {
	query := ListQuery{AllowScan: true}
	if len(status) > 0 {
		query.Status = status[0]
	}
	return s.List(query)
}

// Ready returns open, unblocked beads representing actionable work.
func (s *BeadsLibStore) Ready(query ...ReadyQuery) ([]Bead, error) {
	q := readyQueryFromArgs(query)
	ctx, cancel := s.readContext()
	defer cancel()

	filter := bdlib.WorkFilter{
		Status: bdlib.StatusOpen,
		Limit:  q.Limit,
	}
	if q.Assignee != "" {
		filter.Assignee = &q.Assignee
	}
	issues, err := s.storage.GetReadyWork(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("beads lib ready: %w", err)
	}
	result := make([]Bead, 0, len(issues))
	for _, issue := range issues {
		bead := issueToBead(issue, depsFromLibIssue(issue))
		if IsReadyExcludedType(bead.Type) {
			continue
		}
		if q.Assignee != "" && bead.Assignee != q.Assignee {
			continue
		}
		result = append(result, bead)
	}
	return result, nil
}

// Children returns all beads whose parent-child dependency points at parentID.
func (s *BeadsLibStore) Children(parentID string, opts ...QueryOpt) ([]Bead, error) {
	return s.List(ListQuery{
		ParentID:      parentID,
		IncludeClosed: HasOpt(opts, IncludeClosed),
		Sort:          SortCreatedAsc,
	})
}

// ListByLabel returns beads matching an exact label string.
func (s *BeadsLibStore) ListByLabel(label string, limit int, opts ...QueryOpt) ([]Bead, error) {
	return s.List(ListQuery{
		Label:         label,
		Limit:         limit,
		IncludeClosed: HasOpt(opts, IncludeClosed),
		Sort:          SortCreatedDesc,
	})
}

// ListByAssignee returns beads assigned to assignee with the specified status.
func (s *BeadsLibStore) ListByAssignee(assignee, status string, limit int) ([]Bead, error) {
	return s.List(ListQuery{
		Assignee: assignee,
		Status:   status,
		Limit:    limit,
		Sort:     SortCreatedDesc,
	})
}

// ListByMetadata returns beads matching all metadata filters.
func (s *BeadsLibStore) ListByMetadata(filters map[string]string, limit int, opts ...QueryOpt) ([]Bead, error) {
	return s.List(ListQuery{
		Metadata:      filters,
		Limit:         limit,
		IncludeClosed: HasOpt(opts, IncludeClosed),
		Sort:          SortCreatedDesc,
	})
}

// SetMetadata sets a key-value metadata pair on a bead.
func (s *BeadsLibStore) SetMetadata(id, key, value string) error {
	return s.SetMetadataBatch(id, map[string]string{key: value})
}

// SetMetadataBatch sets multiple metadata values on a bead.
func (s *BeadsLibStore) SetMetadataBatch(id string, kvs map[string]string) error {
	if len(kvs) == 0 {
		return nil
	}
	current, err := s.Get(id)
	if err != nil {
		return err
	}
	metadata := maps.Clone(current.Metadata)
	if metadata == nil {
		metadata = make(map[string]string, len(kvs))
	}
	for k, v := range kvs {
		metadata[k] = v
	}
	ctx, cancel := s.writeContext()
	defer cancel()
	raw, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("setting metadata on %q: %w", id, err)
	}
	if err := s.storage.UpdateIssue(ctx, id, map[string]interface{}{"metadata": json.RawMessage(raw)}, s.actor); err != nil {
		if isBeadsLibNotFound(err) {
			return fmt.Errorf("setting metadata on %q: %w", id, ErrNotFound)
		}
		return fmt.Errorf("setting metadata on %q: %w", id, err)
	}
	return nil
}

// Delete permanently removes a bead from the store.
func (s *BeadsLibStore) Delete(id string) error {
	ctx, cancel := s.writeContext()
	defer cancel()
	if err := s.storage.DeleteIssue(ctx, id); err != nil {
		if isBeadsLibNotFound(err) {
			return fmt.Errorf("deleting bead %q: %w", id, ErrNotFound)
		}
		return fmt.Errorf("deleting bead %q: %w", id, err)
	}
	return nil
}

// Ping verifies that the underlying beads storage is operational.
func (s *BeadsLibStore) Ping() error {
	ctx, cancel := s.readContext()
	defer cancel()
	if _, err := s.storage.GetStatistics(ctx); err != nil {
		return fmt.Errorf("beads lib store ping: %w", err)
	}
	return nil
}

// DepAdd records a dependency.
func (s *BeadsLibStore) DepAdd(issueID, dependsOnID, depType string) error {
	ctx, cancel := s.writeContext()
	defer cancel()
	return s.addDep(ctx, issueID, dependsOnID, depType)
}

// DepRemove removes a dependency.
func (s *BeadsLibStore) DepRemove(issueID, dependsOnID string) error {
	ctx, cancel := s.writeContext()
	defer cancel()
	if err := s.storage.RemoveDependency(ctx, issueID, dependsOnID, s.actor); err != nil && !isBeadsLibNotFound(err) {
		return fmt.Errorf("removing dep %s->%s: %w", issueID, dependsOnID, err)
	}
	return nil
}

// DepList returns dependencies for a bead.
func (s *BeadsLibStore) DepList(id, direction string) ([]Dep, error) {
	ctx, cancel := s.readContext()
	defer cancel()
	return s.depList(ctx, id, direction)
}

func (s *BeadsLibStore) writeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), bdCommandTimeout)
}

func (s *BeadsLibStore) readContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), bdReadCommandTimeout)
}

func (s *BeadsLibStore) rememberCreated(id string) {
	if id == "" {
		return
	}
	s.createdMu.Lock()
	defer s.createdMu.Unlock()
	if _, ok := s.createdSeq[id]; ok {
		return
	}
	s.nextSeq++
	s.createdSeq[id] = s.nextSeq
}

func (s *BeadsLibStore) createdOrder(id string) (uint64, bool) {
	s.createdMu.Lock()
	defer s.createdMu.Unlock()
	seq, ok := s.createdSeq[id]
	return seq, ok
}

func (s *BeadsLibStore) applyListQuery(items []Bead, q ListQuery) []Bead {
	filtered := make([]Bead, 0, len(items))
	for _, b := range items {
		if q.Matches(b) {
			filtered = append(filtered, b)
		}
	}
	s.sortBeadsForQuery(filtered, q.Sort)
	if q.Limit > 0 && len(filtered) > q.Limit {
		filtered = filtered[:q.Limit]
	}
	return filtered
}

func (s *BeadsLibStore) sortBeadsForQuery(items []Bead, order SortOrder) {
	switch order {
	case SortCreatedAsc:
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return s.createdTieLess(items[i], items[j], false)
			}
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		})
	case SortCreatedDesc:
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return s.createdTieLess(items[i], items[j], true)
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})
	}
}

func (s *BeadsLibStore) createdTieLess(a, b Bead, desc bool) bool {
	aseq, aok := s.createdOrder(a.ID)
	bseq, bok := s.createdOrder(b.ID)
	if aok && bok && aseq != bseq {
		if desc {
			return aseq > bseq
		}
		return aseq < bseq
	}
	if aok != bok {
		return aok
	}
	if desc {
		return a.ID > b.ID
	}
	return a.ID < b.ID
}

func (s *BeadsLibStore) beadToIssue(b Bead) (*bdlib.Issue, map[string]string, error) {
	typ := strings.TrimSpace(b.Type)
	if typ == "" {
		typ = "task"
	}
	metadata := maps.Clone(b.Metadata)
	if metadata == nil {
		metadata = make(map[string]string)
	}
	if b.From != "" && metadata["from"] == "" {
		metadata["from"] = b.From
	}
	if b.Ref != "" && metadata["ref"] == "" {
		metadata["ref"] = b.Ref
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, fmt.Errorf("beads lib create: marshaling metadata: %w", err)
	}
	priority := 2
	if b.Priority != nil {
		priority = *b.Priority
	}
	issue := &bdlib.Issue{
		ID:          b.ID,
		Title:       b.Title,
		Description: b.Description,
		Status:      bdlib.StatusOpen,
		Priority:    priority,
		IssueType:   bdlib.IssueType(typ),
		Assignee:    b.Assignee,
		Labels:      append([]string(nil), b.Labels...),
		Metadata:    rawMetadata,
	}
	if b.Status != "" {
		issue.Status = bdlib.Status(b.Status)
	}
	return issue, metadata, nil
}

func issueToBead(issue *bdlib.Issue, deps []Dep) Bead {
	if issue == nil {
		return Bead{}
	}
	metadata := metadataMap(issue.Metadata)
	from := metadata["from"]
	parentID := ""
	for _, dep := range deps {
		if dep.IssueID == issue.ID && dep.Type == "parent-child" {
			parentID = dep.DependsOnID
			break
		}
	}
	priority := issue.Priority
	return Bead{
		ID:           issue.ID,
		Title:        issue.Title,
		Status:       mapBdStatus(string(issue.Status)),
		Type:         string(issue.IssueType),
		Priority:     &priority,
		CreatedAt:    issue.CreatedAt,
		Assignee:     issue.Assignee,
		From:         from,
		ParentID:     parentID,
		Ref:          metadata["ref"],
		Description:  issue.Description,
		Labels:       append([]string(nil), issue.Labels...),
		Metadata:     metadata,
		Dependencies: deps,
	}
}

func metadataMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	result := make(map[string]string, len(decoded))
	for key, value := range decoded {
		var s string
		if err := json.Unmarshal(value, &s); err == nil {
			result[key] = s
			continue
		}
		result[key] = strings.TrimSpace(string(value))
	}
	return result
}

func issueFilterFromListQuery(query ListQuery) bdlib.IssueFilter {
	filter := bdlib.IssueFilter{
		Labels:              labelFilter(query.Label),
		MetadataFields:      query.Metadata,
		CreatedBefore:       timePtrIfSet(query.CreatedBefore),
		Limit:               query.Limit,
		IncludeDependencies: true,
	}
	if query.Status != "" {
		status := bdlib.Status(query.Status)
		filter.Status = &status
	} else if !query.IncludeClosed {
		filter.ExcludeStatus = []bdlib.Status{bdlib.StatusClosed}
	}
	if query.Type != "" {
		typ := bdlib.IssueType(query.Type)
		filter.IssueType = &typ
	}
	if query.Assignee != "" {
		filter.Assignee = &query.Assignee
	}
	if query.ParentID != "" {
		filter.ParentID = &query.ParentID
	}
	return filter
}

func labelFilter(label string) []string {
	if label == "" {
		return nil
	}
	return []string{label}
}

func timePtrIfSet(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func depsFromLibIssue(issue *bdlib.Issue) []Dep {
	if issue == nil || len(issue.Dependencies) == 0 {
		return nil
	}
	result := make([]Dep, 0, len(issue.Dependencies))
	for _, dep := range issue.Dependencies {
		if dep == nil {
			continue
		}
		result = append(result, Dep{
			IssueID:     dep.IssueID,
			DependsOnID: dep.DependsOnID,
			Type:        string(dep.Type),
		})
	}
	return result
}

func (s *BeadsLibStore) addDep(ctx context.Context, issueID, dependsOnID, depType string) error {
	if depType == "" {
		depType = "blocks"
	}
	existing, _ := s.depList(ctx, issueID, "down")
	for _, dep := range existing {
		if dep.DependsOnID != dependsOnID {
			continue
		}
		if dep.Type == depType {
			return nil
		}
		if err := s.storage.RemoveDependency(ctx, issueID, dependsOnID, s.actor); err != nil && !isBeadsLibNotFound(err) {
			return fmt.Errorf("updating dep %s->%s: %w", issueID, dependsOnID, err)
		}
		break
	}
	dep := &bdlib.Dependency{
		IssueID:     issueID,
		DependsOnID: dependsOnID,
		Type:        bdlib.DependencyType(depType),
	}
	if err := s.storage.AddDependency(ctx, dep, s.actor); err != nil {
		return fmt.Errorf("adding dep %s->%s: %w", issueID, dependsOnID, err)
	}
	return nil
}

func (s *BeadsLibStore) replaceParent(ctx context.Context, id, parentID string) error {
	existing, err := s.depList(ctx, id, "down")
	if err != nil {
		return err
	}
	for _, dep := range existing {
		if dep.Type == "parent-child" && dep.DependsOnID != parentID {
			if err := s.storage.RemoveDependency(ctx, id, dep.DependsOnID, s.actor); err != nil && !isBeadsLibNotFound(err) {
				return err
			}
		}
	}
	if parentID != "" {
		return s.addDep(ctx, id, parentID, "parent-child")
	}
	return nil
}

func (s *BeadsLibStore) depList(ctx context.Context, id, direction string) ([]Dep, error) {
	var deps []Dep
	switch direction {
	case "up":
		issues, err := s.storage.GetDependentsWithMetadata(ctx, id)
		if err != nil {
			if isBeadsLibNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("listing deps for %q: %w", id, err)
		}
		deps = make([]Dep, 0, len(issues))
		for _, issue := range issues {
			depType := string(issue.DependencyType)
			if depType == "" {
				depType = "blocks"
			}
			deps = append(deps, Dep{IssueID: issue.ID, DependsOnID: id, Type: depType})
		}
	default:
		issues, err := s.storage.GetDependenciesWithMetadata(ctx, id)
		if err != nil {
			if isBeadsLibNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("listing deps for %q: %w", id, err)
		}
		deps = make([]Dep, 0, len(issues))
		for _, issue := range issues {
			depType := string(issue.DependencyType)
			if depType == "" {
				depType = "blocks"
			}
			deps = append(deps, Dep{IssueID: id, DependsOnID: issue.ID, Type: depType})
		}
	}
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].IssueID == deps[j].IssueID {
			return deps[i].DependsOnID < deps[j].DependsOnID
		}
		return deps[i].IssueID < deps[j].IssueID
	})
	return deps, nil
}

func isBeadsLibNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no issue found")
}

func isInvalidIssueType(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "invalid issue type")
}

func (s *BeadsLibStore) ensureIssueType(ctx context.Context, typ string) error {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return nil
	}
	current, err := s.storage.GetConfig(ctx, "types.custom")
	if err != nil && !isBeadsLibNotFound(err) {
		return fmt.Errorf("read types.custom: %w", err)
	}
	types := splitCustomTypes(current)
	for _, existing := range types {
		if existing == typ {
			return nil
		}
	}
	types = append(types, typ)
	sort.Strings(types)
	if err := s.storage.SetConfig(ctx, "types.custom", strings.Join(types, ",")); err != nil {
		return fmt.Errorf("write types.custom: %w", err)
	}
	return nil
}

func splitCustomTypes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\t' || r == ' '
	})
	result := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	return result
}

func beadsLibActor() string {
	if actor := strings.TrimSpace(os.Getenv("BEADS_ACTOR")); actor != "" {
		return actor
	}
	return "controller"
}

func configureBeadsLibPool(storage bdlib.Storage) {
	seen := map[*sql.DB]struct{}{}
	for _, db := range []interface{ DB() *sql.DB }{
		asDBAccessor(storage),
	} {
		if db == nil || db.DB() == nil {
			continue
		}
		configureSQLDBPool(db.DB(), seen)
	}
	if accessor, ok := storage.(interface{ UnderlyingDB() *sql.DB }); ok {
		configureSQLDBPool(accessor.UnderlyingDB(), seen)
	}
}

func asDBAccessor(v any) interface{ DB() *sql.DB } {
	if accessor, ok := v.(interface{ DB() *sql.DB }); ok {
		return accessor
	}
	return nil
}

func configureSQLDBPool(db *sql.DB, seen map[*sql.DB]struct{}) {
	if db == nil {
		return
	}
	if _, ok := seen[db]; ok {
		return
	}
	seen[db] = struct{}{}
	db.SetMaxOpenConns(beadsLibMaxOpenConns)
	db.SetMaxIdleConns(beadsLibMaxIdleConns)
}
