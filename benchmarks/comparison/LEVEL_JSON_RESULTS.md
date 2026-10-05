# Built-in JSON level serialization — 2026-10-04

## Scope and result

Only `attrPipeline.forJSON` changes in production. Exact built-in levels now
return literal string attrs after resolution and transformations. Arbitrary
levels retain the existing `slog.Level` marshaling fallback. No API, filtering,
source, error-tree, pretty-formatting, locking, or dependency change.

Three-field INFO JSON improves from **1085.0 ns, 8 B, 1 allocation** to
**871.8 ns, 0 B, 0 allocations** per operation: **19.65% less time**.
This does not mean every JSON workload is allocation-free: negative levels,
custom levels, source metadata, floats, Record overflow, and errors still have
costs. Small cross-build timing regressions outside the changed path are
reported below rather than hidden.

The checkout was clean on **main**, HEAD **3696190**, which merged
`perf/benchmark-phase-2`. No branch switch, commit, or push was performed.

## Root cause and implementation

With ReplaceAttr enabled, stdlib `commonHandler.handle` constructs an Any attr
containing the typed `slog.Level`. Previously, gloq converted only exact TRACE,
SUCCESS, and FATAL to strings. Ordinary DEBUG/INFO/WARN/ERROR passed through
`appendJSONMarshal`, `encoding/json`, and `slog.Level.MarshalJSON`.
`MarshalJSON` calls `strconv.AppendQuote(nil, l.String())`: even when String
returns a literal, quoting allocates a fresh byte slice.

The fresh three-field allocation profile assigns **781.25 KiB (800,000 bytes)**
to this quoting call over 100,000 measured operations, confirming 8 B/op.

The replacement is a typed `slog.Level` assertion and an exact-value switch
for the seven built-ins. Each returns `slog.String("level", literal)`, allowing
JSONHandler to append/escape it into its existing buffer. No new encoder,
pool, reflection, formatting helper, or unsafe code is introduced.

Ordering and fallback contracts remain intact:

- Transforms still see the original typed level, and run before normalization.
- Values returned by LogValuer or transforms are resolved before inspection.
- Renaming the key away from `level` still bypasses gloq normalization.
- Primitive integers/strings named `level` are not treated as slog.Level.
- Dropping/replacing the attr and JSON escaping remain stdlib behavior.
- Grouped and bound attrs named `level` retain their existing normalization.
- Arbitrary typed levels keep stdlib offset names, not a nearest gloq name:
  -5 is DEBUG-1, 1 is INFO+1, 13 is ERROR+5, and 100 is ERROR+92.

## Methodology

- Linux/amd64, **Go 1.26.0**, Intel Core i5-12600KF, 16 logical CPUs.
- Both modules still declare Go 1.21; this run does not validate Go 1.21 itself.
- Existing comparison module, unchanged Zap **v1.28.0**; root remains
  dependency-free. No module files changed.
- Existing benchmarks use io.Discard, setup outside timing, ReportAllocs,
  explicit source settings and prebuilt typed fields for the comparison suite.
- Fresh saved before/after binaries; six samples at 200 ms, GOMAXPROCS=1.
  Existing root and comparison parallel cases also run at GOMAXPROCS=4,16.
- Serial suites run sequentially, without other benchmark jobs. No initial
  affinity pinning or machine-wide frequency changes. One-time benchstat
  installation overlapped the tail of the baseline collection; control
  results and follow-up measurements should be considered with this caveat.
- benchstat version `v0.0.0-20260908200009-22c9c6c9d4da`, installed in /tmp,
  matching the prior report. Tables show medians, not best samples.
- Allocation profiles: 100,000 iterations, memprofilerate=1. CPU profile:
  benchtime=3s, default allocation sampling. Instrumented timings are not
  used in the comparison tables. pprof/objdump built from installed Go source.
- Raw samples, binaries, profiles, disassembly and summaries are preserved
  locally under ignored `results/level-json-20261004/`. Prior reports and
  artifacts are untouched, including the pre-existing root `gloq.test`.

