# Releasing sesh-bro

Written for whoever is cutting a release at 2am. Every command below is meant
to be pasted verbatim. Substitute `0.4.0` / `v0.4.0` for the version you are
actually shipping.

## The one weird thing, up front

`scripts/build.sh` is the plugin's `[[build]]` command. On a machine with a Go
toolchain it builds from source and none of this matters. On a machine
*without* one it downloads a prebuilt tarball from the GitHub release and
verifies its SHA256 against a value pinned in `expected_sha256()` — pinned in
the source tree, deliberately, because a checksum shipped alongside the binary
it describes proves nothing.

That pin has to be committed **before** the tag that produces the artifact it
describes. There is no order in which it doesn't. So the pin cannot be
derived; it can only be **asserted**, and CI's job is to check the assertion:
`.github/workflows/release.yml` rebuilds all four targets on the tag and
refuses to publish if any pin disagrees with what it just built.

Which is why the ritual below has a dry run in the middle. It is not
ceremony — it is the only way to learn the hashes.

Two consequences worth having in your head before you start:

- **The build must not depend on which commit it came from.** Go stamps the
  git revision into every binary by default and neither `-trimpath` nor
  `-ldflags='-s -w'` removes it. If it were left on, pasting a hash would
  create a new commit, which would change the hash, which would fail the
  check — forever, with no way out. Hence `-buildvcs=false` in the workflow.
  Do not remove it.
- **Between the dry run and the real tag, change only the four hash lines.**
  Any edit under `cmd/` or `internal/` produces a different binary and the
  hashes you just pasted go stale. Docs, `CHANGELOG.md` and
  `herdr-plugin.toml` are not compiled in and are safe — but do them *before*
  the dry run anyway, so there is one less thing to think about.

## The ritual

### 1. Start clean, on the commit you intend to release

```sh
git checkout main && git pull
git status --porcelain   # must print nothing
make check               # go vet + the full test suite + shellcheck
```

### 2. Bump the version in three places

