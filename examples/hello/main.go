// Command hello makes one real call to the Anthropic Messages API if
// ANTHROPIC_API_KEY is set in the environment, and otherwise explains how
// to set it.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Vaibhavee89/agentflight/internal/provider"
)

func main() {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Println("ANTHROPIC_API_KEY is not set; skipping the real API call.")
		fmt.Println("Set ANTHROPIC_API_KEY and re-run to see a live response.")
		return
	}

	p := provider.NewAnthropicProvider("claude-3-5-haiku-20241022")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := p.Complete(ctx, provider.Request{Prompt: "Say hello in one short sentence."})
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(resp.Content)
	fmt.Printf("tokens: %d in, %d out\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
}
