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
# tagged release they describe. A binary this script cannot find an entry
# for is refused, not silently skipped — see the fallback branch at the
# bottom of platform_target().
VERSION="v0.3.0"
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
# fetched from the release alongside the binary (see header comment). No
# tagged release exists yet, so every entry is a placeholder that
# deliberately fails the check below until this script is updated for a real
# v0.3.0 tag.
expected_sha256() {
    case "$1" in
        darwin-arm64) echo "UNRELEASED" ;;
        darwin-amd64) echo "UNRELEASED" ;;
        linux-amd64)  echo "UNRELEASED" ;;
        linux-arm64)  echo "UNRELEASED" ;;
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
