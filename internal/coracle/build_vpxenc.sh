#!/usr/bin/env sh
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${LIBGOPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vpxenc"
vpxenc_bin=${LIBGOPX_VPXENC_BIN:-"$build_dir/vpxenc"}
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
	if [ ! -f "$archive" ]; then
		curl -L -o "$archive" "https://chromium.googlesource.com/webm/libvpx/+archive/refs/tags/$tag.tar.gz"
	fi
	mkdir -p "$src_dir"
	tar -xzf "$archive" -C "$src_dir"
fi

if [ ! -x "$src_dir/vpxenc" ]; then
	(
		cd "$src_dir"
		./configure \
			--disable-docs \
			--disable-unit-tests \
			--disable-vp9 \
			--disable-vp9-highbitdepth \
			--enable-vp8
		make -j"$jobs"
	)
fi

cp "$src_dir/vpxenc" "$vpxenc_bin"
chmod +x "$vpxenc_bin"
printf '%s\n' "$vpxenc_bin"
