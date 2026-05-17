#!/usr/bin/env sh
set -eu

# Compiles vp9_token_cost_oracle.c against the VP9-enabled libvpx
# install produced by build_libvpx_vp9.sh, then emits the corpus blob
# read by TestVP9CostTokensMatchesLibvpxOracle (and
# TestPtEnergyClassMatchesLibvpxOracleBlob) under
# internal/vp9/encoder/testdata/token_cost_oracle.bin.
#
# Re-run when libvpx is updated (vp9_pareto8_full / vp9_coef_tree /
# vp9_model_to_full_probs) or when the corpus in vp9_token_cost_oracle.c
# changes.

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$root/../.." && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
prefix=${GOVPX_LIBVPX_VP9_PREFIX:-"$build_dir/libvpx-$tag-vp9-install"}

# Ensure the VP9 libvpx build is in place. The companion script is
# idempotent so calling it again on a hot prefix is cheap.
sh "$root/build_libvpx_vp9.sh" >/dev/null

oracle_bin=${GOVPX_VP9_TOKEN_COST_ORACLE_BIN:-"$build_dir/govpx-vp9-token-cost-oracle"}
testdata="$repo_root/internal/vp9/encoder/testdata/token_cost_oracle.bin"

cc=${CC:-cc}
libs=${GOVPX_LIBVPX_VP9_LIBS:-"-lvpx -lm -pthread"}

"$cc" -std=c99 -O2 -Wall -Wextra -I"$prefix/include" \
	"$root/vp9_token_cost_oracle.c" -L"$prefix/lib" $libs -o "$oracle_bin"

mkdir -p "$(dirname "$testdata")"
"$oracle_bin" > "$testdata"
printf '%s\n' "$testdata"
