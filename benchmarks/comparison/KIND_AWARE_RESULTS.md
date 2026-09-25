# Kind-aware attribute detection — 2026-09-25

## Scope and result

Only two production functions changed: `attrPipeline.forJSON` in `attrs.go`
and `appendAttr` in `gloq.go`. Each now inspects special values only when the
resolved, transformed value is `slog.KindAny`, and calls `Any()` once for that
inspection. No helper, extra Resolve call, public API, dependency, output-format,
caller, buffer, synchronization, or error-traversal change was introduced.

**JSON with three typed fields: 120 B / 7 allocations → 8 B / 1 allocation.**
The entire expected 112 B/op boxing source was eliminated: 93.3% fewer bytes
and 85.7% fewer allocations. Median time was 1207 → 1030 ns (-14.7%).
No custom JSON handler or additional optimization was implemented.

Initial branch: `perf/benchmark-phase-2`. The pre-existing modified
`.gitignore` and untracked comparison suite were preserved. Original
`REPORT.md`, benchmark code/fixtures, README/media and existing generated
artifacts were not overwritten. Baseline measurements were collected before
changing production code.

## Why the change preserves semantics

`slog.Value` stores primitive kinds without needing an interface box.
`Value.Any()` reconstructs a Go value in an interface; conversions for strings,
time, float and duration can allocate even if an immediately following type
assertion fails. The former two unconditional checks incurred that cost for
ordinary fields and JSON built-ins.

- Bool, Duration, Float64, Int64, String, Time and Uint64 now bypass special
  type assertions. Their actual formatters and values are unchanged.
- KindAny still handles ordinary custom values, nil, typed-nil errors, errors,
  named TRACE strings and slog.Level. A named string implementing error remains
  KindAny, not KindString.
- LogValuer is resolved at exactly the existing points, including after each
  transform. Resolve guarantees a non-LogValuer result, including a KindAny
  error on excessive recursion. The new check uses that final kind, never the
  original LogValuer kind.
- Existing pretty group traversal and stdlib JSON group traversal are
  unchanged. Nested attrs reach their usual resolution and special handling.
- Level replacement still requires an actual slog.Level value: primitive
  integer 2 under a key named "level" is not renamed SUCCESS.
- Error traversal, nil handling, panic recovery, depth bounds, optional stacks,
  attr ordering, writer locking, WithAttrs/WithGroup and cloning are unchanged.
  Existing cycle protection is still depth-based, not a new graph algorithm.

## Correctness tests

New `attrs_test.go` adds:

- `TestAttributeKinds`: exact pretty output and decoded JSON structure for
  seven primitive kinds, ordinary KindAny, nil, plain error, named-string error,
  typed-nil error, TRACE metadata, and nested groups containing an error.
  Exercises direct attrs, resolved LogValuers, bound LogValuers, and transforms
  returning LogValuers. Counts LogValue calls to protect resolution behavior.
- `TestJSONLevelAttrKinds`: distinguishes primitive level-key values from
  typed and LogValuer-produced gloq levels.
- `TestGroupedLogValuerErrors`: bound error-producing LogValuer plus nested
  group-producing and recursive LogValuers under WithGroup in both formats.

These tests passed against the original implementation before the optimization.
The existing error tests also passed: wrapped/joined errors, typed nils, cyclic
unwrap, deep chains, panicking Error/Unwrap, multiple attrs and optional stacks.
Existing source, custom-level, conformance and concurrency tests remain intact.

## Measurement method

Go **1.26.0**, linux/amd64, Intel i5-12600KF. Same existing comparison module,
Zap **v1.28.0**, fixture values and io.Discard writers throughout.
Serial: six samples, 200 ms per benchmark, GOMAXPROCS=1.
Parallel: six samples at GOMAXPROCS=4 and 16.
Suites ran sequentially. No Go code or fixture changed between production
baseline and final measurements except the scoped optimization and new tests.

