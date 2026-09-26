# Logging performance comparison

This is a separate Go module for measuring gloq, standard-library `log/slog`,
and Uber Zap. The root library remains dependency-free. The local `replace`
directive measures the working checkout, not a published gloq release.
Zap is pinned to **v1.28.0**, whose Go 1.19 minimum is compatible with gloq's
Go 1.21 minimum. Upgrading Zap is an explicit benchmark-maintenance change.

## Running

From this directory:

```sh
go test ./...
go test -race ./...
go vet ./...
sh run.sh measure
sh run.sh profile
```

The root `go test ./...` deliberately does not traverse this nested module.
Validate both modules when changing these benchmarks. Existing root tests and
benchmarks remain in place. No benchmark dependency is imported by gloq.

The measurement script runs workloads **sequentially**, with six samples at
200 ms per benchmark and `-cpu=1` for serial workloads. Parallel workloads run
at `-cpu=1,4,16`; adjust these values for other machines. Results, environment
metadata, profiles, and test binaries go into ignored `results/`.
Repeated runs overwrite that directory's named outputs; archive it first when
comparing revisions. Increase duration or sample count for close comparisons:

```sh
COUNT=10 BENCHTIME=1s sh run.sh measure
benchstat results/comparison.txt
```

`benchstat` is optional and is not a module dependency. Set `BENCHSTAT` to its
executable path for automatic summaries. CPU profiling defaults to 3 seconds;
allocation profiling runs separately for 100,000 iterations with every
allocation sampled. Override `CPU_TIME` and `ALLOC_ITERS` if needed. Use
`go tool pprof`; on Go installations without that bundled executable, build
`cmd/pprof` from the matching Go source and set `PPROF` to its executable path.
For example:

```sh
go tool pprof -alloc_space -list='attrPipeline.forJSON' results/profiles/comparison.test results/profiles/json3.alloc
```

Run on an otherwise idle machine. Record Go version, CPU, affinity, frequency
policy, and environment when comparing revisions. `-cpu=1` sets GOMAXPROCS;
it does **not** pin the process to a physical core. Hybrid CPUs and scheduler
migration can cause noise. Do not run different suites simultaneously.

## Workloads and fairness

- `BenchmarkJSON`: typed, prebuilt fields at 0/1/3/6/10 fields. Same message,
  keys, values, INFO threshold, `io.Discard`, source disabled, newline, and
  RFC3339Nano timestamps. Durations encode as integer nanoseconds. Fixtures
  cycle through string, int, bool, float, duration. Logger and field
  construction are excluded. This measures reuse of typed fields, not their
  construction at the call site.
- `BenchmarkAlternating`: three alternating key/value arguments for gloq,
  slog, and Zap's sugared API. Do not confuse it with the typed Zap comparison.
- `BenchmarkDisabled`: disabled Debug, raw numeric TRACE for slog-compatible
  loggers, and eligibility checks. Zap has no TRACE equivalent; it is omitted.
  Zap's `Core.Enabled` is a core-level check, not a full `Logger.Check`.
- `BenchmarkContextFields`: three pre-bound fields, two nested groups, or
  caller-enabled JSON. Zap namespaces match the nested JSON object intent.
  **Caller payloads differ:** slog/gloq include function/file/line; Zap emits
  file:line. All resolve the application caller, but not identical metadata.
- `BenchmarkNativeErrors`: stdlib plain, wrapped, and joined errors.
  **Not equivalent output:** gloq emits structured type/message/cause trees;
  slog and Zap emit message strings for these particular errors. No library
  gets automatic stack capture in this suite.
- `BenchmarkParallelJSON`: three typed fields, shared logger, serialized
  writes for every library. `ns/op` is **aggregate throughput**, not per-call
  latency. Discard does not model a contended file, pipe, or network sink.
- `BenchmarkScaling`: gloq pretty and JSON with typed and alternating attrs
  at 0/1/2/3/4/5/6/8/10/16/32. Changing field count also changes value mix and
  record length; allocation profiles are required to explain transitions.
- `BenchmarkBoundary`: logger versus preconstructed-record handler calls,
  plus identity `ReplaceAttr`. These are diagnostic controls, not competing
  production configurations. Handler-only excludes filtering, clock, caller
  capture, record construction, and attr insertion; time and PC are fixed.
- `BenchmarkRecord`: an intentionally minimal counting handler measures
  frontend/Record costs without encoding. It is not a general-purpose handler
  and must not be ranked against real loggers.

All actual logger configurations include a timestamp and level. Zap has no
sampling, automatic error stacks, or development-mode behavior. Its sink is
locked to match slog's serialized final writes. Colors are explicitly disabled
for pretty scaling. JSON intent is equivalent for ordinary fields, not
byte-identical escaping or all possible custom values. `TestJSONWorkloads`
decodes output to verify the mixed field values, nesting, built-ins, and newline.

The existing root suite supplies package-helper/direct-logger, source on/off,
pretty errors, and full-stack TRACE measurements. Package TRACE is a richer
diagnostic operation than raw TRACE severity; Fatal helpers are never
benchmarked because they exit the process.

See [the investigation report](REPORT.md) for measurements and profile evidence.
