#!/usr/bin/env sh
set -eu

tag="v1.16.0"
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-$tag-vpxenc"
vpxenc_bin=${GOVPX_VPXENC_BIN:-"$build_dir/vpxenc"}
vpxdec_bin=${GOVPX_VPXDEC_BIN:-"$build_dir/vpxdec"}
temporal_svc_bin=${GOVPX_VPX_TEMPORAL_SVC_ENCODER_BIN:-"$build_dir/vpx_temporal_svc_encoder"}
config_stamp="$src_dir/.govpx-vpxenc-config"
want_config="v1.16.0-vp8-tools-postproc-error-concealment-temporal-svc-optimized-task264-host-pinned
src_dir=$src_dir
vpxenc_bin=$vpxenc_bin
vpxdec_bin=$vpxdec_bin
temporal_svc_bin=$temporal_svc_bin"
jobs=${JOBS:-}

# Determinism hardening — see build_vpxenc_oracle.sh for the full rationale
# (task #264). Pin toolchain triple per host ISA so config.mk's TOOLCHAIN
# string does not drift on macOS minor-rev upgrades, and force LC_ALL/TZ/
# SOURCE_DATE_EPOCH/ZERO_AR_DATE so any time/locale-dependent step in the
# libvpx configure pipeline produces stable output. CPU-extension flags
# (NEON/DOTPROD/I8MM/SVE) are intentionally left at libvpx's auto-detect
# defaults; VP8 uses only baseline NEON kernels so byte output is invariant
# to the dotprod/i8mm TUs that get statically linked in.
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

if [ ! -x "$src_dir/vpxenc" ] || [ "$current_config" != "$want_config" ]; then
	if [ "$current_config" != "$want_config" ]; then
		fetch_source
	fi
	(
		cd "$src_dir"
		./configure \
			--target="$GOVPX_ORACLE_TARGET" \
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
cp "$src_dir/examples/vpx_temporal_svc_encoder" "$temporal_svc_bin"
chmod +x "$temporal_svc_bin"
printf '%s\n' "$vpxenc_bin"
