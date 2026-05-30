package api

import "encoding/json"

// outputTurn is the public transcript turn contract shared by agent output,
// session transcript, and session stream responses.
type outputTurn struct {
	Role      string        `json:"role"`
	Text      string        `json:"text"`
	Timestamp string        `json:"timestamp,omitempty"`
	Parts     []outputPart  `json:"parts,omitempty"`
	Assets    []outputAsset `json:"assets,omitempty"`
	Trace     []outputTrace `json:"trace,omitempty"`
}

// outputPart preserves structured provider blocks for dashboard rendering.
type outputPart struct {
	Type      string          `json:"type"`
	Kind      string          `json:"kind,omitempty"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Path      string          `json:"path,omitempty"`
	URL       string          `json:"url,omitempty"`
	Mime      string          `json:"mime,omitempty"`
	Source    string          `json:"source,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	State     string          `json:"state,omitempty"`
	Prompt    string          `json:"prompt,omitempty"`
	Options   []string        `json:"options,omitempty"`
	Action    string          `json:"action,omitempty"`
}

// outputAsset describes media that can be shown alongside a transcript turn.
type outputAsset struct {
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	URL    string `json:"url,omitempty"`
	Source string `json:"source,omitempty"`
}

// outputTrace carries auxiliary provider trace data, such as reasoning summaries.
type outputTrace struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}
