GO ?= go
BIN := bin/boundedcode
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Release archives use the commit time, so they are reproducible.
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)
LICENSES_TGZ := boundedcode-$(VERSION)-licenses.tar.gz
LDFLAGS := -s -w -X github.com/akynte/boundedcode/internal/buildinfo.Version=$(VERSION)

.PHONY: build install test race vet fmt lint check licenses sbom dist clean

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/boundedcode

# Install the local build as boundedcode and bcode (PREFIX_BIN, default ~/.local/bin).
PREFIX_BIN ?= $(HOME)/.local/bin
install: build
	install -d $(PREFIX_BIN)
	install -m 0755 $(BIN) $(PREFIX_BIN)/boundedcode
	ln -sf boundedcode $(PREFIX_BIN)/bcode

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

# Release targets: one static binary each (CGO disabled), with its own SBOM.
TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

dist: licenses
	rm -rf dist && mkdir -p dist
	set -e; for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=.exe; \
		bin=dist/boundedcode-$$os-$$arch$$ext; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $$bin ./cmd/boundedcode; \
		$(GO) run ./scripts/sbom -version $(VERSION) -goos $$os -goarch $$arch -binary $$bin -o dist/SBOM-$$os-$$arch.spdx.json; \
	done
	cp -r LICENSE NOTICE THIRD_PARTY_NOTICES.md LICENSES dist/
	tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@$(SOURCE_DATE_EPOCH) \
		-czf dist/$(LICENSES_TGZ) LICENSE NOTICE THIRD_PARTY_NOTICES.md LICENSES
	cd dist && sha256sum boundedcode-linux-* boundedcode-darwin-* boundedcode-windows-* SBOM-*.spdx.json $(LICENSES_TGZ) > SHA256SUMS

check: fmt vet test race lint licenses

clean:
	rm -rf bin dist
