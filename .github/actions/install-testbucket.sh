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

# --- candidate: an immutable PRE-PUBLICATION artifact ---------------------------
#
# This is what breaks the release cycle. A scored candidate arm has to run the
# exact binary being proposed, but that binary cannot be installed from a
# published release: publication is itself gated on the campaign the scored arm
# is part of. Building from source instead would deliver bytes nobody attested,
# and there is no release asset at an arbitrary commit to checksum-verify.
#
# So a candidate is delivered as an artifact of the build workflow run that
# produced it, addressed by IMMUTABLE run identity and pinned by digest:
#
#   --version candidate:<workflow-run-id>/<artifact-name>@sha256:<64-hex>
#
# The run id and artifact name say exactly which upload this is — they cannot be
# moved the way a tag can — and the digest is what makes the download evidence
# rather than a fetch. Both are required, and the archive is refused unless its
# bytes hash to the digest that was demanded. This is deliberately NOT a
# published release and never claims to be one.
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

if printf '%s' "$TB_VERSION" | grep -q '^candidate:'; then
  spec="${TB_VERSION#candidate:}"
  cand_digest="${spec##*@}"
  cand_path="${spec%@*}"
  cand_run="${cand_path%%/*}"
  cand_artifact="${cand_path#*/}"
  if [ "$cand_digest" = "$spec" ] || [ "$cand_path" = "$cand_run" ] ||
     [ -z "$cand_run" ] || [ -z "$cand_artifact" ] || [ -z "$cand_digest" ]; then
    echo "install-testbucket: a candidate needs candidate:<run-id>/<artifact>@sha256:<64-hex>; got '$TB_VERSION'" >&2
    echo "An unpinned pre-publication binary is a download, not a delivery identity." >&2
    exit 1
  fi
  if ! printf '%s' "$cand_digest" | grep -Eq '^sha256:[0-9a-f]{64}$'; then
    echo "install-testbucket: candidate digest '$cand_digest' is not sha256:<64-hex>" >&2
    exit 1
  fi
  if ! printf '%s' "$cand_run" | grep -Eq '^[0-9]+$'; then
    echo "install-testbucket: candidate run id '$cand_run' is not a workflow run id" >&2
    exit 1
  fi
  # THE ARTIFACT NAMES THE PLATFORM IT IS FOR.
  #
  # "Exactly one archive" is not "the one archive for this runner": a candidate
  # artifact that happened to hold a single foreign-platform build would have
  # been accepted and installed. The artifact name must end in the runner's own
  # os_arch, so the delivery is uniquely addressed per platform and a candidate
  # built for another one cannot be fetched by accident.
  if ! printf '%s' "$cand_artifact" | grep -q -- "-${os}_${arch}\$"; then
    echo "install-testbucket: candidate artifact '$cand_artifact' does not name this runner's platform (expected a name ending -${os}_${arch})" >&2
    echo "A candidate is published per platform so the delivery is uniquely addressed; one archive is not the same as the right archive." >&2
    exit 1
  fi
  : "${TB_REPO:?TB_REPO is required to download a candidate artifact}"
  if ! command -v gh >/dev/null 2>&1; then
    echo "install-testbucket: downloading a candidate artifact needs the gh CLI" >&2
    exit 1
  fi
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  echo "downloading candidate artifact $cand_artifact from run $cand_run"
  gh run download "$cand_run" --repo "$TB_REPO" --name "$cand_artifact" --dir "$work"
  # EXACTLY ONE ARCHIVE, AND NOTHING BESIDE IT IS TRUSTED.
  #
  # Choosing the first .tar.gz found anywhere under the download, digesting
  # only that, and then searching the WHOLE download for something named
  # testbucket meant the digest authenticated one file while an unrelated
  # sibling executable was the thing that got installed. The digest has to
  # govern the bytes that run, so: the artifact must contain exactly one
  # archive, that archive is what the digest is checked against, and the
  # binary is taken from a FRESH directory holding only that archive's
  # contents.
  archive_count=$(find "$work" -type f -name '*.tar.gz' | wc -l | tr -d ' ')
  if [ "$archive_count" -ne 1 ]; then
    echo "install-testbucket: candidate artifact $cand_artifact holds $archive_count .tar.gz members; a candidate artifact carries exactly one, for one platform" >&2
    echo "Publish a per-platform candidate artifact so the delivery names one archive and cannot be ambiguous." >&2
    exit 1
  fi
  archive=$(find "$work" -type f -name '*.tar.gz')
  got="sha256:$(sha256sum "$archive" | cut -d' ' -f1)"
  if [ "$got" != "$cand_digest" ]; then
    echo "install-testbucket: candidate archive digests to $got, not the demanded $cand_digest" >&2
    echo "A pre-publication artifact is trusted only because its bytes were named in advance." >&2
    exit 1
  fi
  # WHAT IS INSIDE IS CONSTRAINED BEFORE IT IS UNPACKED.
  #
  # A tar can carry symlinks, hardlinks, devices and paths that climb out of
  # the extraction directory. The fixed-member check below uses `[ -f ]`, which
  # FOLLOWS a symlink, while the ambiguity check used `find -type f`, which does
  # not — so a member named `testbucket` that was a symlink to something
  # outside the verified tree was installed and executed. Listing the archive
  # first and refusing anything that is not a regular file, and any path that
  # is absolute or contains "..", closes that before a single byte is written.
  if tar -tzvf "$archive" | grep -qvE '^-'; then
    echo "install-testbucket: the pinned candidate archive holds a member that is not a regular file (symlink, hardlink, device or directory entry)" >&2
    echo "Only regular files are installable; a link is a name for bytes the digest did not cover." >&2
    exit 1
  fi
  if tar -tzf "$archive" | grep -qE '^/|(^|/)\.\.(/|$)'; then
    echo "install-testbucket: the pinned candidate archive holds an absolute or traversing path" >&2
    exit 1
  fi
  # A FRESH directory, so nothing that arrived beside the archive can be
  # mistaken for something the archive contained.
  pinned="$work/pinned"
  mkdir -p "$pinned"
  tar -xzf "$archive" -C "$pinned"
  # A FIXED member, not a search. `testbucket` at the archive root is the
  # member the release archives carry, and asking for it by name means an
  # archive that does not contain it is refused rather than fallen back from.
  cand_bin="$pinned/testbucket"
  # -h first: `[ -f ]` follows symlinks, so the link check has to come before
  # the regular-file check rather than after it.
  if [ -h "$cand_bin" ]; then
    echo "install-testbucket: the pinned candidate archive's testbucket member is a symlink" >&2
    exit 1
  fi
  if [ ! -f "$cand_bin" ]; then
    echo "install-testbucket: the pinned candidate archive holds no testbucket member at its root" >&2
    exit 1
  fi
  # And nothing else executable travelled inside it either: a second binary in
  # the archive is the same ambiguity one level in.
  # ANY execute bit, not just the owner's: a member executable by group or
  # other is executable, and the stated rule is one binary.
  extra=$(find "$pinned" \( -perm -u+x -o -perm -g+x -o -perm -o+x \) ! -type d ! -path "$cand_bin" | head -n1)
  if [ -n "$extra" ]; then
    echo "install-testbucket: the pinned candidate archive also holds executable $extra; a candidate archive carries one binary" >&2
    exit 1
  fi
  # THE INSTALLED BYTES ARE RE-DIGESTED when the caller states what the
  # builder and verifier attested. The archive digest says which archive; this
  # says which binary, and Stage 1 binds the second.
  # MANDATORY, not optional. An optional check that nothing supplies is not a
  # check: the archive digest says which archive was downloaded, and this says
  # which binary is about to run — which is the value Stage 1 binds and the
  # builder and verifier attested. Without it the delivery is verified up to
  # the archive and unverified at the thing that executes.
  if [ -z "${TB_CANDIDATE_BINARY_DIGEST:-}" ]; then
    echo "install-testbucket: a candidate needs TB_CANDIDATE_BINARY_DIGEST, the attested sha256 of the binary itself" >&2
    echo "The archive digest names which archive; this names which binary, and Stage 1 binds the second." >&2
    exit 1
  fi
  if ! printf '%s' "$TB_CANDIDATE_BINARY_DIGEST" | grep -Eq '^sha256:[0-9a-f]{64}$'; then
    echo "install-testbucket: TB_CANDIDATE_BINARY_DIGEST '$TB_CANDIDATE_BINARY_DIGEST' is not sha256:<64-hex>" >&2
    exit 1
  fi
  bin_got="sha256:$(sha256sum "$cand_bin" | cut -d' ' -f1)"
  if [ "$bin_got" != "$TB_CANDIDATE_BINARY_DIGEST" ]; then
    echo "install-testbucket: the installed candidate binary digests to $bin_got, not the attested $TB_CANDIDATE_BINARY_DIGEST" >&2
    exit 1
  fi
  echo "candidate binary digest matches the attested $bin_got"
  mv "$cand_bin" "$bin"
  chmod +x "$bin"
  printf '%s\n' "$TB_BINDIR" >>"${GITHUB_PATH:-/dev/null}"
  "$bin" version || true
  exit 0
fi

# --- released: download + checksum-verify --------------------------------------
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

tar -xzf "$work/$asset" -C "$work" testbucket
install -m 0755 "$work/testbucket" "$bin"
printf '%s\n' "$TB_BINDIR" >>"${GITHUB_PATH:-/dev/null}"
"$bin" version
