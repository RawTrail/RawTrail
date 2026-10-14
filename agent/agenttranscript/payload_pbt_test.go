package agenttranscript

import (
	"encoding/json"
	"testing"

	"pgregory.net/rapid"
)

// wirePayload is agent_tool_call_payload as schema/events.json defines it, decoded the way the server reads it.
type wirePayload struct {
	AgentSessionID string `json:"agent_session_id"`
	ToolUseID      string `json:"tool_use_id"`
	ToolName       string `json:"tool_name"`
	Command        string `json:"command"`
	Description    string `json:"description"`
	FilePath       string `json:"file_path"`
	CWD            string `json:"cwd"`
	Sidechain      bool   `json:"sidechain"`
	Truncated      bool   `json:"truncated"`
	UID            uint32 `json:"uid"`
}

// A tool call's payload survives the wire whole: every field, Unicode included, decodes back to what was sent, and a field left
// empty is absent rather than sent empty.
//
// spec:endpoint-event-collection/agent-tool-call-capture/a-bash-tool-call-is-reported-with-its-command
func TestToolCallPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		call := ToolCall{
			SessionID: rapid.StringN(1, 64, -1).Draw(t, "session"), ToolUseID: rapid.StringN(1, 64, -1).Draw(t, "id"),
			ToolName: rapid.StringN(1, 16, -1).Draw(t, "tool"), Command: rapid.String().Draw(t, "command"),
			Description: rapid.String().Draw(t, "description"), FilePath: rapid.String().Draw(t, "path"),
			CWD: rapid.String().Draw(t, "cwd"), Sidechain: rapid.Bool().Draw(t, "sidechain"), Truncated: rapid.Bool().Draw(t, "truncated"),
			OwnerUID: rapid.Uint32().Draw(t, "uid"),
		}
		payload := call.Payload()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got wirePayload
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		want := wirePayload{call.SessionID, call.ToolUseID, call.ToolName, call.Command, call.Description, call.FilePath, call.CWD,
			call.Sidechain, call.Truncated, call.OwnerUID}
		if got != want {
			t.Fatalf("round trip changed the call: got %+v, want %+v", got, want)
		}
		for key, value := range map[string]string{"command": call.Command, "description": call.Description, "file_path": call.FilePath,
			"cwd": call.CWD} {
			if _, present := payload[key]; present != (value != "") {
				t.Fatalf("%s present=%v for value %q", key, present, value)
			}
		}
		for key, value := range map[string]bool{"sidechain": call.Sidechain, "truncated": call.Truncated} {
			if _, present := payload[key]; present != value {
				t.Fatalf("%s present=%v for %v", key, present, value)
			}
		}
	})
}
