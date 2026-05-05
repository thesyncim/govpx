#!/usr/bin/env sh
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${LIBGOPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag"
prefix=${LIBGOPX_LIBVPX_PREFIX:-"$build_dir/libvpx-$tag-install"}
oracle_bin=${LIBGOPX_ORACLE_BIN:-"$build_dir/gopx-vpx-oracle"}
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

if [ ! -f "$prefix/lib/libvpx.a" ] && [ ! -f "$prefix/lib/libvpx.dylib" ] && [ ! -f "$prefix/lib/libvpx.so" ]; then
	(
		cd "$src_dir"
		./configure \
			--prefix="$prefix" \
			--disable-examples \
			--disable-tools \
			--disable-docs \
			--disable-unit-tests \
			--disable-vp9 \
			--disable-vp9-highbitdepth \
			--enable-vp8 \
			--enable-decoder \
			--disable-encoder
		make -j"$jobs"
		make install
	)
fi

cc=${CC:-cc}
libs=${LIBGOPX_LIBVPX_LIBS:-"-lvpx -lm -pthread"}

"$cc" -std=c99 -O2 -Wall -Wextra -I"$prefix/include" "$root/vpx_oracle.c" -L"$prefix/lib" $libs -o "$oracle_bin"
printf '%s\n' "$oracle_bin"
