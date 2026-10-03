GO ?= go
BIN := bin/boundedcode
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/akynte/boundedcode/internal/buildinfo.Version=$(VERSION)

.PHONY: build test race vet fmt lint check licenses clean

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/boundedcode

test:
	$(GO) test ./...

race:
	CGO_ENABLED=1 $(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	@out=$$(gofmt -l . | grep -v '^\.tmp-home/' || true); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint:
	@if command -v golangci-lint >/dev/null; then golangci-lint run ./...; else echo "golangci-lint not installed; skipping"; fi

licenses:
	rm -rf LICENSES/go && $(GO) run ./scripts/licensecheck -write LICENSES/go

check: fmt vet test race lint licenses

clean:
	rm -rf bin dist
