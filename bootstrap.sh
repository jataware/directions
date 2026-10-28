#!/usr/bin/env bash
# Initialize from this local checkout; no network access or installs.
set -euo pipefail

if (( $# > 1 )); then
  printf 'Usage: %s [repository-directory]\n' "$0" >&2
  exit 2
fi

source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
target="${1:-.}"
if [[ ! -d "$target" ]]; then
  printf 'Repository directory does not exist: %s\n' "$target" >&2
  exit 1
fi
target="$(cd -- "$target" && pwd -P)"
destination="$target/.directions"

# mkdir is an atomic refusal for any existing entry, including dangling links.
# Do not remove this directory on later failure: it may contain user work.
if ! mkdir -- "$destination"; then
  printf 'Refusing to replace an existing .directions entry: %s\n' "$destination" >&2
  exit 1
fi

trap 'printf "Bootstrap incomplete; inspect %s before retrying.\n" "$destination" >&2' ERR
mkdir -- "$destination/contexts" "$destination/policies"
# Exclusive creation also protects files created concurrently after mkdir.
(
  set -o noclobber
  # Read from the local, reviewed checkout. No repository code is executed.
  while IFS= read -r line || [[ -n "$line" ]]; do printf '%s\n' "$line"; done \
    < "$source_dir/.directions/base.toml" > "$destination/base.toml"
  while IFS= read -r line || [[ -n "$line" ]]; do printf '%s\n' "$line"; done \
    < "$source_dir/.directions/contexts/general.md" > "$destination/contexts/general.md"
)
printf 'Initialized %s\n' "$destination"
printf 'Review model preferences and configure your client separately.\n'
