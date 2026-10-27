// Command mcp-filesystem connects the MCP adapter to a real third-party
// server, @modelcontextprotocol/server-filesystem, over stdio. It lists the
// server's tools, reads a file in a sandbox directory through the adapter,
// and checks the content.
//
// Usage:
//
//	go run ./examples/mcp-filesystem -dir /tmp/mcp-sandbox -pkg @modelcontextprotocol/server-filesystem@<version>
//
// The server runs with the -dir argument as its only accessible root. Use a
// throwaway directory, not a personal one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/tools"
	"github.com/Vaibhavee89/agentflight/internal/tools/mcp"
)

const sample = "hello from agentflight over a real MCP server\n"

func main() {
	dir := flag.String("dir", "", "sandbox directory the server may access (required)")
	pkg := flag.String("pkg", "@modelcontextprotocol/server-filesystem", "npm package spec, pin a version with @x.y.z")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: mcp-filesystem -dir <sandbox> [-pkg <npm spec>]")
		os.Exit(2)
	}
	if err := run(*dir, *pkg); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}

func run(dir, pkg string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	file := filepath.Join(abs, "sample.txt")
	if err := os.WriteFile(file, []byte(sample), 0o644); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.Command("npx", "-y", pkg, abs)
	session, err := mcp.ConnectStdio(ctx, cmd)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", pkg, err)
	}
	defer session.Close()

	reg := tools.NewRegistry()
	names, err := mcp.Register(ctx, reg, session)
	if err != nil {
		return err
	}
	fmt.Printf("server %s offers %d tools: %v\n", pkg, len(names), names)

	readName := ""
	for _, candidate := range []string{"read_text_file", "read_file"} {
		if _, ok := reg.Get(candidate); ok {
			readName = candidate
			break
		}
	}
	if readName == "" {
		return fmt.Errorf("no read tool among %v", names)
	}
	read, _ := reg.Get(readName)

	input, err := json.Marshal(map[string]string{"path": file})
	if err != nil {
		return err
	}
	out, err := read.Execute(ctx, tools.ToolCall{Name: readName, Input: input})
	if err != nil {
		return fmt.Errorf("%s: %w", readName, err)
	}
	if out.Result != sample {
		return fmt.Errorf("%s returned %q, want %q", readName, out.Result, sample)
	}
	fmt.Printf("%s returned the expected %d bytes\n", readName, len(out.Result))
	return nil
}
