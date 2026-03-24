package formula

import (
	"fmt"
	"strconv"
)

// ApplyRetries expands inline retry-managed steps into ordinary graph nodes.
//
// A retry-managed step:
//   - keeps its original step ID as the stable logical step
//   - emits a first run attempt:  <step>.run.1
//   - emits a first eval attempt: <step>.eval.1
//
// The generated graph uses only ordinary blocking deps:
//   - eval blocks on run
//   - logical step blocks on eval
//
// Downstream steps continue to depend on the original logical step ID.
func ApplyRetries(steps []*Step) ([]*Step, error) {
	result := make([]*Step, 0, len(steps))

	for _, step := range steps {
		if step.Retry == nil {
			clone := cloneStep(step)
			if len(step.Children) > 0 {
				children, err := ApplyRetries(step.Children)
				if err != nil {
					return nil, err
				}
				clone.Children = children
			}
			result = append(result, clone)
			continue
		}

		expanded, err := expandRetry(step)
		if err != nil {
			return nil, err
		}
		result = append(result, expanded...)
	}

	return result, nil
}

func expandRetry(step *Step) ([]*Step, error) {
	if step.Retry == nil {
		return nil, fmt.Errorf("expanding retry: step %q missing retry spec", step.ID)
	}

	attempt := 1
	runID := fmt.Sprintf("%s.run.%d", step.ID, attempt)
	evalID := fmt.Sprintf("%s.eval.%d", step.ID, attempt)
	onExhausted := step.Retry.OnExhausted
	if onExhausted == "" {
		onExhausted = "hard_fail"
	}

	logical := cloneStep(step)
	logical.Retry = nil
	logical.Children = nil
	logical.Assignee = ""
	logical.Metadata = withMetadata(logical.Metadata, map[string]string{
		"gc.kind":         "retry",
		"gc.step_id":      step.ID,
		"gc.max_attempts": strconv.Itoa(step.Retry.MaxAttempts),
		"gc.on_exhausted": onExhausted,
	})
	if kind := step.Metadata["gc.kind"]; kind != "" {
		logical.Metadata["gc.original_kind"] = kind
	}
	logical.Needs = appendUniqueCopy(logical.Needs, evalID)
	logical.WaitsFor = ""

	run := cloneStep(step)
	run.ID = runID
	run.Retry = nil
	run.OnComplete = nil
	run.Children = nil
	run.Metadata = withMetadata(run.Metadata, map[string]string{
		"gc.kind":          "retry-run",
		"gc.step_id":       step.ID,
		"gc.retry_step_id": step.ID,
		"gc.attempt":       strconv.Itoa(attempt),
		"gc.max_attempts":  strconv.Itoa(step.Retry.MaxAttempts),
		"gc.on_exhausted":  onExhausted,
	})
	if kind := step.Metadata["gc.kind"]; kind != "" {
		run.Metadata["gc.original_kind"] = kind
	}
	delete(run.Metadata, "gc.scope_ref")
	delete(run.Metadata, "gc.scope_role")
	delete(run.Metadata, "gc.on_fail")
	run.SourceLocation = fmt.Sprintf("%s.retry.run.%d", step.SourceLocation, attempt)

	eval := &Step{
		ID:             evalID,
		Title:          fmt.Sprintf("Evaluate %s", step.Title),
		Description:    fmt.Sprintf("Evaluate %s attempt %d", step.ID, attempt),
		Type:           "task",
		Priority:       step.Priority,
		Labels:         append([]string{}, step.Labels...),
		Needs:          []string{runID},
		Condition:      step.Condition,
		SourceFormula:  step.SourceFormula,
		SourceLocation: fmt.Sprintf("%s.retry.eval.%d", step.SourceLocation, attempt),
		Metadata: withMetadata(nil, map[string]string{
			"gc.kind":          "retry-eval",
			"gc.step_id":       step.ID,
			"gc.retry_step_id": step.ID,
			"gc.attempt":       strconv.Itoa(attempt),
			"gc.max_attempts":  strconv.Itoa(step.Retry.MaxAttempts),
			"gc.on_exhausted":  onExhausted,
		}),
	}
	if kind := step.Metadata["gc.kind"]; kind != "" {
		eval.Metadata["gc.original_kind"] = kind
	}

	return []*Step{logical, run, eval}, nil
}
