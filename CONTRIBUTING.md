# Contributing

Thanks for wanting to improve sesh-bro! This project is a Go binary plus a
Herdr manifest; the original bash implementation (and its bats harness)
was removed in 0.4.0 and lives on in git history at commit `0d683a3`.

## Development setup

```sh
git clone https://github.com/cyperx84/herdr-sesh-bro.git
cd herdr-sesh-bro
make check          # shellcheck scripts/build.sh + go vet + go test
```

You need a Go toolchain (version per `go.mod`) and `shellcheck` for
`make lint`. No live Herdr daemon is required to test — see below.

## What needs care

- **Command-level tests go through the `run()` seam** —
  `run(args, stdin, out, err, getenv)` in `cmd/sesh-bro/main_test.go`.
  Never spawn the compiled binary from a test; drive the command in
  process and inject the environment with the `fakeEnv` helper.
- **A fake herdr socket server exists** at `internal/herdrx/herdrtest`.
  It speaks the same wire protocol as `github.com/cyperx84/herdr-api`'s
  client, so a test can exercise real RPC paths by pointing
  `HERDR_SOCKET_PATH` (through `fakeEnv`) at `herdrtest.Start(t)`.
- **No new dependencies** — stdlib plus the existing
  `github.com/cyperx84/herdr-api` only.
- **Comments explain why, not what** — cite the `docs/BEHAVIOUR.md`
  section (the behaviour oracle) or the demand source a behaviour comes
  from, the way the existing code does.
- **Version lives only in `herdr-plugin.toml`** — never hardcode a
  version in the Go code.

## Releasing

1. Bump `version` in `herdr-plugin.toml` and add a `CHANGELOG.md` entry.
2. Tag `vX.Y.Z` and push — the release workflow builds the GitHub Release.
3. The marketplace index (herdr.dev/plugins) picks up the `herdr-plugin`
   topic automatically.
