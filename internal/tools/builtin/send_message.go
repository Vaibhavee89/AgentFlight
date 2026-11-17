package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

// ErrMissingIdempotencyKey indicates a call reached send_message without an
// idempotency key. The tool refuses to act, because without a key it cannot
// tell a repeat from a new message.
var ErrMissingIdempotencyKey = errors.New("send_message: call has no idempotency key")

// ErrInvalidMessage indicates a send_message input without a recipient or body.
var ErrInvalidMessage = errors.New("send_message: input needs non-empty to and body")

// EffectLedger durably records side effects by idempotency key. ApplyEffect
// returns applied=true the first time it sees key and false for every repeat,
// and must make the check and the record one atomic step.
type EffectLedger interface {
	ApplyEffect(ctx context.Context, key, tool, payload string) (applied bool, err error)
}

type messageInput struct {
	To   string `json:"to"`
	Body string `json:"body"`
}

type sendMessageTool struct {
	ledger EffectLedger
}

// NewSendMessage returns a tool that stands in for an action with a real-world
// side effect, such as sending an email. The "send" is a row in ledger keyed by
// the call's idempotency key, so a retried call, or one re-run after a crash
// and resume, sends nothing a second time and returns the same result.
func NewSendMessage(ledger EffectLedger) tools.Tool {
	return sendMessageTool{ledger: ledger}
}

func (sendMessageTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "send_message",
		Description: "Sends a message to a recipient. Safe to retry: the same call is never delivered twice.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"to":{"type":"string","description":"Recipient."},"body":{"type":"string","description":"Message text."}},"required":["to","body"]}`),
	}
}

func (t sendMessageTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("send_message: %w", err)
	}

	var in messageInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("send_message: decode input: %w", err)
	}
	if in.To == "" || in.Body == "" {
		return tools.ToolOutput{}, ErrInvalidMessage
	}
	if call.IdempotencyKey == "" {
		return tools.ToolOutput{}, ErrMissingIdempotencyKey
	}

	payload, err := json.Marshal(in)
	if err != nil {
		return tools.ToolOutput{}, fmt.Errorf("send_message: encode payload: %w", err)
	}
	// Whether this was the first delivery or a repeat, the caller gets the same
	// answer, as an idempotent API would return the original response.
	if _, err := t.ledger.ApplyEffect(ctx, call.IdempotencyKey, "send_message", string(payload)); err != nil {
		return tools.ToolOutput{}, fmt.Errorf("send_message: %w", err)
	}
	return tools.ToolOutput{Result: fmt.Sprintf("message to %s sent", in.To)}, nil
}
