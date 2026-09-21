# Contributing

Thanks for helping make gloq better.

Before opening a pull request, please:

1. Keep changes small and focused.
2. Add tests for new behavior.
3. Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.
4. Update the README when the public API changes.

Bug reports and feature ideas are welcome in
[GitHub Issues](https://github.com/skinleak/gloq/issues). Search existing issues
before opening a new one, and include a small reproduction when possible.

Please report vulnerabilities privately as described in
[SECURITY.md](SECURITY.md), rather than opening a public issue.

## Compatibility

gloq supports Go 1.21 and newer and follows semantic versioning. Breaking API
changes will only happen in a new major version.
