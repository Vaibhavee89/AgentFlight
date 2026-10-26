# Performance: recording overhead and profiling findings

Written for: maintainers profiling or extending AgentFlight's recording path.

## How to profile

Both CLIs accept `--pprof <addr>` to serve `net/http/pprof` on an explicit
mux (no `http.DefaultServeMux`). `harnessbench run` and `harnessbench
compare` also accept `--pprof-hold <duration>` to keep serving after the
command finishes, so heap and allocation profiles can be read from a
completed run.

```sh
go build -o /tmp/hb ./cmd/harnessbench

# CPU profile: the workload must still be running while sampling.
/tmp/hb run --suite benchmarks/basic.jsonl --repeats 1700 --concurrency 8 \
  --pprof 127.0.0.1:6060 --events-db /tmp/hb.db &
curl -o cpu.prof 'http://127.0.0.1:6060/debug/pprof/profile?seconds=5'
go tool pprof -top /tmp/hb cpu.prof

# Allocation profile: read it after the run, while held alive.
/tmp/hb run --suite benchmarks/basic.jsonl --repeats 1700 --concurrency 8 \
  --pprof 127.0.0.1:6061 --pprof-hold 30s --events-db /tmp/hb.db &
curl -o allocs.prof 'http://127.0.0.1:6061/debug/pprof/allocs'
go tool pprof -sample_index=alloc_space -top /tmp/hb allocs.prof
```

## Workload note

The requested "200-task" run is 204 task executions (`--repeats 17` over
the 12-task basic suite); 200 is not a multiple of 12. That run takes about
0.1 s, which is too short to collect a useful CPU profile (about ten samples
at the default 100 Hz). CPU profiles were therefore taken on a 20,400-execution
run (`--repeats 1700`). Heap profiles from the 204-execution run record only
about 4.6 MB of sampled allocations, which is too coarse to rank by, so
allocation rankings below come from the 20,400-execution run.

## Recording microbenchmarks

Run with `go test -run='^$' -bench=... -benchtime=2s -count=3`. The machine
was shared (load average 2–5), so readings vary by roughly 2× between runs;
treat the ratios as more reliable than the absolute numbers.

| Benchmark | Result |
| --- | --- |
| `RecorderAppend/memory` | ~0.7–1.4 µs/op, 15 allocs |
| `RecorderAppend/sqlite` | ~15–23 µs/op, 29 allocs |
| `EncodeEvent` | ~0.3–1.3 µs/op, 3 allocs |
| `RuntimeStep/recording=off` | ~1.3–2.5 µs per step |
| `RuntimeStep/recording=on` | ~5.3–7.0 µs per step |

Recording roughly triples to quadruples per-step runtime cost against an in-memory store,
and SQLite commits add about 15–23 µs per event on top of that. Each three-step
run records about twelve events, so with SQLite the persistence cost is larger
than the agent-loop cost itself.

## Top findings

Ranked by share of the 20,400-execution profiles.

1. **Per-event SQLite commit dominates CPU (55% of samples in
   `SQLiteStore.Append` → `syscall.rawsyscalln`).** Every recorded event is
   its own WAL commit. *Fix applied:* `PRAGMA synchronous = NORMAL` under WAL,
   which removes the fsync per commit. The database stays consistent across
   a process crash; the last few commits can be lost on power failure.
   Measured back to back: about 11.6–11.7 s before, 7.0–9.6 s after on the
   same workload, with the same machine conditions. Wall-clock time for the
   20,400-execution run dropped from about 12.0 s to about 6.7–8.0 s (median
   values); the exact figure varies with machine load.

2. **Single-connection lock contention (about 41% of samples in
   `pthread_cond_wait` / `pthread_cond_signal`).** `SetMaxOpenConns(1)` (added
   earlier to fix `SQLITE_BUSY`) serializes all eight workers behind one
   connection. *Not fixed.* Recommended: a dedicated writer goroutine that
   batches events into one transaction (group commit). That would cut both
   the lock hand-offs and the per-event commit cost. It is a design change to
   the recorder/store boundary, so it is left for a separate change.

3. **Grading read every run's full event log (32% of allocations).**
   `Events()` cumulative allocation was 150 MB out of 468 MB. Only the
   `tool_called` grader reads events. *Fixed:* events are loaded only for
   that grader. `Events()` cumulative allocation dropped to 28 MB (8% of a
   338 MB total).

4. **Tool registry rebuilt for every execution.** `Registry.Register` cost
   12 MB across the run. *Fixed:* one registry is built per process and
   shared across runs. The built-in tools are stateless and `Registry` is
   safe for concurrent use.

5. **One goroutine and allocation per SQL statement (9%, 44 MB in
   `modernc.org/sqlite.interruptOnDone`).** The driver starts a watcher
   goroutine for any context with a deadline, and every run context carries
   one. *Not fixed.* It belongs to the third-party driver. Batching (finding
   2) would reduce the number of statements, which is the lever available
   here.

6. **Per-statement argument conversion (`driverArgsConnLocked`, 47 MB).**
   Preparing the `INSERT` once was tried and reverted: allocations per op
   went from 736 to 728 bytes, within noise, so it did not justify the extra
   state. Recorded as a negative result.

7. **Reflection-based JSON encoding of event payloads (about 19 MB,
   `encoding/json/v2` arshalers).** Each `Record` call marshals a
   `map[string]any` or an ad-hoc struct. *Not fixed.* Typed payload structs
   would avoid most reflection; the gain is probably small next to finding 1.

8. **`results.json` was built as one full buffer.** `json.MarshalIndent`
   held the whole output in memory before writing. *Fixed:* the output is now
   streamed through a `json.Encoder` directly into the file.

9. **Row scanning copies every payload.** `Events()` allocated 25 MB in
   `bytes.Clone` while scanning payload blobs. *Not fixed.* Using
   `sql.RawBytes` would avoid the copy, but the bytes must then be consumed
   before the next `Next()`, which `Events()` does not currently guarantee.

10. **Heap sampling is too coarse for short runs.** A 204-execution run yields
    about 4.6 MB of sampled allocations at Go's default 512 KB sampling rate,
    which is too little to rank by. *Process note:* memory conclusions should
    come from workloads of at least 20,000 executions.

## Fixes applied in this pass

- `PRAGMA synchronous = NORMAL` for the WAL-mode event store (finding 1).
- `harnessbench` loads events only for `tool_called` grading (finding 3).
- One shared tool registry per process (finding 4).
- Streamed `results.json` output (finding 8).
- `--pprof` and `--pprof-hold` on both CLIs, served from `internal/pprofserve`.

Not applied: group-commit writer (finding 2, the next most valuable change),
driver-level statement fixes (finding 5), and typed payloads (finding 7).

## Verification

- `go test -race -count=20 ./...`: all packages pass, exit code 0, no flakes.
- Fuzzing (see `internal/recorder/fuzz_test.go`, `internal/replay/fuzz_test.go`):
  `FuzzDecodeEvent` ran about 5.6 million inputs in 30 s with no failures.
  `FuzzReplayInput` found a real bug: a corrupt recorded payload made
  `ReplayProvider.Complete` return an untyped error. *Fixed* by validating
  recorded requests when the provider is built and returning a typed
  `ErrCorruptRecording`; the same change covers the tool side. After the
  fix, about 5.7 million and then 3.8 million further inputs ran with no
  failures.
