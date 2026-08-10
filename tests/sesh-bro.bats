#!/usr/bin/env bats
# Smoke tests for sesh-bro. Run against a mock `herdr` and stub `zoxide`
# (tests/mock-herdr, tests/mock-bin/zoxide) — no live Herdr needed.
# Run with: bats tests   (or: make test)

SESH_BRO="$BATS_TEST_DIRNAME/../sesh-bro"

fail() { printf 'FAIL: %s\n' "$*" >&2; return 1; }

setup() {
  export HERDR_BIN_PATH="$BATS_TEST_DIRNAME/mock-herdr"
  export PATH="$BATS_TEST_DIRNAME/mock-bin:$PATH"
  export HERDR_MOCK_LOG="$(mktemp "${TMPDIR:-/tmp}/herdr-mock.XXXXXX")"
  export SESH_BRO_PANE_CACHE="$(mktemp "${TMPDIR:-/tmp}/sesh-bro-panes.XXXXXX")"

  # Real dirs so `preview dir` has something to list, including spaces.
  export TESTDIR="$(mktemp -d "${TMPDIR:-/tmp}/sesh-bro.XXXXXX")"
  mkdir -p "$TESTDIR/proj alpha" "$TESTDIR/plain"
  printf 'MOCK README CONTENT\n' >"$TESTDIR/proj alpha/README.md"

  # zoxide stub input: /work/alpha is already open in a pane (excluded),
  # /work/other is not; the space-containing path must survive intact.
  export MOCK_ZOXIDE_LIST_FILE="$(mktemp "${TMPDIR:-/tmp}/zoxide-mock.XXXXXX")"
  printf '%s\n' "/work/alpha" "/work/other" "$TESTDIR/proj alpha" "$TESTDIR/plain" >"$MOCK_ZOXIDE_LIST_FILE"
}

teardown() {
  rm -rf "$TESTDIR" "$HERDR_MOCK_LOG" "$MOCK_ZOXIDE_LIST_FILE" "$SESH_BRO_PANE_CACHE"
}

@test "sesh-bro passes bash syntax check" {
  run bash -n "$SESH_BRO"
  [ "$status" -eq 0 ]
}

@test "list defaults to workspaces, agents, and dirs" {
  run "$SESH_BRO" list
  [ "$status" -eq 0 ]
  printf '%s\n' "$output" | grep -Fq $'workspace\tw1\t' || fail "missing workspace row"
  printf '%s\n' "$output" | grep -Fq $'agent\talpha\t' || fail "missing agent row"
  printf '%s\n' "$output" | grep -Fq $'dir\t' || fail "missing dir row"
}

@test "source flags isolate sources" {
  run "$SESH_BRO" list --agents
  [ "$status" -eq 0 ]
  printf '%s\n' "$output" | grep -Fq $'agent\t' || fail "missing agent row"
  ! printf '%s\n' "$output" | grep -Fq $'workspace\t' || fail "unexpected workspace row"
  ! printf '%s\n' "$output" | grep -Fq $'dir\t' || fail "unexpected dir row"
}

@test "list --json emits machine-readable rows" {
  run "$SESH_BRO" list --workspaces --json
  [ "$status" -eq 0 ]
  [ "$(printf '%s\n' "$output" | jq -s 'length')" -eq 3 ]
  [ "$(printf '%s\n' "$output" | jq -sr '.[0] | .type + ":" + .target')" = 'workspace:w1' ]
  [ "$(printf '%s\n' "$output" | jq -sr '.[0].status')" = 'idle' ]
}

@test "agents are sorted by status rank: blocked, working, idle" {
  run "$SESH_BRO" list --agents
  [ "$status" -eq 0 ]
  # agent rows: type\ttarget\t<icon> label detail — the picker uses the agent
  # *name* as target, so field 2 == label.
  labels="$(printf '%s\n' "$output" | awk -F'\t' '$1=="agent"{print $2}' | tr '\n' ' ')"
  [ "$labels" = 'gamma beta alpha ' ]
}

@test "current workspace sorts first" {
  run "$SESH_BRO" list --workspaces
  [ "$status" -eq 0 ]
  first="$(printf '%s\n' "$output" | head -1 | cut -f2)"
  [ "$first" = 'w1' ] || fail "focused workspace should sort first, got: $first"
}

