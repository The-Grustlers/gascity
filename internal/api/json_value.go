package api

import (
	"encoding/json"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
)

// JSONValue is an arbitrary valid JSON value on the API wire.
//
// It preserves provider/tool payloads as structured JSON while giving OpenAPI
// generators a named recursive schema instead of an unconstrained unknown.
type JSONValue struct {
	Raw json.RawMessage `json:"-"`
}

func outputJSONFromRaw(raw json.RawMessage) *JSONValue {
	if len(raw) == 0 {
		return nil
	}
	return &JSONValue{Raw: append(json.RawMessage(nil), raw...)}
}

// MarshalJSON emits the stored JSON value verbatim.
func (v JSONValue) MarshalJSON() ([]byte, error) {
	if len(v.Raw) == 0 {
		return []byte("null"), nil
	}
	if !json.Valid(v.Raw) {
		return nil, fmt.Errorf("invalid JSON value")
	}
	return append([]byte(nil), v.Raw...), nil
}

// UnmarshalJSON stores the source JSON bytes so round-trips preserve structure.
func (v *JSONValue) UnmarshalJSON(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON value")
	}
	v.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// Schema registers and references the recursive JsonValue schema.
func (JSONValue) Schema(r huma.Registry) *huma.Schema {
	const name = "JsonValue"
	if _, ok := r.Map()[name]; !ok {
		ref := &huma.Schema{Ref: schemaRefPrefix + name}
		r.Map()[name] = &huma.Schema{
			Title:       "JSON value",
			Description: "Any valid JSON value: object, array, string, number, boolean, or null.",
			OneOf: []*huma.Schema{
				{Type: huma.TypeObject, AdditionalProperties: ref},
				{Type: huma.TypeArray, Items: ref},
				{Type: huma.TypeString, Nullable: true},
				{Type: huma.TypeNumber},
				{Type: huma.TypeBoolean},
			},
		}
	}
	return &huma.Schema{Ref: schemaRefPrefix + name}
}

func (outputPartType) Schema(r huma.Registry) *huma.Schema {
	return registerNamedEnum(r, "OutputPartType",
		"Kind of part within a unified transcript turn.",
		string(outputPartTypeText),
		string(outputPartTypeReasoning),
		string(outputPartTypeToolUse),
		string(outputPartTypeToolResult),
		string(outputPartTypeInteraction),
		string(outputPartTypeFile),
	)
}
