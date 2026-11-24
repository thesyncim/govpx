#!/usr/bin/env sh
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag"
prefix=${GOVPX_LIBVPX_PREFIX:-"$build_dir/libvpx-$tag-install"}
oracle_bin=${GOVPX_ORACLE_BIN:-"$build_dir/govpx-vpx-oracle"}
config_stamp="$prefix/.govpx-libvpx-config"
want_config="v1.16.0-vp8-decoder-postproc-error-concealment-optimized-task264-host-pinned-task281-prefix-map
src_dir=$src_dir
prefix=$prefix"
jobs=${JOBS:-}

# Determinism hardening — see build_vpxenc_oracle.sh for the full rationale
# (task #264). Pin toolchain triple per host ISA so config.mk's TOOLCHAIN
# string does not drift on macOS minor-rev upgrades.
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

if { [ ! -f "$prefix/lib/libvpx.a" ] && [ ! -f "$prefix/lib/libvpx.dylib" ] && [ ! -f "$prefix/lib/libvpx.so" ]; } || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		fetch_source
		rm -rf "$prefix"
	fi
	# Path-prefix maps (task #281) — see build_vpxenc_oracle.sh for the
	# full rationale. Pass -ffile-prefix-map / -fdebug-prefix-map /
	# -fmacro-prefix-map to libvpx configure as defense-in-depth so any
	# future change that introduces a __FILE__-stamped string into the
	# decoder TUs cannot leak the per-worktree build path into libvpx.a
	# or the resulting govpx-vpx-oracle binary.
	prefix_map_cflags="-ffile-prefix-map=$src_dir=govpx-oracle -fdebug-prefix-map=$src_dir=govpx-oracle -fmacro-prefix-map=$src_dir=govpx-oracle"
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
			--disable-vp9 \
			--disable-vp9-highbitdepth \
			--enable-vp8 \
			--disable-vp8_encoder \
			--enable-vp8_decoder \
			--enable-postproc \
			--enable-error-concealment \
			--extra-cflags="$prefix_map_cflags"
		make -j"$jobs"
		make install
	)
	printf '%s\n' "$want_config" > "$config_stamp"
fi

cc=${CC:-cc}
libs=${GOVPX_LIBVPX_LIBS:-"-lvpx -lm -pthread"}

# Apply prefix-map flags to the final link step too, so the
# govpx-vpx-oracle binary (which compiles vpx_oracle.c from $root) does
# not embed the worktree path into any debug section the Apple linker
# decides to keep.
oracle_prefix_map="-ffile-prefix-map=$root=govpx-oracle -fdebug-prefix-map=$root=govpx-oracle -fmacro-prefix-map=$root=govpx-oracle"
"$cc" -std=c99 -O3 -Wall -Wextra $oracle_prefix_map -I"$prefix/include" "$root/vpx_oracle.c" -L"$prefix/lib" $libs -o "$oracle_bin"
printf '%s\n' "$oracle_bin"
