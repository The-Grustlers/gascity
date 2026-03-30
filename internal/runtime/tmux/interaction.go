package tmux

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gastownhall/gascity/internal/runtime"
)

// Compile-time check that Tmux implements InteractionProvider.
var _ runtime.InteractionProvider = (*Tmux)(nil)

// ---------------------------------------------------------------------------
// Pane-based approval detection
// ---------------------------------------------------------------------------

// approvalPatterns detect Claude Code's interactive prompts in tmux pane output.
var (
	// "This command requires approval" or "Approve edits?" patterns
	requiresApprovalRe = regexp.MustCompile(`(?m)(This command requires approval|Approve edits\?)`)

	// "Do you want to proceed?" with numbered options
	proceedRe = regexp.MustCompile(`(?m)Do you want to proceed\?`)

	// Tool call header: "● ToolName(args)" or "● ToolName"
	toolHeaderRe = regexp.MustCompile(`(?m)● (\w+)(?:\(([^)]*)\))?`)
)

// parsedApproval holds the parsed approval prompt from a tmux pane capture.
type parsedApproval struct {
	ToolName string
	Input    string
	RawText  string
}

// parseApprovalPrompt parses the tmux pane text for a Claude Code approval prompt.
// Returns nil if no approval prompt is found.
func parseApprovalPrompt(paneText string) *parsedApproval {
	if !requiresApprovalRe.MatchString(paneText) && !proceedRe.MatchString(paneText) {
		return nil
	}

	approval := &parsedApproval{RawText: paneText}

	// Extract tool name from the "● ToolName(args)" line
	if m := toolHeaderRe.FindStringSubmatch(paneText); len(m) >= 2 {
		approval.ToolName = m[1]
		if len(m) >= 3 && m[2] != "" {
			approval.Input = m[2]
		}
	}

	// Try to extract the command/content shown between the tool header and approval prompt.
	// Claude shows it indented under the tool header.
	if approval.ToolName != "" && approval.Input == "" {
		approval.Input = extractToolInput(paneText, approval.ToolName)
	}

	return approval
}

// extractToolInput extracts the indented tool input block from pane text.
// Claude shows tool input as indented lines between the "● ToolName" header
// and the "This command requires approval" / "Do you want to proceed?" line.
func extractToolInput(paneText, toolName string) string {
	lines := strings.Split(paneText, "\n")
	var capturing bool
	var captured []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "● "+toolName) {
			capturing = true
			continue
		}
		if capturing {
			if trimmed == "" || strings.Contains(trimmed, "requires approval") ||
				strings.Contains(trimmed, "Do you want to proceed") ||
				strings.Contains(trimmed, "Approve edits") {
				break
			}
			// Skip UI decoration lines (spinners, box-drawing, etc.)
			if strings.HasPrefix(trimmed, "⎿") || strings.HasPrefix(trimmed, "───") ||
				strings.HasPrefix(trimmed, "│") || trimmed == "Running…" {
				continue
			}
			// Claude indents tool input with leading spaces
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
				captured = append(captured, strings.TrimSpace(line))
			}
		}
	}

	if len(captured) == 0 {
		return ""
	}
	result := strings.Join(captured, "\n")
	// Truncate very long inputs
	if len(result) > 500 {
		result = result[:500] + "…"
	}
	return result
}

// ---------------------------------------------------------------------------
// Deduplication
// ---------------------------------------------------------------------------

// Per-session dedup state to avoid re-emitting the same approval.
type approvalDedup struct {
	mu       sync.Mutex
	lastHash map[string]string // session name → hash of last emitted approval
}

var dedup = &approvalDedup{lastHash: make(map[string]string)}

func approvalHash(a *parsedApproval) string {
	h := sha256.Sum256([]byte(a.ToolName + "\x00" + a.Input))
	return fmt.Sprintf("%x", h[:8])
}

func (d *approvalDedup) isNew(session string, a *parsedApproval) bool {
	hash := approvalHash(a)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastHash[session] == hash {
		return false
	}
	d.lastHash[session] = hash
	return true
}

func (d *approvalDedup) clear(session string) {
	d.mu.Lock()
	delete(d.lastHash, session)
	d.mu.Unlock()
}

// ---------------------------------------------------------------------------
// InteractionProvider implementation
// ---------------------------------------------------------------------------

// Pending checks the tmux pane for an active Claude Code approval prompt.
// Returns nil with no error if no approval is pending.
func (t *Tmux) Pending(name string) (*runtime.PendingInteraction, error) {
	paneText, err := t.CapturePane(name, 40)
	if err != nil {
		// Pane might not exist (session not started yet or already stopped).
		return nil, nil
	}

	approval := parseApprovalPrompt(paneText)
	if approval == nil {
		dedup.clear(name)
		return nil, nil
	}

	// Build a stable request ID from the approval content so the same
	// prompt always produces the same ID (idempotent for retries).
	requestID := "tmux-" + approvalHash(approval)

	prompt := "Allow " + approval.ToolName + "?"
	if approval.Input != "" {
		prompt = approval.ToolName + ": " + approval.Input
	}

	return &runtime.PendingInteraction{
		RequestID: requestID,
		Kind:      "approval",
		Prompt:    prompt,
		Options:   []string{"Yes", "Yes, and don't ask again", "No"},
		Metadata: map[string]string{
			"tool_name": approval.ToolName,
			"source":    "tmux",
		},
	}, nil
}

const (
	respondRetries  = 3
	respondVerifyMs = 400
	respondRetryMs  = 200
)

// Respond sends the appropriate keystroke to the tmux pane to approve or deny
// a pending tool approval, then verifies the prompt was consumed.
func (t *Tmux) Respond(name string, response runtime.InteractionResponse) error {
	// Map action to keystroke. Claude's prompt shows:
	// 1. Yes
	// 2. Yes, and don't ask again for: <tool>
	// 3. No
	var key string
	switch response.Action {
	case "approve":
		key = "1"
	case "approve_accept_edits", "approve_always":
		key = "2"
	case "deny":
		key = "3"
	default:
		return fmt.Errorf("unknown action %q", response.Action)
	}

	// Send keystroke with retries — the pane may not process it immediately.
	for attempt := range respondRetries {
		// Send the number key (literal)
		if _, err := t.run("send-keys", "-t", name, "-l", key); err != nil {
			return fmt.Errorf("send-keys failed: %w", err)
		}

		// Wait for Claude to process
		time.Sleep(time.Duration(respondVerifyMs) * time.Millisecond)

		// Verify the approval prompt is gone
		paneText, err := t.CapturePane(name, 40)
		if err != nil {
			// Pane gone — session ended, treat as success
			dedup.clear(name)
			return nil
		}

		if parseApprovalPrompt(paneText) == nil {
			// Prompt cleared — success
			dedup.clear(name)
			return nil
		}

		// Prompt still there — retry with increasing delay
		if attempt < respondRetries-1 {
			time.Sleep(time.Duration(respondRetryMs*(attempt+1)) * time.Millisecond)
		}
	}

	return fmt.Errorf("approval prompt did not clear after %d retries", respondRetries)
}
