package api

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/runtime"
	"github.com/gastownhall/gascity/internal/telemetry"
)

type slingBody struct {
	Rig            string            `json:"rig"`
	Target         string            `json:"target"`
	Bead           string            `json:"bead"`
	Formula        string            `json:"formula"`
	AttachedBeadID string            `json:"attached_bead_id"`
	Title          string            `json:"title"`
	Vars           map[string]string `json:"vars"`
}

type slingWorkflowResponse struct {
	Status         string `json:"status"`
	Target         string `json:"target"`
	Formula        string `json:"formula,omitempty"`
	Bead           string `json:"bead,omitempty"`
	WorkflowID     string `json:"workflow_id,omitempty"`
	RootBeadID     string `json:"root_bead_id,omitempty"`
	AttachedBeadID string `json:"attached_bead_id,omitempty"`
	Mode           string `json:"mode,omitempty"`
}

var slingFormulaCommandRunner = runSlingFormulaCommand

func (s *Server) handleSling(w http.ResponseWriter, r *http.Request) {
	var body slingBody
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
	if body.Bead != "" && body.AttachedBeadID != "" {
		writeError(w, http.StatusBadRequest, "invalid", "bead and attached_bead_id are mutually exclusive")
		return
	}
	if body.Formula == "" && (body.AttachedBeadID != "" || len(body.Vars) > 0 || body.Title != "") {
		writeError(w, http.StatusBadRequest, "invalid", "formula is required when attached_bead_id, vars, or title are provided")
		return
	}

	if body.Formula != "" {
		resp, status, code, message := s.handleFormulaSling(r.Context(), body)
		if code != "" {
			writeError(w, status, code, message)
			return
		}
		writeJSON(w, status, resp)
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

	// Nudge the target agent if session provider supports it.
	sp := s.state.SessionProvider()
	sessionName := agentSessionName(s.state.CityName(), body.Target, cfg.Workspace.SessionTemplate)
	resp := map[string]string{"status": "slung", "target": body.Target, "bead": body.Bead}
	if err := sp.Nudge(sessionName, runtime.TextContent("New work assigned: "+body.Bead)); err != nil {
		resp["nudge_error"] = err.Error()
		telemetry.RecordNudge(context.Background(), body.Target, err)
	} else {
		telemetry.RecordNudge(context.Background(), body.Target, nil)
	}

	// Poke unconditionally: even if nudge succeeded, the target may be
	// asleep and need a reconciler tick to wake via WakeWork. The poke
	// coalesces via buffered(1) channel so extra pokes are harmless.
	s.state.Poke()

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFormulaSling(ctx context.Context, body slingBody) (*slingWorkflowResponse, int, string, string) {
	args := []string{"--city", s.state.CityPath(), "sling", body.Target}
	mode := "standalone"
	if beadID := strings.TrimSpace(body.AttachedBeadID); beadID != "" {
		mode = "attached"
		args = append(args, beadID, "--on", body.Formula)
	} else {
		args = append(args, body.Formula, "--formula")
	}
	if title := strings.TrimSpace(body.Title); title != "" {
		args = append(args, "--title", title)
	}
	if len(body.Vars) > 0 {
		keys := make([]string, 0, len(body.Vars))
		for key := range body.Vars {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			args = append(args, "--var", key+"="+body.Vars[key])
		}
	}

	stdout, stderr, err := slingFormulaCommandRunner(ctx, s.state.CityPath(), args)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		if message == "" {
			message = err.Error()
		}
		return nil, http.StatusBadRequest, "invalid", message
	}

	workflowID := parseWorkflowIDFromSlingOutput(stdout)
	if workflowID == "" {
		workflowID = parseWorkflowIDFromSlingOutput(stderr)
	}
	if workflowID == "" {
		return nil, http.StatusInternalServerError, "internal", "gc sling did not report a workflow id"
	}

	return &slingWorkflowResponse{
		Status:         "slung",
		Target:         body.Target,
		Formula:        body.Formula,
		WorkflowID:     workflowID,
		RootBeadID:     workflowID,
		AttachedBeadID: strings.TrimSpace(body.AttachedBeadID),
		Mode:           mode,
	}, http.StatusCreated, "", ""
}

func runSlingFormulaCommand(ctx context.Context, cityPath string, args []string) (string, string, error) {
	gcBin, err := os.Executable()
	if err != nil {
		return "", "", err
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, gcBin, args...)
	cmd.Dir = cityPath

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

func parseWorkflowIDFromSlingOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"Started workflow ", "Attached workflow "} {
			if rest, ok := strings.CutPrefix(line, prefix); ok {
				workflowID, _, _ := strings.Cut(rest, " ")
				return strings.TrimSpace(workflowID)
			}
		}
	}
	return ""
}