| File | Change |
|---|---|
| `herdr-plugin.toml` | `version = "0.4.0"` — the authority; the workflow gates the tag against it |
| `CHANGELOG.md` | `## [0.4.0] - Unreleased` → `## [0.4.0] - 2026-09-07` (today's date) |
| `scripts/build.sh` | `VERSION="v0.4.0"` — note the `v`, it is a tag name |

`cmd/sesh-bro/buildscript_test.go` fails if the script and the manifest
disagree, so `make check` catches a forgotten third row. Nothing catches a
forgotten CHANGELOG heading except the release notes coming out empty.

```sh
git commit -am "chore: 0.4.0"
```

### 3. Dry run: push a throwaway tag to learn the hashes

```sh
git tag v0.4.0-dryrun
git push origin v0.4.0-dryrun
```

Expect this run to go **green then red**, on purpose:

- the four `build` jobs succeed and each writes one copy-pasteable line to the
  run's **Summary** page;
- the `release` job fails immediately at *Verify manifest version matches
  tag*, because `0.4.0-dryrun` is not `0.4.0`. Nothing is published.

Open the run in **Actions → the `v0.4.0-dryrun` run → Summary**. You are
looking for four lines like:

```sh
darwin-amd64) echo "3f2a…" ;;
```

### 4. Paste the four hashes into `scripts/build.sh`

Replace the body of `expected_sha256()` with those four lines, keeping the
existing indentation and the `*) return 1 ;;` arm at the bottom. Order does
not matter. Do not edit anything else.

```sh
git commit -am "chore: pin v0.4.0 release checksums"
```

### 5. Bin the throwaway tag

```sh
git push --delete origin v0.4.0-dryrun
git tag -d v0.4.0-dryrun
```

### 6. Tag for real and push

```sh
git push origin main
git tag -a v0.4.0 -m "sesh-bro v0.4.0"
git push origin v0.4.0
```

CI rebuilds all four targets, confirms every pin matches, extracts the
`## [0.4.0]` section of `CHANGELOG.md` as the release notes, and publishes the
release with five files attached: the four tarballs and `checksums.txt`.

### 7. Confirm the thing you actually shipped

The point of all this is the no-toolchain path, and it is the one path no test
exercises. On a machine (or container) with no `go` on `PATH`:

```sh
herdr plugin install cyperx84/herdr-sesh-bro
```

It should print `sesh-bro: checksum verified`. If it prints
`no v0.4.0 release published yet`, a pin is still `UNRELEASED` and the release
should not have been created — check the workflow run.

## Why you cannot just compute the hashes locally

You can reproduce the **binary** locally. You cannot reliably reproduce the
**tarball**.

The binary is deterministic given the same source, the same GOOS/GOARCH, the
same toolchain version and `-trimpath -buildvcs=false`. The tarball is not:
GitHub's runners use GNU `tar` and GNU `gzip`, macOS ships `bsdtar` and Apple
`gzip`. The workflow normalises everything it can (`--format=ustar`,
`--mtime='@0'`, fixed numeric ownership, `gzip -n`), and that is enough to make
CI agree with *itself* across two runs — which is all the dry run needs — but
bsdtar does not even accept those flags, and two different deflate
implementations are not obliged to emit the same bytes. A hash computed on a
Mac will differ, and it will differ in a way that looks exactly like a real
mismatch.

So: **CI is the source of truth for the pinned tarball hashes.** What a local
build is good for is confirming CI built the binary you think it did. Each
`build` job's log prints `binary  sha256: …`; reproduce it with:

```sh
# GOTOOLCHAIN pins the compiler to go.mod's version — CI installs exactly that
# via setup-go, and a newer local Go will silently produce a different binary.
export GOTOOLCHAIN="go$(awk '/^go /{print $2}' go.mod)"
for t in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  GOOS="${t%/*}" GOARCH="${t#*/}" CGO_ENABLED=0 \
    go build -trimpath -buildvcs=false -ldflags='-s -w' \
      -o "/tmp/sesh-bro-${t%/*}-${t#*/}" ./cmd/sesh-bro
done
shasum -a 256 /tmp/sesh-bro-*
```

For reference, the packaging step CI runs on top of each binary — reproducible
only where GNU tar and GNU gzip are what you have:

```sh
tar --format=ustar --owner=0 --group=0 --numeric-owner --mtime='@0' \
    -cf - sesh-bro | gzip -9n > "sesh-bro-v0.4.0-darwin-arm64.tar.gz"
```

## Decoding a failed release run

| What you see | What it means | What to do |
|---|---|---|
| `herdr-plugin.toml version (X) != tag version (Y)` | The manifest and the tag disagree — or you are on a dry-run tag | On a dry run this is expected; otherwise fix step 2 and retag |
| `PENDING <target> pin is still the UNRELEASED placeholder` | You tagged for real without doing step 4 | The step summary now has the hashes; do step 4 and retag |
| `MISMATCH <target>` with `pinned` and `built` hashes | The pins are stale — almost always because a source file changed between the dry run and the tag | Paste the `built` hashes from the summary, retag. If the source genuinely did not change, the build is not reproducible and that is a real bug, not a pin problem |
| `expected 4 pins in scripts/build.sh expected_sha256(), parsed N` | Somebody reformatted the case statement and the workflow's extraction no longer parses it | Restore the one-arm-per-line `target) echo "hash" ;;` shape, or update the `awk`/`sed` in the workflow to match |
| `scripts/build.sh pins VERSION="X" but the tag is "Y"` | Step 2's third row was missed | `make check` would have caught this; fix and retag |

A failed release job publishes nothing, so recovering is always the same
shape: fix the commit, delete the tag locally and remotely, retag, push.

```sh
git push --delete origin v0.4.0
git tag -d v0.4.0
```

Moving a tag is only safe because a failed run creates no release — nobody can
have downloaded anything yet. Once a release exists, do not move its tag; cut
a patch version instead.

## First time doing this?

Do step 3's dry run even if you are confident. The failure modes it catches
are cheap there and expensive after a real tag exists, and the run summary it
produces is the only place the four hashes appear together.
