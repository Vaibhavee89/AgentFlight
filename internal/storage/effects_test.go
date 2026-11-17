package storage

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSQLiteStore_ApplyEffect(t *testing.T) {
	tests := []struct {
		name        string
		keys        []string
		wantApplied []bool
		wantRows    int
	}{
		{name: "new key is applied", keys: []string{"a"}, wantApplied: []bool{true}, wantRows: 1},
		{name: "repeat of a key is a no-op", keys: []string{"a", "a"}, wantApplied: []bool{true, false}, wantRows: 1},
		{name: "distinct keys are independent", keys: []string{"a", "b", "a"}, wantApplied: []bool{true, true, false}, wantRows: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()
			for i, key := range tt.keys {
				got, err := store.ApplyEffect(ctx, key, "send_message", `{"n":`+string(rune('0'+i))+`}`)
				if err != nil {
					t.Fatalf("ApplyEffect(%q) error: %v", key, err)
				}
				if got != tt.wantApplied[i] {
					t.Fatalf("ApplyEffect(%q) #%d = %v, want %v", key, i, got, tt.wantApplied[i])
				}
			}
			effects, err := store.Effects(ctx)
			if err != nil {
				t.Fatalf("Effects() error: %v", err)
			}
			if len(effects) != tt.wantRows {
				t.Fatalf("Effects() returned %d rows, want %d", len(effects), tt.wantRows)
			}
		})
	}
}

func TestSQLiteStore_ApplyEffect_FirstWriteWins(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	store.ApplyEffect(ctx, "k", "send_message", "first")
	store.ApplyEffect(ctx, "k", "send_message", "second")

	effects, err := store.Effects(ctx)
	if err != nil || len(effects) != 1 {
		t.Fatalf("Effects() = %v, %v, want one row", effects, err)
	}
	if effects[0].Payload != "first" || effects[0].Tool != "send_message" || effects[0].CreatedAt.IsZero() {
		t.Fatalf("effect = %+v, want the first write kept", effects[0])
	}
}

// A repeat after a restart must still be recognised, so the ledger has to be on
// disk, not in memory.
func TestSQLiteStore_ApplyEffect_SurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "effects.db")

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	if applied, err := first.ApplyEffect(ctx, "k", "send_message", "p"); err != nil || !applied {
		t.Fatalf("first ApplyEffect = %v, %v, want applied", applied, err)
	}
	first.Close()

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen error: %v", err)
	}
	defer second.Close()
	if applied, err := second.ApplyEffect(ctx, "k", "send_message", "p"); err != nil || applied {
		t.Fatalf("ApplyEffect after reopen = %v, %v, want a no-op", applied, err)
	}
}

func TestSQLiteStore_ApplyEffect_ConcurrentSameKeyAppliesOnce(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	var applied atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := store.ApplyEffect(ctx, "same", "send_message", "p")
			if err != nil {
				t.Errorf("ApplyEffect error: %v", err)
				return
			}
			if ok {
				applied.Add(1)
			}
		}()
	}
	wg.Wait()

	if applied.Load() != 1 {
		t.Fatalf("%d concurrent callers were told they applied the effect, want exactly 1", applied.Load())
	}
}
