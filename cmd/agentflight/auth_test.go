package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

func TestLoadAuthTokens(t *testing.T) {
	const a, b, c = "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"
	tests := []struct {
		name    string
		file    string // contents; "" means no file
		env     string
		want    []string
		wantErr error
	}{
		{name: "nothing configured", want: nil},
		{name: "environment only", env: a, want: []string{a}},
		{name: "environment value is trimmed", env: "  " + a + "\n", want: []string{a}},
		{name: "file with one token", file: a + "\n", want: []string{a}},
		{name: "file with several tokens", file: a + "\n" + b + "\n", want: []string{a, b}},
		{name: "blank lines and comments are skipped", file: "# rotated 2026-01\n\n" + a + "\n  \n#" + b + "\n", want: []string{a}},
		{name: "file then environment", file: a + "\n" + b + "\n", env: c, want: []string{a, b, c}},
		{name: "short token in the file", file: "short\n", wantErr: ErrWeakToken},
		{name: "short token in the environment", env: "short", wantErr: ErrWeakToken},
		{name: "one weak token among good ones", file: a + "\nshort\n", wantErr: ErrWeakToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.file != "" {
				path = filepath.Join(t.TempDir(), "tokens")
				if err := os.WriteFile(path, []byte(tt.file), 0o600); err != nil {
					t.Fatalf("write token file: %v", err)
				}
			}
			got, err := loadAuthTokens(path, tt.env)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tokens = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("a missing file is an error, not an open server", func(t *testing.T) {
		if _, err := loadAuthTokens(filepath.Join(t.TempDir(), "absent"), ""); err == nil {
			t.Fatal("expected an error for a missing token file")
		}
	})
}

func TestCheckExposure(t *testing.T) {
	addr := func(ip string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(ip), Port: 8080} }
	tests := []struct {
		name     string
		addr     net.Addr
		auth     bool
		insecure bool
		wantErr  bool
	}{
		{"loopback v4 open", addr("127.0.0.1"), false, false, false},
		{"loopback v6 open", addr("::1"), false, false, false},
		{"all interfaces open is refused", addr("0.0.0.0"), false, false, true},
		{"all v6 interfaces open is refused", addr("::"), false, false, true},
		{"a LAN address open is refused", addr("192.168.1.20"), false, false, true},
		{"all interfaces with auth", addr("0.0.0.0"), true, false, false},
		{"all interfaces with the explicit opt-out", addr("0.0.0.0"), false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkExposure(tt.addr, tt.auth, tt.insecure)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error = %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrExposedWithoutAuth) {
				t.Fatalf("error = %v, want ErrExposedWithoutAuth", err)
			}
		})
	}
}

func TestGenerateToken(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		tok, err := generateToken()
		if err != nil {
			t.Fatalf("generateToken() error: %v", err)
		}
		if !hex64.MatchString(tok) || seen[tok] {
			t.Fatalf("token %q is malformed or repeated", tok)
		}
		seen[tok] = true
		if len(tok) < minTokenLen {
			t.Fatalf("a generated token is shorter than the minimum")
		}
	}
}
