package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/benchmark"
	"github.com/Vaibhavee89/agentflight/internal/policy"
	"github.com/Vaibhavee89/agentflight/internal/provider"
	"github.com/Vaibhavee89/agentflight/internal/recorder"
	goruntime "github.com/Vaibhavee89/agentflight/internal/runtime"
	"github.com/Vaibhavee89/agentflight/internal/storage"
	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/builtin"
)

// newServerWith builds a test server, letting the caller adjust the config
// (poll interval, bench dir, CORS, policy) before it is created.
func newServerWith(t *testing.T, latency time.Duration, mutate func(*Config)) (*Server, *storage.SQLiteStore) {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "ep.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	reg := tools.NewRegistry()
	if err := reg.Register(builtin.NewEcho()); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	cfg := Config{
		Store:        store,
		Provider:     func() goruntime.ModelProvider { return provider.EchoProvider{Latency: latency} },
		Tools:        reg,
		MaxSteps:     5,
		PollInterval: 5 * time.Millisecond,
		Heartbeat:    time.Hour,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return NewServer(cfg), store
}

// appendRun writes a raw event sequence directly, which is how a run that
// was killed without a final event looks in the log.
func appendRun(t *testing.T, store *storage.SQLiteStore, runID string, events ...recorder.Event) {
	t.Helper()
	for _, e := range events {
		if err := store.Append(context.Background(), e); err != nil {
			t.Fatalf("append %s seq %d: %v", e.Type, e.Seq, err)
		}
	}
}

func ev(runID string, seq int64, typ recorder.EventType, payload string) recorder.Event {
	return recorder.Event{
		RunID: runID, Seq: seq, Version: recorder.CurrentVersion, Type: typ,
		Time: time.Date(2026, 1, 1, 0, 0, int(seq), 0, time.UTC), Payload: json.RawMessage(payload),
	}
}

func TestListRuns(t *testing.T) {
	// Latency keeps the last run executing while the listing is read.
	srv, store := newServerWith(t, 200*time.Millisecond, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Written first, so it is the oldest run.
	appendRun(t, store, "dead-1",
		ev("dead-1", 1, recorder.EventRunStarted, `{"prompt":"x"}`),
		ev("dead-1", 2, recorder.EventModelResponse, `{"Content":"partial"}`),
	)
	done := startRun(t, ts.URL, "hi")
	waitForStatus(t, ts.URL, done, statusCompleted)

	slowID := startRun(t, ts.URL, "slow")
	defer doRequest(t, ts.URL, http.MethodPost, "/runs/"+slowID+"/cancel", "")
	waitForStatus(t, ts.URL, slowID, statusRunning)

	tests := []struct {
		name    string
		query   string
		wantIDs []string
		wantErr int
	}{
		{name: "all runs newest first", query: "", wantIDs: []string{slowID, done, "dead-1"}},
		{name: "completed only", query: "?status=completed", wantIDs: []string{done}},
		{name: "interrupted only", query: "?status=interrupted", wantIDs: []string{"dead-1"}},
		{name: "limit caps the page", query: "?limit=1", wantIDs: []string{slowID}},
		{name: "limit zero rejected", query: "?limit=0", wantErr: http.StatusBadRequest},
		{name: "limit over max rejected", query: "?limit=201", wantErr: http.StatusBadRequest},
		{name: "unknown status rejected", query: "?status=bogus", wantErr: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := doRequest(t, ts.URL, http.MethodGet, "/runs"+tt.query, "")
			if tt.wantErr != 0 {
				if code != tt.wantErr {
					t.Fatalf("GET /runs%s = %d %s, want %d", tt.query, code, body, tt.wantErr)
				}
				return
			}
			if code != http.StatusOK {
				t.Fatalf("GET /runs%s = %d %s", tt.query, code, body)
			}
			var items []runListItem
			if err := json.Unmarshal(body, &items); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := make([]string, len(items))
			for i, it := range items {
				got[i] = it.ID
			}
			if strings.Join(got, ",") != strings.Join(tt.wantIDs, ",") {
				t.Fatalf("ids = %v, want %v", got, tt.wantIDs)
			}
		})
	}

	_, body := doRequest(t, ts.URL, http.MethodGet, "/runs?status=completed", "")
	var items []runListItem
	_ = json.Unmarshal(body, &items)
	if len(items) != 1 || items[0].Steps != 2 || items[0].ToolCalls != 1 || items[0].StartedAt == nil || items[0].FinishedAt == nil {
		t.Fatalf("completed run summary = %+v, want steps 2, one tool call, both timestamps", items)
	}
}

func TestEventsCursorPaging(t *testing.T) {
	srv, _ := newServerWith(t, 0, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "hi")
	waitForStatus(t, ts.URL, id, statusCompleted)

	seqs := func(body []byte) []int64 {
		var evs []eventView
		if err := json.Unmarshal(body, &evs); err != nil {
			t.Fatalf("decode events: %v", err)
		}
		out := make([]int64, len(evs))
		for i, e := range evs {
			out[i] = e.Seq
		}
		return out
	}

	_, all := doRequest(t, ts.URL, http.MethodGet, "/runs/"+id+"/events", "")
	total := len(seqs(all))
	if total < 4 {
		t.Fatalf("run has %d events, want at least 4", total)
	}

	tests := []struct {
		name    string
		query   string
		want    []int64
		wantErr int
	}{
		{name: "no params returns the whole log", query: "", want: nil},
		{name: "first page", query: "?after=0&limit=2", want: []int64{1, 2}},
		{name: "next page follows the cursor", query: "?after=2&limit=2", want: []int64{3, 4}},
		{name: "after the last event is empty", query: fmt.Sprintf("?after=%d", total), want: []int64{}},
		{name: "negative cursor rejected", query: "?after=-1", wantErr: http.StatusBadRequest},
		{name: "non-numeric cursor rejected", query: "?after=abc", wantErr: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := doRequest(t, ts.URL, http.MethodGet, "/runs/"+id+"/events"+tt.query, "")
			if tt.wantErr != 0 {
				if code != tt.wantErr {
					t.Fatalf("status = %d %s, want %d", code, body, tt.wantErr)
				}
				return
			}
			if code != http.StatusOK {
				t.Fatalf("status = %d %s", code, body)
			}
			got := seqs(body)
			if tt.want == nil {
				if len(got) != total {
					t.Fatalf("got %d events, want full log of %d", len(got), total)
				}
				return
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("seqs = %v, want %v", got, tt.want)
			}
		})
	}

	code, _ := doRequest(t, ts.URL, http.MethodGet, "/runs/nope/events?after=0", "")
	if code != http.StatusNotFound {
		t.Fatalf("unknown run with cursor = %d, want 404", code)
	}
}

