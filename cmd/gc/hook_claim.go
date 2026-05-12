package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gastownhall/gascity/internal/beads"
)

const (
	hookPreStartResultEnv = "GC_PRE_START_RESULT"
)

type hookClaimOptions struct {
	Assignee           string
	Identities         []string
	PreStartResultPath string
}

type hookWorkItem struct {
	ID       string            `json:"id"`
	Status   string            `json:"status,omitempty"`
	Assignee string            `json:"assignee,omitempty"`
	Owner    string            `json:"owner,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type hookPreStartResult struct {
	Action        string            `json:"action"`
	Reason        string            `json:"reason,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	ClaimedBeadID string            `json:"claimed_bead_id,omitempty"`
}

type hookClaimExecutor interface {
	Claim(ctx context.Context, dir, beadID, assignee string) (hookWorkItem, bool, error)
	Show(ctx context.Context, dir, beadID string) (hookWorkItem, error)
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
	output, err := runner(workQuery, dir)
	if err != nil {
		fmt.Fprintf(stderr, "gc hook: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	normalized := normalizeWorkQueryOutput(strings.TrimSpace(output))
	if !workQueryHasReadyWork(normalized) {
		if opts.PreStartResultPath != "" {
			if err := writeHookPreStartResult(opts.PreStartResultPath, hookPreStartResult{Action: "drain", Reason: "no_work"}); err != nil {
				fmt.Fprintf(stderr, "gc hook: writing pre-start result: %v\n", err) //nolint:errcheck // best-effort stderr
				return 1
			}
			return 0
		}
		if normalized != "" {
			fmt.Fprint(stdout, normalized) //nolint:errcheck // best-effort stdout
		}
		return 1
	}

	assignee := strings.TrimSpace(opts.Assignee)
	if assignee == "" {
		fmt.Fprintln(stderr, "gc hook: --claim requires a session assignee") //nolint:errcheck // best-effort stderr
		return 1
	}
	candidates, err := parseHookWorkItems(normalized)
	if err != nil {
		fmt.Fprintf(stderr, "gc hook: --claim requires JSON work_query output: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	claimed, claimedNow, ok, err := claimHookCandidate(context.Background(), dir, candidates, assignee, opts.Identities, claimer)
	if err != nil {
		fmt.Fprintf(stderr, "gc hook: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	if !ok {
		if opts.PreStartResultPath != "" {
			if err := writeHookPreStartResult(opts.PreStartResultPath, hookPreStartResult{Action: "drain", Reason: "claim_conflict"}); err != nil {
				fmt.Fprintf(stderr, "gc hook: writing pre-start result: %v\n", err) //nolint:errcheck // best-effort stderr
				return 1
			}
			return 0
		}
		return 1
	}

	if opts.PreStartResultPath != "" {
		result := hookPreStartResult{
			Action: "continue",
			Reason: "claimed",
			Env:    map[string]string{"GC_BEAD_ID": claimed.ID},
		}
		if claimedNow {
			result.ClaimedBeadID = claimed.ID
		}
		if err := writeHookPreStartResult(opts.PreStartResultPath, result); err != nil {
			fmt.Fprintf(stderr, "gc hook: writing pre-start result: %v\n", err) //nolint:errcheck // best-effort stderr
			return 1
		}
		return 0
	}

	data, err := json.Marshal([]hookWorkItem{claimed})
	if err != nil {
		fmt.Fprintf(stderr, "gc hook: encoding claimed work: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	fmt.Fprint(stdout, string(data)) //nolint:errcheck // best-effort stdout
	return 0
}

func claimHookCandidate(
	ctx context.Context,
	dir string,
	candidates []hookWorkItem,
	assignee string,
	identities []string,
	claimer hookClaimExecutor,
) (hookWorkItem, bool, bool, error) {
	identity := hookIdentitySet(append([]string{assignee}, identities...))
	for _, candidate := range candidates {
		candidate = normalizeHookWorkItem(candidate)
		if candidate.ID == "" {
			continue
		}
		switch {
		case candidate.Assignee != "" && identity[candidate.Assignee] && candidate.Status == "in_progress":
			if full, err := claimer.Show(ctx, dir, candidate.ID); err == nil && strings.TrimSpace(full.ID) != "" {
				return normalizeHookWorkItem(full), false, true, nil
			}
			return candidate, false, true, nil
		case candidate.Assignee != "" && identity[candidate.Assignee]:
			claimed, ok, err := claimer.Claim(ctx, dir, candidate.ID, assignee)
			if err != nil {
				return hookWorkItem{}, false, false, err
			}
			if ok {
				return normalizeHookWorkItem(claimed), false, true, nil
			}
		case candidate.Assignee == "":
			claimed, ok, err := claimer.Claim(ctx, dir, candidate.ID, assignee)
			if err != nil {
				return hookWorkItem{}, false, false, err
			}
			if ok {
				return normalizeHookWorkItem(claimed), true, true, nil
			}
		}
	}
	return hookWorkItem{}, false, false, nil
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
		return nil, err
	}
	item = normalizeHookWorkItem(item)
	if strings.TrimSpace(item.ID) == "" {
		return nil, fmt.Errorf("work item missing id")
	}
	return []hookWorkItem{item}, nil
}

func normalizeHookWorkItem(item hookWorkItem) hookWorkItem {
	item.ID = strings.TrimSpace(item.ID)
	item.Status = strings.TrimSpace(item.Status)
	item.Assignee = strings.TrimSpace(item.Assignee)
	item.Owner = ""
	return item
}

func writeHookPreStartResult(path string, result hookPreStartResult) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s is empty", hookPreStartResultEnv)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
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
	environ := workQueryEnvForDir(mergeRuntimeEnv(s.env, env), dir)
	return beads.NewBdStore(dir, beads.ExecCommandRunnerWithEnviron(environ))
}

func (s shellHookClaimExecutor) Claim(_ context.Context, dir, beadID, assignee string) (hookWorkItem, bool, error) {
	claimed, err := s.store(dir, map[string]string{"BEADS_ACTOR": assignee}).Claim(beadID)
	if err != nil {
		current, showErr := s.Show(context.Background(), dir, beadID)
		if showErr == nil {
			current = normalizeHookWorkItem(current)
			if current.Assignee == assignee {
				return current, true, nil
			}
			if current.Assignee != "" {
				return hookWorkItem{}, false, nil
			}
		}
		return hookWorkItem{}, false, fmt.Errorf("claiming bead %s: %w", beadID, err)
	}
	return hookWorkItemFromBead(claimed), true, nil
}

func (s shellHookClaimExecutor) Show(_ context.Context, dir, beadID string) (hookWorkItem, error) {
	bead, err := s.store(dir, nil).Get(beadID)
	if err != nil {
		return hookWorkItem{}, err
	}
	return hookWorkItemFromBead(bead), nil
}
