package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/spf13/cobra"
)

func newSkillCmd(stdout, stderr io.Writer) *cobra.Command {
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:   "skill",
		Short: "List visible skills",
		Long: `List visible Pack/City skills for the current city pack.

Use "gc skill list" to show discovered skills, optionally scoped to an
agent or session. The built-in topic/reference viewer now lives under
"gc skills".`,
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			fmt.Fprintf(stderr, "gc skill: unknown subcommand %q\n", args[0]) //nolint:errcheck // best-effort stderr
			return errExit
		},
	}
	cmd.AddCommand(newSkillListCmd(stdout, stderr))
	return cmd
}

func newSkillListCmd(stdout, stderr io.Writer) *cobra.Command {
	var agentName string
	var sessionID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List visible skills",
		Long:  "List the current city pack's visible skills, optionally scoped to an agent or session.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(agentName) != "" && strings.TrimSpace(sessionID) != "" {
				fmt.Fprintln(stderr, "gc skill list: --agent and --session are mutually exclusive") //nolint:errcheck // best-effort stderr
				return errExit
			}
			cityPath, err := resolveCity()
			if err != nil {
				fmt.Fprintf(stderr, "gc skill list: %v\n", err) //nolint:errcheck // best-effort stderr
				return errExit
			}
			cfg, err := loadCityConfig(cityPath)
			if err != nil {
				fmt.Fprintf(stderr, "gc skill list: %v\n", err) //nolint:errcheck // best-effort stderr
				return errExit
			}

			var store beads.Store
			if strings.TrimSpace(sessionID) != "" {
				store, err = openCityStoreAt(cityPath)
				if err != nil {
					fmt.Fprintf(stderr, "gc skill list: %v\n", err) //nolint:errcheck // best-effort stderr
					return errExit
				}
			}

			entries, err := listVisibleSkillEntries(cityPath, cfg, store, agentName, sessionID)
			if err != nil {
				fmt.Fprintf(stderr, "gc skill list: %v\n", err) //nolint:errcheck // best-effort stderr
				return errExit
			}
			writeVisibilityEntries(stdout, entries)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "show the effective skill view for this agent")
	cmd.Flags().StringVar(&sessionID, "session", "", "show the effective skill view for this session")
	return cmd
}

func listVisibleSkillEntries(cityPath string, cfg *config.City, store beads.Store, agentName, sessionID string) ([]visibilityEntry, error) {
	entries := discoverSkillEntries(cityPath, "city")
	if strings.TrimSpace(agentName) == "" && strings.TrimSpace(sessionID) == "" {
		return entries, nil
	}
	agent, err := resolveVisibilityAgent(cityPath, cfg, store, agentName, sessionID)
	if err != nil {
		return nil, err
	}
	entries = append(entries, discoverAgentSkillEntries(agentAssetRoot(cityPath, agent), agent.Name, "agent")...)
	sortVisibilityEntries(entries)
	return entries, nil
}

type visibilityEntry struct {
	Name   string
	Source string
	Path   string
}

func resolveVisibilityAgent(cityPath string, cfg *config.City, store beads.Store, agentName, sessionID string) (*config.Agent, error) {
	switch {
	case strings.TrimSpace(agentName) != "":
		resolved, ok := resolveAgentIdentity(cfg, agentName, currentRigContext(cfg))
		if !ok {
			return nil, fmt.Errorf("unknown agent %q", agentName)
		}
		template := resolveAgentTemplate(resolved.QualifiedName(), cfg)
		agent := findAgentByTemplate(cfg, template)
		if agent == nil {
			return nil, fmt.Errorf("unknown agent %q", agentName)
		}
		return agent, nil
	case strings.TrimSpace(sessionID) != "":
		if store == nil {
			return nil, fmt.Errorf("session store unavailable")
		}
		id, err := resolveSessionIDAllowClosedWithConfig(cityPath, cfg, store, sessionID)
		if err != nil {
			return nil, err
		}
		bead, err := store.Get(id)
		if err != nil {
			return nil, fmt.Errorf("loading session %q: %w", sessionID, err)
		}
		template := normalizedSessionTemplate(bead, cfg)
		if template == "" {
			template = strings.TrimSpace(bead.Metadata["agent_name"])
		}
		template = resolveAgentTemplate(template, cfg)
		agent := findAgentByTemplate(cfg, template)
		if agent == nil {
			return nil, fmt.Errorf("session %q maps to unknown agent template %q", sessionID, template)
		}
		return agent, nil
	default:
		return nil, nil
	}
}

func agentAssetRoot(cityPath string, agent *config.Agent) string {
	if agent != nil && strings.TrimSpace(agent.SourceDir) != "" {
		return agent.SourceDir
	}
	return cityPath
}

func discoverSkillEntries(root, source string) []visibilityEntry {
	return discoverSkillDirEntries(filepath.Join(root, "skills"), "skills", source)
}

func discoverAgentSkillEntries(root, agentName, source string) []visibilityEntry {
	return discoverSkillDirEntries(filepath.Join(root, "agents", agentName, "skills"), filepath.Join("agents", agentName, "skills"), source)
}

func discoverSkillDirEntries(dir, relBase, source string) []visibilityEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []visibilityEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		skillPath := filepath.Join(dir, name, "SKILL.md")
		if info, err := os.Stat(skillPath); err != nil || info.IsDir() {
			continue
		}
		out = append(out, visibilityEntry{
			Name:   name,
			Source: source,
			Path:   filepath.ToSlash(filepath.Join(relBase, name, "SKILL.md")),
		})
	}
	sortVisibilityEntries(out)
	return out
}

func discoverMcpEntries(root, source string) []visibilityEntry {
	return discoverMcpDirEntries(filepath.Join(root, "mcp"), "mcp", source)
}

func discoverAgentMcpEntries(root, agentName, source string) []visibilityEntry {
	return discoverMcpDirEntries(filepath.Join(root, "agents", agentName, "mcp"), filepath.Join("agents", agentName, "mcp"), source)
}

func discoverMcpDirEntries(dir, relBase, source string) []visibilityEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []visibilityEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name, ok := mcpIdentityForFilename(entry.Name())
		if !ok {
			continue
		}
		out = append(out, visibilityEntry{
			Name:   name,
			Source: source,
			Path:   filepath.ToSlash(filepath.Join(relBase, entry.Name())),
		})
	}
	sortVisibilityEntries(out)
	return out
}

func mcpIdentityForFilename(name string) (string, bool) {
	switch {
	case strings.HasSuffix(name, ".template.toml"):
		return strings.TrimSuffix(name, ".template.toml"), true
	case strings.HasSuffix(name, ".toml"):
		return strings.TrimSuffix(name, ".toml"), true
	default:
		return "", false
	}
}

func sortVisibilityEntries(entries []visibilityEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Source != entries[j].Source {
			return entries[i].Source < entries[j].Source
		}
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Path < entries[j].Path
	})
}

func writeVisibilityEntries(w io.Writer, entries []visibilityEntry) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tFROM\tPATH") //nolint:errcheck // best-effort
	for _, entry := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", entry.Name, entry.Source, entry.Path) //nolint:errcheck // best-effort
	}
	_ = tw.Flush()
}
