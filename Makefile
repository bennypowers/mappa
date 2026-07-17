.PHONY: all test lint clean install
.PHONY: linux-x64 linux-arm64 darwin-x64 darwin-arm64 win32-x64 win32-arm64
.PHONY: wasm wasm-release wasm-serve
.PHONY: release patch minor major

BINARY_NAME := mappa
DIST_DIR := dist/bin
GO_BUILD_FLAGS := -ldflags="-s -w"

# Windows cross-compilation image (CI provides WINDOWS_IMAGE via go-release-workflows)
WINDOWS_IMAGE ?= grw-windows-cross

# Workaround for Gentoo Linux "hole in findfunctab" error with race detector
# See: https://bugs.gentoo.org/961618
# Gentoo's Go build has issues with the race detector and internal linker.
# Using external linker resolves the issue.
ifeq ($(shell test -f /etc/gentoo-release && echo yes),yes)
    RACE_LDFLAGS := -ldflags="-linkmode=external"
else
    RACE_LDFLAGS :=
endif

all:
	go build -o dist/bin/mappa .

install: all
	cp dist/bin/mappa ~/.local/bin/mappa

clean:
	rm -f mappa
	rm -rf dist/
	go clean -cache -testcache

test:
	gotestsum -- -race $(RACE_LDFLAGS) ./...

lint:
	go vet ./...
	golangci-lint run

# Cross-compilation targets for go-release-workflows
# Requires CGO for tree-sitter bindings

linux-x64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-x64 .

linux-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 .

# Darwin targets (must run on macOS)
# Explicit -arch flags ensure correct architecture when cross-compiling on macOS
darwin-x64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
		CC="clang -arch x86_64" \
		CGO_CFLAGS="-arch x86_64" CGO_LDFLAGS="-arch x86_64" \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-x64 .

darwin-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
		CC="clang -arch arm64" \
		CGO_CFLAGS="-arch arm64" CGO_LDFLAGS="-arch arm64" \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 .

# Windows targets (requires Podman)
win32-x64:
	@mkdir -p $(DIST_DIR)
	podman run --rm \
		-v $(PWD):/src:Z \
		-w /src \
		-e GOOS=windows \
		-e GOARCH=amd64 \
		-e CGO_ENABLED=1 \
		-e CC=x86_64-w64-mingw32-gcc \
		-e CXX=x86_64-w64-mingw32-g++ \
		$(WINDOWS_IMAGE) \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-win32-x64.exe .

win32-arm64:
	@mkdir -p $(DIST_DIR)
	podman run --rm \
		-v $(PWD):/src:Z \
		-w /src \
		-e GOOS=windows \
		-e GOARCH=arm64 \
		-e CGO_ENABLED=1 \
		-e CC=aarch64-w64-mingw32-gcc \
		-e CXX=aarch64-w64-mingw32-g++ \
		$(WINDOWS_IMAGE) \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-win32-arm64.exe .

# WASM build targets (no CGO required)
# Note: trace package is excluded (uses tree-sitter CGO), only generate functionality is available

wasm:
	GOOS=js GOARCH=wasm go build -o web/mappa.wasm ./wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
	@echo "WASM build complete: web/mappa.wasm"

wasm-release:
	GOOS=js GOARCH=wasm go build $(GO_BUILD_FLAGS) -o web/mappa.wasm ./wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
	@echo "WASM release build complete: web/mappa.wasm"

wasm-serve: wasm
	@echo "Serving WASM demo at http://localhost:8080"
	# Requires Python 3 for the simple HTTP server
	python3 -m http.server 8080 -d web

# Extract version from goals if present (e.g., "make release v0.0.4" or "make release patch")
VERSION ?= $(filter v% patch minor major,$(MAKECMDGOALS))

## Make version targets (v*) and bump types no-ops for "make release" syntax
v%:
	@:

patch minor major:
	@:

## Release (creates version commit, pushes it, then uses gh to tag and create release)
release:
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION or bump type is required"; \
		echo "Usage: make release <version|patch|minor|major>"; \
		echo "  make release v0.0.4   - Release explicit version"; \
		echo "  make release patch    - Bump patch version (0.0.x)"; \
		echo "  make release minor    - Bump minor version (0.x.0)"; \
		echo "  make release major    - Bump major version (x.0.0)"; \
		exit 1; \
	fi
	@./scripts/release.sh $(VERSION)
