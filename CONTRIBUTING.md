# Contributing

Thanks for helping make gloq better.

Before opening a pull request, please:

1. Keep changes small and focused.
2. Add tests for new behavior.
3. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `gofmt -l .`.
   The `gloqotel` directory is a separate module; run the same commands there
   when changing it.
4. Run `go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...`.
5. For changes to the pretty handler, fuzz it for a while:
   `go test -run '^$' -fuzz '^FuzzPrettyHandler$' -fuzztime 1m .`
6. For performance-sensitive changes, compare benchmarks against `main` with
   [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat). CI posts
   this comparison on every pull request.
7. Update the README when the public API changes.

Bug reports and feature ideas are welcome in
[GitHub Issues](https://github.com/skinleak/gloq/issues). Search existing issues
before opening a new one, and include a small reproduction when possible.

Please report vulnerabilities privately as described in
[SECURITY.md](SECURITY.md), rather than opening a public issue.

## Compatibility

gloq supports Go 1.21 and newer and follows semantic versioning. Breaking API
changes will only happen in a new major version.
