package pprofserve

import (
	"io"
	"net/http"
	"testing"
)

func TestServe_ExposesPprofEndpoints(t *testing.T) {
	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "index", path: "/debug/pprof/", want: http.StatusOK},
		{name: "heap profile", path: "/debug/pprof/heap?debug=1", want: http.StatusOK},
		{name: "unknown path", path: "/debug/pprof/does-not-exist", want: http.StatusNotFound},
	}

	addr, stop, err := Serve("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Serve() error: %v", err)
	}
	t.Cleanup(func() { _ = stop() })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get("http://" + addr + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != tt.want {
				t.Fatalf("GET %s status = %d, want %d", tt.path, resp.StatusCode, tt.want)
			}
		})
	}
}

func TestServe_BadAddressErrors(t *testing.T) {
	_, _, err := Serve("not-a-valid-address:::")
	if err == nil {
		t.Fatalf("Serve() with invalid address returned nil error")
	}
}