benchstat from the prior investigation was reused. Tables report medians;
`~` means no statistically significant change in that batch (p >= .05).
Percentages are not universal guarantees. Unchanged slog/Zap controls generally
remained stable, but some had several-percent timing shifts. Small changes,
especially in parallel runs, should not be causally attributed to this patch.

Raw samples, saved before/after test binaries, benchstat summaries, disassembly,
and allocation profile are retained in ignored
`results/kind-aware-20260925/`. Prior investigation results are untouched.

Commands used for each before/after side (with isolated Go cache environment):

```sh
# Repository root:
go test -run='^$' -bench=. -benchmem -benchtime=200ms -count=6 -cpu=1 .
go test -run='^$' -bench='^BenchmarkParallel$' -benchmem -benchtime=200ms -count=6 -cpu=4,16 .
# benchmarks/comparison:
go test -run='^$' -bench='Benchmark(JSON|ContextFields|NativeErrors|Scaling)$' -benchmem -benchtime=200ms -count=6 -cpu=1 .
go test -run='^$' -bench='^BenchmarkParallelJSON$' -benchmem -benchtime=200ms -count=6 -cpu=4,16 .
# Compare corresponding before/after output files:
benchstat before.txt after.txt
```

## JSON results

These compare gloq before versus gloq after, not richer gloq errors versus a
competitor's string-only errors.

| Benchmark | Before: ns / B / allocs | After: ns / B / allocs | Time change |
| --- | ---: | ---: | ---: |
| JSON/Fields0/Gloq | 870.7 / 88 / 5 | 749.7 / 8 / 1 | -13.89% |
| JSON/Fields1/Gloq | 1038.0 / 120 / 7 | 838.2 / 8 / 1 | -19.25% |
| JSON/Fields3/Gloq | 1207.0 / 120 / 7 | 1029.5 / 8 / 1 | -14.71% |
| JSON/Fields6/Gloq | 1677.5 / 240 / 15 | 1454.5 / 64 / 3 | -13.29% |
| JSON/Fields10/Gloq | 2371.0 / 440 / 20 | 2066.0 / 232 / 4 | -12.86% |
| ContextFields/Bound3/Gloq | 914.5 / 88 / 5 | 766.0 / 8 / 1 | -16.24% |
| ContextFields/NestedGroups/Gloq | 1310.0 / 120 / 7 | 1080.0 / 8 / 1 | -17.56% |
| ContextFields/Source/Gloq | 2235.0 / 768 / 17 | 1972.0 / 592 / 7 | -11.77% |
| NativeErrors/Plain/Gloq | 1334.0 / 192 / 7 | 1246.0 / 112 / 3 | -6.60% |
| NativeErrors/Wrapped/Gloq | 1708.5 / 304 / 10 | 1622.0 / 224 / 6 | -5.06% |
| NativeErrors/Joined/Gloq | 2037.0 / 568 / 14 | 1910.5 / 488 / 10 | -6.21% |

## Root benchmark results

Includes all disabled, basic, attr, bound, group, source, error, TRACE, FATAL
severity and package/direct comparisons. Parallel rows here are GOMAXPROCS=1;
multi-CPU throughput is reported separately below.

