#!/usr/bin/env bash
# install-testbucket.sh — put a `testbucket` binary on PATH for the composite
# actions (plan / run-bucket / record). It is the ONE installer the three
# actions share: each runs it via "${{ github.action_path }}/../install-testbucket.sh"
# so there is a single, reviewed way the binary appears on a runner.
#
# Two modes, chosen by TB_VERSION:
#
#   local            build from the checked-out source (go build ./cmd/testbucket).
#                    This is the dogfood / pre-release path: testbucket's own
#                    CI has no published release to download yet, and a consumer
#                    who vendors the source can pin to it too.
#
#   v1 | v1.2 | v1.2.3   download the released binary from the GitHub Release and
#                    verify its sha256 against the release's checksums.txt. A bare
#                    major (v1) or major.minor (v1.2) is the *moving alias*: it
#                    resolves to the HIGHEST published, non-draft, non-prerelease
#                    release under that prefix — the same convention the release
#                    workflow's `v<major>` tag points at — so `@v1` always fetches
#                    the newest 1.x binary. An exact vX.Y.Z downloads that release.
#
# The binary is dropped in TB_BINDIR and that dir is prepended to $GITHUB_PATH,
# so later steps in the same job just call `testbucket`.
#
# Required env:
#   TB_VERSION   local | vX | vX.Y | vX.Y.Z            (what to install)
#   TB_REPO      owner/name of the testbucket repo     (for release downloads)
#   TB_BINDIR    directory to install the binary into   (created if missing)
# Optional env:
#   GH_TOKEN     token for `gh release list`            (only the alias path needs it)
set -euo pipefail

: "${TB_VERSION:?TB_VERSION is required (local | vX | vX.Y | vX.Y.Z)}"
: "${TB_BINDIR:?TB_BINDIR is required}"
mkdir -p "$TB_BINDIR"
bin="$TB_BINDIR/testbucket"

# --- local: build from source --------------------------------------------------
if [ "$TB_VERSION" = "local" ] || [ "$TB_VERSION" = "source" ]; then
  # Build the testbucket that OWNS this action, resolved from the script's OWN
  # location inside the ($/-fetched) action checkout — NOT the suite's
  # working-directory, which is where the consumer's tests live and holds no
  # testbucket source. This makes `local` correct for any caller shape (Go,
  # Vitest, nested working-directory) and for the dogfood alike: the script sits
  # at <repo>/.github/actions/install-testbucket.sh, so <repo> is two levels up.
  source_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
  echo "installing testbucket from source at $source_root (TB_VERSION=$TB_VERSION)"
  if ! command -v go >/dev/null 2>&1; then
    echo "install-testbucket: a local build needs Go on PATH; provision Go or pin a released --version" >&2
    exit 1
  fi
  ( cd "$source_root" && go build -o "$bin" ./cmd/testbucket )
  chmod +x "$bin"
  printf '%s\n' "$TB_BINDIR" >>"${GITHUB_PATH:-/dev/null}"
  "$bin" version || true
  exit 0
fi

# --- released: download + checksum-verify --------------------------------------
: "${TB_REPO:?TB_REPO is required to download a released binary}"

# Map the runner's OS/arch onto goreleaser's naming. Fall back to uname so the
# script is exercisable outside Actions.
os_raw="${RUNNER_OS:-$(uname -s)}"
arch_raw="${RUNNER_ARCH:-$(uname -m)}"
case "$os_raw" in
  Linux|linux)   os=linux ;;
  macOS|Darwin|darwin) os=darwin ;;
  *) echo "install-testbucket: unsupported OS '$os_raw' (linux/darwin are released)" >&2; exit 1 ;;
esac
case "$arch_raw" in
  X64|x86_64|amd64) arch=amd64 ;;
  ARM64|arm64|aarch64) arch=arm64 ;;
  *) echo "install-testbucket: unsupported arch '$arch_raw' (amd64/arm64 are released)" >&2; exit 1 ;;
esac

# Resolve TB_VERSION to a concrete release tag.
tag=""
if printf '%s' "$TB_VERSION" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  # An exact release tag: use it directly, no API call needed.
  tag="$TB_VERSION"
elif printf '%s' "$TB_VERSION" | grep -qE '^v[0-9]+(\.[0-9]+)?$'; then
  # A moving alias (v1 or v1.2): resolve to the highest PUBLISHED stable release
  # under the prefix. A published, non-draft, non-prerelease release is the only
  # kind guaranteed to have downloadable binaries — the same reason the release
  # workflow points its v<major> tag there rather than at the highest raw tag.
  if ! command -v gh >/dev/null 2>&1; then
    echo "install-testbucket: resolving the '$TB_VERSION' alias needs the gh CLI (or pin an exact vX.Y.Z)" >&2
    exit 1
  fi
  tag=$(gh release list --repo "$TB_REPO" --json tagName,isDraft,isPrerelease --limit 200 \
      | jq -r '.[] | select(.isDraft==false and .isPrerelease==false) | .tagName' \
      | grep -E "^${TB_VERSION}\.[0-9]+" | grep -E "^v[0-9]+\.[0-9]+\.[0-9]+$" \
      | sort -V | tail -n1)
  if [ -z "$tag" ]; then
    echo "install-testbucket: no published stable release under '$TB_VERSION' in $TB_REPO" >&2
    exit 1
  fi
  echo "resolved alias $TB_VERSION -> $tag"
else
  echo "install-testbucket: invalid --version '$TB_VERSION' (want local | vX | vX.Y | vX.Y.Z)" >&2
  exit 1
fi

version_noV="${tag#v}"
asset="testbucket_${version_noV}_${os}_${arch}.tar.gz"
base="https://github.com/${TB_REPO}/releases/download/${tag}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "downloading $asset from $tag"
curl -fsSL -o "$work/$asset" "$base/$asset"
curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt"

# Verify the archive against the release's checksums before trusting it.
( cd "$work"
  line=$(grep -E "  ${asset}\$" checksums.txt || true)
  if [ -z "$line" ]; then
    echo "install-testbucket: $asset not listed in checksums.txt" >&2
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s\n' "$line" | sha256sum -c -
  else
    printf '%s\n' "$line" | shasum -a 256 -c -
  fi )

tar -xzf "$work/$asset" -C "$work" testbucket
install -m 0755 "$work/testbucket" "$bin"
printf '%s\n' "$TB_BINDIR" >>"${GITHUB_PATH:-/dev/null}"
"$bin" version