Representative commands, run separately for each saved revision:

```sh
./root-before.test -test.run='^$' -test.bench=. -test.benchmem -test.benchtime=200ms -test.count=6 -test.cpu=1
./comparison-before.test -test.run='^$' -test.bench='Benchmark(JSON|ContextFields|NativeErrors)$' -test.benchmem -test.benchtime=200ms -test.count=6 -test.cpu=1
benchstat comparison-before.txt comparison-after.txt
./comparison-after.test -test.run='^$' -test.bench='^BenchmarkJSON$/^Fields3$/^Gloq$' -test.benchtime=100000x -test.cpu=1 -test.memprofilerate=1 -test.memprofile=json-after.alloc
./comparison-after.test -test.run='^$' -test.bench='^BenchmarkJSON$/^Fields3$/^Gloq$' -test.benchtime=3s -test.cpu=1 -test.cpuprofile=json-after.cpu
```

## Common JSON results

All timing reductions in this table have benchstat p=0.002, n=6 per revision.
Allocation reductions are consistent across all samples. B/A means bytes and
allocations per operation, not a before/after ratio.

| Workload | Before ns/op | After ns/op | Time change | Before B/A | After B/A |
|---|---:|---:|---:|---:|---:|
| No fields | 794.4 | 577.0 | -27.37% | 8 / 1 | 0 / 0 |
| 1 field | 902.0 | 678.6 | -24.77% | 8 / 1 | 0 / 0 |
| 3 fields | 1085.0 | 871.8 | -19.65% | 8 / 1 | 0 / 0 |
| 6 mixed fields | 1537 | 1370 | -10.87% | 64 / 3 | 56 / 2 |
| 10 mixed fields | 2213 | 1990 | -10.05% | 232 / 4 | 224 / 3 |
| 3 bound fields | 797.9 | 583.5 | -26.87% | 8 / 1 | 0 / 0 |
| Nested groups | 1133.5 | 925.0 | -18.39% | 8 / 1 | 0 / 0 |
| Source enabled | 2126 | 1877 | -11.69% | 592 / 7 | 584 / 6 |
| Plain error | 1318 | 1097 | -16.74% | 112 / 3 | 104 / 2 |
| Wrapped error | 1646 | 1481 | -10.05% | 224 / 6 | 216 / 5 |
| Joined errors | 2050 | 1810 | -11.71% | 488 / 10 | 480 / 9 |

The unchanged slog three-field control is 481.2 → 484.8 ns (+0.75%); Zap is
287.1 → 292.8 ns (+2.00%). Both remain 0 B/0 allocs. Controls exhibit small
timing shifts, so small percentage changes should not be overinterpreted.
Gloq is still slower than these controls despite matching their allocations.
Native error trees and source payloads are richer than some competitor
outputs; those rows are before/after gloq comparisons, not competitor rankings.

## Enabled levels (no fields, source disabled)

TRACE and FATAL here are raw slog records, not the special package helpers.
DEBUG/INFO/WARN/ERROR improvements have p=0.002; small shifts on the other
levels are not treated as meaningful optimization wins.

| Level | Before ns/op | After ns/op | Time change | Before B/A | After B/A |
|---|---:|---:|---:|---:|---:|
| TRACE -8 | 601.5 | 590.4 | -1.85% | 8 / 1 | 8 / 1 |
| DEBUG -4 | 809.1 | 592.9 | -26.72% | 16 / 2 | 8 / 1 |
| INFO 0 | 790.8 | 564.9 | -28.57% | 8 / 1 | 0 / 0 |
| SUCCESS 2 | 571.7 | 565.8 | -1.03%, p=0.093 | 0 / 0 | 0 / 0 |
| WARN 4 | 788.2 | 570.3 | -27.65% | 8 / 1 | 0 / 0 |
| ERROR 8 | 801.1 | 567.5 | -29.16% | 8 / 1 | 0 / 0 |
| FATAL 12 | 565.3 | 565.1 | -0.04%, p=0.848 | 0 / 0 | 0 / 0 |
| Custom -5 | 885.6 | 885.8 | +0.02%, p=1.000 | 32 / 4 | 32 / 4 |
| Custom 1 | 862.7 | 859.2 | -0.40%, p=0.368 | 16 / 2 | 16 / 2 |
| Custom 13 | 874.4 | 869.9 | -0.52% | 16 / 2 | 16 / 2 |
| Custom 100 | 897.2 | 885.3 | -1.33% | 24 / 2 | 24 / 2 |

