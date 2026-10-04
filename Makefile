GO ?= go
BIN := bin/boundedcode
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/akynte/boundedcode/internal/buildinfo.Version=$(VERSION)

.PHONY: build test race vet fmt lint check licenses sbom dist clean

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

# Locally a missing golangci-lint is skipped; in CI (CI=true) it is an error.
# The CI lint job runs golangci-lint through its pinned action.
lint:
	@if command -v golangci-lint >/dev/null; then golangci-lint run ./...; \
	elif [ -n "$$CI" ]; then echo "golangci-lint not installed (required in CI)" >&2; exit 1; \
	else echo "golangci-lint not installed; skipping"; fi

licenses:
	rm -rf LICENSES/go && $(GO) run ./scripts/licensecheck -write LICENSES/go

sbom: build
	$(GO) run ./scripts/sbom -version $(VERSION) -binary $(BIN) -o SBOM.spdx.json

dist: licenses build
	rm -rf dist && mkdir -p dist
	cp $(BIN) dist/boundedcode-linux-amd64
	$(GO) run ./scripts/sbom -version $(VERSION) -binary $(BIN) -o dist/SBOM.spdx.json
	cp -r LICENSE NOTICE THIRD_PARTY_NOTICES.md LICENSES dist/
	cd dist && sha256sum boundedcode-linux-amd64 SBOM.spdx.json > SHA256SUMS

check: fmt vet test race lint licenses

clean:
	rm -rf bin dist
