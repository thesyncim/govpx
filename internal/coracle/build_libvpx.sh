#!/usr/bin/env sh
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag"
prefix=${GOVPX_LIBVPX_PREFIX:-"$build_dir/libvpx-$tag-install"}
oracle_bin=${GOVPX_ORACLE_BIN:-"$build_dir/govpx-vpx-oracle"}
config_stamp="$prefix/.govpx-libvpx-config"
want_config="v1.16.0-vp8-decoder-postproc-error-concealment-optimized
src_dir=$src_dir
prefix=$prefix"
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

if { [ ! -f "$prefix/lib/libvpx.a" ] && [ ! -f "$prefix/lib/libvpx.dylib" ] && [ ! -f "$prefix/lib/libvpx.so" ]; } || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		fetch_source
		rm -rf "$prefix"
	fi
	(
		cd "$src_dir"
		./configure \
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
			--enable-error-concealment
		make -j"$jobs"
		make install
	)
	printf '%s\n' "$want_config" > "$config_stamp"
fi

cc=${CC:-cc}
libs=${GOVPX_LIBVPX_LIBS:-"-lvpx -lm -pthread"}

"$cc" -std=c99 -O3 -Wall -Wextra -I"$prefix/include" "$root/vpx_oracle.c" -L"$prefix/lib" $libs -o "$oracle_bin"
printf '%s\n' "$oracle_bin"
