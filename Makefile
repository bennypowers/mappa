.PHONY: test lint

# Workaround for Gentoo Linux "hole in findfunctab" error with race detector
# See: https://bugs.gentoo.org/961618
# Gentoo's Go build has issues with the race detector and internal linker.
# Using external linker resolves the issue.
ifeq ($(shell test -f /etc/gentoo-release && echo yes),yes)
    RACE_LDFLAGS := -ldflags="-linkmode=external"
else
    RACE_LDFLAGS :=
endif

test:
	gotestsum -- -race $(RACE_LDFLAGS) ./...

lint:
	go vet ./...
	golangci-lint run
