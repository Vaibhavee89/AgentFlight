package provider

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Vaibhavee89/agentflight/internal/tools"
)

func result(name, out string) ToolResult {
	return ToolResult{Call: tools.ToolCall{Name: name, Input: json.RawMessage(`{}`)}, Output: tools.ToolOutput{Result: out}}
}

func TestRequest_Exchanges(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want []string
	}{
		{name: "prompt only", req: Request{Prompt: "p"}},
		{name: "latest step only", req: Request{ToolResults: []ToolResult{result("a", "1")}}, want: []string{"a"}},
		{
			name: "history then latest, in order",
			req: Request{
				History:     []Exchange{{ToolResults: []ToolResult{result("a", "1")}}, {ToolResults: []ToolResult{result("b", "2"), result("c", "3")}}},
				ToolResults: []ToolResult{result("d", "4")},
			},
			want: []string{"a", "b,c", "d"},
		},
		{name: "empty exchanges are skipped", req: Request{History: []Exchange{{}}, ToolResults: []ToolResult{result("a", "1")}}, want: []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, ex := range tt.req.Exchanges() {
				var names []string
				for _, tr := range ex {
					names = append(names, tr.Call.Name)
				}
				got = append(got, strings.Join(names, ","))
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Exchanges() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestToolResult_JSON guards the error round trip. An error value used to encode
// as {} and fail to decode, which made a checkpoint holding any failed tool
// result unreadable, so a run could not be resumed after a tool failed.
func TestToolResult_JSON(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantErrMsg string // empty means Err must be nil
		wantErr    bool
	}{
		{name: "no error encodes as null", in: `{"Call":{"Name":"x","Input":null},"Output":{"Result":"ok"},"Err":null}`},
		{name: "error is read back as text", in: `{"Call":{"Name":"x","Input":null},"Output":{"Result":""},"Err":"division by zero"}`, wantErrMsg: "division by zero", wantErr: true},
		{name: "old recordings wrote an empty object", in: `{"Call":{"Name":"x","Input":null},"Output":{"Result":""},"Err":{}}`, wantErrMsg: errNotRecorded.Error(), wantErr: true},
		{name: "missing Err means no error", in: `{"Call":{"Name":"x","Input":null},"Output":{"Result":"ok"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tr ToolResult
			if err := json.Unmarshal([]byte(tt.in), &tr); err != nil {
				t.Fatalf("Unmarshal() error: %v", err)
			}
			if (tr.Err != nil) != tt.wantErr || (tr.Err != nil && tr.Err.Error() != tt.wantErrMsg) {
				t.Fatalf("Err = %v, want %q", tr.Err, tt.wantErrMsg)
			}

			// What is written can be read back unchanged.
			enc, err := json.Marshal(tr)
			if err != nil {
				t.Fatalf("Marshal() error: %v", err)
			}
			var back ToolResult
			if err := json.Unmarshal(enc, &back); err != nil {
				t.Fatalf("round trip Unmarshal(%s) error: %v", enc, err)
			}
			if (back.Err == nil) != (tr.Err == nil) || (tr.Err != nil && back.Err.Error() != tr.Err.Error()) {
				t.Fatalf("round trip Err = %v, want %v", back.Err, tr.Err)
			}
		})
	}

	t.Run("a nil error keeps the field the log has always had", func(t *testing.T) {
		enc, _ := json.Marshal(ToolResult{Call: tools.ToolCall{Name: "x"}})
		if !strings.Contains(string(enc), `"Err":null`) {
			t.Fatalf("encoding %s does not contain \"Err\":null, so recorded requests would change", enc)
		}
	})
	t.Run("an error is written as its message", func(t *testing.T) {
		enc, _ := json.Marshal(ToolResult{Err: errors.New("boom")})
		if !strings.Contains(string(enc), `"Err":"boom"`) {
			t.Fatalf("encoding %s does not contain the message", enc)
		}
	})
}

// twoStepRequest is a conversation where the model has made two rounds of tool
// calls: one with two parallel calls (one of which failed), then one more.
func twoStepRequest() Request {
	failed := result("calculator", "")
	failed.Err = errors.New("division by zero")
	return Request{
		Prompt:      "do things",
		History:     []Exchange{{ToolResults: []ToolResult{result("echo", "one"), failed}}},
		ToolResults: []ToolResult{result("echo", "two")},
	}
}

func TestBuildAnthropicRequest_IncludesEarlierSteps(t *testing.T) {
	got := buildAnthropicRequest("m", nil, twoStepRequest())

	var roles []string
	for _, m := range got.Messages {
		roles = append(roles, m.Role)
	}
	if want := "user,assistant,user,assistant,user"; strings.Join(roles, ",") != want {
		t.Fatalf("roles = %v, want %s", roles, want)
	}

	// Every tool_use id is unique across the conversation, and each tool_result
	// points at the tool_use in the turn before it.
	seen := map[string]bool{}
	for i := 1; i < len(got.Messages); i += 2 {
		uses, results := got.Messages[i].Content, got.Messages[i+1].Content
		if len(uses) != len(results) {
			t.Fatalf("turn %d: %d tool_use blocks, %d tool_result blocks", i, len(uses), len(results))
		}
		for j := range uses {
			if seen[uses[j].ID] {
				t.Fatalf("tool_use id %q is used twice", uses[j].ID)
			}
			seen[uses[j].ID] = true
			if results[j].ToolUseID != uses[j].ID {
				t.Fatalf("tool_result %d points at %q, want %q", j, results[j].ToolUseID, uses[j].ID)
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("saw %d tool calls in the request, want 3", len(seen))
	}
	if first := got.Messages[2].Content[1]; !first.IsError || first.Content != "error: division by zero" {
		t.Fatalf("failed call from the earlier step = %+v, want is_error with its message", first)
	}
}

func TestBuildOpenAIRequest_IncludesEarlierSteps(t *testing.T) {
	got := buildOpenAIRequest("m", nil, twoStepRequest())

	var roles []string
	for _, m := range got.Messages {
		roles = append(roles, m.Role)
	}
	if want := "user,assistant,tool,tool,assistant,tool"; strings.Join(roles, ",") != want {
		t.Fatalf("roles = %v, want %s", roles, want)
	}

	ids := map[string]bool{}
	for _, m := range got.Messages {
		for _, c := range m.ToolCalls {
			if ids[c.ID] {
				t.Fatalf("tool call id %q is used twice", c.ID)
			}
			ids[c.ID] = true
		}
	}
	for _, m := range got.Messages {
		if m.Role == "tool" && !ids[m.ToolCallID] {
			t.Fatalf("tool message points at unknown call id %q", m.ToolCallID)
		}
	}
	if got.Messages[3].Content != "error: division by zero" {
		t.Fatalf("failed call from the earlier step = %q, want its message", got.Messages[3].Content)
	}
}