func TestStreamDeliversEventsAsTheyAreAppended(t *testing.T) {
	srv, _ := newServerWith(t, 60*time.Millisecond, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "live")

	resp, err := http.Get(ts.URL + "/runs/" + id + "/stream")
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	type frame struct {
		id, event string
	}
	frames := make(chan frame, 64)
	go func() {
		defer close(frames)
		sc := bufio.NewScanner(resp.Body)
		var f frame
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				f.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case line == "":
				if f.event != "" {
					frames <- f
				}
				f = frame{}
			}
		}
	}()

	deadline := time.After(10 * time.Second)
	var gotTypes []string
	var lastID string
	sawRunningBeforeFinish := false
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				if len(gotTypes) == 0 || gotTypes[len(gotTypes)-1] != string(recorder.EventRunFinished) {
					t.Fatalf("stream closed before run_finished; got %v", gotTypes)
				}
				if !sawRunningBeforeFinish {
					t.Fatalf("no event arrived while the run was still executing; the stream is not live")
				}
				for i := 1; i < len(gotTypes); i++ {
					if gotTypes[i] == "" {
						t.Fatalf("empty event type at %d", i)
					}
				}
				if lastID == "" {
					t.Fatalf("no SSE id was sent")
				}
				return
			}
			if f.event != string(recorder.EventRunFinished) {
				if snap, _ := srv.runs.snapshot(id); snap.status == statusRunning {
					sawRunningBeforeFinish = true
				}
			}
			if lastID != "" && f.id <= lastID && len(f.id) == len(lastID) {
				t.Fatalf("SSE ids not increasing: %s after %s", f.id, lastID)
			}
			lastID = f.id
			gotTypes = append(gotTypes, f.event)
		case <-deadline:
			t.Fatalf("stream did not finish within 10s; got %v", gotTypes)
		}
	}
}

