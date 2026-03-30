package tmux

import (
	"testing"
)

func TestParseApprovalPrompt_BashCommand(t *testing.T) {
	pane := `● Bash(bd list --assignee=$GC_AGENT --status=in_progress 2>&1)
  ⎿  Running…

────────────────────────────────────────────────────────────────────────────────
 Bash command

   bd list --assignee=$GC_AGENT --status=in_progress 2>&1
   Check for in-progress work (crash recovery)

 This command requires approval

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for: bd list:*
   3. No

 Esc to cancel · Tab to amend · ctrl+e to explain`

	a := parseApprovalPrompt(pane)
	if a == nil {
		t.Fatal("expected approval prompt, got nil")
	}
	if a.ToolName != "Bash" {
		t.Errorf("expected ToolName=Bash, got %q", a.ToolName)
	}
	if a.Input == "" {
		t.Error("expected non-empty Input")
	}
}

func TestParseApprovalPrompt_EditCommand(t *testing.T) {
	pane := `● Edit(file_path: /tmp/test.go)
  old_string: "foo"
  new_string: "bar"

────────────────────────────────────────────────────────────────────────────────
 Approve edits?

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for edits
   3. No`

	a := parseApprovalPrompt(pane)
	if a == nil {
		t.Fatal("expected approval prompt, got nil")
	}
	if a.ToolName != "Edit" {
		t.Errorf("expected ToolName=Edit, got %q", a.ToolName)
	}
}

func TestParseApprovalPrompt_NoPrompt(t *testing.T) {
	pane := `Just some regular output
$ echo hello
hello`

	a := parseApprovalPrompt(pane)
	if a != nil {
		t.Errorf("expected nil, got %+v", a)
	}
}

func TestParseApprovalPrompt_WriteCommand(t *testing.T) {
	pane := `● Write(file_path: /tmp/new.txt)

 This command requires approval

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for: Write:*
   3. No`

	a := parseApprovalPrompt(pane)
	if a == nil {
		t.Fatal("expected approval prompt, got nil")
	}
	if a.ToolName != "Write" {
		t.Errorf("expected ToolName=Write, got %q", a.ToolName)
	}
}

func TestParseApprovalPrompt_ToolWithParens(t *testing.T) {
	pane := `● Bash(curl -s https://example.com)

 This command requires approval

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for: Bash:curl*
   3. No`

	a := parseApprovalPrompt(pane)
	if a == nil {
		t.Fatal("expected approval prompt, got nil")
	}
	if a.ToolName != "Bash" {
		t.Errorf("expected ToolName=Bash, got %q", a.ToolName)
	}
	if a.Input != "curl -s https://example.com" {
		t.Errorf("expected input from parens, got %q", a.Input)
	}
}

func TestApprovalDedup(t *testing.T) {
	d := &approvalDedup{lastHash: make(map[string]string)}

	a := &parsedApproval{ToolName: "Bash", Input: "ls"}
	if !d.isNew("s1", a) {
		t.Error("first call should be new")
	}
	if d.isNew("s1", a) {
		t.Error("second call with same content should not be new")
	}

	b := &parsedApproval{ToolName: "Bash", Input: "pwd"}
	if !d.isNew("s1", b) {
		t.Error("different content should be new")
	}

	d.clear("s1")
	if !d.isNew("s1", a) {
		t.Error("after clear, should be new again")
	}
}

func TestExtractToolInput(t *testing.T) {
	pane := `● Bash
   bd list --assignee=$GC_AGENT --status=in_progress 2>&1
   Check for in-progress work (crash recovery)

 This command requires approval`

	input := extractToolInput(pane, "Bash")
	if input == "" {
		t.Error("expected non-empty input")
	}
	if !containsSubstring(input, "bd list") {
		t.Errorf("expected input to contain 'bd list', got %q", input)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && findSubstring(s, sub)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
