module hypnotz

go 1.24.0

toolchain go1.24.9

replace github.com/takara-ai/go-attention => ./go-attention

require (
	github.com/mattn/go-sqlite3 v1.14.44
	github.com/takara-ai/go-attention v0.0.0-20250718094311-28cb85330443
	github.com/uber/h3-go/v4 v4.5.0
	gonum.org/v1/gonum v0.17.0
)
