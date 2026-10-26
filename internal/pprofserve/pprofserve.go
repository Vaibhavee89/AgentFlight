// Package pprofserve serves net/http/pprof endpoints from an explicit mux,
// so enabling profiling never touches http.DefaultServeMux.
package pprofserve

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
)

// Serve starts an HTTP server on addr exposing the pprof endpoints under
// /debug/pprof/. It returns the bound address (useful when addr uses port 0)
// and a stop function that closes the listener.
func Serve(addr string) (boundAddr string, stop func() error, err error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("pprofserve: listen on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	return ln.Addr().String(), srv.Close, nil
}
