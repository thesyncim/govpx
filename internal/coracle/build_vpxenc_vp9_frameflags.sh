#!/usr/bin/env sh
#
# Build vpxenc-vp9-frameflags, a small VP9 encoder driver that exposes
# per-frame VPX_EFLAG_* / VP8_EFLAG_* flags to byte-parity tests. Stock
# vpxenc accepts stream-level options only, so tests that need to drive
# EncodeForceKeyFrame / EncodeNoUpdate* / EncodeNoReference* / forced
# GOLDEN/ALTREF refreshes per frame use this binary as the libvpx reference.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-v1.16.0-vpxdec-vp9"
bin=${GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN:-"$build_dir/vpxenc-vp9-frameflags"}
config_stamp="$src_dir/.govpx-vpxenc-vp9-frameflags-config"
want_config="vpxenc-vp9-frameflags-2026-05-15-r15"

mkdir -p "$build_dir"

if [ ! -f "$src_dir/libvpx.a" ]; then
	sh "$root/build_vpxdec_vp9.sh" >/dev/null
fi
if [ ! -f "$src_dir/libvpx.a" ]; then
	echo "build_vpxenc_vp9_frameflags.sh: libvpx.a missing under $src_dir" >&2
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
$cc $cflags \
	-I"$src_dir" \
	-I"$src_dir/vpx_ports" \
	"$root/vpxenc_vp9_frameflags.c" \
	"$src_dir/libvpx.a" \
	-lm -lpthread \
	-o "$bin"

chmod +x "$bin"
printf '%s\n' "$want_config" > "$config_stamp"
printf '%s\n' "$bin"
