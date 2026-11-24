#!/bin/sh
# Independent native comparison against pristine optimized upstream libvpx.
set -eu
case "$(uname -s)/$(uname -m)" in
	Linux/x86_64) ;;
	*) printf '%s\n' 'This benchmark requires native Linux/amd64.' >&2; exit 1 ;;
esac
root=$(git rev-parse --show-toplevel)
cd "$root"
output_dir=${1:?provide an output directory}
mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/govpx-libvpx-performance.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT
trap 'exit 1' HUP INT TERM
# The peeled v1.16.0 tag, not an oracle-patched source tree. Gitiles gzip
# envelopes can differ for identical trees, so retain each download's hash.
libvpx_commit=1024874c5919305883187e2953de8fcb4c3d7fa6
curl --fail --location --retry 3 --output "$build_dir/source.tar.gz" \
	"https://chromium.googlesource.com/webm/libvpx/+archive/$libvpx_commit.tar.gz"
sha256sum "$build_dir/source.tar.gz" > "$output_dir/source-archive-sha256.txt"
mkdir "$build_dir/source"
tar -xzf "$build_dir/source.tar.gz" -C "$build_dir/source"
(
	cd "$build_dir/source"
	./configure --target=x86_64-linux-gcc --disable-docs --disable-unit-tests \
		--disable-debug --disable-gprof --enable-optimizations \
		--enable-runtime-cpu-detect --enable-vp8 --enable-vp9 \
		--enable-vp8-encoder --enable-vp8-decoder \
		--enable-vp9-encoder --enable-vp9-decoder \
		--enable-vp9-temporal-denoising --disable-vp9-highbitdepth \
		--enable-postproc --enable-vp9-postproc --enable-error-concealment
	make -j"${JOBS:-2}"
) > "$output_dir/libvpx-build.txt" 2>&1
cp "$build_dir/source/vpx_config.h" "$output_dir/vpx_config.h"
cp "$build_dir/source/config.mk" "$output_dir/libvpx-config.mk"
ar t "$build_dir/source/libvpx.a" > "$output_dir/libvpx-objects.txt"
# Check actual optimized build selection rather than assuming ASM is on.
python3 - "$output_dir" <<'PY'
import pathlib, re, sys
root = pathlib.Path(sys.argv[1])
config = (root / 'vpx_config.h').read_text()
for name in ['HAVE_SSE2', 'HAVE_AVX2', 'CONFIG_RUNTIME_CPU_DETECT', 'CONFIG_VP8_ENCODER', 'CONFIG_VP9_ENCODER']:
    if not re.search(r'^#define ' + name + r' 1$', config, re.M):
        raise SystemExit('optimized libvpx config missing ' + name)
if '.asm.o' not in (root / 'libvpx-objects.txt').read_text():
    raise SystemExit('no upstream x64 assembly objects in libvpx.a')
PY
export GOTOOLCHAIN=local GOEXPERIMENT=simd GOAMD64=v1 GOMAXPROCS=1
export CODEX_AGENT_ID=amd64-libvpx-bench
{
	printf 'govpx commit: %s\n' "$(git rev-parse HEAD)"
	printf 'libvpx release: v1.16.0\nlibvpx commit: %s\n' "$libvpx_commit"
	printf '%s\n' 'Native encode-only timing; six rounds alternate reference-first.'
	printf '%s\n' 'GOMAXPROCS=1 threads=1 PGO=off; separate decoded quality pass.'
	scripts/go.sh version
	scripts/go.sh env GOOS GOARCH GOAMD64 GOEXPERIMENT GOCACHE
	cc --version
	nasm -v
	lscpu
} > "$output_dir/environment.txt"
scripts/go.sh build -pgo=off -o "$build_dir/govpx-bench" ./cmd/govpx-bench
GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel \
	-run '^TestBackendSelected$' -count=1 -v > "$output_dir/dispatch.txt" 2>&1
sha256sum "$build_dir/govpx-bench" "$build_dir/source/vpxenc" > "$output_dir/binary-sha256.txt"
python3 scripts/benchmark-libvpx.py "$build_dir/govpx-bench" \
	"$build_dir/source/vpxenc" "$output_dir"
