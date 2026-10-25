package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/Vaibhavee89/agentflight/internal/replay"
	"github.com/Vaibhavee89/agentflight/internal/storage"
)

const version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("agentflight " + version)
		return
	}

	switch os.Args[1] {
	case "replay":
		runReplay(os.Args[2:])
	default:
		fmt.Println("agentflight " + version)
	}
}

// runReplay implements `agentflight replay [-db path] <run-id>`: it prints
// OK if the recorded run replays to the same result, or the first mismatch
// otherwise.
func runReplay(args []string) {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	dbPath := fs.String("db", "agentflight.db", "path to the SQLite event store")
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: agentflight replay [-db path] <run-id>")
		os.Exit(2)
	}
	runID := fs.Arg(0)

	ctx := context.Background()
	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if _, err := replay.Replay(ctx, store, runID); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("OK")
}
