#!/usr/bin/env bash
# Prints the body of the section of one version in CHANGELOG.md ("## vX.Y.Z - date").
# Used by the release workflow for the notes of a release.
set -euo pipefail

version=${1:?usage: changelog-section.sh vX.Y.Z [CHANGELOG.md]}
file=${2:-CHANGELOG.md}

awk -v v="$version" '
  /^## / { if (on) exit; if ($2 == v) on = 1; next }
  on { if (!started && $0 == "") next; started = 1; print }
' "$file"
