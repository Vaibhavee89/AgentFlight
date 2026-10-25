package recorder

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CurrentVersion is the only event schema version this build understands.
const CurrentVersion = 1

// EventType names the kind of occurrence an Event records.
type EventType string

const (
	EventRunStarted     EventType = "run_started"
	EventRunFinished    EventType = "run_finished"
	EventModelRequest   EventType = "model_request"
	EventModelResponse  EventType = "model_response"
	EventToolCall       EventType = "tool_call"
	EventToolResult     EventType = "tool_result"
	EventPolicyDecision EventType = "policy_decision"
	EventError          EventType = "error"
)

var knownEventTypes = map[EventType]struct{}{
	EventRunStarted:     {},
	EventRunFinished:    {},
	EventModelRequest:   {},
	EventModelResponse:  {},
	EventToolCall:       {},
	EventToolResult:     {},
	EventPolicyDecision: {},
	EventError:          {},
}

// Event is a single, immutable occurrence in a run's event log.
type Event struct {
	RunID   string
	Seq     int64
	Version int
	Type    EventType
	Time    time.Time
	Payload json.RawMessage
}

// wireEvent is Event's JSON encoding.
type wireEvent struct {
	RunID   string          `json:"run_id"`
	Seq     int64           `json:"seq"`
	Version int             `json:"version"`
	Type    string          `json:"type"`
	Time    time.Time       `json:"time"`
	Payload json.RawMessage `json:"payload"`
}

// ErrMalformedEvent indicates the input was not well-formed encoded Event JSON.
var ErrMalformedEvent = errors.New("recorder: malformed event")

// ErrUnsupportedVersion indicates an event's Version is not CurrentVersion.
var ErrUnsupportedVersion = errors.New("recorder: unsupported event version")

// ErrUnknownEventType indicates an event's Type is not recognized.
var ErrUnknownEventType = errors.New("recorder: unknown event type")

// Encode serializes e to its wire format.
func (e Event) Encode() ([]byte, error) {
	data, err := json.Marshal(wireEvent{
		RunID:   e.RunID,
		Seq:     e.Seq,
		Version: e.Version,
		Type:    string(e.Type),
		Time:    e.Time,
		Payload: e.Payload,
	})
	if err != nil {
		return nil, fmt.Errorf("recorder: encode event: %w", err)
	}
	return data, nil
}

// DecodeEvent parses and validates an encoded Event, rejecting malformed
// JSON, an unsupported Version, or an unrecognized Type.
func DecodeEvent(data []byte) (Event, error) {
	var w wireEvent
	if err := json.Unmarshal(data, &w); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrMalformedEvent, err)
	}

	if w.Version != CurrentVersion {
		return Event{}, fmt.Errorf("recorder: version %d: %w", w.Version, ErrUnsupportedVersion)
	}

	t := EventType(w.Type)
	if _, ok := knownEventTypes[t]; !ok {
		return Event{}, fmt.Errorf("recorder: type %q: %w", w.Type, ErrUnknownEventType)
	}

	return Event{
		RunID:   w.RunID,
		Seq:     w.Seq,
		Version: w.Version,
		Type:    t,
		Time:    w.Time,
		Payload: w.Payload,
	}, nil
}
