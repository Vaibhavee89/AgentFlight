package replay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
)

// ErrReplayExhausted indicates a replay ran out of recorded events for a
// call the live run attempted.
var ErrReplayExhausted = errors.New("replay: no more recorded events")

type requestResponsePair struct {
	request  json.RawMessage
	response provider.ModelResponse
}

// ReplayProvider implements runtime.ModelProvider by returning a run's
// recorded model_response events in order. Before returning each one, it
// checks that the live request matches the model_request that preceded the
// recorded response, by comparing the hash of their canonical JSON, and
// returns an error wrapping runtime.ErrReplayMismatch with a diff summary
// if they differ.
type ReplayProvider struct {
	pairs []requestResponsePair
	next  int
}

// NewReplayProvider builds a ReplayProvider from a run's recorded events,
// pairing each model_response with the model_request immediately before it.
func NewReplayProvider(events []recorder.Event) (*ReplayProvider, error) {
	var pairs []requestResponsePair
	var pendingRequest json.RawMessage

	for _, e := range events {
		switch e.Type {
		case recorder.EventModelRequest:
			pendingRequest = e.Payload
		case recorder.EventModelResponse:
			var resp provider.ModelResponse
			if err := json.Unmarshal(e.Payload, &resp); err != nil {
				return nil, fmt.Errorf("replay: decode model_response seq %d: %w", e.Seq, err)
			}
			pairs = append(pairs, requestResponsePair{request: pendingRequest, response: resp})
			pendingRequest = nil
		}
	}

	return &ReplayProvider{pairs: pairs}, nil
}

// Len reports how many recorded model_request/model_response pairs remain
// to be replayed, including the current one.
func (p *ReplayProvider) Len() int {
	return len(p.pairs)
}

// Complete returns the next recorded response, after verifying req matches
// the request recorded alongside it.
func (p *ReplayProvider) Complete(ctx context.Context, req provider.Request) (provider.ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return provider.ModelResponse{}, fmt.Errorf("replay: %w", err)
	}
	if p.next >= len(p.pairs) {
		return provider.ModelResponse{}, fmt.Errorf("replay: no recorded model_response for step %d: %w", p.next, ErrReplayExhausted)
	}

	pair := p.pairs[p.next]
	step := p.next
	p.next++

	got, err := json.Marshal(req)
	if err != nil {
		return provider.ModelResponse{}, fmt.Errorf("replay: encode request: %w", err)
	}

	match, err := canonicalJSONEqual(pair.request, got)
	if err != nil {
		return provider.ModelResponse{}, fmt.Errorf("replay: compare request at step %d: %w", step, err)
	}
	if !match {
		return provider.ModelResponse{}, fmt.Errorf(
			"replay: request mismatch at step %d: %w\n%s",
			step, runtime.ErrReplayMismatch, diffSummary(pair.request, got),
		)
	}

	return pair.response, nil
}

// canonicalJSONEqual reports whether a and b encode the same canonical JSON,
// compared by hashing each after removing insignificant whitespace.
func canonicalJSONEqual(a, b []byte) (bool, error) {
	ha, err := canonicalHash(a)
	if err != nil {
		return false, fmt.Errorf("recorded value: %w", err)
	}
	hb, err := canonicalHash(b)
	if err != nil {
		return false, fmt.Errorf("live value: %w", err)
	}
	return ha == hb, nil
}

func canonicalHash(data []byte) ([sha256.Size]byte, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("compact json: %w", err)
	}
	return sha256.Sum256(compact.Bytes()), nil
}

// diffSummary renders a line-oriented diff between two JSON documents, with
// "-" for lines only in recorded and "+" for lines only in got.
func diffSummary(recorded, got []byte) string {
	recordedLines := strings.Split(indentJSON(recorded), "\n")
	gotLines := strings.Split(indentJSON(got), "\n")

	max := len(recordedLines)
	if len(gotLines) > max {
		max = len(gotLines)
	}

	var b strings.Builder
	for i := 0; i < max; i++ {
		var r, g string
		if i < len(recordedLines) {
			r = recordedLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if r == g {
			continue
		}
		if i < len(recordedLines) {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		if i < len(gotLines) {
			fmt.Fprintf(&b, "+ %s\n", g)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func indentJSON(data []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return string(data)
	}
	return buf.String()
}
