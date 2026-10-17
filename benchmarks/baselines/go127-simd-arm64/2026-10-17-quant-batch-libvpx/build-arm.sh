#!/bin/sh
set -eu
source_dir=/private/tmp/govpx-libvpx-arm-upstream
output_dir=/private/tmp/govpx-libvpx-arm-evidence
mkdir -p "$source_dir" "$output_dir"
archive=/Users/thesyncim/GolandProjects/govpx/internal/coracle/build/libvpx-v1.16.0.tar.gz
shasum -a 256 "$archive" > "$output_dir/source-archive-sha256.txt"
tar -xzf "$archive" -C "$source_dir"
export SOURCE_DATE_EPOCH=0 ZERO_AR_DATE=1 LC_ALL=C TZ=UTC
cd "$source_dir"
./configure --target=arm64-darwin20-gcc --disable-docs --disable-unit-tests \
 --disable-debug --disable-gprof --enable-optimizations --enable-runtime-cpu-detect \
 --enable-vp8 --enable-vp9 --enable-vp8-encoder --enable-vp8-decoder \
 --enable-vp9-encoder --enable-vp9-decoder --enable-vp9-temporal-denoising \
 --disable-vp9-highbitdepth --enable-postproc --enable-vp9-postproc --enable-error-concealment
make -j4
cp vpx_config.h "$output_dir/vpx_config.h"
cp config.mk "$output_dir/libvpx-config.mk"
ar t libvpx.a > "$output_dir/libvpx-objects.txt"
shasum -a 256 vpxenc > "$output_dir/libvpx-binary-sha256.txt"
cc --version > "$output_dir/c-compiler.txt"
