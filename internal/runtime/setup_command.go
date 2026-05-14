package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultSetupCommandTimeout is the fallback timeout for provider setup
// commands when the provider has no configurable setup timeout.
const DefaultSetupCommandTimeout = 10 * time.Second

// SetupCommandRunner runs one provider setup command with the supplied
// environment and timeout.
type SetupCommandRunner func(ctx context.Context, command string, env map[string]string, timeout time.Duration) error

// RunSetupCommand runs a shell setup command using GC_DIR as the working
// directory when it exists.
func RunSetupCommand(ctx context.Context, command string, env map[string]string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", command)
	if workDir := strings.TrimSpace(env["GC_DIR"]); workDir != "" {
		info, err := os.Stat(workDir)
		switch {
		case err == nil && info.IsDir():
			c.Dir = workDir
		case err == nil:
			return fmt.Errorf("GC_DIR %q is not a directory", workDir)
		case os.IsNotExist(err):
			if strings.TrimSpace(env[StartGateEnv]) == "" {
				return fmt.Errorf("GC_DIR %q does not exist", workDir)
			}
		default:
			return fmt.Errorf("stat GC_DIR %q: %w", workDir, err)
		}
	}
	c.Env = os.Environ()
	for k, v := range env {
		c.Env = append(c.Env, k+"="+v)
	}
	return c.Run()
}

// RunStartGate runs cfg.StartGate and applies supported env written through
// GC_START_ENV. Exit 1 with no env is a clean decline.
func RunStartGate(ctx context.Context, cfg Config, setupTimeout time.Duration, run SetupCommandRunner) (Config, error) {
	if strings.TrimSpace(cfg.StartGate) == "" {
		return cfg, nil
	}
	if run == nil {
		return cfg, fmt.Errorf("start_gate runner is nil")
	}
	clonedEnv := make(map[string]string, len(cfg.Env))
	for k, v := range cfg.Env {
		clonedEnv[k] = v
	}
	cfg.Env = clonedEnv
	cfg.StartGateEnv = nil
	resultDir, err := os.MkdirTemp("", "gc-start-gate-*")
	if err != nil {
		return cfg, fmt.Errorf("creating start_gate env dir: %w", err)
	}
	defer os.RemoveAll(resultDir) //nolint:errcheck // best-effort temp cleanup
	envPath := filepath.Join(resultDir, "env")
	setupEnv := make(map[string]string, len(cfg.Env)+1)
	for k, v := range cfg.Env {
		setupEnv[k] = v
	}
	setupEnv[StartGateEnv] = envPath
	cmdErr := run(ctx, cfg.StartGate, setupEnv, setupTimeout)
	updates, ok, readErr := ReadStartGateEnvFile(envPath)
	if readErr != nil {
		if cmdErr != nil {
			return cfg, fmt.Errorf("start_gate: %w", errors.Join(cmdErr, readErr))
		}
		return cfg, fmt.Errorf("start_gate: %w", readErr)
	}
	if ok {
		if err := ApplyStartGateEnv(cfg.Env, updates); err != nil {
			return cfg, WithStartGateEnv(fmt.Errorf("start_gate: %w", err), updates)
		}
		cfg.StartGateEnv = cloneStartGateEnv(updates)
	}
	if cmdErr == nil {
		return cfg, nil
	}
	if code, hasCode := setupExitCode(cmdErr); hasCode && code == StartGateDeclinedExitCode && !ok {
		return cfg, ErrStartGateDeclined
	}
	return cfg, WithStartGateEnv(fmt.Errorf("start_gate: %w", cmdErr), cfg.StartGateEnv)
}

// RunPreStart runs cfg.PreStart commands in order.
func RunPreStart(ctx context.Context, cfg Config, setupTimeout time.Duration, run SetupCommandRunner) (Config, error) {
	if len(cfg.PreStart) == 0 {
		return cfg, nil
	}
	if run == nil {
		return cfg, fmt.Errorf("pre_start runner is nil")
	}
	clonedEnv := make(map[string]string, len(cfg.Env))
	for k, v := range cfg.Env {
		clonedEnv[k] = v
	}
	cfg.Env = clonedEnv
	for i, command := range cfg.PreStart {
		setupEnv := make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			setupEnv[k] = v
		}
		if err := run(ctx, command, setupEnv, setupTimeout); err != nil {
			return cfg, fmt.Errorf("pre_start[%d]: %w", i, err)
		}
	}
	return cfg, nil
}

type setupExitCoder interface {
	ExitCode() int
}

func setupExitCode(err error) (int, bool) {
	var exitErr setupExitCoder
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}
