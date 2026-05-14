package runtime

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	// StartGateEnv is the environment variable that points start_gate scripts
	// at the line-oriented env handoff file they may write before exiting 0.
	StartGateEnv = "GC_START_ENV"

	// StartGateDeclinedExitCode tells providers not to start the session
	// without treating the gate as a failure.
	StartGateDeclinedExitCode = 1

	// MaxStartGateEnvSize caps the env handoff file a runtime will parse.
	MaxStartGateEnvSize = 64 * 1024

	maxStartGateValueSize = 8 * 1024
)

// StartGateEnvError annotates a startup failure with env produced by a
// start_gate command before the session started.
type StartGateEnvError struct {
	Err error
	Env map[string]string
}

// Error implements error.
func (e *StartGateEnvError) Error() string {
	if e == nil || e.Err == nil {
		return "start_gate env"
	}
	return e.Err.Error()
}

// Unwrap returns the underlying startup error.
func (e *StartGateEnvError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// WithStartGateEnv annotates err with validated env produced by start_gate.
func WithStartGateEnv(err error, env map[string]string) error {
	if err == nil {
		return nil
	}
	snapshot := startGateEnvSnapshot(env)
	if len(snapshot) == 0 {
		return err
	}
	return &StartGateEnvError{Err: err, Env: snapshot}
}

// StartGateErrorEnv returns a copy of the start_gate env carried by err.
func StartGateErrorEnv(err error) map[string]string {
	var gateErr *StartGateEnvError
	if !errors.As(err, &gateErr) || gateErr == nil || len(gateErr.Env) == 0 {
		return nil
	}
	return cloneStartGateEnv(gateErr.Env)
}

// ReadStartGateEnvFile reads and validates a start_gate env handoff file.
func ReadStartGateEnvFile(path string) (map[string]string, bool, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close() //nolint:errcheck // best-effort close after read
	data, err := io.ReadAll(io.LimitReader(file, MaxStartGateEnvSize+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > MaxStartGateEnvSize {
		return nil, false, fmt.Errorf("%s is too large", StartGateEnv)
	}
	return ParseStartGateEnv(data, StartGateEnv)
}

// ParseStartGateEnv validates a line-oriented start_gate env handoff.
func ParseStartGateEnv(data []byte, source string) (map[string]string, bool, error) {
	if len(data) > MaxStartGateEnvSize {
		return nil, false, fmt.Errorf("%s is too large", source)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, false, nil
	}
	if strings.TrimSpace(source) == "" {
		source = StartGateEnv
	}
	env := map[string]string{}
	for lineNo, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, false, fmt.Errorf("parsing %s line %d: expected KEY=VALUE", source, lineNo+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if err := ValidateStartGateEnv(key, value); err != nil {
			return nil, false, err
		}
		if _, exists := env[key]; exists {
			return nil, false, fmt.Errorf("start_gate env %s is duplicated", key)
		}
		env[key] = value
	}
	return env, len(env) > 0, nil
}

// ApplyStartGateEnv validates and applies start_gate env updates.
func ApplyStartGateEnv(env map[string]string, updates map[string]string) error {
	envUpdates := make(map[string]string, len(updates))
	for key, value := range updates {
		if err := ValidateStartGateEnv(key, value); err != nil {
			return err
		}
		envUpdates[key] = value
	}
	if len(envUpdates) > 0 && env == nil {
		return fmt.Errorf("start_gate env target is nil")
	}
	for key, value := range envUpdates {
		env[key] = value
	}
	return nil
}

// ValidateStartGateEnv validates one start_gate env update.
func ValidateStartGateEnv(key, value string) error {
	if !validStartGateEnvKey(key) {
		return fmt.Errorf("start_gate env invalid env key %q", key)
	}
	return validateStartGateValue(key, value)
}

func startGateEnvSnapshot(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string)
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := env[key]
		if err := ValidateStartGateEnv(key, value); err != nil {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validStartGateEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func cloneStartGateEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	return out
}

func validateStartGateValue(key, value string) error {
	if strings.ContainsAny(value, "\x00\n") {
		return fmt.Errorf("start_gate env %s contains an invalid value", key)
	}
	if len(value) > maxStartGateValueSize {
		return fmt.Errorf("start_gate env %s is too large", key)
	}
	return nil
}
