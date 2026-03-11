package sessionlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentMapping links a subagent to the parent Task tool_use that spawned it.
type AgentMapping struct {
	AgentID         string `json:"agent_id"`
	ParentToolUseID string `json:"parent_tool_use_id"`
}

// AgentStatus describes the lifecycle state of a subagent.
type AgentStatus string

// Agent lifecycle states.
const (
	AgentStatusPending   AgentStatus = "pending"
	AgentStatusRunning   AgentStatus = "running"
	AgentStatusCompleted AgentStatus = "completed"
	AgentStatusFailed    AgentStatus = "failed"
)

// AgentSession is a subagent's transcript and inferred status.
type AgentSession struct {
	Messages []*Entry    `json:"messages"`
	Status   AgentStatus `json:"status"`
}

// FindAgentFiles returns all agent-*.jsonl files in the same directory
// as the parent session JSONL. Returns nil if none are found.
func FindAgentFiles(parentLogPath string) []string {
	dir := filepath.Dir(parentLogPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

// FindAgentMappings scans agent-*.jsonl files alongside the parent session
// and extracts the parent_tool_use_id from each agent's first entry.
func FindAgentMappings(parentLogPath string) ([]AgentMapping, error) {
	agentPaths := FindAgentFiles(parentLogPath)
	if len(agentPaths) == 0 {
		return nil, nil
	}

	var mappings []AgentMapping
	for _, path := range agentPaths {
		agentID := agentIDFromPath(path)
		toolUseID := extractParentToolUseID(path)
		if agentID == "" {
			continue
		}
		mappings = append(mappings, AgentMapping{
			AgentID:         agentID,
			ParentToolUseID: toolUseID,
		})
	}
	return mappings, nil
}

// ReadAgentSession reads a subagent JSONL file and returns its transcript
// and inferred status. Uses the same DAG resolution as parent sessions.
func ReadAgentSession(parentLogPath, agentID string) (*AgentSession, error) {
	dir := filepath.Dir(parentLogPath)
	agentPath := filepath.Join(dir, "agent-"+agentID+".jsonl")

	if _, err := os.Stat(agentPath); err != nil {
		return nil, fmt.Errorf("agent file not found: %w", err)
	}

	entries, err := parseFile(agentPath)
	if err != nil {
		return nil, err
	}

	dag := BuildDag(entries)
	status := inferAgentStatus(dag.ActiveBranch)

	return &AgentSession{
		Messages: dag.ActiveBranch,
		Status:   status,
	}, nil
}

// agentIDFromPath extracts the agent ID from a path like
// "/path/to/agent-{id}.jsonl".
func agentIDFromPath(path string) string {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "agent-") || !strings.HasSuffix(base, ".jsonl") {
		return ""
	}
	name := strings.TrimPrefix(base, "agent-")
	name = strings.TrimSuffix(name, ".jsonl")
	if name == "" {
		return ""
	}
	return name
}

// extractParentToolUseID reads the first few lines of an agent JSONL file
// and looks for the parentToolUseId field. Claude Code writes this on
// the first entry of every subagent session.
func extractParentToolUseID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read-only

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Check first 5 lines — the field is usually on line 1.
	for i := 0; i < 5 && scanner.Scan(); i++ {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry struct {
			ParentToolUseID string `json:"parentToolUseId"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.ParentToolUseID != "" {
			return entry.ParentToolUseID
		}
	}
	return ""
}

// inferAgentStatus determines the agent's status from its message history.
func inferAgentStatus(messages []*Entry) AgentStatus {
	if len(messages) == 0 {
		return AgentStatusPending
	}

	// Scan from the end for a result entry.
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type == "result" {
			if len(messages[i].Message) > 0 {
				var msg struct {
					IsError bool `json:"is_error"`
				}
				if err := json.Unmarshal(messages[i].Message, &msg); err == nil && msg.IsError {
					return AgentStatusFailed
				}
			}
			return AgentStatusCompleted
		}
	}
	return AgentStatusRunning
}