| Benchmark | Before: ns / B / allocs | After: ns / B / allocs | Time change |
| --- | ---: | ---: | ---: |
| DisabledLogging/PackageDebug | 3.671 / 0 / 0 | 3.253 / 0 / 0 | -11.37% |
| DisabledLogging/PackageTrace | 3.362 / 0 / 0 | 3.276 / 0 / 0 | -2.54% |
| DisabledLogging/LoggerDebug | 3.743 / 0 / 0 | 3.526 / 0 / 0 | -5.78% |
| DisabledLogging/LoggerTrace | 3.573 / 0 / 0 | 3.678 / 0 / 0 | +2.92% |
| PrettyBasic/Info | 394.6 / 64 / 1 | 393.4 / 64 / 1 | ~ |
| PrettyBasic/Success | 396.5 / 64 / 1 | 397.8 / 64 / 1 | ~ |
| PrettyBasic/Warn | 402.9 / 64 / 1 | 398.3 / 64 / 1 | ~ |
| PrettyBasic/Error | 396.1 / 64 / 1 | 388.5 / 64 / 1 | ~ |
| PrettyAttributes/Alternating/1 | 554.0 / 96 / 3 | 489.9 / 64 / 1 | -11.59% |
| PrettyAttributes/Alternating/3 | 717.8 / 224 / 4 | 641.9 / 192 / 2 | -10.58% |
| PrettyAttributes/Alternating/6 | 1029.0 / 336 / 9 | 847.6 / 240 / 3 | -17.63% |
| PrettyAttributes/LogAttrs/3 | 673.8 / 224 / 4 | 603.8 / 192 / 2 | -10.38% |
| PrettyBoundAttrs | 819.7 / 288 / 8 | 659.4 / 192 / 2 | -19.56% |
| PrettyGroups/Single | 662.6 / 224 / 4 | 601.1 / 192 / 2 | -9.29% |
| PrettyGroups/Nested | 684.0 / 224 / 4 | 609.4 / 192 / 2 | -10.91% |
| PrettySource/Disabled | 394.1 / 64 / 1 | 400.0 / 64 / 1 | ~ |
| PrettySource/Enabled | 570.9 / 320 / 4 | 564.7 / 320 / 4 | ~ |
| Formats/Pretty | 714.8 / 224 / 4 | 633.4 / 192 / 2 | -11.39% |
| Formats/JSON | 1322.5 / 120 / 7 | 1075.0 / 8 / 1 | -18.71% |
| Errors/Pretty/Plain | 594.4 / 224 / 3 | 597.5 / 224 / 3 | ~ |
| Errors/Pretty/Wrapped | 762.8 / 248 / 5 | 755.6 / 248 / 5 | ~ |
| Errors/Pretty/Joined | 1274.0 / 688 / 13 | 1246.5 / 688 / 13 | -2.16% |
| Errors/JSON/Plain | 1382.5 / 192 / 7 | 1230.0 / 112 / 3 | -11.03% |
| Errors/JSON/Wrapped | 1697.0 / 304 / 10 | 1569.5 / 224 / 6 | -7.51% |
| Errors/JSON/Joined | 2032.5 / 568 / 14 | 1916.5 / 488 / 10 | -5.71% |
| Errors/Pretty/Stack | 866.3 / 272 / 5 | 866.8 / 272 / 5 | ~ |
| Errors/JSON/Stack | 1529.5 / 208 / 8 | 1454.0 / 128 / 4 | -4.94% |
| Trace/PackageHelper | 2264.5 / 2232 / 17 | 2316.0 / 2232 / 17 | +2.27% |
| Trace/RawLevel | 408.1 / 72 / 2 | 416.1 / 72 / 2 | ~ |
| FatalLevel | 390.1 / 64 / 1 | 397.0 / 64 / 1 | +1.77% |
| PackageVsLogger/PackageInfo | 565.7 / 320 / 4 | 549.5 / 320 / 4 | -2.87% |
| PackageVsLogger/LoggerInfo | 557.5 / 320 / 4 | 563.2 / 320 / 4 | ~ |
| Parallel/Pretty | 547.6 / 96 / 3 | 480.1 / 64 / 1 | -12.33% |
| Parallel/JSON | 1053.5 / 120 / 7 | 853.0 / 8 / 1 | -19.03% |

## Attribute scaling

Both typed and alternating-argument variants were rerun at every requested
count. Allocations/bytes match between those two APIs at each count.
The original six-field root fixture is not the same type mix as scaling/6.