func TestStreamResumesFromLastEventID(t *testing.T) {
	srv, _ := newServerWith(t, 0, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "resume me")
	waitForStatus(t, ts.URL, id, statusCompleted)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/runs/"+id+"/stream", nil)
	req.Header.Set("Last-Event-ID", "2")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := readAll(resp)

	var ids []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "id: ") {
			ids = append(ids, strings.TrimPrefix(line, "id: "))
		}
	}
	if len(ids) == 0 || ids[0] != "3" {
		t.Fatalf("ids after Last-Event-ID 2 = %v, want to start at 3", ids)
	}
	if !strings.Contains(body, "event: run_finished") {
		t.Fatalf("stream did not include run_finished: %q", body)
	}
}

func readAll(resp *http.Response) (string, error) {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			if errors.Is(err, context.Canceled) || err.Error() == "EOF" {
				return b.String(), nil
			}
			return b.String(), err
		}
	}
}

// TestStreamStopsOnClientDisconnectWithoutLeaks holds a stream open on a slow
// run, disconnects the client, and checks the goroutine count returns to its
// level before the stream was opened.
func TestStreamStopsOnClientDisconnectWithoutLeaks(t *testing.T) {
	srv, _ := newServerWith(t, 30*time.Second, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "hold open")
	defer doRequest(t, ts.URL, http.MethodPost, "/runs/"+id+"/cancel", "")
	waitForStatus(t, ts.URL, id, statusRunning)

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/runs/"+id+"/stream", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	buf := make([]byte, 512)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("read first bytes: %v", err)
	}

	cancel()
	resp.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines after disconnect = %d, want <= %d (stream leaked)", after, before)
	}
}

func TestReplayEndpoint(t *testing.T) {
	srv, store := newServerWith(t, 0, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ok := startRun(t, ts.URL, "replay me")
	waitForStatus(t, ts.URL, ok, statusCompleted)

	// Recorded request says prompt "y", but the run started with "x", so a
	// faithful replay sends "x" and diverges at the model_request, seq 2.
	appendRun(t, store, "tampered",
		ev("tampered", 1, recorder.EventRunStarted, `{"prompt":"x"}`),
		ev("tampered", 2, recorder.EventModelRequest, `{"Prompt":"y","ToolResults":null}`),
		ev("tampered", 3, recorder.EventModelResponse, `{"Content":"hi"}`),
		ev("tampered", 4, recorder.EventRunFinished, `{"RunID":"tampered","Content":"hi","Steps":1,"Status":"completed"}`),
	)

	tests := []struct {
		name       string
		id         string
		wantCode   int
		wantOK     bool
		wantSeq    *int64
		wantDetail string
	}{
		{name: "faithful run replays ok", id: ok, wantCode: http.StatusOK, wantOK: true},
		{name: "tampered request reports its seq", id: "tampered", wantCode: http.StatusOK, wantOK: false, wantSeq: ptr(int64(2)), wantDetail: "seq 2"},
		{name: "unknown run is 404", id: "nope", wantCode: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := doRequest(t, ts.URL, http.MethodPost, "/runs/"+tt.id+"/replay", "")
			if code != tt.wantCode {
				t.Fatalf("status = %d %s, want %d", code, body, tt.wantCode)
			}
			if tt.wantCode != http.StatusOK {
				return
			}
			var resp replayResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.OK != tt.wantOK {
				t.Fatalf("ok = %v, want %v (%s)", resp.OK, tt.wantOK, resp.Detail)
			}
			if (resp.MismatchAtSeq == nil) != (tt.wantSeq == nil) || (tt.wantSeq != nil && *resp.MismatchAtSeq != *tt.wantSeq) {
				t.Fatalf("mismatch_at_seq = %v, want %v", resp.MismatchAtSeq, tt.wantSeq)
			}
			if tt.wantDetail != "" && !strings.Contains(resp.Detail, tt.wantDetail) {
				t.Fatalf("detail %q does not mention %q", resp.Detail, tt.wantDetail)
			}
		})
	}
}

func TestReplayRefusesActiveRun(t *testing.T) {
	srv, _ := newServerWith(t, 30*time.Second, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "busy")
	defer doRequest(t, ts.URL, http.MethodPost, "/runs/"+id+"/cancel", "")
	waitForStatus(t, ts.URL, id, statusRunning)

	code, _ := doRequest(t, ts.URL, http.MethodPost, "/runs/"+id+"/replay", "")
	if code != http.StatusConflict {
		t.Fatalf("replay of an active run = %d, want 409", code)
	}
}

