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

# THE CANDIDATE DELIVERY PATH IS REMOVED.
#
# `candidate:<run-id>/<artifact>@sha256:<digest>` downloaded a pre-publication
# build artifact and re-derived its digest, so a scored arm could run a binary
# no release names. That is the unpublished-candidate authority chain the
# component map removes: it existed to break a release cycle for a research
# proof, and the practical product installs a released binary or builds from
# the checked-out source. TB_CANDIDATE_BINARY_DIGEST goes with it.

# --- released: download + checksum-verify --------------------------------------
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

: "${TB_REPO:?TB_REPO is required to download a released binary}"

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

# THE ARCHIVE IS ENUMERATED COMPLETELY BEFORE ANYTHING IS EXTRACTED.
#
# This validation used to live only on the candidate delivery path, so removing
# that path would have taken it with it — and it is not candidate-specific: it
# is what makes extracting a downloaded archive safe at all. The component map
# keeps exhaustive archive-member validation; only the delivery mode goes.
#
# There is no pipeline in the decision. `tar | grep -q` let a SIGPIPE hide a bad
# FIRST entry — grep exits on its match and tar dies writing to a closed pipe —
# so each listing is captured in one command substitution whose status is tar's
# own, every line is walked, and every offender is counted and named before
# anything is reported.
entry_listing=$(tar -tzvf "$work/$asset") || {
  echo "install-testbucket: $asset could not be enumerated" >&2
  exit 1
}
name_listing=$(tar -tzf "$work/$asset") || {
  echo "install-testbucket: $asset member names could not be enumerated" >&2
  exit 1
}
if [ -z "$name_listing" ]; then
  echo "install-testbucket: $asset enumerates no members" >&2
  exit 1
fi

irregular_count=0
irregular_first=""
while IFS= read -r entry; do
  [ -n "$entry" ] || continue
  case "$entry" in
    -*) ;;
    *)
      irregular_count=$((irregular_count + 1))
      [ -n "$irregular_first" ] || irregular_first="$entry"
      ;;
  esac
done <<EOF
$entry_listing
EOF
if [ "$irregular_count" -ne 0 ]; then
  echo "install-testbucket: $asset holds $irregular_count member(s) that are not regular files (symlink, hardlink, device or directory entry)" >&2
  echo "first such entry: $irregular_first" >&2
  echo "Only regular files are installable; a link is a name for bytes the checksum did not cover." >&2
  exit 1
fi

unsafe_count=0
unsafe_first=""
while IFS= read -r member; do
  [ -n "$member" ] || continue
  case "$member" in
    /*|../*|*/../*|*/..|..)
      unsafe_count=$((unsafe_count + 1))
      [ -n "$unsafe_first" ] || unsafe_first="$member"
      ;;
  esac
done <<EOF
$name_listing
EOF
if [ "$unsafe_count" -ne 0 ]; then
  echo "install-testbucket: $asset holds $unsafe_count absolute or traversing path(s)" >&2
  echo "first such path: $unsafe_first" >&2
  exit 1
fi

# A FRESH directory, so nothing that arrived beside the archive can be mistaken
# for something the archive contained.
extracted="$work/extracted"
mkdir -p "$extracted"
tar -xzf "$work/$asset" -C "$extracted"
# A FIXED member, not a search: `testbucket` at the archive root is what the
# release archives carry, so an archive without it is refused rather than
# fallen back from.
release_bin="$extracted/testbucket"
# -h first: `[ -f ]` follows symlinks, so the link check comes before the
# regular-file check rather than after it.
if [ -h "$release_bin" ]; then
  echo "install-testbucket: $asset's testbucket member is a symlink" >&2
  exit 1
fi
if [ ! -f "$release_bin" ]; then
  echo "install-testbucket: $asset holds no testbucket member at its root" >&2
  exit 1
fi
# ANY execute bit, not just the owner's: a member executable by group or other
# is executable, and the stated rule is one binary.
extra=$(find "$extracted" \( -perm -u+x -o -perm -g+x -o -perm -o+x \) ! -type d ! -path "$release_bin" | head -n1)
if [ -n "$extra" ]; then
  echo "install-testbucket: $asset also holds executable $extra; a release archive carries one binary" >&2
  exit 1
fi
cp "$release_bin" "$work/testbucket"

# THE BYTES ARE CHECKED AGAINST A ROOT OUTSIDE THE RELEASE.
#
# The verification above compares the archive with `checksums.txt` — and both
# are assets of the SAME release. `gh release upload --clobber` can replace
# them together, and the Releases API reports every current testbucket release
# as `immutable: false`, so an actor able to publish assets can swap the
# archive and its checksum in one step and that check still passes. A tag name
# is metadata, and this authenticated an archive against metadata that moves
# with it.
#
# THE RELEASE-PIN SECOND ROOT IS REMOVED.
#
# What stood here verified a released binary against `released-binary-digests.tsv`
# — a reviewed file in this repository naming the digest of the binary inside
# each published archive — together with an optional later commit that carried
# the pin for a release published after this action's own.
#
# That data file is REMOVE-classified by the component map, and a check whose
# root of trust no longer exists cannot be kept: it would refuse every release
# on a missing file. Release proof is excluded from this scope (the map's own
# "candidate delivery and release proof"), so the path goes with its root.
#
# WHAT STILL PROTECTS THIS INSTALL, unchanged and deliberately so:
#
#   - the archive is enumerated COMPLETELY before anything is installed, with
#     every offending entry counted and named — the repair R54's own parent
#     commit landed, after `tar | grep -q` let a SIGPIPE hide a bad first entry;
#   - the one-archive rule, the fixed member name, and the symlink and
#     any-execute-bit refusals;
#
# The archive validation above was written on the candidate path and now runs
# on the one delivery path that remains, because it never depended on which
# delivery it was: it is what makes extracting a downloaded archive safe.


install -m 0755 "$work/testbucket" "$bin"
printf '%s\n' "$TB_BINDIR" >>"${GITHUB_PATH:-/dev/null}"
"$bin" version