| Benchmark | Before: ns / B / allocs | After: ns / B / allocs | Time change |
| --- | ---: | ---: | ---: |
| Scaling/Pretty/Typed/0 | 396.5 / 64 / 1 | 397.6 / 64 / 1 | ~ |
| Scaling/Pretty/Typed/1 | 521.8 / 96 / 3 | 467.6 / 64 / 1 | -10.39% |
| Scaling/Pretty/Typed/2 | 616.5 / 224 / 4 | 557.6 / 192 / 2 | -9.55% |
| Scaling/Pretty/Typed/3 | 667.4 / 224 / 4 | 587.6 / 192 / 2 | -11.95% |
| Scaling/Pretty/Typed/4 | 772.5 / 240 / 6 | 678.2 / 192 / 2 | -12.21% |
| Scaling/Pretty/Typed/5 | 848.1 / 256 / 8 | 745.8 / 192 / 2 | -12.06% |
| Scaling/Pretty/Typed/6 | 990.5 / 336 / 11 | 851.5 / 240 / 3 | -14.04% |
| Scaling/Pretty/Typed/8 | 1186.0 / 672 / 12 | 1007.0 / 576 / 4 | -15.09% |
| Scaling/Pretty/Typed/10 | 1366.5 / 784 / 16 | 1179.5 / 656 / 4 | -13.68% |
| Scaling/Pretty/Typed/16 | 2052.5 / 1632 / 25 | 1703.0 / 1408 / 5 | -17.03% |
| Scaling/Pretty/Typed/32 | 3409.0 / 2528 / 43 | 2818.5 / 2112 / 5 | -17.32% |
| Scaling/Pretty/Args/0 | 394.5 / 64 / 1 | 389.6 / 64 / 1 | ~ |
| Scaling/Pretty/Args/1 | 528.5 / 96 / 3 | 467.1 / 64 / 1 | -11.60% |
| Scaling/Pretty/Args/2 | 630.9 / 224 / 4 | 578.8 / 192 / 2 | -8.25% |
| Scaling/Pretty/Args/3 | 696.0 / 224 / 4 | 626.0 / 192 / 2 | -10.05% |
| Scaling/Pretty/Args/4 | 801.5 / 240 / 6 | 719.6 / 192 / 2 | -10.22% |
| Scaling/Pretty/Args/5 | 897.8 / 256 / 8 | 802.5 / 192 / 2 | -10.62% |
| Scaling/Pretty/Args/6 | 1055.0 / 336 / 11 | 886.0 / 240 / 3 | -16.02% |
| Scaling/Pretty/Args/8 | 1235.5 / 672 / 12 | 1090.0 / 576 / 4 | -11.78% |
| Scaling/Pretty/Args/10 | 1489.0 / 784 / 16 | 1264.5 / 656 / 4 | -15.08% |
| Scaling/Pretty/Args/16 | 2202.0 / 1632 / 25 | 1883.0 / 1408 / 5 | -14.49% |
| Scaling/Pretty/Args/32 | 3790.0 / 2528 / 43 | 3252.0 / 2112 / 5 | -14.20% |
| Scaling/JSON/Typed/0 | 893.4 / 88 / 5 | 751.9 / 8 / 1 | -15.84% |
| Scaling/JSON/Typed/1 | 1067.0 / 120 / 7 | 873.0 / 8 / 1 | -18.18% |
| Scaling/JSON/Typed/2 | 1170.0 / 120 / 7 | 978.5 / 8 / 1 | -16.37% |
| Scaling/JSON/Typed/3 | 1261.0 / 120 / 7 | 1078.0 / 8 / 1 | -14.51% |
| Scaling/JSON/Typed/4 | 1485.0 / 144 / 10 | 1273.0 / 16 / 2 | -14.28% |
| Scaling/JSON/Typed/5 | 1604.0 / 160 / 12 | 1367.5 / 16 / 2 | -14.74% |
| Scaling/JSON/Typed/6 | 1799.0 / 240 / 15 | 1485.5 / 64 / 3 | -17.43% |
| Scaling/JSON/Typed/8 | 2125.5 / 320 / 15 | 1801.0 / 144 / 3 | -15.27% |
| Scaling/JSON/Typed/10 | 2514.0 / 440 / 20 | 2184.5 / 232 / 4 | -13.11% |
| Scaling/JSON/Typed/16 | 3516.5 / 784 / 29 | 3145.5 / 480 / 5 | -10.55% |
| Scaling/JSON/Typed/32 | 6112.0 / 1704 / 50 | 5538.5 / 1208 / 8 | -9.38% |
| Scaling/JSON/Args/0 | 907.1 / 88 / 5 | 761.5 / 8 / 1 | -16.05% |
| Scaling/JSON/Args/1 | 1069.0 / 120 / 7 | 887.1 / 8 / 1 | -17.02% |
| Scaling/JSON/Args/2 | 1196.0 / 120 / 7 | 975.8 / 8 / 1 | -18.41% |
| Scaling/JSON/Args/3 | 1288.0 / 120 / 7 | 1103.5 / 8 / 1 | -14.32% |
| Scaling/JSON/Args/4 | 1512.5 / 144 / 10 | 1334.5 / 16 / 2 | -11.77% |
| Scaling/JSON/Args/5 | 1650.0 / 160 / 12 | 1417.0 / 16 / 2 | -14.12% |
| Scaling/JSON/Args/6 | 1854.5 / 240 / 15 | 1611.0 / 64 / 3 | -13.13% |
| Scaling/JSON/Args/8 | 2165.0 / 320 / 15 | 1876.0 / 144 / 3 | -13.35% |
| Scaling/JSON/Args/10 | 2619.5 / 440 / 20 | 2302.0 / 232 / 4 | -12.12% |
| Scaling/JSON/Args/16 | 3654.0 / 784 / 29 | 3374.5 / 480 / 5 | -7.65% |
| Scaling/JSON/Args/32 | 6335.5 / 1704 / 50 | 5969.5 / 1208 / 8 | -5.78% |

