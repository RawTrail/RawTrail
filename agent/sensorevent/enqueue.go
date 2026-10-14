package sensorevent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"uuid"
)

// envelope is the on-the-wire shape the ingest handler expects. Duplicated locally rather than imported from the server,
// because the agent must not depend on server packages. Mirrors the same struct in agent/reconcile, which emits the other
// agent-synthesised events; `platform` is omitted there too and is not a required field in schema/events.json.
type envelope struct {
	EventID     string         `json:"event_id"`
	HostID      string         `json:"host_id"`
	TimestampNs int64          `json:"timestamp_ns"`
	EventType   string         `json:"event_type"`
	Payload     map[string]any `json:"payload"`
}

// Marshal builds one agent-synthesised event's wire envelope, for an event that carries an id and a time of its own rather than a
// fresh id and the time it is enqueued.
func Marshal(eventID, hostID, eventType string, timestampNs int64, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(envelope{
		EventID: eventID, HostID: hostID, TimestampNs: timestampNs, EventType: eventType, Payload: payload,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", eventType, err)
	}
	return body, nil
}

// NewEnqueueEmitter adapts the agent's queue to the Emitter seam.
//
// hostID is read per event rather than captured, because the value can change: the agent derives one before enrollment and
// adopts the server's afterwards, and an event carrying the pre-enrollment id would be attributed to a host that does not
// exist. nowNs is injected so tests pin timestamps.
func NewEnqueueEmitter(hostID func() string, enqueue func(context.Context, []byte) error, nowNs func() int64) Emitter {
	return func(ctx context.Context, eventType string, payload map[string]any) error {
		host := hostID()
		if host == "" {
			// Enrollment has not completed. Dropping is right rather than emitting with an empty host_id: an event nothing
			// can be attributed to is not evidence, and the extension re-publishes liveness on every handshake, so the
			// state is re-observed once enrollment lands.
			return errors.New("no host id yet")
		}
		body, err := Marshal(uuid.New().String(), host, eventType, nowNs(), payload)
		if err != nil {
			return err
		}
		return enqueue(ctx, body)
	}
}
