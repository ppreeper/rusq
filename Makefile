.PHONY: build release install test race bench bench-record bench-gate bench-corpus corpus lint fmt clean

BINARY := bin/rusq
PKG := ./...

# Release metadata, stamped into the binary with -ldflags so `rusq version`
# reports what you are running.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/ppreeper/rusq/internal/version
LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT) -X $(VERSION_PKG).Date=$(DATE)

# Benchmark knobs (see docs/PERFORMANCE.md §5).
BENCH_PATTERN ?= .
BENCHTIME ?= 200ms
BENCH_COUNT ?= 1
BENCH_BASELINE ?=
BENCH_THRESHOLD ?= 5

# Corpus knobs (see docs/PERFORMANCE.md §6).
CORPUS_DIR ?= /tmp/rusq-corpus
CORPUS_FILES ?= 20000

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/rusq

install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/rusq

# Cross-platform archives plus checksums into dist/ (see scripts/release.sh).
# CI runs this on every v* tag; .github/workflows/release.yml publishes them.
release:
	scripts/release.sh $(VERSION)

test:
	go test $(PKG)

race:
	go test -race $(PKG)

bench:
	go test -run '^$$' -bench '$(BENCH_PATTERN)' -benchmem -benchtime $(BENCHTIME) -count $(BENCH_COUNT) ./internal/...

# scripts/bench.sh is the same run plus a benchstat comparison and an archive
# under bench/.
bench-record:
	BENCHTIME=$(BENCHTIME) scripts/bench.sh '$(BENCH_PATTERN)'

# Fail when any benchmark regresses more than BENCH_THRESHOLD% against
# BENCH_BASELINE (default: the newest archive in bench/).
bench-gate:
	BENCH_PATTERN='$(BENCH_PATTERN)' BENCHTIME=$(BENCHTIME) BENCH_COUNT=$(BENCH_COUNT) BENCH_THRESHOLD=$(BENCH_THRESHOLD) \
		scripts/bench-gate.sh '$(BENCH_BASELINE)' $(BENCH_THRESHOLD)

corpus:
	scripts/gen-corpus.sh $(CORPUS_DIR) $(CORPUS_FILES)

# End-to-end cold/warm timing over the generated corpus.
bench-corpus: build corpus
	./$(BINARY) clean >/dev/null 2>&1 || true
	@echo "--- cold (cache empty, parallel) ---"
	/usr/bin/time -v ./$(BINARY) check --no-cache --exit-zero --statistics $(CORPUS_DIR) >/dev/null
	@echo "--- warm (cache populated, parallel) ---"
	/usr/bin/time -v ./$(BINARY) check --exit-zero --statistics $(CORPUS_DIR) >/dev/null
	@echo "--- single worker (--workers 1) ---"
	/usr/bin/time -v ./$(BINARY) check --no-cache --workers 1 --exit-zero --statistics $(CORPUS_DIR) >/dev/null

lint:
	go vet $(PKG)

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

clean:
	rm -rf bin
