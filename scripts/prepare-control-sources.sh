#!/bin/sh
# The public-control coverage tests inspect the pinned libvpx control tables
# even when no external codec binaries have been built yet.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/internal/coracle/build"}
tag=v1.16.0
source_dir="$build_dir/libvpx-$tag-vpxenc-purec"
if [ -s "$source_dir/vp8/vp8_cx_iface.c" ] && [ -s "$source_dir/vp8/vp8_dx_iface.c" ]; then
	exit 0
fi

mkdir -p "$build_dir"
archive="$build_dir/libvpx-$tag.tar.gz"
if [ ! -s "$archive" ]; then
	archive_tmp=$(mktemp "$build_dir/libvpx-$tag.tar.gz.XXXXXX")
	trap 'rm -f "$archive_tmp"' EXIT
	curl -fsSL --retry 3 -o "$archive_tmp" "https://chromium.googlesource.com/webm/libvpx/+archive/refs/tags/$tag.tar.gz"
	mv "$archive_tmp" "$archive"
fi
mkdir -p "$source_dir"
tar -xzf "$archive" -C "$source_dir" vp8/vp8_cx_iface.c vp8/vp8_dx_iface.c
test -s "$source_dir/vp8/vp8_cx_iface.c"
test -s "$source_dir/vp8/vp8_dx_iface.c"
