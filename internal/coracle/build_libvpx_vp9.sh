#!/usr/bin/env sh
set -eu

# Builds a VP9-enabled libvpx (separate from the main VP8 oracle build)
# and compiles vp9_dsp_oracle.c against it. The output binary emits a
# pre-computed corpus of (input, dest_before, dest_after) records that
# the Go-side DSP oracle test reads from internal/vp9/dsp/testdata.

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vp9"
prefix=${GOVPX_LIBVPX_VP9_PREFIX:-"$build_dir/libvpx-$tag-vp9-install"}
oracle_bin=${GOVPX_VP9_DSP_ORACLE_BIN:-"$build_dir/govpx-vp9-dsp-oracle"}
config_stamp="$prefix/.govpx-libvpx-vp9-config"
want_config="v1.16.0-vp9-decoder+encoder-dsp-only-denoise-task264-host-pinned-r2
src_dir=$src_dir
prefix=$prefix"

# Determinism hardening — see build_vpxenc_oracle.sh for rationale (task #264).
if [ -z "${GOVPX_ORACLE_TARGET:-}" ]; then
	host_uname_s=$(uname -s 2>/dev/null || printf '')
	host_uname_m=$(uname -m 2>/dev/null || printf '')
	case "$host_uname_s/$host_uname_m" in
		Darwin/arm64)     GOVPX_ORACLE_TARGET=arm64-darwin20-gcc ;;
		Darwin/x86_64)    GOVPX_ORACLE_TARGET=x86_64-darwin19-gcc ;;
		Linux/aarch64)    GOVPX_ORACLE_TARGET=arm64-linux-gcc ;;
		Linux/arm64)      GOVPX_ORACLE_TARGET=arm64-linux-gcc ;;
		Linux/x86_64)     GOVPX_ORACLE_TARGET=x86_64-linux-gcc ;;
		*)                GOVPX_ORACLE_TARGET=generic-gnu ;;
	esac
fi
export SOURCE_DATE_EPOCH=0
export ZERO_AR_DATE=1
export LC_ALL=C
export TZ=UTC

archive="$build_dir/libvpx-$tag.tar.gz"
jobs=${JOBS:-}
if [ -z "$jobs" ]; then
	if command -v getconf >/dev/null 2>&1; then
		jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')
	else
		jobs=2
	fi
fi

mkdir -p "$build_dir"
if [ ! -f "$archive" ]; then
	curl -L -o "$archive" "https://chromium.googlesource.com/webm/libvpx/+archive/refs/tags/$tag.tar.gz"
fi

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if { [ ! -f "$prefix/lib/libvpx.a" ] && [ ! -f "$prefix/lib/libvpx.dylib" ] && [ ! -f "$prefix/lib/libvpx.so" ]; } || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		rm -rf "$src_dir" "$prefix"
	fi
	mkdir -p "$src_dir"
	tar -xzf "$archive" -C "$src_dir"
	(
		cd "$src_dir"
		./configure \
			--target="$GOVPX_ORACLE_TARGET" \
			--prefix="$prefix" \
			--disable-examples \
			--disable-tools \
			--disable-docs \
			--disable-unit-tests \
			--disable-debug \
			--disable-gprof \
			--enable-optimizations \
			--disable-vp8 \
			--enable-vp9 \
			--enable-vp9-encoder \
			--enable-vp9-decoder \
			--enable-vp9-temporal-denoising \
			--disable-vp9-highbitdepth \
			--disable-postproc \
			--disable-internal-stats
		make -j"$jobs"
		make install
	)
	printf '%s\n' "$want_config" > "$config_stamp"
fi

cc=${CC:-cc}
libs=${GOVPX_LIBVPX_VP9_LIBS:-"-lvpx -lm -pthread"}

"$cc" -std=c99 -O3 -Wall -Wextra -I"$prefix/include" \
	"$root/vp9_dsp_oracle.c" -L"$prefix/lib" $libs -o "$oracle_bin"
printf '%s\n' "$oracle_bin"
