#!/usr/bin/env bash
# candidate-resolver.sh — decide whether a value may be trusted to RESOLVE a
# pre-publication candidate's authorised binary digest.
#
# The resolver reads the signed Stage-1 manifest and prints the binary digest
# the campaign authority approved, and the installer then refuses any candidate
# whose bytes do not match. That check is only worth anything if the party
# doing the reading is not the party being checked.
#
# So the resolver must be a PUBLISHED IMMUTABLE RELEASE, and this refuses every
# other spelling:
#
#   (empty)          nothing to resolve with, and silence is not a default
#   local | source   builds the checked-out tree — which, on a candidate run,
#                    IS the candidate. The candidate would authenticate its own
#                    approval, which is the one thing this route exists to
#                    prevent. The previous guard only rejected `candidate:`,
#                    so `local` walked straight through it.
#   candidate:…      the same, said out loud
#   vN | vN.M        a MOVING ALIAS. Two runs can resolve it differently and
#                    whoever may move the alias chooses the resolver, so it is
#                    descriptive metadata rather than a delivery identity.
#   vN.M.P           accepted: one immutable published release
#
# Required env:
#   TB_RESOLVER_VERSION   the value to judge
set -euo pipefail

v="${TB_RESOLVER_VERSION-}"
v="$(printf '%s' "$v" | tr -d '[:space:]')"

refuse() {
  echo "candidate delivery: candidate-resolver-version $1" >&2
  echo "A candidate's authorised binary digest is read from the signed Stage-1 manifest by a PUBLISHED IMMUTABLE RELEASE (an exact vMAJOR.MINOR.PATCH), never by the candidate itself and never through a moving alias." >&2
  exit 1
}

case "$v" in
  "")
    refuse "is empty, so a candidate: delivery has nothing to authenticate it"
    ;;
  local|source)
    refuse "is '$v', which BUILDS THE CHECKED-OUT TREE — on a candidate run that is the candidate, so it would verify its own approval"
    ;;
  candidate:*)
    refuse "is '$v', a candidate pin; a candidate cannot authenticate its own approval"
    ;;
esac
if ! printf '%s' "$v" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  if printf '%s' "$v" | grep -Eq '^v[0-9]+(\.[0-9]+)?$'; then
    refuse "is the moving alias '$v'; an alias is descriptive metadata, not a delivery identity, and whoever may move it chooses the resolver"
  fi
  refuse "is '$v', which is not an exact published release tag (vMAJOR.MINOR.PATCH)"
fi
echo "candidate delivery: resolver pinned to the published release $v"
