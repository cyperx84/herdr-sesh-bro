SHELL := bash

# Prefer vendored tools (tools/) so `make check` works even when Homebrew is
# unavailable; fall back to system-installed shellcheck/bats (e.g. in CI).
SHELLCHECK := $(or $(wildcard tools/bin/shellcheck),shellcheck)
BATS := $(or $(wildcard tools/bin/bats),bats)

.PHONY: lint test check

lint:
	$(SHELLCHECK) sesh-bro tests/mock-herdr tests/mock-bin/zoxide

test:
	$(BATS) tests

check: lint test
