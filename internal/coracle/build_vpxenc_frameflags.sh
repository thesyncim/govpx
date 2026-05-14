#!/usr/bin/env sh
#
# Build vpxenc-frameflags, a companion VP8 encoder driver that exposes
# per-frame VPX_EFLAG_* / VP8_EFLAG_* flags to the test harness. Stock
# vpxenc only accepts whole-stream config, so byte-parity probes that
# need to drive EncodeForceKeyFrame / EncodeNoUpdateLast /
# EncodeNoReferenceGolden / EncodeForceGoldenFrame / ... per call use
# this binary as their libvpx reference. The driver is intentionally
# tiny — it links against libvpx.a from build_vpxenc_oracle.sh's source
# tree so no extra libvpx build step is needed.
#
# Sandbox limits: relies on build_vpxenc_oracle.sh having already
# materialized the patched libvpx-v1.16.0-vpxenc-oracle tree and built
# libvpx.a. If that step has not run yet, this script invokes it.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-v1.16.0-vpxenc-oracle"
bin=${GOVPX_VPXENC_FRAMEFLAGS_BIN:-"$build_dir/vpxenc-frameflags"}
config_stamp="$src_dir/.govpx-vpxenc-frameflags-config"
# Bump this whenever the C source or compile flags change; the test
# harness re-runs the build whenever this value differs from the
# stamp.
want_config="vpxenc-frameflags-2026-05-14-r11-runtime-roicustom"

mkdir -p "$build_dir"

# Make sure the patched libvpx tree and libvpx.a are ready.
if [ ! -f "$src_dir/libvpx.a" ]; then
	sh "$root/build_vpxenc_oracle.sh" >/dev/null
fi
if [ ! -f "$src_dir/libvpx.a" ]; then
	echo "build_vpxenc_frameflags.sh: libvpx.a missing under $src_dir" >&2
	exit 1
fi

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if [ -x "$bin" ] && [ "$current_config" = "$want_config" ]; then
	printf '%s\n' "$bin"
	exit 0
fi

cc=${CC:-cc}
cflags="${CFLAGS:-} -O2 -std=c11"
# We write the IVF container ourselves so we only need the public
# libvpx encoder interface from libvpx.a; no vpxenc tools_common /
# ivfenc objects are linked, which keeps this driver independent of
# the rest of the vpxenc build artifacts.
$cc $cflags \
	-I"$src_dir" \
	-I"$src_dir/vpx_ports" \
	"$root/vpxenc_frameflags.c" \
	"$src_dir/libvpx.a" \
	-lm -lpthread \
	-o "$bin"

chmod +x "$bin"
printf '%s\n' "$want_config" > "$config_stamp"
printf '%s\n' "$bin"