func TestBenchEndpoints(t *testing.T) {
	dir := t.TempDir()
	good := benchmark.CompareDocument{SchemaVersion: benchmark.CompareSchemaVersion, Repeats: 2}
	if err := benchmark.WriteCompareDocument(filepath.Join(dir, "compare.json"), good); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), []byte(`[{"task_id":"task-01"}]`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	srv, _ := newServerWith(t, 0, func(c *Config) { c.BenchDir = dir })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	code, body := doRequest(t, ts.URL, http.MethodGet, "/bench/compare", "")
	var doc benchmark.CompareDocument
	if code != http.StatusOK || json.Unmarshal(body, &doc) != nil || doc.SchemaVersion != benchmark.CompareSchemaVersion {
		t.Fatalf("GET /bench/compare = %d %s", code, body)
	}
	code, body = doRequest(t, ts.URL, http.MethodGet, "/bench/results", "")
	if code != http.StatusOK || !strings.Contains(string(body), "task-01") {
		t.Fatalf("GET /bench/results = %d %s", code, body)
	}

	empty, _ := newServerWith(t, 0, func(c *Config) { c.BenchDir = t.TempDir() })
	ets := httptest.NewServer(empty.Handler())
	defer ets.Close()
	for _, path := range []string{"/bench/compare", "/bench/results"} {
		code, body := doRequest(t, ets.URL, http.MethodGet, path, "")
		if code != http.StatusNotFound || !strings.Contains(string(body), "not found") {
			t.Fatalf("GET %s with no file = %d %s, want 404 with a message", path, code, body)
		}
	}

	bad := newBadSchemaDir(t)
	bsrv, _ := newServerWith(t, 0, func(c *Config) { c.BenchDir = bad })
	bts := httptest.NewServer(bsrv.Handler())
	defer bts.Close()
	if code, body := doRequest(t, bts.URL, http.MethodGet, "/bench/compare", ""); code != http.StatusInternalServerError {
		t.Fatalf("wrong schema version = %d %s, want 500", code, body)
	}
}

func newBadSchemaDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compare.json"), []byte(`{"schema_version": 99}`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

func TestCORS(t *testing.T) {
	const allowed = "http://localhost:5173"
	srv, _ := newServerWith(t, 0, func(c *Config) { c.CORSOrigin = allowed })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tests := []struct {
		name       string
		method     string
		origin     string
		wantHeader string
		wantCode   int
	}{
		{name: "allowed origin gets the header", method: http.MethodGet, origin: allowed, wantHeader: allowed, wantCode: http.StatusOK},
		{name: "other origin gets no header", method: http.MethodGet, origin: "https://evil.example", wantHeader: "", wantCode: http.StatusOK},
		{name: "preflight from allowed origin", method: http.MethodOptions, origin: allowed, wantHeader: allowed, wantCode: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, ts.URL+"/healthz", nil)
			req.Header.Set("Origin", tt.origin)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.wantCode {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != tt.wantHeader {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantHeader)
			}
		})
	}
}

func TestCORSOffByDefault(t *testing.T) {
	srv, _ := newServerWith(t, 0, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("CORS header %q present with CORS disabled", got)
	}
}

// flakyTool hangs on its first call and succeeds afterwards, so a policy
// with a per-attempt timeout must retry once for the run to complete.
type flakyTool struct{ calls int }

func (f *flakyTool) Execute(ctx context.Context, call tools.ToolCall) (tools.ToolOutput, error) {
	f.calls++
	if f.calls == 1 {
		<-ctx.Done()
		return tools.ToolOutput{}, ctx.Err()
	}
	return tools.ToolOutput{Result: "ok"}, nil
}

func TestPerRunPolicyRetriesAppearInListing(t *testing.T) {
	srv, _ := newServerWith(t, 0, func(c *Config) {
		c.Tools = &flakyTool{}
		c.NewPolicy = func() policy.Policy {
			return policy.NewDefaultPolicy([]string{"echo"},
				policy.WithMaxAttempts(3),
				policy.WithTimeout(20*time.Millisecond),
				policy.WithBackoff(time.Millisecond, 2*time.Millisecond),
			)
		}
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := startRun(t, ts.URL, "flaky")
	waitForStatus(t, ts.URL, id, statusCompleted)

	_, body := doRequest(t, ts.URL, http.MethodGet, "/runs?status=completed", "")
	var items []runListItem
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].Retries != 1 {
		t.Fatalf("listing = %+v, want one completed run with one retry", items)
	}
}

func ptr[T any](v T) *T { return &v }
