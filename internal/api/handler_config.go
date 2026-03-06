package api

import (
	"net/http"

	"github.com/gastownhall/gascity/internal/config"
)

// configResponse is the JSON representation of the city configuration.
// It provides a structured view of the expanded (post-pack, post-patch)
// configuration state.
type configResponse struct {
	Workspace workspaceResponse           `json:"workspace"`
	Agents    []configAgentResponse       `json:"agents"`
	Rigs      []configRigResponse         `json:"rigs"`
	Providers map[string]providerSpecJSON `json:"providers,omitempty"`
	Patches   *configPatchesResponse      `json:"patches,omitempty"`
}

type workspaceResponse struct {
	Name            string `json:"name"`
	Provider        string `json:"provider,omitempty"`
	Suspended       bool   `json:"suspended"`
	SessionTemplate string `json:"session_template,omitempty"`
}

type configAgentResponse struct {
	Name      string `json:"name"`
	Dir       string `json:"dir,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Suspended bool   `json:"suspended"`
}

type configRigResponse struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Prefix    string `json:"prefix,omitempty"`
	Suspended bool   `json:"suspended"`
}

type providerSpecJSON struct {
	DisplayName  string            `json:"display_name,omitempty"`
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	PromptMode   string            `json:"prompt_mode,omitempty"`
	PromptFlag   string            `json:"prompt_flag,omitempty"`
	ReadyDelayMs int               `json:"ready_delay_ms,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
}

type configPatchesResponse struct {
	AgentCount    int `json:"agent_count"`
	RigCount      int `json:"rig_count"`
	ProviderCount int `json:"provider_count"`
}

func (s *Server) handleConfigGet(w http.ResponseWriter, _ *http.Request) {
	cfg := s.state.Config()

	agents := make([]configAgentResponse, 0, len(cfg.Agents))
	for _, a := range cfg.Agents {
		agents = append(agents, configAgentResponse{
			Name:      a.Name,
			Dir:       a.Dir,
			Provider:  a.Provider,
			Scope:     a.Scope,
			Suspended: a.Suspended,
		})
	}

	rigs := make([]configRigResponse, 0, len(cfg.Rigs))
	for _, r := range cfg.Rigs {
		rigs = append(rigs, configRigResponse{
			Name:      r.Name,
			Path:      r.Path,
			Prefix:    r.Prefix,
			Suspended: r.Suspended,
		})
	}

	providers := make(map[string]providerSpecJSON, len(cfg.Providers))
	for name, spec := range cfg.Providers {
		providers[name] = providerSpecJSON{
			DisplayName:  spec.DisplayName,
			Command:      spec.Command,
			Args:         spec.Args,
			PromptMode:   spec.PromptMode,
			PromptFlag:   spec.PromptFlag,
			ReadyDelayMs: spec.ReadyDelayMs,
			Env:          spec.Env,
		}
	}

	resp := configResponse{
		Workspace: workspaceResponse{
			Name:            cfg.Workspace.Name,
			Provider:        cfg.Workspace.Provider,
			Suspended:       cfg.Workspace.Suspended,
			SessionTemplate: cfg.Workspace.SessionTemplate,
		},
		Agents:    agents,
		Rigs:      rigs,
		Providers: providers,
	}

	if !cfg.Patches.IsEmpty() {
		resp.Patches = &configPatchesResponse{
			AgentCount:    len(cfg.Patches.Agents),
			RigCount:      len(cfg.Patches.Rigs),
			ProviderCount: len(cfg.Patches.Providers),
		}
	}

	writeIndexJSON(w, s.latestIndex(), resp)
}

// handleConfigExplain returns the config with provenance annotations showing
// where each resource originates: raw config, pack-derived, or patched.
func (s *Server) handleConfigExplain(w http.ResponseWriter, _ *http.Request) {
	cfg := s.state.Config()
	builtins := config.BuiltinProviders()

	type annotatedAgent struct {
		configAgentResponse
		Origin string `json:"origin"` // "inline" or "pack-derived"
	}

	type annotatedProvider struct {
		providerSpecJSON
		Origin string `json:"origin"` // "builtin", "city", or "builtin+city"
	}

	agents := make([]annotatedAgent, 0, len(cfg.Agents))
	for _, a := range cfg.Agents {
		agents = append(agents, annotatedAgent{
			configAgentResponse: configAgentResponse{
				Name:      a.Name,
				Dir:       a.Dir,
				Provider:  a.Provider,
				Scope:     a.Scope,
				Suspended: a.Suspended,
			},
			// In the expanded config, all agents are present. Without
			// raw config access at the API layer, we annotate based on
			// patches section: if a patch targets this agent, it's
			// likely pack-derived. Otherwise it's inline.
			Origin: agentOriginFromPatches(a, cfg.Patches),
		})
	}

	// Annotate providers with origin.
	provMap := make(map[string]annotatedProvider)
	// City-level providers.
	for name, spec := range cfg.Providers {
		origin := "city"
		if _, isBuiltin := builtins[name]; isBuiltin {
			origin = "builtin+city"
		}
		provMap[name] = annotatedProvider{
			providerSpecJSON: providerSpecJSON{
				DisplayName:  spec.DisplayName,
				Command:      spec.Command,
				Args:         spec.Args,
				PromptMode:   spec.PromptMode,
				PromptFlag:   spec.PromptFlag,
				ReadyDelayMs: spec.ReadyDelayMs,
				Env:          spec.Env,
			},
			Origin: origin,
		}
	}
	// Builtins not overridden.
	for name, spec := range builtins {
		if _, ok := provMap[name]; !ok {
			provMap[name] = annotatedProvider{
				providerSpecJSON: providerSpecJSON{
					DisplayName:  spec.DisplayName,
					Command:      spec.Command,
					Args:         spec.Args,
					PromptMode:   spec.PromptMode,
					PromptFlag:   spec.PromptFlag,
					ReadyDelayMs: spec.ReadyDelayMs,
					Env:          spec.Env,
				},
				Origin: "builtin",
			}
		}
	}

	writeIndexJSON(w, s.latestIndex(), map[string]any{
		"agents":    agents,
		"providers": provMap,
		"patches": map[string]int{
			"agents":    len(cfg.Patches.Agents),
			"rigs":      len(cfg.Patches.Rigs),
			"providers": len(cfg.Patches.Providers),
		},
	})
}

// handleConfigValidate checks the current config for validation errors
// and semantic warnings without writing anything.
func (s *Server) handleConfigValidate(w http.ResponseWriter, _ *http.Request) {
	cfg := s.state.Config()

	var errors []string

	if err := config.ValidateAgents(cfg.Agents); err != nil {
		errors = append(errors, err.Error())
	}
	if err := config.ValidateRigs(cfg.Rigs, cfg.Workspace.Name); err != nil {
		errors = append(errors, err.Error())
	}

	warnings := config.ValidateSemantics(cfg, "city.toml")
	warnings = append(warnings, config.ValidateDurations(cfg, "city.toml")...)

	valid := len(errors) == 0
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":    valid,
		"errors":   errors,
		"warnings": warnings,
	})
}

// agentOriginFromPatches heuristically determines agent origin based on
// whether a patch targets it.
func agentOriginFromPatches(a config.Agent, patches config.Patches) string {
	for _, p := range patches.Agents {
		if p.Dir == a.Dir && p.Name == a.Name {
			return "pack-derived"
		}
	}
	return "inline"
}
