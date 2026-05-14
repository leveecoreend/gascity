package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	gcruntime "github.com/gastownhall/gascity/internal/runtime"
)

const (
	hookStartGateEnv        = gcruntime.StartGateEnv
	hookStartGateAttempts   = 3
	hookStartGateRetryDelay = time.Second
	hookClaimExitClaimed    = 0
	hookClaimExitNoWork     = 1
	hookClaimExitFailure    = 2
)

var (
	hookClaimRetrySleep             = time.Sleep
	errHookOwnedInProgressUncertain = errors.New("owned in-progress candidate revalidation uncertain")
)

type hookClaimOptions struct {
	Assignee         string
	Identities       []string
	StartGate        bool
	StartGateEnvPath string
}

type hookWorkItem struct {
	ID       string            `json:"id"`
	Status   string            `json:"status,omitempty"`
	Assignee string            `json:"assignee,omitempty"`
	Owner    string            `json:"owner,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type hookClaimExecutor interface {
	Claim(ctx context.Context, dir, beadID, assignee string) (hookWorkItem, bool, bool, error)
	Show(ctx context.Context, dir, beadID string) (hookWorkItem, error)
}

type hookClaimCandidateResult struct {
	item hookWorkItem
	ok   bool
}

type shellHookClaimExecutor struct {
	env []string
}

func doHookClaim(
	workQuery, dir string,
	runner WorkQueryRunner,
	claimer hookClaimExecutor,
	opts hookClaimOptions,
	stdout, stderr io.Writer,
) int {
	startGateMode := opts.StartGate || opts.StartGateEnvPath != ""
	if opts.StartGate && strings.TrimSpace(opts.StartGateEnvPath) == "" {
		fmt.Fprintf(stderr, "gc hook: %s is required with --start-gate\n", hookStartGateEnv) //nolint:errcheck // best-effort stderr
		return hookClaimExitFailure
	}
	attempts := 1
	if startGateMode {
		attempts = hookStartGateAttempts
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		output, err := runner(workQuery, dir)
		if err != nil {
			fmt.Fprintf(stderr, "gc hook: %v\n", err) //nolint:errcheck // best-effort stderr
			return hookClaimExitFailure
		}

		normalized := normalizeWorkQueryOutput(strings.TrimSpace(output))
		if !workQueryHasReadyWork(normalized) {
			if startGateMode {
				return hookClaimExitNoWork
			}
			return hookClaimExitNoWork
		}
		if startGateMode {
			if err := preflightHookStartGateEnv(opts.StartGateEnvPath); err != nil {
				fmt.Fprintf(stderr, "gc hook: %s: %v\n", hookStartGateEnv, err) //nolint:errcheck // best-effort stderr
				return hookClaimExitFailure
			}
		}

		assignee := strings.TrimSpace(opts.Assignee)
		if assignee == "" {
			fmt.Fprintln(stderr, "gc hook: --claim requires a session assignee") //nolint:errcheck // best-effort stderr
			return hookClaimExitFailure
		}
		candidates, err := parseHookWorkItems(normalized)
		if err != nil {
			fmt.Fprintf(stderr, "gc hook: --claim requires JSON work_query output: %v\n", err) //nolint:errcheck // best-effort stderr
			return hookClaimExitFailure
		}

		claimResult, err := claimHookCandidate(
			context.Background(),
			dir,
			candidates,
			assignee,
			opts.Identities,
			claimer,
		)
		if err != nil {
			if startGateMode && errors.Is(err, errHookOwnedInProgressUncertain) {
				return hookClaimExitNoWork
			}
			fmt.Fprintf(stderr, "gc hook: %v\n", err) //nolint:errcheck // best-effort stderr
			return hookClaimExitFailure
		}
		if !claimResult.ok {
			if !startGateMode {
				return hookClaimExitNoWork
			}
			if attempt < attempts {
				hookClaimRetrySleep(hookStartGateRetryDelay)
				continue
			}
			return hookClaimExitNoWork
		}

		if startGateMode {
			if err := writeHookStartGateEnv(opts.StartGateEnvPath, map[string]string{"GC_BEAD_ID": claimResult.item.ID}); err != nil {
				fmt.Fprintf(stderr, "gc hook: writing start_gate env: %v\n", err) //nolint:errcheck // best-effort stderr
				return hookClaimExitFailure
			}
			return hookClaimExitClaimed
		}

		data, err := json.Marshal([]hookWorkItem{claimResult.item})
		if err != nil {
			fmt.Fprintf(stderr, "gc hook: encoding claimed work: %v\n", err) //nolint:errcheck // best-effort stderr
			return hookClaimExitFailure
		}
		fmt.Fprintln(stdout, string(data)) //nolint:errcheck // best-effort stdout
		return hookClaimExitClaimed
	}
	return hookClaimExitNoWork
}

func claimHookCandidate(
	ctx context.Context,
	dir string,
	candidates []hookWorkItem,
	assignee string,
	identities []string,
	claimer hookClaimExecutor,
) (hookClaimCandidateResult, error) {
	identity := hookIdentitySet(append([]string{assignee}, identities...))
	normalized := make([]hookWorkItem, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = normalizeHookWorkItem(candidate)
		if candidate.ID != "" {
			normalized = append(normalized, candidate)
		}
	}

	var candidateErrs []error
	var ownedInProgressErrs []error
	// Three-pass priority:
	// 1. Revalidate already-owned in-progress work and return it before claiming.
	// 2. Reclaim ready/open work already assigned to this session identity.
	// 3. Claim unassigned ready/open work from the routed pool.
	for i, candidate := range normalized {
		if candidate.Assignee != "" && identity[candidate.Assignee] && candidate.Status == "in_progress" {
			full, err := claimer.Show(ctx, dir, candidate.ID)
			if err != nil {
				if errors.Is(err, beads.ErrNotFound) {
					continue
				}
				ownedInProgressErrs = append(ownedInProgressErrs, fmt.Errorf("revalidating owned in-progress bead %s: %w", candidate.ID, err))
				continue
			}
			full = normalizeHookWorkItem(full)
			if full.ID == "" {
				continue
			}
			if full.Assignee != "" && identity[full.Assignee] && full.Status == "in_progress" {
				return hookClaimCandidateResult{item: full, ok: true}, nil
			}
			if full.Assignee != "" {
				if identity[full.Assignee] {
					normalized[i] = full
				}
				continue
			}
			normalized[i] = full
		}
		if candidate.Assignee == "" && candidate.Status == "" {
			full, err := claimer.Show(ctx, dir, candidate.ID)
			if err != nil {
				if errors.Is(err, beads.ErrNotFound) {
					continue
				}
				candidateErrs = append(candidateErrs, fmt.Errorf("revalidating bead %s: %w", candidate.ID, err))
				continue
			}
			full = normalizeHookWorkItem(full)
			if full.ID == "" {
				continue
			}
			if full.Assignee != "" && identity[full.Assignee] && full.Status == "in_progress" {
				return hookClaimCandidateResult{item: full, ok: true}, nil
			}
			normalized[i] = full
		}
	}
	if len(ownedInProgressErrs) > 0 {
		return hookClaimCandidateResult{}, fmt.Errorf("%w: revalidating owned in-progress candidates: %w", errHookOwnedInProgressUncertain, errors.Join(ownedInProgressErrs...))
	}

	for _, candidate := range normalized {
		if candidate.Assignee == "" || !identity[candidate.Assignee] || candidate.Status == "in_progress" || !hookWorkItemClaimable(candidate) {
			continue
		}
		claimed, claimAccepted, ok, err := claimer.Claim(ctx, dir, candidate.ID, assignee)
		if err != nil {
			if errors.Is(err, beads.ErrNotFound) && !claimAccepted {
				continue
			}
			if claimAccepted {
				return hookClaimCandidateResult{}, fmt.Errorf("accepted claim for %s has uncertain readback: %w", candidate.ID, err)
			}
			candidateErrs = append(candidateErrs, err)
			continue
		}
		if ok {
			return hookClaimCandidateResult{
				item: normalizeHookWorkItem(claimed),
				ok:   true,
			}, nil
		}
		if claimAccepted {
			return hookClaimCandidateResult{}, fmt.Errorf("accepted claim for %s has uncertain readback", candidate.ID)
		}
	}

	for _, candidate := range normalized {
		if candidate.Assignee != "" || !hookWorkItemClaimable(candidate) {
			continue
		}
		claimed, claimAccepted, ok, err := claimer.Claim(ctx, dir, candidate.ID, assignee)
		if err != nil {
			if errors.Is(err, beads.ErrNotFound) && !claimAccepted {
				continue
			}
			if claimAccepted {
				return hookClaimCandidateResult{}, fmt.Errorf("accepted claim for %s has uncertain readback: %w", candidate.ID, err)
			}
			candidateErrs = append(candidateErrs, err)
			continue
		}
		if ok {
			return hookClaimCandidateResult{
				item: normalizeHookWorkItem(claimed),
				ok:   true,
			}, nil
		}
		if claimAccepted {
			return hookClaimCandidateResult{}, fmt.Errorf("accepted claim for %s has uncertain readback", candidate.ID)
		}
	}
	if len(candidateErrs) > 0 {
		return hookClaimCandidateResult{}, fmt.Errorf("claiming candidates: %w", errors.Join(candidateErrs...))
	}
	return hookClaimCandidateResult{}, nil
}

func hookWorkItemClaimable(item hookWorkItem) bool {
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "", "open", "ready":
		return true
	default:
		return false
	}
}

func hookIdentitySet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func parseHookWorkItems(output string) ([]hookWorkItem, error) {
	var items []hookWorkItem
	if err := json.Unmarshal([]byte(output), &items); err == nil {
		for i := range items {
			items[i] = normalizeHookWorkItem(items[i])
		}
		return items, nil
	}
	var item hookWorkItem
	if err := json.Unmarshal([]byte(output), &item); err != nil {
		if legacy := parseLegacyHookWorkItems(output); len(legacy) > 0 {
			return legacy, nil
		}
		return nil, err
	}
	item = normalizeHookWorkItem(item)
	if strings.TrimSpace(item.ID) == "" {
		return nil, fmt.Errorf("work item missing id")
	}
	return []hookWorkItem{item}, nil
}

func parseLegacyHookWorkItems(output string) []hookWorkItem {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := strings.Trim(fields[0], "[](){}:,;")
		if isPlausibleHookWorkID(id) {
			return []hookWorkItem{{ID: id}}
		}
	}
	return nil
}

func isPlausibleHookWorkID(id string) bool {
	if len(id) < 4 || (!strings.HasPrefix(id, "bd-") && !strings.HasPrefix(id, "gc-")) {
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return hasLetter && hasDigit
}

func normalizeHookWorkItem(item hookWorkItem) hookWorkItem {
	item.ID = strings.TrimSpace(item.ID)
	item.Status = strings.TrimSpace(item.Status)
	item.Assignee = strings.TrimSpace(item.Assignee)
	item.Owner = ""
	return item
}

func writeHookStartGateEnv(path string, env map[string]string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s is empty", hookStartGateEnv)
	}
	var b strings.Builder
	for key, value := range env {
		if err := gcruntime.ValidateStartGateEnv(key, value); err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s=%s\n", key, value)
	}
	if b.Len() == 0 {
		return fmt.Errorf("start_gate env is empty")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func preflightHookStartGateEnv(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s is empty", hookStartGateEnv)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".preflight-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpName)
		return closeErr
	}
	return os.Remove(tmpName)
}

func hookWorkItemFromBead(bead beads.Bead) hookWorkItem {
	return normalizeHookWorkItem(hookWorkItem{
		ID:       bead.ID,
		Status:   bead.Status,
		Assignee: bead.Assignee,
		Metadata: bead.Metadata,
	})
}

func (s shellHookClaimExecutor) store(dir string, env map[string]string) *beads.BdStore {
	base := s.env
	if base == nil {
		base = mergeRuntimeEnv(os.Environ(), nil)
	}
	environ := workQueryEnvForDir(overlayHookClaimStoreEnv(base, env), dir)
	return beads.NewBdStore(dir, beads.ExecCommandRunnerWithEnviron(environ))
}

func overlayHookClaimStoreEnv(base []string, overrides map[string]string) []string {
	out := append([]string(nil), base...)
	if len(overrides) == 0 {
		return out
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = removeEnvKey(out, key)
	}
	for _, key := range keys {
		out = append(out, key+"="+overrides[key])
	}
	return out
}

func (s shellHookClaimExecutor) Claim(_ context.Context, dir, beadID, assignee string) (hookWorkItem, bool, bool, error) {
	claimed, err := s.store(dir, map[string]string{"BEADS_ACTOR": assignee}).Claim(beadID)
	if err != nil {
		claimAccepted := beads.IsClaimReadError(err)
		current, showErr := s.Show(context.Background(), dir, beadID)
		if showErr == nil {
			if accepted, ok := acceptedClaimWorkItem(current, assignee, claimAccepted); ok {
				return accepted, claimAccepted, true, nil
			}
			current = normalizeHookWorkItem(current)
			if current.Assignee != "" {
				return hookWorkItem{}, false, false, nil
			}
			if current.ID == "" || !hookWorkItemClaimable(current) {
				return hookWorkItem{}, false, false, nil
			}
		} else if errors.Is(showErr, beads.ErrNotFound) {
			if claimAccepted {
				return hookWorkItem{}, true, false, fmt.Errorf("claiming bead %s: accepted claim but claimed bead disappeared during revalidation: %s", beadID, showErr.Error())
			}
			return hookWorkItem{}, false, false, nil
		}
		return hookWorkItem{}, claimAccepted, false, fmt.Errorf("claiming bead %s: %w", beadID, err)
	}
	if accepted, ok := acceptedClaimWorkItem(hookWorkItemFromBead(claimed), assignee, true); ok {
		return accepted, true, true, nil
	}
	current, showErr := s.Show(context.Background(), dir, beadID)
	if showErr != nil {
		if errors.Is(showErr, beads.ErrNotFound) {
			return hookWorkItem{}, false, false, fmt.Errorf("claiming bead %s: accepted claim but claimed bead disappeared during revalidation: %s", beadID, showErr.Error())
		}
		return hookWorkItem{}, true, false, fmt.Errorf("claiming bead %s: revalidating claimed bead: %w", beadID, showErr)
	}
	if accepted, ok := acceptedClaimWorkItem(current, assignee, true); ok {
		return accepted, true, true, nil
	}
	current = normalizeHookWorkItem(current)
	if current.Assignee != "" {
		return hookWorkItem{}, true, false, nil
	}
	if current.ID == "" || !hookWorkItemClaimable(current) {
		return hookWorkItem{}, true, false, nil
	}
	return hookWorkItem{}, true, false, nil
}

func (s shellHookClaimExecutor) Show(_ context.Context, dir, beadID string) (hookWorkItem, error) {
	bead, err := s.store(dir, nil).Get(beadID)
	if err != nil {
		return hookWorkItem{}, err
	}
	return hookWorkItemFromBead(bead), nil
}

func acceptedClaimWorkItem(item hookWorkItem, assignee string, claimAccepted bool) (hookWorkItem, bool) {
	item = normalizeHookWorkItem(item)
	if item.ID == "" || item.Assignee != assignee {
		return hookWorkItem{}, false
	}
	if item.Status == "in_progress" {
		return item, true
	}
	if claimAccepted && hookWorkItemClaimable(item) {
		item.Status = "in_progress"
		return item, true
	}
	return hookWorkItem{}, false
}
