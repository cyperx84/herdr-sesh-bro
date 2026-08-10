# Contributing

Thanks for wanting to improve sesh-bro! This project is a single bash script
plus a Herdr manifest, so the bar to contribute is low.

## Development setup

```sh
git clone https://github.com/cyperx84/herdr-sesh-bro.git
cd herdr-sesh-bro
make check          # shellcheck + bats tests (vendored tools, no install needed)
```

## What needs care

- **`sesh-bro` runs under `set -euo pipefail`** — new code must be strict-mode
  safe (guard array expansions, quote expansions, handle `$(...)` failures).
- **Keep shellcheck clean** — `make lint` must pass. If a warning is a
  deliberate pattern (e.g. glob matching in the blacklist), add a targeted
  `# shellcheck disable` with a reason.
- **Every new command/flag needs a bats test** — `tests/mock-herdr` and
  `tests/mock-bin/zoxide` let you test without a live daemon. Extend the mock
  when you touch the herdr API surface.
- **Version lives only in `herdr-plugin.toml`** — the script reads it at
  startup. Never hardcode a version in the script.

## Feature ideas (unclaimed)

- tmux session source (like sesh's `-t`)
- per-entry `preview_command` config (sesh-style)
- `--watch` live preview refresh
- Windows support (would need a non-bash runtime)

## Releasing

1. Bump `version` in `herdr-plugin.toml` and add a `CHANGELOG.md` entry.
2. Tag `vX.Y.Z` and push — the release workflow builds the GitHub Release.
3. The marketplace index (herdr.dev/plugins) picks up the `herdr-plugin`
   topic automatically.
