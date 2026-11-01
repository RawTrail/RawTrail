// Package agenttranscript reads the tool calls an AI agent made, and the prompts its user typed, from the session transcripts it keeps
// on disk (issues #1288, #1426), so the server can show each Bash tool call beside the processes it started, under the prompt that led
// to it. Claude Code writes one JSONL transcript per session at ~/.claude/projects/<project>/<session id>.jsonl. The format is
// undocumented, so the parser is strict: a line it cannot read is counted and skipped, never guessed at.
//
// What leaves the host: tool-call metadata (the tool's name and id, a Bash call's command and description, a file tool's path) and,
// unless prompt capture is off, the text of each prompt the user typed, with known secrets redacted and cut to a bound. Model output
// and tool results are never read into an event. A Bash command is already on the wire in the exec of the shell that runs it.
package agenttranscript

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

// EventType is the wire event type of one tool call.
const EventType = "agent_tool_call"

// PromptEventType is the wire event type of one prompt the user typed.
const PromptEventType = "agent_prompt"

// UsageEventType is the event type of the tokens one assistant message used (issue #1427).
const UsageEventType = "agent_usage"

// sdkPromptSource is the prompt source Claude Code records for a prompt passed on the command line (claude -p), which records no origin.
const sdkPromptSource = "sdk"

// maxPromptBytes caps a prompt's text on the wire. A prompt can hold a pasted file; the first 16 KiB say what was asked.
const maxPromptBytes = 16 << 10

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
	// PromptID names the turn the call was made in (issue #1426): the prompt that began it, or the agent's own notification. The reader
	// sets it from the user entry before the call.
	PromptID string
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
	return eventID(EventType, c.OwnerUID, c.SessionID, c.ToolUseID)
}

