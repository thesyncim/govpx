#!/usr/bin/env sh
set -eu

# Build a SIMD-disabled ("pure C") libvpx v1.16.0 vpxenc/vpxdec by forcing the
# generic-gnu target. This binary is the fair byte-parity reference for govpx's
# pure-Go build (`go test -tags purego`): both sides are scalar, so any byte
# divergence is an algorithm difference rather than a SIMD-rounding artifact.
#
# The default oracle (build_vpxenc.sh / build_vpxenc_oracle.sh) configures the
# host ISA target (e.g. arm64-darwin20-gcc), which links in NEON kernels. That
# is the fair reference for govpx's *assembly* build. Keep the two lanes
# separate: pure-Go vs generic-gnu here; Go+asm vs host-ISA there. See
# docs/validation.md and the fair-parity-comparison note.
#
# Determinism hardening mirrors build_vpxenc.sh (task #264): pinned locale/TZ
# and zeroed archive timestamps so the binary hash does not drift across
# rebuilds. The generic-gnu target itself carries no host-ISA / version-min
# state, so no toolchain-triple pinning is needed here.

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vpxenc-purec"
vpxenc_bin=${GOVPX_VPXENC_PUREC_BIN:-"$build_dir/vpxenc-purec"}
vpxdec_bin=${GOVPX_VPXDEC_PUREC_BIN:-"$build_dir/vpxdec-purec"}
config_stamp="$src_dir/.govpx-vpxenc-purec-config"
want_config="v1.16.0-vp8-purec-generic-gnu-fair-lane"
jobs=${JOBS:-}

export SOURCE_DATE_EPOCH=0
export ZERO_AR_DATE=1
export LC_ALL=C
export TZ=UTC

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

if [ ! -x "$src_dir/vpxenc" ] || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		fetch_source
	fi
	(
		cd "$src_dir"
		./configure \
			--target=generic-gnu \
			--disable-docs \
			--disable-unit-tests \
			--disable-debug \
			--disable-gprof \
			--enable-optimizations \
			--disable-vp9 \
			--disable-vp9-highbitdepth \
			--enable-vp8_encoder \
			--enable-vp8_decoder \
			--enable-postproc \
			--enable-error-concealment \
			--enable-vp8
		make -j"$jobs"
	)
	printf '%s\n' "$want_config" > "$config_stamp"
fi

cp "$src_dir/vpxenc" "$vpxenc_bin"
chmod +x "$vpxenc_bin"
cp "$src_dir/vpxdec" "$vpxdec_bin"
chmod +x "$vpxdec_bin"
printf '%s\n' "$vpxenc_bin"
