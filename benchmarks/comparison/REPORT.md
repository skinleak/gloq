# gloq performance investigation — 2026-09-22

## Conclusion

Keep the slog architecture and the current JSON handler. The highest-value next
optimization is **kind-aware special-value detection in the attr pipeline**:
avoid converting ordinary typed values to `any` merely to check for an error or
TRACE stack. Allocation profiles identify those checks directly, in both
formats. No production optimization was implemented in this investigation.

gloq's ordinary JSON path is materially slower than both comparators on this
machine. That gap is not predominantly an unavoidable `slog.Logger` tax.
The stdlib JSON callback path and gloq's normalization both contribute. Native
error and TRACE timings include richer semantics and must not be marketed as
equivalent-workload comparisons.

## Checkout, environment, and tooling

- HEAD: `af7acad8cf2b2e17cdb7709c7c10a62dbbfbc05d`. Initial worktree and diff
  were clean. The existing `gloq_bench_test.go` is tracked.
- Linux/amd64, **Go 1.26.0**, GOAMD64=v1, CGO_ENABLED=1, no GOFLAGS override.
  Both modules declare Go 1.21. This investigation did not run Go 1.21 itself.
- CPU: **12th Gen Intel Core i5-12600KF**, 10 physical cores / 16 logical CPUs.
  Affinity allowed CPUs 0–15; no explicit pinning. `intel_pstate`, `powersave`
  governor observed. Frequency/turbo and background system activity were not
  controlled; no machine-wide settings were changed.
