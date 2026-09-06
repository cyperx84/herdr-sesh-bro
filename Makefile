SHELL := bash

# shellcheck is only needed for scripts/build.sh now — the bash
# implementation and its bats harness are gone (0.4.0; see
# docs/BEHAVIOUR.md §0 for where the old tests/ harness lives). The
# vendored tools/ copy went with them, so a system shellcheck is required
# for `make lint`; CI installs it explicitly.
# Skip rather than fail when shellcheck is absent: it lints one 40-line
# script, so a missing linter must not block `make check` on a fresh
# checkout. CI installs shellcheck explicitly, so the skip never hides a
# real finding there.
lint:
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck scripts/build.sh; \
	else \
		echo "make: shellcheck not installed - skipping lint (CI runs it)"; \
	fi

# gotest is the real gate: vet + the full Go test suite, which includes
# the fake-herdr-socket tests (internal/herdrx/herdrtest), so it needs no
# live daemon.
gotest:
	go vet ./... && go test ./...

.PHONY: lint gotest check

check: lint gotest
