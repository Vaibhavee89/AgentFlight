package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// minTokenLen is the shortest token accepted. It rules out guessable ones such
// as "password"; `agentflight token` makes a 64-character one.
const minTokenLen = 16

// tokenEnvVar names the environment variable that can hold a single token.
const tokenEnvVar = "AGENTFLIGHT_API_TOKEN"

// ErrWeakToken indicates a configured token is shorter than minTokenLen.
var ErrWeakToken = errors.New("token is too short")

// ErrExposedWithoutAuth indicates the server would listen on a non-loopback
// address with no authentication and without the explicit opt-out.
var ErrExposedWithoutAuth = errors.New("refusing to listen beyond loopback without authentication")

// loadAuthTokens returns the API tokens from a file (one per line, blank lines
// and lines starting with # ignored) and from the environment value, in that
// order. Several tokens let one be rotated out while the new one is already in
// use. A file is preferred over a command-line flag so the token does not show
// in the process list.
func loadAuthTokens(file, env string) ([]string, error) {
	var tokens []string
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("open token file: %w", err)
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			tokens = append(tokens, line)
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("read token file: %w", err)
		}
	}
	if env = strings.TrimSpace(env); env != "" {
		tokens = append(tokens, env)
	}
	for _, t := range tokens {
		if len(t) < minTokenLen {
			return nil, fmt.Errorf("%w: tokens must be at least %d characters; make one with `agentflight token`", ErrWeakToken, minTokenLen)
		}
	}
	return tokens, nil
}

// checkExposure refuses to serve an open API on a reachable address. Binding
// to loopback is always allowed, because only this machine can connect.
func checkExposure(addr net.Addr, authEnabled, insecure bool) error {
	if authEnabled || insecure {
		return nil
	}
	if tcp, ok := addr.(*net.TCPAddr); ok && tcp.IP.IsLoopback() {
		return nil
	}
	return fmt.Errorf("%w: %s; set %s or --auth-token-file, or pass --insecure-no-auth to accept the risk", ErrExposedWithoutAuth, addr, tokenEnvVar)
}

// generateToken returns a random 256-bit token as 64 hex characters.
func generateToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// runToken implements `agentflight token`: it prints a new random API token.
func runToken() {
	t, err := generateToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate token: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(t)
}
