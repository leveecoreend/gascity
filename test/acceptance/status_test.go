//go:build acceptance_a

// Status command acceptance tests.
//
// These exercise gc status as a black box. Status shows a city-wide
// overview including controller state, agents, rigs, and sessions.
package acceptance_test

import (
	"path/filepath"
	"strings"
	"testing"

	helpers "github.com/gastownhall/gascity/test/acceptance/helpers"
)

// --- gc status ---

func TestStatus_BasicCity_ShowsCityName(t *testing.T) {
	c := helpers.NewCity(t, testEnv)
	c.Init("claude")

	out, err := c.GC("status", c.Dir)
	if err != nil {
		t.Fatalf("gc status: %v\n%s", err, out)
	}
	// Status should show the city directory path.
	if !strings.Contains(out, c.Dir) {
		t.Errorf("status should contain city path %q, got:\n%s", c.Dir, out)
	}
}

func TestStatus_ShowsControllerLine(t *testing.T) {
	c := helpers.NewCity(t, testEnv)
	c.Init("claude")

	out, err := c.GC("status", c.Dir)
	if err != nil {
		t.Fatalf("gc status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Controller:") {
		t.Errorf("status should show 'Controller:' line, got:\n%s", out)
	}
}

func TestStatus_ShowsSuspendedState(t *testing.T) {
	c := helpers.NewCity(t, testEnv)
	c.Init("claude")

	out, err := c.GC("status", c.Dir)
	if err != nil {
		t.Fatalf("gc status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Suspended:") {
		t.Errorf("status should show 'Suspended:' line, got:\n%s", out)
	}
}

func TestStatus_GastownCity_ShowsAgents(t *testing.T) {
	c := helpers.NewCity(t, testEnv)
	c.InitFrom(filepath.Join(helpers.ExamplesDir(), "gastown"))

	out, err := c.GC("status", c.Dir)
	if err != nil {
		t.Fatalf("gc status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Agents:") {
		t.Errorf("gastown status should show 'Agents:' section, got:\n%s", out)
	}
	if !strings.Contains(out, "agents running") {
		t.Errorf("status should show agent count summary, got:\n%s", out)
	}
}

func TestStatus_ShowsSessionsSummary(t *testing.T) {
	c := helpers.NewCity(t, testEnv)
	c.InitFrom(filepath.Join(helpers.ExamplesDir(), "gastown"))

	out, err := c.GC("status", c.Dir)
	if err != nil {
		t.Fatalf("gc status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sessions:") {
		t.Errorf("status should show 'Sessions:' summary, got:\n%s", out)
	}
}

func TestStatus_JSON_ReturnsValidJSON(t *testing.T) {
	c := helpers.NewCity(t, testEnv)
	c.Init("claude")

	out, err := c.GC("status", "--json", c.Dir)
	if err != nil {
		t.Fatalf("gc status --json: %v\n%s", err, out)
	}
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "{") {
		t.Errorf("--json output should start with '{', got:\n%s", out)
	}
}

func TestStatus_NotInitialized_ReturnsError(t *testing.T) {
	emptyDir := t.TempDir()
	_, err := helpers.RunGC(testEnv, emptyDir, "status", emptyDir)
	if err == nil {
		t.Fatal("expected error for status on non-city directory, got success")
	}
}
