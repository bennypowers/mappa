.PHONY: all test lint clean install

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
	go build -o mappa .

install: all
	cp mappa ~/.local/bin/mappa

clean:
	rm -f mappa
	go clean -cache -testcache

test:
	gotestsum -- -race $(RACE_LDFLAGS) ./...

lint:
	go vet ./...
	golangci-lint run
