package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantCode   int
		wantInBody string
	}{
		{name: "root serves index", method: http.MethodGet, path: "/ui/", wantCode: http.StatusOK, wantInBody: `id="root"`},
		{name: "bare prefix serves index", method: http.MethodGet, path: "/ui", wantCode: http.StatusOK, wantInBody: `id="root"`},
		{name: "client route falls back to index", method: http.MethodGet, path: "/ui/runs/abc123", wantCode: http.StatusOK, wantInBody: `id="root"`},
		{name: "nested client route falls back", method: http.MethodGet, path: "/ui/benchmarks", wantCode: http.StatusOK, wantInBody: `id="root"`},
		{name: "missing asset is 404, not HTML", method: http.MethodGet, path: "/ui/assets/missing.js", wantCode: http.StatusNotFound},
		{name: "POST is refused", method: http.MethodPost, path: "/ui/", wantCode: http.StatusMethodNotAllowed},
	}

	h := Handler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("%s %s = %d, want %d", tt.method, tt.path, rec.Code, tt.wantCode)
			}
			if tt.wantInBody != "" && !strings.Contains(rec.Body.String(), tt.wantInBody) {
				t.Fatalf("body does not contain %q: %s", tt.wantInBody, rec.Body.String())
			}
		})
	}
}