- Zap: **v1.28.0**, verified as the latest stable release through the Go module
  proxy during the investigation. Its [module declares Go 1.19](https://github.com/uber-go/zap/blob/v1.28.0/go.mod).
  It and indirect `multierr v1.10.0` are isolated in this benchmark module.
  The root module and runtime dependency set are unchanged.
- benchstat: `golang.org/x/perf` at
  `v0.0.0-20260908200009-22c9c6c9d4da`, installed outside either module.
  The environment lacked the `go tool pprof` executable; `cmd/pprof` was built
  from the installed Go 1.26.0 source and used instead.
- Existing ignored artifacts were preserved: root `gloq.test` (ELF test
  binary), `benchmarks/performance-before.md`, and `todo.txt`.

Initial root tests, race tests, vet, and whitespace checks all passed before
changes. README, media, examples, GitHub metadata, and production Go files
were not modified.

## Methodology and fairness

The [benchmark README](README.md) and [runner](run.sh) specify reproducible
commands. Serial measurements used `-benchmem -benchtime=200ms -count=6 -cpu=1`.
The existing root suite was rerun with the same settings. Parallel comparison
used `-cpu=1,4,16`. Suites ran sequentially, not against each other concurrently.
Tables report **medians** from benchstat, not the fastest sample.

All comparisons use `io.Discard`, the same message and field values, explicit
caller settings, INFO minimum, timestamps, newline-delimited JSON, and
serialized final writes. Zap uses RFC3339Nano time and nanosecond durations to
match slog's JSON intent, without sampling or automatic error stacks. Typed
fields are built outside the timer for all implementations. Alternating
arguments and Zap Sugar are measured separately. Construction, binding, and
group creation are excluded from steady-state timings.

`TestJSONWorkloads` decodes mixed string/int/bool/float/duration output and
checks nesting, built-ins, and newline. `TestConfigurations` checks disabled
Debug and application caller attribution for all three logger configurations.

Important limitations:

- Source output differs: gloq/slog emit function/file/line, Zap emits file:line.
- Native errors differ: gloq emits structured cause/type trees; the other two
  stringify these particular stdlib errors. Zap's other error interfaces are
  not exercised. These rows are **not apples-to-apples**.
- Numeric TRACE exists for gloq/slog, not Zap. Package TRACE additionally
  captures a stack and has no equivalent competitor row.
- Typed-field reuse does not include call-site field-construction cost.
  Scaling changes value mix and output length as well as field count.
- Handler-only controls freeze time and PC; the counting handler does not
  encode anything. Neither is a production competitor score.
- `-cpu=1` is GOMAXPROCS, not CPU affinity. Six short samples establish large
  gaps, not universal nanosecond rankings. Longer, interleaved, pinned-core
  runs would be appropriate for a small optimization decision.
- Serial JSON benchstat intervals were generally about 1–6%; the identity
  handler-only control reached 11%, pretty zero-field scaling 16%, and
  four-way parallel slog 27%. Close differences should not be interpreted as
  wins. Parallel ns/op is aggregate throughput, **not single-call latency**.

Raw samples and full summaries are retained locally in ignored `results/`:
`comparison.txt`, `scaling.txt`, `parallel.txt`, `gloq.txt`, and corresponding
`*-summary.txt`. Profiles and their text summaries are in `results/profiles/`.
They are generated evidence, not committed binaries or module dependencies.
Rerunning the script overwrites named outputs; archive a run before comparison.

## Comparative results

Each cell is **ns/op / B/op / allocs/op**. Ordinary rows use typed JSON fields
unless labeled otherwise.

| Workload | gloq | slog | Zap |
| --- | ---: | ---: | ---: |
| Disabled Debug | 3.918 / 0 / 0 | 3.882 / 0 / 0 | 3.982 / 0 / 0 |
| Disabled-level eligibility check | 2.709 / 0 / 0 | 2.683 / 0 / 0 | 2.029 / 0 / 0 |
| Disabled raw TRACE | 4.194 / 0 / 0 | 4.174 / 0 / 0 | N/A |
| Static message, 0 fields | 916.7 / 88 / 5 | 327.4 / 0 / 0 | 206.9 / 0 / 0 |
| 1 field | 1069 / 120 / 7 | 393.6 / 0 / 0 | 232.7 / 0 / 0 |
| 3 fields | 1282 / 120 / 7 | 484.1 / 0 / 0 | 284.8 / 0 / 0 |
| 6 mixed fields | 1788 / 240 / 15 | 866.7 / 56 / 2 | 397.8 / 0 / 0 |
| 10 mixed fields | 2522 / 440 / 20 | 1312 / 224 / 3 | 518.1 / 0 / 0 |
| 3 alternating args / Zap Sugar | 1317 / 120 / 7 | 518.8 / 0 / 0 | 562.8 / 384 / 1 |
| 3 pre-bound fields, no event fields | 951.2 / 88 / 5 | 331.0 / 0 / 0 | 206.0 / 0 / 0 |
| Nested groups, 3 fields | 1336 / 120 / 7 | 505.6 / 0 / 0 | 277.6 / 0 / 0 |
| Caller enabled, 3 fields (different metadata) | 2294 / 768 / 17 | 1279 / 584 / 6 | 893.1 / 320 / 2 |
| Plain native error (different output) | 1397 / 192 / 7 | 408.6 / 0 / 0 | 245.1 / 0 / 0 |
| Wrapped native error (different output) | 1733 / 304 / 10 | 433.9 / 0 / 0 | 269.3 / 0 / 0 |
| Joined native error (different output) | 2063 / 568 / 14 | 535.4 / 72 / 2 | 394.2 / 72 / 2 |
| Parallel, GOMAXPROCS=4 | 388.2 / 120 / 7 | 295.6 / 0 / 0 | 81.65 / 0 / 0 |
| Parallel, GOMAXPROCS=16 | 227.3 / 121 / 7 | 209.0 / 0 / 0 | 138.7 / 0 / 0 |

The three-field JSON path is approximately 2.65× slog's time and 4.50× Zap's
time in this configuration. Disabled calls are all effectively in the same
single-digit, zero-allocation class. Zap's eligibility row calls
`Core.Enabled`, not the fuller `Logger.Check` operation.

Parallel scaling is not monotonic: Zap at 16 is slower than at 4 despite
identical encoding. Contention, hybrid-core scheduling, and runtime overhead
can affect throughput. These CPU profiles are serial; no mutex/block profile
was taken, so this run does not assign a precise cause to that parallel result.

## Attribute scaling

Typed cells show **ns / bytes / allocations**. Alternating-argument cells show
ns; their measured bytes and allocation counts equal the typed case in every
row. Prefix values repeat string, int, bool, float, duration.

| Attrs | Pretty typed | Pretty args ns | JSON typed | JSON args ns |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 420.2 / 64 / 1 | 408.1 | 924.2 / 88 / 5 | 905.8 |
| 1 | 551.2 / 96 / 3 | 540.6 | 1067 / 120 / 7 | 1068 |
| 2 | 639.5 / 224 / 4 | 674.1 | 1187 / 120 / 7 | 1203 |
| 3 | 694.6 / 224 / 4 | 732.5 | 1274 / 120 / 7 | 1326 |
| 4 | 801.7 / 240 / 6 | 870.2 | 1519 / 144 / 10 | 1567 |
| 5 | 888.6 / 256 / 8 | 952.5 | 1619 / 160 / 12 | 1687 |
| 6 | 1036 / 336 / 11 | 1103 | 1806 / 240 / 15 | 1869 |
| 8 | 1218 / 672 / 12 | 1296 | 2113 / 320 / 15 | 2139 |
| 10 | 1436 / 784 / 16 | 1543 | 2536 / 440 / 20 | 2650 |
| 16 | 2125 / 1632 / 25 | 2276 | 3523 / 784 / 29 | 3764 |
| 32 | 3616 / 2528 / 43 | 3870 | 6163 / 1704 / 50 | 6357 |

Allocation transitions are explainable, not one universal six-attr cliff:

1. **Record storage:** Go 1.26 `slog.Record` holds five attrs inline. The
   sixth causes backing storage allocation. `BenchmarkRecord` measures
   0 B/0 allocations through five; 48 B/1 at six; 208 B/1 at ten; 1152 B/1
   at 32. `Record.AddAttrs` / `slices.Grow` appear at those sites in profiles.
2. **Primitive boxing:** `gloq.go:363,367` and `attrs.go:45,48` call `Value.Any`
   for special-value checks. String, float, duration and time conversions
   allocate in these workloads. The chosen small int and bool do not; that
   is not a guarantee for all integers. These costs remain with typed attrs.
3. **Pretty buffer growth:** `Grow(64)` is followed by larger allocations as
   output crosses capacity. The second attr already crosses a boundary for
   these keys/message; another is crossed by eight. It is record-length
   dependent, not an API threshold. At six: 192 B of builder storage, 96 B
   from `Any`, 48 B from Record = 336 B. At ten: 448 + 128 + 208 = 784 B.
4. **Stdlib JSON float path:** Go 1.26's `appendJSONValue` delegates floats
   through `encoding/json`, adding a boxed float allocation. That contributes
   at four attrs and again at nine. An identity callback also routes the
   built-in level through `Level.MarshalJSON`, unlike the nil-callback path.

The original six-attr root benchmark still measures **336 B / 9 allocations**.
The new mixed six-attr case is **336 B / 11** because its types differ
(float/duration replace some string/integer fields). That is not a regression.

## Existing gloq suite: fresh baseline

These are measurements of unchanged production code, not after-optimization
claims. Source is disabled unless identified. Values are ns / B / allocations.

| Root benchmark | Result |
| --- | ---: |
| Disabled package Debug / Trace | 3.669 / 0 / 0; 3.514 / 0 / 0 |
| Disabled logger Debug / Trace | 3.940 / 0 / 0; 3.782 / 0 / 0 |
| Pretty Info / Success / Warn / Error | 419.9 / 64 / 1; 415.0 / 64 / 1; 420.3 / 64 / 1; 414.8 / 64 / 1 |
| Pretty alternating 1 / 3 / 6 attrs | 576.4 / 96 / 3; 740.2 / 224 / 4; 1068 / 336 / 9 |
| Pretty LogAttrs, 3 attrs | 707.2 / 224 / 4 |
| Pretty bound attrs | 860.6 / 288 / 8 |
| Pretty single / nested group | 687.5 / 224 / 4; 707.1 / 224 / 4 |
| Pretty source off / on | 415.0 / 64 / 1; 601.7 / 320 / 4 |
| Formats Pretty / JSON | 754.6 / 224 / 4; 1364 / 120 / 7 |
| Pretty plain / wrapped / joined error | 629.9 / 224 / 3; 791.1 / 248 / 5; 1313 / 688 / 13 |
| JSON plain / wrapped / joined error | 1379 / 192 / 7; 1750 / 304 / 10; 2084 / 568 / 14 |
| Error stack, Pretty / JSON | 897.9 / 272 / 5; 1606 / 208 / 8 |
| TRACE helper / raw level | 2317 / 2232 / 17; 423.4 / 72 / 2 |
| Raw FATAL level | 412.2 / 64 / 1 |
| Source-enabled package / logger Info | 603.7 / 320 / 4; 596.4 / 320 / 4 |

The supplied historical baseline at `46e2a58` was not re-created in a separate
checkout. Some fresh times are lower; root JSON is higher (1364 versus the
supplied 1279 ns). Source, stack depth, CPU scheduling, Go/runtime details and
run conditions prevent attributing historical timing changes to this task.
There is **no implementation before/after delta**: no implementation changed.

## Profiling evidence

Ten targets were profiled: JSON 3/10 fields; pretty 6/10 fields; pretty bound
attrs; pretty and JSON source; joined errors in both formats; package TRACE.
CPU runs targeted 3 seconds per benchmark (roughly 4–6 seconds of total samples
including benchmark calibration). Separate processes recorded 100,000
iterations with `-test.memprofilerate=1` for allocations. Thus exhaustive
allocation instrumentation did not distort the CPU runs.

Figures below are approximate **cumulative** CPU percentages unless marked
flat. Nested entries overlap and must not be added. Allocation totals include
small test/setup overhead; benchmark B/op and allocs/op are the steady-state
measurements. Tiny-object accounting can make pprof object counts differ from
the benchmark allocation counter.

| Profile | CPU evidence | Allocation evidence |
| --- | --- | --- |
| `json3` | JSON Handle 82.7%; gloq callback 31.4%; `appendJSONMarshal` 14.9%; Callers 9.6%; Resolve 5.1% | ~112 of 120 B/op from `Value.Any` at the two gloq checks; remaining ~8 B from level JSON quoting |
| `json10` | JSON Handle 84.0%; gloq callback 33.8%; Record insertion 6.7%; Callers 5.9%; Resolve 5.2% | ~208 B `Any`, 208 B Record backing, 16 B float boxing, 8 B level quoting = 440 B |
| `pretty10` | appendAttr 49.8%; appendValue 15.2%; Record insertion 11.4%; `Any` 10.6%; allocation runtime 17.9% | 448 B builder storage, 128 B `Any`, 208 B Record |
| `bound` | appendAttr 40.7%; `Any` 12.8%; Callers 13.9%; time formatting 13.7% | 192 B builder storage and 96 B boxing = 288 B |
| `source` | Callers 16.8%; Frames.Next 9.5%; CallersFrames 7.6%; time formatting 20.1% | Caller-frame expression accounts for ~248 B, line formatting ~8 B, builder 64 B |
| `json-source` | Record.Source 10.5%; Source.group 5.9%; gloq callback 30.9%; Resolve 5.4% | Source and its group representation ~288 B each; primitive `Any` ~176 B; quoting/accounting remainder |
| `joined-pretty` | appendPrettyError 59.8%; fmt.Sprintf 19.8%; errorDisplayMessage 19.4%; writeErrorLine 17.6% | Main record builder ~448 B; joinError.Error 72 B; strings.Join 48 B; remaining cause/label/indent/error collection storage |
| `joined-json` | describeError 24.9%; JSON Encoder.Encode 26.5%; struct encoder 17.8%; fmt.Sprintf 10.5% | Cause-slice growth ~224 B, boxed structured root ~80 B, builtin boxing ~80 B, type strings ~72 B, joined message 72 B, filtered causes 32 B, level quoting ~8 B |
| `trace` | Callers 25.0%; Frames.Next 26.0%; captureTraceStack 40.6%; appendPrettyTraceStack 10.4%; pretty Handle 26.3% | ~75% of bytes in stack/output builder allocations; initial PC slice 256 B; stack attachment and other small allocations remain |

For the strongest allocation finding, this command points at the actual lines:

```sh
go tool pprof -alloc_space -list='attrPipeline.forJSON' \
  results/profiles/comparison.test results/profiles/json3.alloc
```

Each of `attrs.go:45` and `attrs.go:48` accounts for approximately 5.34 MiB
over the allocation run, despite neither assertion matching these primitive
values. `Value.Any` is a stdlib function, but **gloq chooses to call it**.
Classifying these allocations as an unavoidable slog tax would be incorrect.

### JSON architecture and the slog boundary

The boundary controls use the same three typed attrs:

| Configuration | Logger ns | Direct Handle ns | B / allocations |
| --- | ---: | ---: | ---: |
| Plain slog JSON | 467.1 | 286.8 | 0 / 0 |
| slog JSON + identity ReplaceAttr | 827.4 | 639.0 | 8 / 1 |
| gloq JSON | 1271 | 1059 | 120 / 7 |
| gloq pretty | 678.2 | 494.7 | 224 / 4 |

The local logger-to-handler gap is about **180–212 ns**: clock, caller capture,
record setup/insertion, Enabled and dispatch together. It is not pure interface
dispatch, and the fixed-record control is not a perfect algebraic subtraction.
The zero-attr counting handler measures 127 ns; five attrs 155 ns; six 192 ns;
ten 258 ns; 32 attrs 583 ns. These establish a frontend/storage scale, not an
encoder-independent invariant.

For three-field JSON, the frontend is roughly 14–17% of gloq's local total.
The callback-enabled slog path adds about 360 ns over plain slog, and gloq's
callback adds another roughly 444 ns over the identity callback. Those are
diagnostic deltas, not independently additive profile categories or guaranteed
optimization savings.

Go 1.26's `commonHandler.handle` has optimized nil-ReplaceAttr paths for time,
level and message. A callback routes them through generic Attr processing,
group bookkeeping, resolution, and (for unchanged slog.Level) JSON marshaling.
`handleState.appendAttr` resolves before and after the callback; gloq's
pipeline additionally resolves. Resolve is visible at ~5% even though these
fixtures contain no user LogValuer. Actual user LogValue execution cost is not
measured here, and its bounded/panic-safe semantics must remain intact.

**A custom gloq JSON slog.Handler is not justified as the next step.** It could
avoid callback machinery, generic level marshaling, some Source/group object
construction, and permit tailored error encoding while retaining slog.Record.
But it would own escaping/invalid UTF-8, floats and non-finite values,
JSONMarshaler behavior, groups/empty groups, duplicate attrs, binding semantics,
LogValuer resolution, custom levels, source, errors, newline, writer failures,
and clone synchronization. That is substantial correctness and maintenance
cost. The profile does not establish a safe speedup for such an encoder.

Plain slog already handles these three fields in under 0.5 µs. First address
gloq's avoidable boxing. Sub-microsecond gloq JSON for this simple case is a
reasonable investigation target, **not proven achievable** by one change.
For ten mixed fields even plain slog measured 1.31 µs; a universal <1 µs promise
would not follow from this evidence. No architectural change is warranted now.

### Bound attrs, groups, source, and locks

The stdlib JSON handler preformats WithAttrs at binding time, as does Zap;
gloq JSON inherits that benefit. Its remaining bound-case cost is largely
builtins/callback work. The pretty handler instead retains bound attrs and
formats them per record. Its profile exposes repeated string boxing and
formatting. Caching them would require care around current transform and
LogValuer evaluation timing; do not silently alter that behavior.

The nested comparison uses WithGroup / Zap namespaces, not arbitrary deeply
nested event group trees. In pretty code, named event-group attrs copy group
paths in appendAttr; that is a separate unquantified workload. No blanket
claim about arbitrary group performance follows from the namespace results.

Ordinary package helpers already check Enabled before caller work and use a
single-PC `runtime.Callers(3, ...)`. Only package TRACE captures a full stack.
The suspected ordinary full-stack inefficiency is **not present** at this HEAD.
Standard slog still captures its caller PC when output source is disabled.
Source formatting adds frame resolution in the handler. Do not replace
CallersFrames with naive PC lookup that could break inlined caller attribution.

Pretty derived handlers share the writer mutex and format outside it. JSON
uses slog's shared clone mutex. Nothing changed in that ownership or lock
scope. Serial CPU profiles do not show final writer locking among the leading
costs; that does not establish behavior with slow/contended real sinks.

### Error-tree costs and reliability

Pretty joined errors build both the original joined message and a comparison
string, then format count/cause labels and indentation into a growing record.
`errorDisplayMessage`'s temporary messages slice is present in source, but it
does not appear as a heap allocation in this two-cause profile. Do not infer
heap allocations merely from `make` syntax. Builder growth dominates bytes.

JSON builds structuredError cause slices incrementally before reflect-based
encoding. The cause slice grows twice for two children (~224 B total);
`describeError` then boxes the result, while `%T` creates type strings. These
are gloq-controlled representation costs, not slog.Record requirements.
Careful capacity sizing could be investigated later without weakening safety.
It is not this report's primary next optimization.

Existing nil checks, panic recovery around Error/Unwrap, depth limits,
multi-error handling, optional stack formatting, and tests remain untouched.
The code uses a depth bound of 32, **not identity-based cycle detection**.
Single-branch cyclic chains are covered. A depth bound alone does not limit
total nodes in a branching cyclic graph or stop recursion inside user Error
methods; this investigation does not claim those cases are solved. No safety
checks were removed to improve benchmark numbers.

### TRACE

Full-stack TRACE is about 2.32 µs / 2232 B / 17 allocations here, versus raw
TRACE severity at 423 ns / 72 B / 2 allocations. It is intentionally richer.
Roughly 25% CPU is caller capture, 26% frame resolution, 15% other work inside
captureTraceStack, 10% appending the formatted stack, and 16% other pretty
handler work; remaining sampling is surrounding/runtime overhead. Builder
growth and runtime allocation overlap those categories, rather than forming
additional percentages. Stack depth and path lengths affect both time and bytes.

The trace profile attributes ~75% of allocated bytes to builder storage,
including construction of the intermediate stack string and the final record.
This suggests future sizing opportunities, but does not justify truncating
stacks, dropping frame metadata, or introducing shared-buffer ownership risks.

## Ownership of costs and next phase

- **Controlled by gloq:** unnecessary Any boxing, redundant normalization,
  pretty buffer sizing and bound-attr processing, group-path construction,
  structured error representation, and TRACE string formatting.
- **Current slog frontend constraints:** timestamp/caller/record construction,
  inline-five attr storage and overflow, handler dispatch. These persist with
  a custom slog.Handler and are not all unique costs compared with other loggers.
- **Chosen stdlib JSON backend:** ReplaceAttr's generic path, JSON float/Any
  marshaling, Source/group representation, buffer pools and synchronization.
  These are not intrinsic requirements of every slog-compatible handler.
- **Runtime / semantic costs:** stack walking, frame resolution, allocation/GC,
  clock, and user-defined Error/Unwrap/LogValue work. Rich error trees and full
  stacks do work the simpler competitor rows do not perform.

**Next single task:** make error/TRACE-stack detection kind-aware in both attr
paths, preserving resolution/transform order and all existing output contracts.
Re-run this entire suite, not just JSON3, and conformance/race/fuzz tests.
The profile identifies up to 112 B of boxing per ordinary JSON3 record and
128 B per pretty ten-field record as the addressable allocation mechanism;
those are opportunities, not measured savings or a promised latency result.
Do not combine that task with an encoder rewrite, pooling, caller caching, or
changes to error safety.

## Changes and validation

Changes are limited to `.gitignore` allowing this comparison directory, and
new benchmark-module files: `.gitignore`, `go.mod`, `go.sum`,
`comparison_test.go`, `run.sh`, `README.md`, and this report. Existing ignored
benchmark notes remain ignored. No public API, production behavior, palette,
level, minimum Go version, root dependency, Fatal or TRACE semantic changed.
No new `sync.Pool`, unsafe code, commit, or push.

| Validation | Result |
| --- | --- |
| Root `go test ./...` | PASS |
| Root `go test -race ./...` | PASS |
| Root `go vet ./...` | PASS |
| Comparison module `go test ./...` | PASS |
| Comparison module `go test -race ./...` | PASS |
| Comparison module `go vet ./...` | PASS |
| Parallel comparison benchmarks under `-race`, 100 iterations, 4 CPUs | PASS; instrumented timings excluded from performance tables |
| Existing fuzz seed corpus | PASS via root tests |
| `FuzzPrettyHandler`, 5-second target, 2 workers | PASS; 122,154 executions, 51 new interesting inputs, no failures |
| `gofmt` / `sh -n run.sh` | PASS |
| `git diff --check` | PASS |

Root coverage measured **86.9%** with `go test -cover ./...` (example package
0%). The supplied 88.3% figure was not reproduced; this is the observed value,
not an asserted coverage regression caused by these benchmark-only changes.
The existing slogtest conformance, custom-level, caller, writer-error, dynamic
level, error robustness, Fatal subprocess, and concurrent-write tests all
remain part of the passing root suite. Existing GitHub CI was not changed;
it does not automatically traverse the nested benchmark module.

New comparison files are untracked until deliberately staged by the author.
Plain `git diff --stat` therefore shows only `.gitignore` (2 insertions,
1 deletion), not the new module. Generated raw results/profiles/test binaries
remain ignored under `benchmarks/comparison/results/`; the pre-existing root
`gloq.test`, benchmark note and `todo.txt` were neither deleted nor overwritten.
