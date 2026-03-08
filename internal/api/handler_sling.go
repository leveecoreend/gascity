package api

import (
	"net/http"

	"github.com/gastownhall/gascity/internal/beads"
)

func (s *Server) handleSling(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rig     string `json:"rig"`
		Target  string `json:"target"`
		Bead    string `json:"bead"`
		Formula string `json:"formula"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if body.Target == "" {
		writeError(w, http.StatusBadRequest, "invalid", "target agent is required")
		return
	}

	// Validate target agent exists in config.
	cfg := s.state.Config()
	agentCfg, ok := findAgent(cfg, body.Target)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "target agent "+body.Target+" not found")
		return
	}

	if body.Bead == "" && body.Formula == "" {
		writeError(w, http.StatusBadRequest, "invalid", "bead or formula is required")
		return
	}
	if body.Bead != "" && body.Formula != "" {
		writeError(w, http.StatusBadRequest, "invalid", "bead and formula are mutually exclusive")
		return
	}

	// Derive rig from target agent's config if not explicitly provided.
	rig := body.Rig
	if rig == "" {
		rig = agentCfg.Dir
	}
	store := s.findStore(rig)
	if store == nil {
		writeError(w, http.StatusBadRequest, "invalid", "no bead store available")
		return
	}

	// If a bead is specified, assign it to the target agent.
	if body.Bead != "" {
		assignee := body.Target
		if err := store.Update(body.Bead, beads.UpdateOpts{
			Assignee: &assignee,
		}); err != nil {
			writeStoreError(w, err)
			return
		}
	}

	// If a formula is specified, cook a molecule and assign to target.
	// Note: cook+assign is not atomic — if assign fails after cook, the molecule
	// exists unassigned. Acceptable for v0 single-process server; true atomicity
	// requires transactional store operations (future work).
	if body.Formula != "" {
		rootID, err := store.MolCook(body.Formula, body.Formula, nil)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		// Assign root bead to target agent (matching bead path semantics).
		assignee := body.Target
		if err := store.Update(rootID, beads.UpdateOpts{Assignee: &assignee}); err != nil {
			writeStoreError(w, err)
			return
		}
		// Nudge target agent.
		sp := s.state.SessionProvider()
		sessionName := agentSessionName(s.state.CityName(), body.Target, cfg.Workspace.SessionTemplate)
		resp := map[string]string{"status": "slung", "molecule": rootID, "target": body.Target}
		if err := sp.Nudge(sessionName, "New molecule assigned: "+rootID); err != nil {
			resp["nudge_error"] = err.Error()
		}
		// Poke for immediate wake (same as bead path).
		s.state.Poke()
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Nudge the target agent if session provider supports it.
	sp := s.state.SessionProvider()
	sessionName := agentSessionName(s.state.CityName(), body.Target, cfg.Workspace.SessionTemplate)
	resp := map[string]string{"status": "slung", "target": body.Target, "bead": body.Bead}
	if err := sp.Nudge(sessionName, "New work assigned: "+body.Bead); err != nil {
		resp["nudge_error"] = err.Error()
	}

	// Poke the controller to trigger immediate reconciler tick so WakeWork
	// can wake the target session without waiting for the next patrol.
	s.state.Poke()

	writeJSON(w, http.StatusOK, resp)
}
