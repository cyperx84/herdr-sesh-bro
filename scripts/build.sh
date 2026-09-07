#!/bin/sh
# sesh-bro bootstrap build.
#
# Runs on `herdr plugin install`, after user confirmation. Two paths:
#
#   1. A local Go toolchain is present: build from source. This is always the
#      preferred path — it produces an exact match for the source in this
#      checkout, not whatever the last tagged release happened to contain.
#   2. No toolchain: download a prebuilt binary from GitHub Releases and
#      verify its SHA256 before it touches disk. Verification is mandatory,
#      never optional — a checksum fetched from the same release the binary
#      came from proves nothing about tampering in transit or a compromised
#      release; it only catches truncated downloads. The checksum this script
#      trusts is pinned in expected_sha256() below, in the source tree, not
#      fetched alongside the binary.
#
# Those pins have to be written before the release they describe exists —
# there is no other order available — so they are treated as an assertion CI
# checks rather than a value anyone has to trust. .github/workflows/release.yml
# rebuilds all four targets on the tag and refuses to publish the release if
# any pin below disagrees with what it just built. A stale or mistyped hash
# therefore fails loudly, in front of whoever is cutting the release, instead
# of shipping a fallback that silently refuses to install on every machine
# without a Go toolchain. docs/RELEASING.md is the ritual that keeps it true.
#
# Pattern copied verbatim from herdr-loop's scripts/build.sh (itself adopted
# from cloudmanic/herdr-plus per herdr-loop's PLAN.md §7), with OUT/PKG/REPO/
# VERSION swapped for this plugin. herdr-plugin.toml gives platforms=["linux",
# "macos"] their own [[build]] entry pointing at this script — see that
# file's own comment for why Windows has no entry here (no POSIX sh, and
# fzf/zoxide's Windows story is unverified for this plugin specifically).
set -eu

# cd to the plugin root regardless of where herdr invokes this from, so the
# relative "bin/sesh-bro" output path below is unambiguous.
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$root_dir"

OUT="bin/sesh-bro"
PKG="./cmd/sesh-bro"

if command -v go >/dev/null 2>&1; then
    echo "sesh-bro: building with local Go toolchain..." >&2
    exec go build -o "$OUT" "$PKG"
fi

echo "sesh-bro: no Go toolchain on PATH — falling back to a prebuilt release binary" >&2

# --- release pin -----------------------------------------------------------
#
# Bump VERSION and the platform table together, in the same commit as the
# tagged release they describe — that commit is the one the tag points at,
# so the two can never be a release apart. A binary this script cannot find
# an entry for is refused, not silently skipped — see the fallback branch at
# the bottom of platform_target(). cmd/sesh-bro/buildscript_test.go fails the
# ordinary test suite if VERSION here and the version in herdr-plugin.toml
# drift apart, which is how this line came to say v0.3.0 for a tag that was
# never cut.
VERSION="v0.4.0"
REPO="cyperx84/herdr-sesh-bro"

os=$(uname -s)
arch=$(uname -m)

platform_target() {
    case "$os" in
        Darwin)
            case "$arch" in
                arm64) echo "darwin-arm64"; return 0 ;;
                x86_64) echo "darwin-amd64"; return 0 ;;
            esac
            ;;
        Linux)
            case "$arch" in
                x86_64) echo "linux-amd64"; return 0 ;;
                aarch64|arm64) echo "linux-arm64"; return 0 ;;
            esac
            ;;
    esac
    echo "sesh-bro: unsupported platform $os/$arch — install a Go toolchain and rerun" >&2
    return 1
}

target=$(platform_target)

# SHA256 of the release asset for each target, pinned here rather than
# fetched from the release alongside the binary (see header comment).
#
# These have to be committed BEFORE the tag that produces the artifacts they
# describe, which looks like a chicken-and-egg and is resolved by making the
# pin a verified assertion rather than a hope: the release workflow rebuilds
# each target, recomputes its SHA256, and FAILS THE RELEASE if any value here
# disagrees. So a wrong pin breaks the release loudly instead of shipping a
# fallback that silently refuses to install. docs/RELEASING.md has the ritual.
#
# UNRELEASED is the deliberate pre-release state: it fails the check below,
# which is correct — until a tag exists there is no asset to download, and a
# machine with no Go toolchain should be told to install one rather than sent
# to a 404.
expected_sha256() {
    case "$1" in
        darwin-arm64) echo "5e6f414a26422fe0288242192ee16df5bec3602d29467c2a7ee9d617655bd9f6" ;;
        darwin-amd64) echo "fd0869474f43b6a430a4cb7262c4f20b08da93b7307cc314bb58e1b30862016e" ;;
        linux-amd64)  echo "09fe04b8ccf1fb0a391535bcc8793bc0d5c98f451a2899dbcacaf4048e60e4fb" ;;
        linux-arm64)  echo "d08008ff010e6dc044f7aecbf38e872d35f39e2e206890056a81f8953bd95ca7" ;;
        *) return 1 ;;
    esac
}

want_sha256=$(expected_sha256 "$target") || {
    echo "sesh-bro: no pinned checksum for $target — install a Go toolchain and rerun" >&2
    exit 1
}
if [ "$want_sha256" = "UNRELEASED" ]; then
    echo "sesh-bro: no $VERSION release published yet for $target." >&2
    echo "sesh-bro: install a Go toolchain (https://go.dev/dl/) and rerun, or wait for a tagged release." >&2
    exit 1
fi

asset="sesh-bro-${VERSION}-${target}.tar.gz"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "sesh-bro: downloading $url" >&2
if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$tmp/$asset" "$url"
elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$tmp/$asset" "$url"
else
    echo "sesh-bro: neither curl nor wget is available — install a Go toolchain instead" >&2
    exit 1
fi

# macOS ships shasum, not sha256sum; Linux is the reverse. Try both rather
# than assuming one.
sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        echo "sesh-bro: no sha256sum/shasum available — cannot verify the download, aborting" >&2
        exit 1
    fi
}

got_sha256=$(sha256 "$tmp/$asset")
if [ "$got_sha256" != "$want_sha256" ]; then
    # Loud and fatal — never fall through to using an unverified binary.
    echo "sesh-bro: CHECKSUM MISMATCH for $asset" >&2
    echo "sesh-bro:   expected $want_sha256" >&2
    echo "sesh-bro:   got      $got_sha256" >&2
    echo "sesh-bro: refusing to install a binary that fails verification" >&2
    exit 1
fi
echo "sesh-bro: checksum verified" >&2

mkdir -p bin
tar -xzf "$tmp/$asset" -C "$tmp"
mv "$tmp/sesh-bro" "$OUT"
chmod +x "$OUT"
echo "sesh-bro: installed $OUT ($target, $VERSION)" >&2
