module github.com/skinleak/gloq/benchmarks/comparison

go 1.21

require (
	github.com/skinleak/gloq v0.0.0
	go.uber.org/zap v1.28.0
)

require go.uber.org/multierr v1.10.0 // indirect

replace github.com/skinleak/gloq => ../..
