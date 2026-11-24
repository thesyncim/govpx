#!/bin/sh
# Use the project-wide Codex cache wrapper when installed. CI and other
# contributors get the same git-common-dir cache scope without Codex.
set -eu
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.4}"
export GOEXPERIMENT="${GOEXPERIMENT-simd}"
go_bin=${GO:-go}
project_wrapper="${HOME}/.codex/bin/project-env"
if [ -x "$project_wrapper" ]; then
 exec "$project_wrapper" "$go_bin" "$@"
fi
project_common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
project_id=$(printf '%s\n' "$project_common_dir" | git hash-object --stdin)
project_cache_root="${CODEX_PROJECT_CACHE_ROOT:-${XDG_CACHE_HOME:-${TMPDIR:-/tmp}/govpx-cache}}/$project_id"
project_agent_id=$(printf '%s' "${CODEX_AGENT_ID:-interactive}" | tr -cs '[:alnum:]._-' '_')
export GOCACHE="$project_cache_root/go-build"
export GOTMPDIR="$project_cache_root/tmp/$project_agent_id"
mkdir -p "$GOCACHE" "$GOTMPDIR"
exec "$go_bin" "$@"
