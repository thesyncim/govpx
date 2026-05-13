#!/usr/bin/env sh
set -eu

# Build a VP9-enabled libvpx vpxdec for the byte-parity oracle gate.
# The default build_libvpx.sh / build_vpxenc.sh both pass
# --disable-vp9 to keep their VP8 binaries lean; build_libvpx_vp9.sh
# enables VP9 but passes --disable-tools (it only needs libvpx.a for
# vp9_dsp_oracle.c). This script builds the standalone vpxdec tool
# with VP9 enabled so the Go-side oracle test can pipe the encoder's
# IVF output through it.
#
# Output binary lands at $build/vpxdec-vp9 by default; override via
# GOVPX_VPXDEC_VP9_BIN.

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vpxdec-vp9"
vpxdec_vp9_bin=${GOVPX_VPXDEC_VP9_BIN:-"$build_dir/vpxdec-vp9"}
vpxenc_vp9_bin=${GOVPX_VPXENC_VP9_BIN:-"$build_dir/vpxenc-vp9"}
config_stamp="$src_dir/.govpx-vpxdec-vp9-config"
want_config="v1.16.0-vp9-encoder+decoder-tools-optimized
src_dir=$src_dir
vpxdec_vp9_bin=$vpxdec_vp9_bin
vpxenc_vp9_bin=$vpxenc_vp9_bin"
jobs=${JOBS:-}

if [ -z "$jobs" ]; then
	if command -v getconf >/dev/null 2>&1; then
		jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')
	else
		jobs=2
	fi
fi

mkdir -p "$build_dir"
archive="$build_dir/libvpx-$tag.tar.gz"

fetch_source() {
	if [ ! -f "$archive" ]; then
		curl -L -o "$archive" "https://chromium.googlesource.com/webm/libvpx/+archive/refs/tags/$tag.tar.gz"
	fi
	rm -rf "$src_dir"
	mkdir -p "$src_dir"
	tar -xzf "$archive" -C "$src_dir"
}

if [ ! -d "$src_dir" ]; then
	fetch_source
fi

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if [ ! -x "$src_dir/vpxdec" ] || [ ! -x "$src_dir/vpxenc" ] || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		fetch_source
	fi
	(
		cd "$src_dir"
		./configure \
			--disable-docs \
			--disable-unit-tests \
			--disable-debug \
			--disable-gprof \
			--enable-optimizations \
			--disable-vp8 \
			--enable-vp9 \
			--enable-vp9_decoder \
			--enable-vp9_encoder \
			--disable-vp9-highbitdepth \
			--disable-postproc
		make -j"$jobs"
	)
	printf '%s\n' "$want_config" > "$config_stamp"
fi

cp "$src_dir/vpxdec" "$vpxdec_vp9_bin"
chmod +x "$vpxdec_vp9_bin"
cp "$src_dir/vpxenc" "$vpxenc_vp9_bin"
chmod +x "$vpxenc_vp9_bin"
printf '%s\n' "$vpxdec_vp9_bin"
printf '%s\n' "$vpxenc_vp9_bin"
