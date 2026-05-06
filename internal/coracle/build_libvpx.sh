#!/usr/bin/env sh
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOPVX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag"
prefix=${GOPVX_LIBVPX_PREFIX:-"$build_dir/libvpx-$tag-install"}
oracle_bin=${GOPVX_ORACLE_BIN:-"$build_dir/gopvx-vpx-oracle"}
config_stamp="$prefix/.gopvx-libvpx-config"
want_config="v1.16.0-vp8-decoder-postproc-error-concealment-optimized"
jobs=${JOBS:-}

if [ -z "$jobs" ]; then
	if command -v getconf >/dev/null 2>&1; then
		jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')
	else
		jobs=2
	fi
fi

mkdir -p "$build_dir"

if [ ! -d "$src_dir" ]; then
	archive="$build_dir/libvpx-$tag.tar.gz"
	curl -L -o "$archive" "https://chromium.googlesource.com/webm/libvpx/+archive/refs/tags/$tag.tar.gz"
	mkdir -p "$src_dir"
	tar -xzf "$archive" -C "$src_dir"
fi

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if { [ ! -f "$prefix/lib/libvpx.a" ] && [ ! -f "$prefix/lib/libvpx.dylib" ] && [ ! -f "$prefix/lib/libvpx.so" ]; } || [ "$current_config" != "$want_config" ]; then
	(
		cd "$src_dir"
		if [ -f config.mk ]; then
			make distclean
		fi
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
libs=${GOPVX_LIBVPX_LIBS:-"-lvpx -lm -pthread"}

"$cc" -std=c99 -O2 -Wall -Wextra -I"$prefix/include" "$root/vpx_oracle.c" -L"$prefix/lib" $libs -o "$oracle_bin"
printf '%s\n' "$oracle_bin"
