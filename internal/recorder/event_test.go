package recorder

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type wireOverride struct {
	Version int    `json:"version"`
	Type    string `json:"type"`
}

func mustEvent(t *testing.T, w wireOverride) []byte {
	t.Helper()
	data, err := json.Marshal(struct {
		RunID   string          `json:"run_id"`
		Seq     int64           `json:"seq"`
		Version int             `json:"version"`
		Type    string          `json:"type"`
		Time    time.Time       `json:"time"`
		Payload json.RawMessage `json:"payload"`
	}{
		RunID: "run-1", Seq: 1, Version: w.Version, Type: w.Type,
		Time: time.Now(), Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("marshal test event: %v", err)
	}
	return data
}

func TestDecodeEvent(t *testing.T) {
	valid := Event{
		RunID:   "run-1",
		Seq:     1,
		Version: CurrentVersion,
		Type:    EventRunStarted,
		Time:    time.Now().UTC(),
		Payload: json.RawMessage(`{"prompt":"hi"}`),
	}
	validData, err := valid.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	tests := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		{name: "valid event decodes", data: validData},
		{name: "malformed json", data: []byte("not json"), wantErr: ErrMalformedEvent},
		{
			name:    "unsupported version",
			data:    mustEvent(t, wireOverride{Version: 2, Type: string(EventRunStarted)}),
			wantErr: ErrUnsupportedVersion,
		},
		{
			name:    "unknown type",
			data:    mustEvent(t, wireOverride{Version: CurrentVersion, Type: "not_a_real_type"}),
			wantErr: ErrUnknownEventType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeEvent(tt.data)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("DecodeEvent() error = %v, want wrapping %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeEvent() unexpected error: %v", err)
			}
			if got.RunID != valid.RunID || got.Seq != valid.Seq || got.Type != valid.Type {
				t.Fatalf("DecodeEvent() = %+v, want matching %+v", got, valid)
			}
		})
	}
}
