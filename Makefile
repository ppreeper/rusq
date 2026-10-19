.PHONY: build test race bench bench-corpus corpus lint fmt clean install

BINARY := bin/rusq
PKG := ./...

# Benchmark knobs (see docs/PERFORMANCE.md §5).
BENCH_PATTERN ?= .
BENCHTIME ?= 200ms
BENCH_COUNT ?= 1

# Corpus knobs (see docs/PERFORMANCE.md §6).
CORPUS_DIR ?= /tmp/rusq-corpus
CORPUS_FILES ?= 20000

build:
	go build -trimpath -o $(BINARY) ./cmd/rusq

install:
	go install -trimpath ./cmd/rusq

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
