#!/usr/bin/env sh
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${LIBGOPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vpxenc"
vpxenc_bin=${LIBGOPX_VPXENC_BIN:-"$build_dir/vpxenc"}
vpxdec_bin=${LIBGOPX_VPXDEC_BIN:-"$build_dir/vpxdec"}
config_stamp="$src_dir/.libgopx-vpxenc-config"
want_config="v1.16.0-vp8-tools-postproc-error-concealment-optimized"
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

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

if [ ! -x "$src_dir/vpxenc" ] || [ "$current_config" != "$want_config" ]; then
	(
		cd "$src_dir"
		if [ -f config.mk ]; then
			make distclean
		fi
		./configure \
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