DEBUG still allocates 8 bytes before ReplaceAttr: its post-change allocation
profile assigns 781.25 KiB to `commonHandler.handle` at
`state.appendAttr(Any(key, val))` over 100,000 records. Negative levels cannot
use runtime.convT64's small nonnegative integer cache. TRACE already bypassed
MarshalJSON and has the same 8 B/1 allocation. The callback cannot undo this
prior boxing; changing levels or the stdlib handler to avoid it is out of scope.
Custom levels retain String offset construction and quoting costs; sufficiently
large positive levels can also require boxing.

## Other paths and timing regressions

Pretty allocation counts are unchanged. Representative timings:

| Root workload | Before ns/op | After ns/op | Observation |
|---|---:|---:|---|
| Pretty Info | 418.6 | 425.0 | p=0.065, no significant difference |
| Pretty 1 attr | 516.8 | 531.4 | +2.82%, p=0.002 |
| Pretty 3 attrs | 686.9 | 678.6 | p=0.074 |
| Pretty 6 attrs | 923.0 | 918.4 | p=0.310 |
| Pretty bound attrs | 704.6 | 701.7 | -0.42%, not a material win |
| Pretty nested groups | 647.8 | 649.8 | p=0.197 |
| Pretty source | 602.1 | 601.9 | p=0.615 |
| Pretty plain error | 628.9 | 632.6 | +0.60%, p=0.002 |
| Pretty wrapped error | 790.9 | 794.2 | p=0.132 |
| Pretty joined errors | 1328 | 1328 | p=0.829 |
| Full-stack package Trace | 2427 | 2418 | p=0.100 |

Initial disabled medians were PackageDebug 3.523 → 3.814 ns, PackageTrace
3.494 → 3.805 ns, LoggerDebug 3.751 → 3.868 ns, LoggerTrace 3.969 → 4.236 ns.
These are statistically detectable cross-build increases, not proven noise.
All remain single-digit ns and 0 B/0 allocs.

Follow-up: six interleaved before/after samples, pinned to CPU 2. A second
baseline binary used a Go overlay of HEAD's attrs.go with the exact same new
tests as the final binary, eliminating the extra grouped test as a confound.
Matched-test results (200 ms, all p=0.002):

| Disabled workload | Before ns/op | After ns/op | Change |
|---|---:|---:|---:|
| Package Debug | 3.603 | 3.827 | +6.23% |
| Package Trace | 3.596 | 3.850 | +7.08% |
| Logger Debug | 3.973 | 3.898 | -1.90% |
| Logger Trace | 3.787 | 4.279 | +13.01% |

The disabled path never invokes the modified callback. Disassembly of gloq.log
shows the same instruction sequence with relocated addresses, including the
same Enabled check and early return. Binary-layout sensitivity is a plausible
explanation, not a proven root cause. Do not claim disabled timing is identical
or hide the observed regression. No padding, inlining directives, or unrelated
helper optimization was added to chase sub-nanosecond measurements.

Parallel ns/op is aggregate throughput, not single-call latency:

| Workload / GOMAXPROCS | Before ns/op | After ns/op | Timing observation |
|---|---:|---:|---|
| Root pretty / 4 | 138.4 | 144.3 | +4.30%, p=0.002 |
| Root pretty / 16 | 144.6 | 149.0 | +3.01%, p=0.002 |
| Root JSON / 4 | 323.2 | 282.5 | -12.59%, p=0.002 |
| Root JSON / 16 | 157.8 | 151.5 | p=0.093 |
| Comparison JSON / 4 | 386.3 | 351.9 | -8.93%, p=0.026 |
| Comparison JSON / 16 | 195.7 | 194.9 | p=0.394 |

