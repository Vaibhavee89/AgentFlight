package main

import (
	"fmt"
	"os"

	"github.com/Vaibhavee89/agentflight/internal/pprofserve"
)

// startPprof serves pprof on addr if non-empty, logging the bound address to
// stderr so profiles can be fetched while the command runs.
func startPprof(addr string) {
	if addr == "" {
		return
	}
	bound, _, err := pprofserve.Serve(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pprof: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "pprof listening on http://%s/debug/pprof/\n", bound)
}
