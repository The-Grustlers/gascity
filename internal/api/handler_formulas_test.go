package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFormulaListReturnsCatalogSummaries(t *testing.T) {
	state := newFakeState(t)
	formulaDir := t.TempDir()
	state.cfg.FormulaLayers.City = []string{formulaDir}

	writeTestFormula(t, formulaDir, "mol-adopt-pr-v2", `
description = "Review and fix a PR with a retry loop."
formula = "mol-adopt-pr-v2"
version = 2

[vars]
[vars.pr_url]
description = "Pull request URL to adopt."
required = true

[[steps]]
id = "review"
title = "Review PR"
`)

	server := New(state)
	req := httptest.NewRequest(http.MethodGet, "/v0/formulas?scope_kind=city&scope_ref=test-city&target=worker", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Items []formulaSummaryResponse `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode(catalog): %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %+v, want 1 entry", resp.Items)
	}
	item := resp.Items[0]
	if item.Name != "mol-adopt-pr-v2" {
		t.Fatalf("name = %q, want mol-adopt-pr-v2", item.Name)
	}
	if item.Description != "Review and fix a PR with a retry loop." {
		t.Fatalf("description = %q", item.Description)
	}
	if item.Version != "2" {
		t.Fatalf("version = %q, want 2", item.Version)
	}
	if len(item.VarDefs) != 1 || item.VarDefs[0].Name != "pr_url" || !item.VarDefs[0].Required {
		t.Fatalf("var_defs = %+v, want required pr_url", item.VarDefs)
	}
	if item.RunCount != 0 || len(item.RecentRuns) != 0 {
		t.Fatalf("run data = count %d runs %+v, want zeroed projection placeholders", item.RunCount, item.RecentRuns)
	}
}

func TestFormulaDetailReturnsCompiledPreview(t *testing.T) {
	state := newFakeState(t)
	formulaDir := t.TempDir()
	state.cfg.FormulaLayers.City = []string{formulaDir}

	writeTestFormula(t, formulaDir, "mol-preview", `
description = "Preview {{issue}}"
formula = "mol-preview"
version = 2

[vars]
[vars.issue]
description = "Issue bead ID"
required = true

[[steps]]
id = "prep"
title = "Prep {{issue}}"

[[steps]]
id = "review"
title = "Review {{issue}}"
needs = ["prep"]
metadata = { "gc.kind" = "run", "gc.scope_ref" = "body" }
`)

	server := New(state)
	req := httptest.NewRequest(http.MethodGet, "/v0/formulas/mol-preview?scope_kind=city&scope_ref=test-city&target=worker&var.issue=BD-123", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var detail formulaDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("Decode(detail): %v", err)
	}
	if detail.Name != "mol-preview" {
		t.Fatalf("name = %q, want mol-preview", detail.Name)
	}
	if detail.Description != "Preview BD-123" {
		t.Fatalf("description = %q, want substituted preview", detail.Description)
	}
	if len(detail.Steps) != 2 {
		t.Fatalf("steps = %+v, want 2 non-root steps", detail.Steps)
	}
	if detail.Steps[0]["title"] != "Prep BD-123" {
		t.Fatalf("step[0].title = %v, want substituted title", detail.Steps[0]["title"])
	}
	if len(detail.Deps) != 1 || detail.Deps[0].From != "mol-preview.prep" || detail.Deps[0].To != "mol-preview.review" {
		t.Fatalf("deps = %+v, want prep -> review", detail.Deps)
	}
	if len(detail.Preview.Nodes) != 2 {
		t.Fatalf("preview.nodes = %+v, want 2 nodes", detail.Preview.Nodes)
	}
	if detail.Preview.Nodes[1].Kind != "run" || detail.Preview.Nodes[1].ScopeRef != "body" {
		t.Fatalf("preview node = %+v, want run node with scope_ref", detail.Preview.Nodes[1])
	}
}

func TestFormulaDetailRequiresTarget(t *testing.T) {
	state := newFakeState(t)
	formulaDir := t.TempDir()
	state.cfg.FormulaLayers.City = []string{formulaDir}

	writeTestFormula(t, formulaDir, "mol-preview", `
description = "Preview"
formula = "mol-preview"
version = 2

[[steps]]
id = "prep"
title = "Prep"
`)

	server := New(state)
	req := httptest.NewRequest(http.MethodGet, "/v0/formulas/mol-preview?scope_kind=city&scope_ref=test-city", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func writeTestFormula(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name+".formula.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