// eventID is a version 8 UUID over an event type, the transcript's owner, the session and the entry's id within it.
func eventID(eventType string, owner uint32, session, entry string) string {
	sum := sha256.Sum256([]byte(eventType + "\x00" + strconv.FormatUint(uint64(owner), 10) + "\x00" + session + "\x00" + entry))
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
		"origin_session_id": c.OriginSessionID, "forked_from_session_id": c.ForkedFromSessionID, "prompt_id": c.PromptID} {
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

// Prompt is one prompt the user gave an agent, typed or passed on the command line, as its transcript records it.
type Prompt struct {
	SessionID string
	// EntryID is the transcript entry's own id, which names the prompt however many times it is read.
	EntryID string
	// PromptID names the turn the prompt began; the tool calls the agent made for it carry the same id.
	PromptID string
	// Text is what the user typed or passed, known secrets redacted and cut to maxPromptBytes; a slash command reads as "/name args".
	Text string
	// Length is the prompt's length in bytes as given, before redaction and the cut.
	Length int
	// Truncated reports that Text was cut; Redacted counts the secrets replaced in it.
	Truncated bool
	Redacted  int
	// Source is how the prompt was entered, as the agent records it: typed, queued while the agent worked, a suggestion accepted, or
	// sdk for one passed on the command line.
	Source string
	CWD    string
	At     time.Time
	// OwnerUID is the uid of the transcript's owner, as on a tool call.
	OwnerUID uint32
}

// EventID is the prompt's event id, the same however many times it is read, derived as a tool call's is.
func (p Prompt) EventID() string {
	return eventID(PromptEventType, p.OwnerUID, p.SessionID, p.EntryID)
}

// Payload is the prompt's wire payload; empty fields are left out.
func (p Prompt) Payload() map[string]any {
	out := map[string]any{"agent_session_id": p.SessionID, "prompt_id": p.PromptID, "text": p.Text, "length": p.Length, "uid": p.OwnerUID}
	for key, value := range map[string]string{"source": p.Source, "cwd": p.CWD} {
		if value != "" {
			out[key] = value
		}
	}
	if p.Truncated {
		out["truncated"] = true
	}
	if p.Redacted > 0 {
		out["redacted"] = p.Redacted
	}
	return out
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

// transcriptLine is the part of a transcript entry a tool call or a prompt is read from. Entries of other types (attachments,
// bookkeeping) decode into it too and are recognized by Type.
type transcriptLine struct {
	Type          string `json:"type"`
	UUID          string `json:"uuid"`
	SessionID     string `json:"sessionId"`
	OriginSession string `json:"session_id"`
	PromptID      string `json:"promptId"`
	Timestamp     string `json:"timestamp"`
	CWD           string `json:"cwd"`
	IsSidechain   bool   `json:"isSidechain"`
	// IsMeta marks a turn the agent wrote into the conversation itself (a skill's instructions, a command's caveat).
	IsMeta bool `json:"isMeta"`
	// IsCompactSummary marks the history the agent wrote in place of a compacted conversation.
	IsCompactSummary bool `json:"isCompactSummary"`
	// Origin says who a user turn came from: a person typing ("human"), or the agent's own notifications and peers.
	Origin struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	PromptSource string `json:"promptSource"`
	// PermissionMode is the permission mode a user entry, or a permission-mode entry, records as in force from then on.
	PermissionMode string `json:"permissionMode"`
	Message        struct {
		Content json.RawMessage `json:"content"`
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Usage   *struct {
			Input         int64 `json:"input_tokens"`
			Output        int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			Details       *struct {
				Thinking int64 `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
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

// Line is what one transcript line records: the tool calls of an assistant turn, or the prompt of a user turn a person typed, and the
// turn a user entry belongs to. Claude Code names the turn on its user entries (the prompt that began it, and each tool result in it)
// and not on the assistant's, so a reader takes a call's turn from the user entry before it.
type Line struct {
	Calls  []ToolCall
	Prompt *Prompt
	Turn   string
	// TurnSession is the session the user entry that names Turn belongs to, so a reader takes the turn only from its own session.
	TurnSession string
	// Usage is the tokens an assistant message used, and Mode the permission mode an entry records as in force (issue #1427), named
	// for ModeSession's session.
	Usage       *Usage
	Mode        string
	ModeSession string
}

// syntheticModel is the model Claude Code names on a message it wrote itself (an error, an interrupted turn), which used no tokens.
const syntheticModel = "<synthetic>"

// Usage is the tokens one assistant message used (issue #1427), as its transcript records them. Claude Code writes one entry per
// content block of a streamed message, each carrying the whole message's usage at its own time, so the reader sends a message's first
// entry only, and its event id is the same each time the transcript is read.
type Usage struct {
	SessionID string
	MessageID string
	Model     string
	// Input, Output, CacheCreation and CacheRead are the message's input and output tokens and the input tokens written to and read
	// from the prompt cache; Thinking is the part of Output spent thinking.
	Input, Output, CacheCreation, CacheRead, Thinking int64
	Sidechain                                         bool
	At                                                time.Time
	// PromptID is the turn the message was made in, and PermissionMode the mode in force then, both from the reader's position.
	PromptID       string
	PermissionMode string
	OwnerUID       uint32
	// OriginSessionID and ForkedFromSessionID name the session a line may have been copied from, as on a tool call. A line of a
	// session forked from another, or of one /clear started, names the first session either way, so the server tells a copy apart
	// by whether that session holds the same message, as it does for tool calls.
	OriginSessionID     string
	ForkedFromSessionID string
}

// EventID is the message's event id, the same however many of its entries are read.
func (u Usage) EventID() string {
	return eventID(UsageEventType, u.OwnerUID, u.SessionID, u.MessageID)
}

// Payload is the usage's wire payload; empty fields are left out.
func (u Usage) Payload() map[string]any {
	out := map[string]any{
		"agent_session_id": u.SessionID, "message_id": u.MessageID, "model": u.Model, "input_tokens": u.Input,
		"output_tokens": u.Output, "cache_creation_input_tokens": u.CacheCreation, "cache_read_input_tokens": u.CacheRead,
		"thinking_tokens": u.Thinking, "uid": u.OwnerUID,
	}
	for key, value := range map[string]string{"prompt_id": u.PromptID, "permission_mode": u.PermissionMode,
		"origin_session_id": u.OriginSessionID, "forked_from_session_id": u.ForkedFromSessionID} {
		if value != "" {
			out[key] = value
		}
	}
	if u.Sidechain {
		out["sidechain"] = true
	}
	return out
}

// ParseLine returns what one transcript line records: one tool call per tool_use block of an assistant turn, or the prompt of a user
// turn a person typed, and nothing for any other entry. It fails for a line that is not JSON, and for a tool call or prompt that
// lacks its session, id or time, which a guess would attach to the wrong session.
func ParseLine(raw []byte) (Line, error) {
	var line transcriptLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return Line{}, errUnreadable
	}
	switch line.Type {
	case "assistant":
		calls, err := parseCalls(line)
		if err != nil {
			return Line{}, err
		}
		usage, err := parseUsage(line)
		return Line{Calls: calls, Usage: usage}, err
	case "user":
		prompt, err := parsePrompt(line)
		return Line{Prompt: prompt, Turn: line.PromptID, TurnSession: line.SessionID, Mode: line.PermissionMode,
			ModeSession: line.SessionID}, err
	case "permission-mode":
		return Line{Mode: line.PermissionMode, ModeSession: line.SessionID}, nil
	}
	return Line{}, nil
}

// parseUsage reads the tokens an assistant message used. A message Claude Code wrote itself used none. A line naming another session
// is sent with that session named: it may be a fork's copy, which the first session already counts, or a message of a session /clear
// started, which names its first session too, and only the server can tell the two apart.
func parseUsage(line transcriptLine) (*Usage, error) {
	u := line.Message.Usage
	if u == nil || line.Message.Model == syntheticModel {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339Nano, line.Timestamp)
	if err != nil || line.SessionID == "" || line.Message.ID == "" {
		return nil, errUnreadable
	}
	usage := &Usage{
		SessionID: line.SessionID, MessageID: line.Message.ID, Model: line.Message.Model, Input: u.Input, Output: u.Output,
		CacheCreation: u.CacheCreation, CacheRead: u.CacheRead, Sidechain: line.IsSidechain, At: at,
	}
	if line.OriginSession != line.SessionID {
		usage.OriginSessionID = line.OriginSession
	}
	if u.Details != nil {
		usage.Thinking = u.Details.Thinking
	}
	return usage, nil
}

// parseCalls reads an assistant turn's tool calls.
func parseCalls(line transcriptLine) ([]ToolCall, error) {
	if len(line.Message.Content) == 0 || line.Message.Content[0] != '[' {
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
		if line.OriginSession != line.SessionID {
			call.OriginSessionID = line.OriginSession
		}
		calls = append(calls, call)
	}
	return calls, nil
}

// parsePrompt reads the prompt of a user turn a person gave the agent: typed (its origin is human), or passed on the command line with
// claude -p, which records no origin and the prompt source "sdk"; not one the agent wrote itself; and its content the text as given.
// A tool's result, the agent's own notifications, a compacted history, a subagent's instructions (a sidechain, which the agent writes)
// and a turn recording neither origin nor source, as an older agent's and a /clear do, are not prompts.
func parsePrompt(line transcriptLine) (*Prompt, error) {
	given := line.Origin.Kind == "human" || line.Origin.Kind == "" && line.PromptSource == sdkPromptSource
	if !given || line.IsMeta || line.IsSidechain || line.IsCompactSummary || len(line.Message.Content) == 0 || line.Message.Content[0] != '"' {
		return nil, nil
	}
	var typed string
	if err := json.Unmarshal(line.Message.Content, &typed); err != nil {
		return nil, errUnreadable
	}
	at, err := time.Parse(time.RFC3339Nano, line.Timestamp)
	if err != nil || line.SessionID == "" || line.UUID == "" {
		return nil, errUnreadable
	}
	text, redacted := Redact(slashCommand(typed))
	text, truncated := capText(text, maxPromptBytes)
	return &Prompt{
		SessionID: line.SessionID, EntryID: line.UUID, PromptID: line.PromptID, Text: text, Length: len(typed), Truncated: truncated,
		Redacted: redacted, Source: line.PromptSource, CWD: line.CWD, At: at,
	}, nil
}

// commandEnvelope is how the transcript records a slash command the user typed: nothing but its <command-message>, <command-name>
// and <command-args> tags. Text that only mentions the tags is a prompt like any other.
var commandEnvelope = regexp.MustCompile(`^(?:\s*<command-(?:message|name|args)>[\s\S]*?</command-(?:message|name|args)>)+\s*$`)

// slashCommand reads a slash command the user typed, recorded as its tags (<command-name>/x</command-name> and its
// <command-args>), as "/x args". Any other text, the tags inside a prompt included, is returned as it is.
func slashCommand(typed string) string {
	if !commandEnvelope.MatchString(typed) {
		return typed
	}
	name, ok := between(typed, "<command-name>", "</command-name>")
	if !ok {
		return typed
	}
	args, _ := between(typed, "<command-args>", "</command-args>")
	return strings.TrimSpace(name + " " + strings.TrimSpace(args))
}

// between returns the text between the first open tag and the close tag after it.
func between(s, open, closing string) (string, bool) {
	_, after, ok := strings.Cut(s, open)
	if !ok {
		return "", false
	}
	inner, _, ok := strings.Cut(after, closing)
	return inner, ok
}

// capCommand cuts command to maxCommandBytes on a rune boundary.
func capCommand(command string) (string, bool) {
	return capText(command, maxCommandBytes)
}

// capText cuts text to limit bytes on a rune boundary.
func capText(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}
