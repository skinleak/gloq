#!/bin/sh
# Run from any directory. Override COUNT, BENCHTIME, CPU_TIME, ALLOC_ITERS,
# BENCHSTAT or PPROF through the environment; Go cache settings are inherited.
set -eu
cd "$(dirname "$0")"
mkdir -p results

measure() {
    {
        date -u
        go version
        go env GOOS GOARCH GOAMD64 GOTOOLCHAIN GOFLAGS CGO_ENABLED
        git rev-parse HEAD
        git status --short
        go list -m all
        printf 'COUNT=%s BENCHTIME=%s\n' "${COUNT:-6}" "${BENCHTIME:-200ms}"
        printf 'GOGC=%s GOMEMLIMIT=%s GOMAXPROCS=%s\n' "${GOGC:-default}" "${GOMEMLIMIT:-default}" "${GOMAXPROCS:-default}"
        if command -v lscpu >/dev/null 2>&1; then lscpu; fi
    } > results/environment.txt
    go test -run='^$' -bench='Benchmark(JSON|Alternating|Disabled|ContextFields|NativeErrors|Boundary|Record)$' \
        -benchmem -benchtime="${BENCHTIME:-200ms}" -count="${COUNT:-6}" -cpu=1 . > results/comparison.txt
    go test -run='^$' -bench='BenchmarkScaling$' \
        -benchmem -benchtime="${BENCHTIME:-200ms}" -count="${COUNT:-6}" -cpu=1 . > results/scaling.txt
    go test -run='^$' -bench='BenchmarkParallelJSON$' \
        -benchmem -benchtime="${BENCHTIME:-200ms}" -count="${COUNT:-6}" -cpu=1,4,16 . > results/parallel.txt
    (cd ../.. && go test -run='^$' -bench='.' -benchmem \
        -benchtime="${BENCHTIME:-200ms}" -count="${COUNT:-6}" -cpu=1 .) > results/gloq.txt
    if command -v "${BENCHSTAT:-benchstat}" >/dev/null 2>&1; then
        for name in comparison scaling parallel gloq; do
            "${BENCHSTAT:-benchstat}" "results/$name.txt" > "results/$name-summary.txt"
        done
    fi
}

profile_one() {
    name=$1
    binary=$2
    benchmark=$3
    # CPU sampling uses the default memory sampling rate. A separate process
    # records all allocations, so allocation tracing does not distort CPU data.
    "$binary" -test.run='^$' -test.bench="$benchmark" -test.cpu=1 \
        -test.benchtime="${CPU_TIME:-3s}" -test.cpuprofile="$profile_dir/$name.cpu" \
        > "$profile_dir/$name-cpu-run.txt"
    "$binary" -test.run='^$' -test.bench="$benchmark" -test.cpu=1 \
        -test.benchtime="${ALLOC_ITERS:-100000x}" -test.memprofilerate=1 \
        -test.memprofile="$profile_dir/$name.alloc" > "$profile_dir/$name-alloc-run.txt"
    pprof -top -nodecount=40 "$binary" "$profile_dir/$name.cpu" > "$profile_dir/$name-cpu.txt"
    pprof -top -cum -nodecount=40 "$binary" "$profile_dir/$name.cpu" > "$profile_dir/$name-cpu-cum.txt"
    pprof -top -alloc_space -nodecount=40 "$binary" "$profile_dir/$name.alloc" > "$profile_dir/$name-bytes.txt"
    pprof -top -alloc_objects -nodecount=40 "$binary" "$profile_dir/$name.alloc" > "$profile_dir/$name-objects.txt"
}

pprof() {
    if [ -n "${PPROF:-}" ]; then "$PPROF" "$@"; else go tool pprof "$@"; fi
}

profile() {
    mkdir -p results/profiles
    profile_dir="$(pwd)/results/profiles"
    go test -c -o "$profile_dir/comparison.test" .
    (cd ../.. && go test -c -o "$profile_dir/gloq.test" .)
    profile_one json3 "$profile_dir/comparison.test" '^BenchmarkJSON$/^Fields3$/^Gloq$'
    profile_one json10 "$profile_dir/comparison.test" '^BenchmarkJSON$/^Fields10$/^Gloq$'
    profile_one pretty6 "$profile_dir/comparison.test" '^BenchmarkScaling$/^Pretty$/^Typed$/^6$'
    profile_one pretty10 "$profile_dir/comparison.test" '^BenchmarkScaling$/^Pretty$/^Typed$/^10$'
    profile_one bound "$profile_dir/gloq.test" '^BenchmarkPrettyBoundAttrs$'
    profile_one source "$profile_dir/gloq.test" '^BenchmarkPrettySource$/^Enabled$'
    profile_one json-source "$profile_dir/comparison.test" '^BenchmarkContextFields$/^Source$/^Gloq$'
    profile_one joined-pretty "$profile_dir/gloq.test" '^BenchmarkErrors$/^Pretty$/^Joined$'
    profile_one joined-json "$profile_dir/gloq.test" '^BenchmarkErrors$/^JSON$/^Joined$'
    profile_one trace "$profile_dir/gloq.test" '^BenchmarkTrace$/^PackageHelper$'
}

case "${1:-measure}" in
    measure) measure ;;
    profile) profile ;;
    *) printf 'Usage: sh run.sh [measure|profile]\n' >&2; exit 2 ;;
esac