Removing boxing does not remove slog.Record's five-inline-attr threshold,
Record backing storage, pretty buffer growth, or stdlib float encoding.
At 32 typed attrs, pretty drops 43 → 5 allocations; JSON drops 50 → 8.
At six typed attrs, pretty drops 11 → 3; JSON drops 15 → 3.

## Parallel measurements

Aggregate ns/op, not individual-call latency:

| Workload | Before ns | After ns | Timing inference | B/op before → after | allocs before → after |
| --- | ---: | ---: | --- | ---: | ---: |
| Root pretty / 4 | 162.3 | 135.1 | -16.8%, p=.002 | 96 → 64 | 3 → 1 |
| Root pretty / 16 | 146.4 | 137.3 | -6.2%, p=.009 | 96 → 64 | 3 → 1 |
| Root JSON / 4 | 302.8 | 311.6 | no significant difference, p=.394 | 120 → 8 | 7 → 1 |
| Root JSON / 16 | 171.1 | 147.7 | -13.7%, p=.002 | 121 → 8 | 7 → 1 |
| Comparison JSON3 / 4 | 358.0 | 374.3 | no significant difference, p=.132 | 120 → 8 | 7 → 1 |
| Comparison JSON3 / 16 | 206.0 | 194.1 | no significant difference, p=.368 | 121 → 8 | 7 → 1 |

The numerically slower four-way JSON results are not hidden. They were not
statistically significant in these runs. Unchanged comparator parallel
results also shifted: slog/4 +13.9% and Zap/16 +11.5%. This experiment does
not establish a universal parallel speedup or a causal regression there.

## Fast-path follow-up and regressions

Small initial increases in disabled logger TRACE (+2.9%), full TRACE (+2.3%)
and raw FATAL severity (+1.8%) prompted a separate check. Saved binaries were
run on CPU 0, alternating before/after/after/before for three rounds:
six samples per side, 300 ms, GOMAXPROCS=1. This does not overwrite or replace
the primary measurements.

