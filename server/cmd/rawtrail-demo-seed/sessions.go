package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/rawtrail/rawtrail/test/fakeagent"
)

// sessionTailOffset is how far before "now" a host's woven agent sessions end: behind the captured tail and the woven attacks
// (recentTailOffset plus a few seconds), so the sessions read as the work that came before them rather than interleaving with them.
const sessionTailOffset = 2 * time.Minute

// sessionTimeFields are the payload fields that carry a device-clock time of their own, besides the envelope's timestamp_ns: an
// exec's process start and a file activity record's last fold. They move with the envelope, or a session's processes would start
// on the day of the capture while their events read as recent.
var sessionTimeFields = []string{"start_time_ns", "last_timestamp_ns"}

// sessionParentFields pair each payload field naming another process with the field holding that process's generation, for the
// fields a woven session re-points when the process they name is outside the capture.
var sessionParentFields = []struct{ pid, version string }{
	{pid: "parent_pid", version: "parent_pidversion"},
	{pid: "ppid", version: "parent_pidversion"},
	{pid: "original_ppid", version: ""},
	{pid: "responsible_pid", version: "responsible_pidversion"},
}

// wovenSessions is a capture of real AI agent sessions woven onto a captured host: the processes each session ran with their
// file, network and DNS activity, the agent's prompts, tool calls and token usage, and any credential reads the guard refused.
// Unlike the host captures it keeps the kernel's own pidversions, which its file activity and credential blocks are keyed on too,
// so it is never re-stamped (see weaveSessions).
type wovenSessions struct {
	File       string // under corpus/sessions/
	ExpectRule string // the catalog rule its refused credential reads should trip; verify asserts it fired
}

// loadSessionEnvelopes reads a session capture. Its envelopes carry the host_id of the host they are woven onto.
func loadSessionEnvelopes(file string) ([]fakeagent.Envelope, string, error) {
	raw, err := fs.ReadFile(hostCorpusFS, "corpus/sessions/"+file)
	if err != nil {
		return nil, "", fmt.Errorf("read session corpus %s: %w", file, err)
	}
	return decodeHostEnvelopes(raw, file)
}

// weaveSessions posts a host's captured agent sessions after its ambient stream: shifted to end sessionTailOffset before now, with
// the processes that started each session (outside the capture) replaced by the host's shell, so a session hangs off the host's
// genuine tree. Connections and refused credential reads wait for their process, as a woven attack's connections do.
func (s *seeder) weaveSessions(
	ctx context.Context, hostID, token string, sessions wovenSessions, now time.Time, anchorPID int, stamper *pidVersionStamper,
) error {
	// The capture already carries hostID: the scrub stamps the host it is woven onto, and TestSessionCaptures holds it to that.
	envs, _, err := loadSessionEnvelopes(sessions.File)
	if err != nil {
		return err
	}
	if err := reparentSessionsToHost(envs, anchorPID, stamper.current[anchorPID]); err != nil {
		return fmt.Errorf("re-parent %s: %w", sessions.File, err)
	}
	if err := shiftSessionEnvelopes(envs, now.Add(-sessionTailOffset)); err != nil {
		return fmt.Errorf("shift %s: %w", sessions.File, err)
	}
	if err := s.postWovenEnvelopes(ctx, hostID, token, sessions.File, envs); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "wove agent sessions onto host", "file", sessions.File, "host_id", hostID, "events", len(envs))
	return nil
}

// reparentSessionsToHost re-points every reference to a process outside the capture (the shell that started claude, and the SSH
// session behind it) at the host's anchor shell and that shell's generation. A process is in the capture when the capture forks or
// execs it. launchd and the kernel (pids <= 1) stay as they are: a process adopted by launchd is genuinely its child.
func reparentSessionsToHost(envs []fakeagent.Envelope, anchorPID int, anchorVersion int64) error {
	captured := map[int]bool{}
	payloads := make([]map[string]any, len(envs))
	for i := range envs {
		payload, err := decodePayload(envs[i].Payload)
		if err != nil {
			return fmt.Errorf("%s %s: %w", envs[i].EventType, envs[i].EventID, err)
		}
		payloads[i] = payload
		if pid, ok := intFrom(payload[processField(envs[i].EventType)]); ok {
			captured[pid] = true
		}
	}
	for i, payload := range payloads {
		changed := false
		for _, f := range sessionParentFields {
			pid, ok := intFrom(payload[f.pid])
			if !ok || pid <= 1 || captured[pid] {
				continue
			}
			payload[f.pid] = anchorPID
			if f.version != "" {
				payload[f.version] = anchorVersion
			}
			changed = true
		}
		if !changed {
			continue
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("re-encode %s: %w", envs[i].EventID, err)
		}
		envs[i].Payload = raw
	}
	return nil
}

// processField names the payload field holding the process an event creates: the child of a fork, the process an exec images.
func processField(eventType string) string {
	if eventType == "fork" {
		return "child_pid"
	}
	if eventType == "exec" {
		return "pid"
	}
	return ""
}

// shiftSessionEnvelopes moves a session capture so its latest event lands at end, keeping every gap between its events. The
// device-clock fields inside a payload (sessionTimeFields) move by the same amount. The envelopes are left sorted by time, the
// order the agent sent them in.
func shiftSessionEnvelopes(envs []fakeagent.Envelope, end time.Time) error {
	sort.SliceStable(envs, func(a, b int) bool { return envs[a].TimestampNs < envs[b].TimestampNs })
	delta := end.UnixNano() - envs[len(envs)-1].TimestampNs
	for i := range envs {
		envs[i].TimestampNs += delta
		payload, err := decodePayload(envs[i].Payload)
		if err != nil {
			return fmt.Errorf("%s %s: %w", envs[i].EventType, envs[i].EventID, err)
		}
		changed := false
		for _, f := range sessionTimeFields {
			num, ok := payload[f].(json.Number)
			if !ok {
				continue
			}
			v, err := num.Int64()
			if err != nil {
				return fmt.Errorf("%s %s: %s: %w", envs[i].EventType, envs[i].EventID, f, err)
			}
			payload[f] = v + delta
			changed = true
		}
		if !changed {
			continue
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("re-encode %s: %w", envs[i].EventID, err)
		}
		envs[i].Payload = raw
	}
	return nil
}

// sessionIDs returns the distinct agent session ids a session capture's prompts belong to, which verify expects to find.
func sessionIDs(envs []fakeagent.Envelope) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, e := range envs {
		if e.EventType != "agent_prompt" {
			continue
		}
		var p struct {
			SessionID string `json:"agent_session_id"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return nil, fmt.Errorf("agent_prompt %s: %w", e.EventID, err)
		}
		ids[p.SessionID] = true
	}
	return ids, nil
}
