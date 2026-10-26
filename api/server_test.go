package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	"github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

func newTestServer(t *testing.T, latency time.Duration) (*Server, *storage.SQLiteStore) {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		t.Fatalf("register echo: %v", err)
	}

	srv := NewServer(Config{
		Store:    store,
		Provider: func() runtime.ModelProvider { return provider.EchoProvider{Latency: latency} },
		Tools:    reg,
		MaxSteps: 5,
	})
	return srv, store
}

func doRequest(t *testing.T, base, method, path, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// startRun posts a prompt and returns the new run's ID.
func startRun(t *testing.T, base, prompt string) string {
	t.Helper()
	code, body := doRequest(t, base, http.MethodPost, "/runs", `{"prompt":"`+prompt+`"}`)
	if code != http.StatusAccepted {
		t.Fatalf("POST /runs status = %d, body %s", code, body)
	}
	var v runView
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	return v.ID
}

// waitForStatus polls GET /runs/{id} until its status equals want.
func waitForStatus(t *testing.T, base, id, want string) runView {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, body := doRequest(t, base, http.MethodGet, "/runs/"+id, "")
		var v runView
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatalf("decode run view: %v", err)
		}
		if v.Status == want {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s status = %q, want %q", id, v.Status, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t, 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	code, body := doRequest(t, ts.URL, http.MethodGet, "/healthz", "")
	if code != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("GET /healthz = %d %s, want 200 with status ok", code, body)
	}
}

func TestStartAndInspectCompletedRun(t *testing.T) {
	srv, _ := newTestServer(t, 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "hi")
	v := waitForStatus(t, ts.URL, id, statusCompleted)
	if v.Content != "echo: hi" {
		t.Fatalf("content = %q, want %q", v.Content, "echo: hi")
	}

	code, body := doRequest(t, ts.URL, http.MethodGet, "/runs/"+id+"/events", "")
	if code != http.StatusOK {
		t.Fatalf("GET events = %d %s", code, body)
	}
	var events []eventView
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if events[0].Type != string(recorder.EventRunStarted) || events[len(events)-1].Type != string(recorder.EventRunFinished) {
		t.Fatalf("event log bounds = %s..%s, want run_started..run_finished", events[0].Type, events[len(events)-1].Type)
	}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("events[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
	}
}

func TestEndpointErrors(t *testing.T) {
	srv, _ := newTestServer(t, 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	done := startRun(t, ts.URL, "hi")
	waitForStatus(t, ts.URL, done, statusCompleted)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "start with empty prompt", method: http.MethodPost, path: "/runs", body: `{"prompt":""}`, want: http.StatusBadRequest},
		{name: "start with malformed JSON", method: http.MethodPost, path: "/runs", body: `{not json`, want: http.StatusBadRequest},
		{name: "get unknown run", method: http.MethodGet, path: "/runs/nope", want: http.StatusNotFound},
		{name: "events for unknown run", method: http.MethodGet, path: "/runs/nope/events", want: http.StatusNotFound},
		{name: "cancel unknown run", method: http.MethodPost, path: "/runs/nope/cancel", want: http.StatusNotFound},
		{name: "cancel finished run", method: http.MethodPost, path: "/runs/" + done + "/cancel", want: http.StatusConflict},
		{name: "resume finished run", method: http.MethodPost, path: "/runs/" + done + "/resume", want: http.StatusConflict},
		{name: "resume unknown run", method: http.MethodPost, path: "/runs/nope/resume", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := doRequest(t, ts.URL, tt.method, tt.path, tt.body)
			if code != tt.want {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.path, code, body, tt.want)
			}
		})
	}
}

func TestCancelThenResumeCompletesWithoutDuplicateEvents(t *testing.T) {
	srv, store := newTestServer(t, 300*time.Millisecond)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "hi")
	code, body := doRequest(t, ts.URL, http.MethodPost, "/runs/"+id+"/cancel", "")
	if code != http.StatusAccepted {
		t.Fatalf("cancel = %d %s, want 202", code, body)
	}
	waitForStatus(t, ts.URL, id, statusCancelled)

	code, body = doRequest(t, ts.URL, http.MethodPost, "/runs/"+id+"/resume", "")
	if code != http.StatusAccepted {
		t.Fatalf("resume = %d %s, want 202", code, body)
	}
	v := waitForStatus(t, ts.URL, id, statusCompleted)
	if v.Content != "echo: hi" {
		t.Fatalf("resumed content = %q, want %q", v.Content, "echo: hi")
	}

	events, err := store.Events(context.Background(), id)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	seen := map[int64]bool{}
	finished := 0
	for _, e := range events {
		if seen[e.Seq] {
			t.Fatalf("duplicate seq %d in event log", e.Seq)
		}
		seen[e.Seq] = true
		if e.Type == recorder.EventRunFinished {
			finished++
		}
	}
	if finished != 2 {
		t.Fatalf("got %d run_finished events, want 2 (one cancelled, one completed)", finished)
	}
}

func TestBodyLimitRejectsOversizedRequest(t *testing.T) {
	srv, _ := newTestServer(t, 0)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	huge := bytes.Repeat([]byte("a"), maxBodyBytes+1)
	body := `{"prompt":"` + string(huge) + `"}`
	code, _ := doRequest(t, ts.URL, http.MethodPost, "/runs", body)
	if code != http.StatusBadRequest {
		t.Fatalf("oversized body = %d, want 400", code)
	}
}
