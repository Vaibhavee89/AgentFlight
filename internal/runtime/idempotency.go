package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// idempotencyKey names one logical tool operation: run, step, position within
// the step, and a digest of what the call does. It is a pure function of those
// inputs, so a retry, or a call re-executed after a crash and resume, carries
// the same key as the first attempt.
//
// The digest is there for the resume case. Resume asks the model for the step
// again, and it may now request something different at the same position. A
// different call is a different operation and must not share the old key, or a
// tool would wrongly treat it as a repeat of the call that already ran.
func idempotencyKey(runID string, step, index int, call tools.ToolCall) string {
	h := sha256.New()
	h.Write([]byte(call.Name))
	h.Write([]byte{0})
	h.Write(canonicalJSON(call.Input))
	return fmt.Sprintf("%s:%d:%d:%s", runID, step, index, hex.EncodeToString(h.Sum(nil))[:12])
}

// canonicalJSON re-encodes input with sorted object keys and no insignificant
// whitespace, so two spellings of the same arguments hash alike. Input that is
// not valid JSON is used as it is.
func canonicalJSON(input json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return input
	}
	out, err := json.Marshal(v)
	if err != nil {
		return input
	}
	return out
}
