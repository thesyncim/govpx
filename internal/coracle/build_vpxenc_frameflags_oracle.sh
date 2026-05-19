#!/usr/bin/env sh
#
# Build `vpxenc-frameflags-oracle`, the combined VP8 reference driver
# that exposes BOTH:
#
#   (a) the per-frame `--control-script` plumbing from
#       vpxenc_frameflags.c (build_vpxenc_frameflags.sh), and
#
#   (b) the per-frame / per-MB JSON Lines oracle trace emitted from the
#       patched libvpx-v1.16.0 tree under
#       `$build_dir/libvpx-v1.16.0-vpxenc-oracle` (build_vpxenc_oracle.sh).
#
# The two patch sets are mutually compatible because oracle_trace.c is
# compiled into libvpx.a via the vp8cx.mk anchor edit in
# build_vpxenc_oracle.sh, and build_vpxenc_frameflags.sh links its
# self-contained driver against that same libvpx.a. This script just
# pins the contract: it (re)runs the oracle patch+libvpx build, links
# the frameflags driver against the patched libvpx.a, and copies the
# result to a separately-named `vpxenc-frameflags-oracle` binary so
# downstream Go tests can require the combined capability explicitly
# (per-frame VPX_EFLAG_*/VP8_EFLAG_* + GOVPX_ORACLE_TRACE_OUT in one
# encode pass).
#
# Sandbox limits: same as the parents — the libvpx tarball is reused
# from `$build_dir/libvpx-v1.16.0.tar.gz` if present, otherwise the
# oracle script will curl it. No additional libvpx clone is created.
#
# Env overrides (mirrors the parents):
#   GOVPX_CORACLE_BUILD_DIR              build dir (default: alongside this script)
#   GOVPX_VPXENC_FRAMEFLAGS_ORACLE_BIN   override output binary path
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
build_dir=${GOVPX_CORACLE_BUILD_DIR:-"$root/build"}
src_dir="$build_dir/libvpx-v1.16.0-vpxenc-oracle"
frameflags_bin="$build_dir/vpxenc-frameflags"
out_bin=${GOVPX_VPXENC_FRAMEFLAGS_ORACLE_BIN:-"$build_dir/vpxenc-frameflags-oracle"}
config_stamp="$build_dir/.govpx-vpxenc-frameflags-oracle-config"
# Bump this whenever the dependent want_config strings in
# build_vpxenc_oracle.sh / build_vpxenc_frameflags.sh change or when
# the verification surface here changes.
want_config="vpxenc-frameflags-oracle-2026-05-19-task218-mb-iter-rate-v2-task281-prefix-map"

mkdir -p "$build_dir"

# (1) Ensure the patched libvpx tree + libvpx.a + oracle hooks are in
#     place. This is idempotent: build_vpxenc_oracle.sh exits fast when
#     the patch+config stamps already match.
sh "$root/build_vpxenc_oracle.sh" >/dev/null

if [ ! -f "$src_dir/libvpx.a" ]; then
	echo "build_vpxenc_frameflags_oracle.sh: libvpx.a missing under $src_dir" >&2
	exit 1
fi

# (2) Ensure the frameflags driver is built and freshly linked against
#     the (now-patched) libvpx.a. Same idempotence guarantee as above.
sh "$root/build_vpxenc_frameflags.sh" >/dev/null

if [ ! -x "$frameflags_bin" ]; then
	echo "build_vpxenc_frameflags_oracle.sh: $frameflags_bin missing" >&2
	exit 1
fi

current_config=
if [ -f "$config_stamp" ]; then
	current_config=$(cat "$config_stamp")
fi

# (3) Materialize the explicitly-named combined binary. We copy rather
#     than symlink so the artifact has the same on-disk semantics as
#     vpxenc-oracle / vpxenc-frameflags (callers can stat it, exec it,
#     and feed it through coracle helpers that expect a regular file).
needs_copy=0
if [ ! -x "$out_bin" ] || [ "$current_config" != "$want_config" ]; then
	needs_copy=1
elif [ "$frameflags_bin" -nt "$out_bin" ]; then
	needs_copy=1
fi

if [ "$needs_copy" -eq 1 ]; then
	cp "$frameflags_bin" "$out_bin"
	chmod +x "$out_bin"
fi

# (4) Sanity-check the combined binary carries the oracle TU symbols.
#     If the patched libvpx.a somehow loses oracle_trace.c (anchor
#     drift, partial rebuild, ...) we want to fail loudly here rather
#     than silently producing a frameflags binary with no MB hooks.
if command -v nm >/dev/null 2>&1; then
	if ! nm "$out_bin" 2>/dev/null | grep -q '_govpx_oracle_capture_mb'; then
		echo "build_vpxenc_frameflags_oracle.sh: govpx_oracle_capture_mb symbol missing in $out_bin" >&2
		echo "  (libvpx.a appears to lack the oracle_trace.c TU -- rerun build_vpxenc_oracle.sh)" >&2
		exit 1
	fi
fi

printf '%s\n' "$want_config" > "$config_stamp"
printf '%s\n' "$out_bin"
