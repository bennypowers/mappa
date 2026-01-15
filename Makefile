.PHONY: all test lint clean install
.PHONY: linux-x64 linux-arm64 darwin-x64 darwin-arm64 win32-x64 win32-arm64
.PHONY: build-shared-windows-image

BINARY_NAME := mappa
DIST_DIR := dist/bin
GO_BUILD_FLAGS := -ldflags="-s -w"

# Shared Windows cross-compilation image (from go-release-workflows)
SHARED_WINDOWS_CC_IMAGE := mappa-shared-windows-cc

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

# Build the shared Windows cross-compilation image (uses go-release-workflows Containerfile)
build-shared-windows-image:
	@if ! podman image exists $(SHARED_WINDOWS_CC_IMAGE); then \
		echo "Building shared Windows cross-compilation image..."; \
		curl -fsSL https://raw.githubusercontent.com/bennypowers/go-release-workflows/main/.github/actions/setup-windows-build/Containerfile \
			| podman build -t $(SHARED_WINDOWS_CC_IMAGE) -f - .; \
	else \
		echo "Image $(SHARED_WINDOWS_CC_IMAGE) already exists, skipping build."; \
	fi

# Windows targets (requires Podman)
win32-x64: build-shared-windows-image
	@mkdir -p $(DIST_DIR)
	podman run --rm \
		-v $(PWD):/src:Z \
		-w /src \
		-e GOOS=windows \
		-e GOARCH=amd64 \
		-e CGO_ENABLED=1 \
		-e CC=x86_64-w64-mingw32-gcc \
		-e CXX=x86_64-w64-mingw32-g++ \
		$(SHARED_WINDOWS_CC_IMAGE) \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-win32-x64.exe .

win32-arm64: build-shared-windows-image
	@mkdir -p $(DIST_DIR)
	podman run --rm \
		-v $(PWD):/src:Z \
		-w /src \
		-e GOOS=windows \
		-e GOARCH=arm64 \
		-e CGO_ENABLED=1 \
		-e CC=aarch64-w64-mingw32-gcc \
		-e CXX=aarch64-w64-mingw32-g++ \
		$(SHARED_WINDOWS_CC_IMAGE) \
		go build $(GO_BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-win32-arm64.exe .
