// Package agenttranscript reads the tool calls an AI agent made from the session transcripts it keeps on disk (issue #1288), so the
// server can show each Bash tool call beside the processes it started. Claude Code writes one JSONL transcript per session at
// ~/.claude/projects/<project>/<session id>.jsonl. The format is undocumented, so the parser is strict: a line it cannot read is
// counted and skipped, never guessed at.
//
// Only tool-call metadata leaves the host: the tool's name and id, a Bash call's command and description, a file tool's path, the
// session, the working directory and the time. Prompt text, model output and tool results are never read into an event. A Bash
// command is already on the wire in the exec of the shell that runs it.
package agenttranscript

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"
	"uuid"
)

// EventType is the wire event type of one tool call.
const EventType = "agent_tool_call"

// maxCommandBytes caps a Bash command on the wire. A heredoc can make a command arbitrarily long; the exec of its shell carries it
// whole already, so the cut copy only needs to be enough to label and match the call.
const maxCommandBytes = 8 << 10

// ToolCall is one tool call an agent made, as its transcript records it.
type ToolCall struct {
	SessionID   string
	ToolUseID   string
	ToolName    string
	Command     string
	Description string
	FilePath    string
	CWD         string
	// OriginSessionID is the line's own session_id where it differs from SessionID. A session forked from another (Claude Code's
	// /fork) begins with a copy of its parent's history, and each copied line keeps the session_id it was written under, so a copied
	// call names the session it came from. A process that switched sessions (/clear) also keeps writing its first session's id, so
	// the field alone does not make a call a copy: the server counts it as one only where that session holds the same call.
	OriginSessionID string
	// ForkedFromSessionID is the session this call's session was forked from, as its /fork job names it (issue #1354): direct
	// evidence of the immediate parent, where OriginSessionID only names the process a copied line was first written by.
	ForkedFromSessionID string
	// Sidechain marks a call a subagent made inside the session.
	Sidechain bool
	// Truncated reports that Command was cut to maxCommandBytes.
	Truncated bool
	At        time.Time
	// OwnerUID is the uid of the transcript's owner: the user the call was read for. Transcripts are their users' to write, so it is
	// what keeps one user's lines from standing in for another's.
	OwnerUID uint32
}

// EventID is the call's event id, the same however many times the call is read: the archive keeps one event per id, so a transcript
// read again after a restart adds nothing. A version 8 UUID over the transcript's owner, the session and the tool_use_id. The owner is
// in it because the rest is text the user wrote: without it, a line another user forged with this session's id and a call's id would
// take the real call's event id, and the archive would keep whichever came first.
func (c ToolCall) EventID() string {
	sum := sha256.Sum256([]byte(EventType + "\x00" + strconv.FormatUint(uint64(c.OwnerUID), 10) + "\x00" + c.SessionID + "\x00" +
		c.ToolUseID))
	var id uuid.UUID
	copy(id[:], sum[:len(id)])
	id[6] = id[6]&uuidLowNibble | uuidVersion8
	id[8] = id[8]&uuidVariantBits | uuidVariantRFC
	return id.String()
}

// Payload is the call's wire payload; empty fields are left out.
func (c ToolCall) Payload() map[string]any {
	p := map[string]any{"agent_session_id": c.SessionID, "tool_use_id": c.ToolUseID, "tool_name": c.ToolName, "uid": c.OwnerUID}
	for key, value := range map[string]string{"command": c.Command, "description": c.Description, "file_path": c.FilePath, "cwd": c.CWD,
		"origin_session_id": c.OriginSessionID, "forked_from_session_id": c.ForkedFromSessionID} {
		if value != "" {
			p[key] = value
		}
	}
	if c.Sidechain {
		p["sidechain"] = true
	}
	if c.Truncated {
		p["truncated"] = true
	}
	return p
}

// The bits RFC 9562 fixes in a UUID: byte 6's high nibble holds the version, 8 for a vendor-specific UUID (section 5.8), and byte 8's
// two high bits the variant, 0b10.
const (
	uuidLowNibble   = 0x0f
	uuidVersion8    = 0x80
	uuidVariantBits = 0x3f
	uuidVariantRFC  = 0x80
)

// errUnreadable is a line that is not a transcript entry this parser understands.
var errUnreadable = errors.New("unreadable transcript line")

// transcriptLine is the part of a transcript entry a tool call is read from. Entries of other types (user turns, attachments,
// bookkeeping) decode into it too and are recognized by Type.
type transcriptLine struct {
	Type        string `json:"type"`
	SessionID   string `json:"sessionId"`
	Origin      string `json:"session_id"`
	Timestamp   string `json:"timestamp"`
	CWD         string `json:"cwd"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		FilePath    string `json:"file_path"`
	} `json:"input"`
}

// ParseLine returns the tool calls one transcript line records: none for an entry that is not an assistant turn, or one per tool_use
// block of an assistant turn. It fails for a line that is not JSON, and for an assistant turn whose tool call lacks its session, id,
// name or time, which a guess would attach to the wrong session or call.
func ParseLine(raw []byte) ([]ToolCall, error) {
	var line transcriptLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return nil, errUnreadable
	}
	if line.Type != "assistant" || len(line.Message.Content) == 0 || line.Message.Content[0] != '[' {
		// A text-only turn carries its content as a string; only the block form holds tool calls.
		return nil, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(line.Message.Content, &blocks); err != nil {
		return nil, errUnreadable
	}
	var calls []ToolCall
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, line.Timestamp)
		if err != nil || line.SessionID == "" || b.ID == "" || b.Name == "" {
			return nil, errUnreadable
		}
		command, truncated := capCommand(b.Input.Command)
		call := ToolCall{
			SessionID: line.SessionID, ToolUseID: b.ID, ToolName: b.Name, Command: command, Description: b.Input.Description,
			FilePath: b.Input.FilePath, CWD: line.CWD, Sidechain: line.IsSidechain, Truncated: truncated, At: at,
		}
		if line.Origin != line.SessionID {
			call.OriginSessionID = line.Origin
		}
		calls = append(calls, call)
	}
	return calls, nil
}

// capCommand cuts command to maxCommandBytes on a rune boundary.
func capCommand(command string) (string, bool) {
	if len(command) <= maxCommandBytes {
		return command, false
	}
	cut := maxCommandBytes
	for cut > 0 && !utf8.RuneStart(command[cut]) {
		cut--
	}
	return command[:cut], true
}
