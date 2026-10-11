# Benchmark results

Measured on 2026-10-09 with `sh run.sh measure` (6 samples × 200 ms,
`-cpu=1` unless noted), Go 1.26.0, linux/amd64, 12th Gen Intel Core
i5-12600KF, zap v1.28.0. Values are benchstat medians. Desktop hardware, no
core pinning: treat differences under about 10% as noise. See the
[benchmark README](README.md) for workloads, fairness rules, and how to run
them yourself.

These numbers include gloq's native JSON encoder
([#4](https://github.com/skinleak/gloq/pull/4)). Earlier reports were
measured on a different machine, so compare gloq with slog and zap within
this report rather than with older numbers.

## Summary

- **JSON output** is 15–25% faster than `slog.JSONHandler` for typical
  records (383 ns vs 448 ns with three fields) and allocates less: one
  allocation for 6 or 10 fields where slog needs two or three.
- **Source locations** cost gloq little: 581 ns against 1201 ns for slog and
  832 ns for zap, with no allocations.
- **Errors** are written as structured trees with their type and causes, so
  wrapped and joined errors take about 10–15% longer than slog's plain
  message string. Plain errors are as fast as slog's.
- **Pretty output** costs about the same as slog's JSON (437 ns vs 447 ns per
  record through a logger) and needs no allocations for typical records.
- **Zap** is faster still, as expected for an encoder that does not go through
  `slog.Record`. If raw JSON throughput is your main concern, use zap.
- **Disabled levels** cost about 3.2–3.9 ns for every logger.

## JSON, typed fields (ns/op · allocs/op)

| Fields | gloq      | slog      | zap      |
| -----: | --------: | --------: | -------: |
|      0 | 259 · 0   | 317 · 0   | 205 · 0  |
|      1 | 304 · 0   | 382 · 0   | 217 · 0  |
|      3 | 383 · 0   | 448 · 0   | 263 · 0  |
|      6 | 596 · 1   | 801 · 2   | 378 · 0  |
|     10 | 957 · 1   | 1214 · 3  | 495 · 0  |

## Other workloads (ns/op · allocs/op)

| Workload                                | gloq      | slog      | zap       |
| --------------------------------------- | --------: | --------: | --------: |
| Disabled `Debug`                        | 3.2 · 0   | 3.5 · 0   | 3.9 · 0   |
| 3 alternating args (zap: Sugar)         | 405 · 0   | 491 · 0   | 516 · 1   |
| 3 fields bound with `With`              | 260 · 0   | 313 · 0   | 196 · 0   |
| Nested groups, 3 fields                 | 410 · 0   | 485 · 0   | 270 · 0   |
| Caller enabled, 3 fields ¹              | 581 · 0   | 1201 · 6  | 832 · 2   |
| Plain error ²                           | 375 · 0   | 395 · 0   | 234 · 0   |
| Wrapped error ²                         | 459 · 0   | 416 · 0   | 253 · 0   |
| Joined error ²                          | 589 · 2   | 518 · 2   | 371 · 2   |

¹ slog and gloq record function, file, and line; zap records file and line.
² Not equivalent: gloq writes each error's message, type, and causes as a
structured tree; slog and zap write the message string only.

## Parallel JSON, 3 fields (aggregate ns/op, 0 allocs for all)

| GOMAXPROCS | gloq | slog | zap |
| ---------: | ---: | ---: | --: |
|          1 | 377  | 445  | 262 |
|          4 | 133  | 218  |  82 |
|         16 | 199  | 210  | 145 |

The 4- and 16-thread results vary by 5–19% between samples.

## Pretty output, typed fields (gloq only)

| Fields | ns/op | allocs/op |
| -----: | ----: | --------: |
|      0 |   292 |         0 |
|      3 |   435 |         0 |
|     10 |   997 |         1 |
|     32 |  2512 |         1 |

## Where the time goes

The boundary benchmarks separate the cost of building a record from handling
it (ns/op, three fields):

| Path                                        | Through a logger | `Handle` only |
| ------------------------------------------- | ---------------: | ------------: |
| `slog.JSONHandler`                          |              447 |           274 |
| `slog.JSONHandler` + identity `ReplaceAttr` |              785 |           608 |
| gloq JSON                                   |              379 |           217 |
| gloq pretty                                 |              437 |           282 |

About 160 ns of every record is spent by `slog.Logger` itself, creating the
record and capturing the caller, before any handler runs; it is the same for
every slog handler. gloq's JSON handler no longer pays for slog's
`ReplaceAttr` path, which made its JSON output about twice as slow as slog's
before the native encoder. Setting a `WithReplaceAttr` function adds a call
for every attribute, as it does in slog.