@test "agents in the current workspace sort first" {
  run env HERDR_WORKSPACE_ID=w1 "$SESH_BRO" list --agents
  [ "$status" -eq 0 ]
  first="$(printf '%s\n' "$output" | head -1 | cut -f2)"
  [ "$first" = 'alpha' ] || fail "current workspace agent should sort first, got: $first"
}

@test "create makes a workspace for the given path" {
  run "$SESH_BRO" create "$TESTDIR/plain"
  [ "$status" -eq 0 ]
  grep -Fq "create workspace cwd=$TESTDIR/plain label=plain" "$HERDR_MOCK_LOG" \
    || fail "workspace create got wrong args"
}

@test "create rejects a nonexistent path" {
  run "$SESH_BRO" create "$TESTDIR/nope-missing"
  [ "$status" -ne 0 ]
}

@test "list --blocked filters to blocked agents only" {
  run "$SESH_BRO" list --blocked --json
  [ "$status" -eq 0 ]
  [ "$(printf '%s\n' "$output" | jq -s 'length')" -eq 1 ]
  [ "$(printf '%s\n' "$output" | jq -sr '.[0].target')" = 'gamma' ]
  [ "$(printf '%s\n' "$output" | jq -sr '.[0].status')" = 'blocked' ]
}

@test "--hide-current drops the current workspace and its agents" {
  run env HERDR_WORKSPACE_ID=w1 "$SESH_BRO" list --hide-current
  [ "$status" -eq 0 ]
  ! printf '%s\n' "$output" | grep -Fq $'workspace\tw1\t' || fail "current workspace should be hidden"
  ! printf '%s\n' "$output" | grep -Fq $'agent\talpha\t' || fail "current workspace's agents should be hidden"
  printf '%s\n' "$output" | grep -Fq $'workspace\tw2\t' || fail "other workspaces should remain"
}

@test "list --dirs excludes cwds already open in panes" {
  run "$SESH_BRO" list --dirs
  [ "$status" -eq 0 ]
  ! printf '%s\n' "$output" | grep -Fq '/work/alpha' || fail "open cwd should be excluded"
  printf '%s\n' "$output" | grep -Fq '/work/other' || fail "unknown cwd should be listed"
}

@test "list --dirs preserves paths with spaces" {
  run "$SESH_BRO" list --dirs
  [ "$status" -eq 0 ]
  printf '%s\n' "$output" | grep -Fq "$TESTDIR/proj alpha" || fail "dir row with spaces missing"
}

@test "list rejects unknown flags" {
  run "$SESH_BRO" list --bogus
  [ "$status" -eq 2 ]
}

@test "connect workspace focuses the workspace" {
  run "$SESH_BRO" connect workspace w2
  [ "$status" -eq 0 ]
  grep -Fq 'focus workspace w2' "$HERDR_MOCK_LOG" || fail "workspace was not focused"
}

@test "connect agent focuses the agent" {
  run "$SESH_BRO" connect agent beta
  [ "$status" -eq 0 ]
  grep -Fq 'focus agent beta' "$HERDR_MOCK_LOG" || fail "agent was not focused"
}

@test "connect dir focuses the workspace already open at that cwd" {
  run "$SESH_BRO" connect dir /work/alpha
  [ "$status" -eq 0 ]
  grep -Fq 'focus workspace w1' "$HERDR_MOCK_LOG" || fail "open workspace was not focused"
  ! grep -Fq 'create workspace' "$HERDR_MOCK_LOG" || fail "no workspace should be created"
}

@test "connect dir creates a workspace for an unknown cwd, spaces preserved" {
  run "$SESH_BRO" connect dir "$TESTDIR/proj alpha"
  [ "$status" -eq 0 ]
  grep -Fq "create workspace cwd=$TESTDIR/proj alpha label=proj alpha" "$HERDR_MOCK_LOG" \
    || fail "workspace create got wrong args"
}

@test "preview workspace renders header and live terminal content" {
  run "$SESH_BRO" preview workspace w1
  [ "$status" -eq 0 ]
  printf '%s\n' "$output" | grep -Fq 'alpha' || fail "missing workspace label"
  printf '%s\n' "$output" | grep -Fq 'MOCK-PANE-TERM w1:p1' || fail "missing pane terminal output"
}

@test "preview agent renders header and live terminal content" {
  run "$SESH_BRO" preview agent beta
  [ "$status" -eq 0 ]
  printf '%s\n' "$output" | grep -Fq 'beta' || fail "missing agent name"
  printf '%s\n' "$output" | grep -Fq 'working' || fail "missing agent status"
  printf '%s\n' "$output" | grep -Fq 'MOCK-AGENT-TERM beta' || fail "missing agent terminal output"
}