Pretty remains 64 B/1 allocation; both JSON scenarios fall from 8 B/1 to
0 B/0. Parallel JSON timing intervals are wide (up to about 20%). Do not claim
a 16-worker throughput improvement. Pretty slowdowns are reported despite
no change to pretty code, synchronization, or allocations.

## Post-change profiles and remaining work

Three-field allocation profiling has **no Level.MarshalJSON samples** and no
replacement per-record allocation. Total profiled bytes fall from 897.30 KiB
to 114.03 KiB; the latter contains benchmark regex/setup, timezone initialization
and initial stdlib buffer/pool allocations, not 100,000 per-record allocations.
The measured loop reports 0 B/0 allocations even with every allocation sampled.

CPU profile: 4.49 seconds of samples. Cumulative percentages overlap and must
not be added:

- stdlib `handleState.appendAttr`: 68.82% cumulative, 10.02% flat.
- gloq JSON callback closure: 33.63% cumulative, 7.80% flat.
- `attrPipeline.forJSON`: 25.84% cumulative, 9.13% flat.
- `attrPipeline.apply`: 15.37% cumulative, 10.47% flat.
- `Value.Resolve` across the pipeline: 8.69% cumulative.
- `runtime.Callers`: 11.80% cumulative, still used by slog's frontend even
  with output source disabled.
- RFC3339 time formatting: 6.90% cumulative.

Remaining mixed-field allocations match the unchanged slog control: 56 B/2
for six and 224 B/3 for ten fields. Record storage beyond five attrs and the
stdlib float encoding path remain; source metadata and structured error trees
also retain their separate allocations. None were optimized in this task.

**One next target:** measure and reduce redundant resolved-attr processing in
the JSON callback entry path. Stdlib resolves attrs before ReplaceAttr, while
gloq calls apply/Resolve again. Investigate an already-resolved entry path,
preserving transform-produced LogValuer resolution and all drop/group semantics.
The profile supports investigating this localized cost before any custom JSON
handler. No architectural rewrite is justified by this result.

## Tests, validation, and files

New `json_level_test.go` contains:

- TestJSONLevelSerialization: exact JSON including key/order/newline for seven
  built-ins and four custom levels.
- TestJSONLevelTransforms: original typed callback input, typed replacement,
  custom level, LogValuer, renamed key, escaped string, integer, removed attr.
- TestJSONGroupedLevelAttrs: level attrs under WithGroup/WithAttrs and nested
  record groups.
- TestJSONDynamicLevels: LevelVar thresholds WARN/SUCCESS/TRACE/100 across all
  eleven test levels.
- BenchmarkJSONLevels: eleven enabled raw levels, source off, no timed setup.

The new tests pass before and after the production change. Initial and final
root tests, race tests, vet, and diff checks pass. Final validation:

- `gofmt -w attrs.go json_level_test.go`: PASS.
- Root `go test -count=1 ./...`: PASS, including existing fuzz seeds and
  source/custom-level/error/LogValuer/concurrency tests.
- Root `go test -race -count=1 ./...`: PASS.
- Root `go vet ./...`: PASS.
- Explicit `TestPrettyHandlerConformance` and `FuzzPrettyHandler` seeds: PASS.
- Short `FuzzPrettyHandler`, 3s, two workers: PASS, 182,964 executions.
- Comparison module tests, race tests and vet: PASS.
- `git diff --check`: PASS.

Tracked `git diff --stat`:

```text
 attrs.go | 28 ++++++++++++++++++++--------
 1 file changed, 20 insertions(+), 8 deletions(-)
```

New untracked files (not included in plain diff --stat): `json_level_test.go`
and this report. Generated evidence stays in the ignored results directory.
No existing user files were removed or unrelated changes made.