- Basic Info: 385.9 → 392.0 ns, not significant (p=.180).
- Full TRACE: 2237 → 2261 ns, not significant (p=.264).
- Raw TRACE and FATAL: no significant change.
- Disabled package TRACE: 3.355 → 3.352 ns, not significant.
- Disabled package Debug: 3.502 → 3.337 ns.
- Disabled logger Debug: 3.723 → 3.558 ns.
- **Disabled logger TRACE: 3.534 → 3.674 ns (+3.95%, p=.004).**
  This small ~0.14 ns increase persisted and is a measured regression, not
  dismissed as statistically insignificant. It remains zero-allocation and
  single-digit ns. The modified attr paths are never reached by disabled calls.
  Disassembly of the benchmark, slog.Logger.log and prettyHandler.Enabled
  has the same instruction sequence after normalizing addresses/offsets;
  binary layout changes remain a possible explanation, not a proven cause.

Thus there is no blanket claim that every benchmark improved. No alignment
tricks or unrelated disabled-path changes were made to chase a sub-nanosecond
difference. Basic/source-enabled pretty paths retain their allocation counts
and show no significant timing regression.

## Allocation profile and remaining JSON costs

A post-change JSON3 allocation profile used a separate process with 100,000
iterations, GOMAXPROCS=1 and memprofilerate=1. pprof shows approximately
781 KiB at `strconv.appendQuotedWith` through `slog.Level.MarshalJSON`:
about **8 B per operation**. The former repeated primitive boxing from
`attrPipeline.forJSON` is gone. Remaining profile bytes are small test/setup,
pool warm-up and timezone initialization costs, not an unexplained 112 B leak.

The expected reduction occurred, so compiler escape-analysis changes were not
needed. Larger records still allocate Record backing storage and boxed JSON
floats; source logging still creates stdlib source/group representations;
structured errors still require their existing representations. The previous
investigation's callback/Resolve and encoding costs remain. JSON3 is still
around 1.03 µs here, not claimed to be below 1 µs.

A custom JSON handler remains unjustified as the next step. This small change
removed the measured allocation source while retaining the stdlib backend and
all public slog interoperability.

**ONE recommended next target:** investigate normal level serialization through
the existing JSON ReplaceAttr path. The remaining ordinary-record allocation
is `slog.Level.MarshalJSON` quoting. Measure whether returning the equivalent
string for typed levels can remove it while preserving exact custom-level,
transformation and user-attr behavior. Do not combine it with a custom encoder.

## Validation and repository state

Initial root tests/race/vet/diff checks passed. After implementation:

- gofmt on attrs.go, gloq.go and attrs_test.go: PASS.
- Root `go test -count=1 ./...`: PASS.
- Root `go test -race -count=1 ./...`: PASS.
- Root `go vet ./...`: PASS.
- Explicit `TestPrettyHandlerConformance`: PASS.
- Existing fuzz seeds: PASS; short FuzzPrettyHandler run: PASS, 65,630
  executions, six new interesting inputs, no failures.
- Comparison module tests, race tests and vet: PASS.
- `git diff --check`: PASS.

No public API, behavior, error traversal, caller implementation, filtering,
cloning, writer lock, or dependency change. No commit or push.

Tracked diff (including the pre-existing .gitignore change):

```text
 .gitignore |  3 ++-
 attrs.go   | 16 +++++++++++-----
 gloq.go    | 18 +++++++++++-------
 3 files changed, 24 insertions(+), 13 deletions(-)
```

New files from this task: `attrs_test.go` and this report. The seven comparison
module files already untracked at entry remain untracked and unchanged.
Raw measurements/profiles/test binaries are ignored; the existing root
`gloq.test` and historical benchmark notes were preserved. Untracked files are
not included by plain git diff --stat.
