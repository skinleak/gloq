# Benchmark results

Measured on 2026-10-06 with `sh run.sh measure` (6 samples × 200 ms,
`-cpu=1` unless noted), Go 1.26.4, linux/amd64, Intel Core Ultra 7 268V
(8 cores), zap v1.28.0. Values are benchstat medians. Shared laptop hardware,
no core pinning: treat differences under about 10% as noise. See the
[benchmark README](README.md) for workloads, fairness rules, and how to run
them yourself.

## Summary

- **Allocations:** gloq's JSON output allocates exactly as much as
  `slog.JSONHandler`: nothing for up to five fields, and the same small
  amounts slog itself needs beyond that. Pretty output needs no allocations for
  typical records either.
- **Pretty output** costs about the same as plain slog JSON
  (388 ns vs 389 ns per record through a logger).
- **JSON output** takes about twice as long as `slog.JSONHandler` on a single
  core. Most of the difference is slog's own `ReplaceAttr` path, which gloq
  uses for its level names and error trees: slog alone with an identity
  `ReplaceAttr` takes 668 ns, against gloq's 817 ns. Under parallel load the
  difference disappears, since writes are serialized for both.
- **Zap** is faster still, as expected for an encoder that does not go through
  `slog.Record`. If raw JSON throughput is your main concern, use zap.
- **Disabled levels** cost about 3.5 ns for every logger.

## JSON, typed fields (ns/op · allocs/op)

| Fields | gloq      | slog     | zap      |
| -----: | --------: | -------: | -------: |
|      0 | 535 · 0   | 248 · 0  | 154 · 0  |
|      1 | 616 · 0   | 297 · 0  | 170 · 0  |
|      3 | 814 · 0   | 365 · 0  | 205 · 0  |
|      6 | 1204 · 2  | 628 · 2  | 276 · 0  |
|     10 | 1787 · 3  | 982 · 3  | 361 · 0  |

## Other workloads (ns/op · allocs/op)

| Workload                                | gloq      | slog      | zap       |
| --------------------------------------- | --------: | --------: | --------: |
| Disabled `Debug`                        | 3.6 · 0   | 3.3 · 0   | 3.2 · 0   |
| 3 alternating args (zap: Sugar)         | 823 · 0   | 435 · 0   | 430 · 1   |
| 3 fields bound with `With`              | 533 · 0   | 265 · 0   | 160 · 0   |
| Nested groups, 3 fields                 | 881 · 0   | 411 · 0   | 208 · 0   |
| Caller enabled, 3 fields ¹              | 1681 · 6  | 956 · 6   | 655 · 2   |
| Plain error ²                           | 1014 · 2  | 321 · 0   | 190 · 0   |
| Wrapped error ²                         | 1262 · 5  | 356 · 0   | 196 · 0   |
| Joined error ²                          | 1510 · 9  | 433 · 2   | 293 · 2   |

¹ slog and gloq record function, file, and line; zap records file and line.
² Not equivalent: gloq writes each error's message, type, and causes as a
structured tree; slog and zap write the message string only.

## Parallel JSON, 3 fields (aggregate ns/op, 0 allocs for all)

| GOMAXPROCS | gloq | slog | zap |
| ---------: | ---: | ---: | --: |
|          1 | 1049 | 668  | 312 |
|          4 |  427 | 440  | 179 |
|         16 |  394 | 403  | 323 |

## Pretty output, typed fields (gloq only)

| Fields | ns/op | allocs/op |
| -----: | ----: | --------: |
|      0 |   234 |         0 |
|      3 |   376 |         0 |
|     10 |  1114 |         1 |
|     32 |  2721 |         1 |

## Where JSON time goes

The boundary benchmarks separate the cost of building a record from handling
it:

| Path                                   | Through a logger | `Handle` only |
| -------------------------------------- | ---------------: | ------------: |
| `slog.JSONHandler`                     |              389 |           230 |
| `slog.JSONHandler` + identity `ReplaceAttr` |         668 |           517 |
| gloq JSON                              |              817 |           681 |
| gloq pretty                            |              388 |           245 |

Calling `ReplaceAttr` for every attribute, including the built-in time, level,
and message, is what makes slog's handler slower. A JSON encoder of gloq's own
that applies level names and error trees directly, as the pretty handler
already does, would remove that cost. That is the main remaining performance
opportunity.