@test "preview dir lists the directory (spaces preserved) and its README" {
  run "$SESH_BRO" preview dir "$TESTDIR/proj alpha"
  [ "$status" -eq 0 ]
  printf '%s\n' "$output" | grep -Fq "▸ $TESTDIR/proj alpha" || fail "missing dir header"
  printf '%s\n' "$output" | grep -Fq 'MOCK README CONTENT' || fail "missing README content"
}

@test "picker fails fast when fzf is missing" {
  run env PATH="/usr/bin:/bin" "$SESH_BRO" picker
  [ "$status" -eq 1 ]
  printf '%s\n' "$output" | grep -Fq 'fzf is required' || fail "expected fzf-required error"
}

@test "version reads from the manifest (single source of truth)" {
  run "$SESH_BRO" --version
  [ "$status" -eq 0 ]
  manifest_ver="$(grep -E '^version = ' "$BATS_TEST_DIRNAME/../herdr-plugin.toml" | head -1 | sed -E 's/version = "(.*)"/\1/')"
  printf '%s\n' "$output" | grep -Fq "sesh-bro $manifest_ver" || fail "version mismatch: $output"
}

@test "startup validates deps and clears the cache" {
  run "$SESH_BRO" startup
  [ "$status" -eq 0 ]
  printf '%s\n' "$output" | grep -Fq 'deps ok' || fail "startup did not report deps ok"
}

@test "list fails loudly when herdr binary is missing" {
  run env HERDR_BIN_PATH=/nonexistent/herdr "$SESH_BRO" list
  [ "$status" -ne 0 ]
  printf '%s\n' "$output" | grep -iFq 'herdr' || fail "expected herdr error message"
}

@test "list fails loudly when herdr daemon is down" {
  # A mock that errors on workspace list simulates a dead daemon.
  cat > "$BATS_TEST_TMPDIR/broken-herdr" <<'EOF'
#!/usr/bin/env bash
echo '{"error":{"code":"daemon_down"}}' >&2
exit 1
EOF
  chmod +x "$BATS_TEST_TMPDIR/broken-herdr"
  run env HERDR_BIN_PATH="$BATS_TEST_TMPDIR/broken-herdr" "$SESH_BRO" list
  [ "$status" -ne 0 ]
}

@test "open invokes the plugin pane open API" {
  run env MOCK_POPUP_OPEN= "$SESH_BRO" open --agents
  [ "$status" -eq 0 ]
  grep -Fq 'plugin pane open --plugin sesh-bro --entrypoint picker --env SESH_BRO_ARGS=--agents' "$HERDR_MOCK_LOG" \
    || fail "open did not forward the right args"
}

@test "open rejects unknown flags" {
  run "$SESH_BRO" open --bogus
  [ "$status" -eq 2 ]
}

@test "worktree creates a workspace from a GitHub URL" {
  run env HERDR_PLUGIN_CLICKED_URL="https://github.com/joshmedeski/sesh/issues/409" "$SESH_BRO" worktree
  [ "$status" -eq 0 ]
  grep -Fq 'create workspace' "$HERDR_MOCK_LOG" || fail "worktree did not create a workspace"
}

@test "worktree rejects a malformed URL" {
  run "$SESH_BRO" worktree "not-a-url"
  [ "$status" -ne 0 ]
}

@test "sort order config reorders source blocks" {
  run env SESH_BRO_SORT_ORDER="agents,workspaces" "$SESH_BRO" list --json
  [ "$status" -eq 0 ]
  types="$(printf '%s\n' "$output" | jq -r '.type' | head -1)"
  [ "$types" = 'agent' ] || fail "agents should sort first, got: $types"
}

@test "blacklist filters dirs" {
  run env SESH_BRO_BLACKLIST="/work/*" "$SESH_BRO" list --dirs
  [ "$status" -eq 0 ]
  ! printf '%s\n' "$output" | grep -Fq '/work/other' || fail "blacklisted dir should be hidden"
}

@test "icon config overrides glyphs" {
  run env SESH_BRO_ICON_WORKSPACE="W" "$SESH_BRO" list --workspaces
  [ "$status" -eq 0 ]
  # Icon is wrapped in ANSI color codes: <color>W<reset>
  printf '%s\n' "$output" | grep -Fq "W$(printf '\033[0m')" || fail "custom icon missing"
}
