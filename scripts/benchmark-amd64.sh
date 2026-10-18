#!/bin/sh
# Compare assembly and native Go SIMD on one native amd64 runner, optionally
# adding a previous SIMD commit as the third argument. Two variants alternate
# A/B and B/A; three variants use every permutation over the six rounds.
set -eu
case "$(uname -s)/$(uname -m)" in
	Linux/x86_64) ;;
	*) printf '%s\n' 'This benchmark requires native Linux/amd64.' >&2; exit 1 ;;
esac

root=$(git rev-parse --show-toplevel)
cd "$root"
baseline_ref=${1:-691cd0512c4829d9ee40d483205dd223f9eb028d}
output_dir=${2:?provide an output directory}
previous_ref=${3:-}
baseline_commit=$(git rev-parse --verify --end-of-options "$baseline_ref^{commit}")
previous_commit=
if [ -n "$previous_ref" ]; then
	previous_commit=$(git rev-parse --verify --end-of-options "$previous_ref^{commit}")
fi
mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
reference_dir=
previous_dir=
cleanup() {
	for directory in "$reference_dir" "$previous_dir"; do
		[ -n "$directory" ] || continue
		git worktree remove --force "$directory" >/dev/null 2>&1 || rmdir "$directory" >/dev/null 2>&1 || true
	done
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
reference_dir=$(mktemp -d "${TMPDIR:-/tmp}/govpx-assembly-reference.XXXXXX")
git worktree add --detach "$reference_dir" "$baseline_commit"
variants='assembly simd'
if [ -n "$previous_commit" ]; then
	previous_dir=$(mktemp -d "${TMPDIR:-/tmp}/govpx-previous-reference.XXXXXX")
	git worktree add --detach "$previous_dir" "$previous_commit"
	variants='assembly previous simd'
fi

# Apply identical test-only fixtures to every reference. Production files
# remain unchanged at their respective pinned commits.
for source_dir in "$reference_dir" "$previous_dir"; do
	[ -n "$source_dir" ] || continue
	cp "$root/internal/vp8/encoder/dct_batch_native_test.go" "$source_dir/internal/vp8/encoder/dct_batch_native_test.go"
	cp "$root/internal/vp8/encoder/denoiser_native_bench_test.go" "$source_dir/internal/vp8/encoder/denoiser_native_bench_test.go"
	cp "$root/internal/vp8/encoder/loopfilter_score_native_test.go" "$source_dir/internal/vp8/encoder/loopfilter_score_native_test.go"
	cp "$root/internal/vp8/decoder/picker_filter_native_bench_test.go" "$source_dir/internal/vp8/decoder/picker_filter_native_bench_test.go"
	cp "$root/internal/vp8/encoder/quant_batch_native_test.go" "$source_dir/internal/vp8/encoder/quant_batch_native_test.go"
	cp "$root/internal/vp8/dsp/sad_limit_native_test.go" "$source_dir/internal/vp8/dsp/sad_limit_native_test.go"
	cp "$root/internal/vp8/dsp/sad_limit_slice_native_test.go" "$source_dir/internal/vp8/dsp/sad_limit_slice_native_test.go"
	cp "$root/internal/vp8/dsp/sad_limit_x4_native_test.go" "$source_dir/internal/vp8/dsp/sad_limit_x4_native_test.go"
	cp "$root/internal/vp8/dsp/uv_zero_native_test.go" "$source_dir/internal/vp8/dsp/uv_zero_native_test.go"
	cp "$root/internal/vp8/dsp/gather_pointer_native_test.go" "$source_dir/internal/vp8/dsp/gather_pointer_native_test.go"
	cp "$root/internal/vp8/dsp/sse_strip_test.go" "$source_dir/internal/vp8/dsp/sse_strip_test.go"
	cp "$root/internal/vp8/dsp/sse8_rows_native_test.go" "$source_dir/internal/vp8/dsp/sse8_rows_native_test.go"
	cp "$root/internal/vp8/dsp/residual_gather_native_test.go" "$source_dir/internal/vp8/dsp/residual_gather_native_test.go"
	cp "$root/internal/vp8/dsp/dc_pair_bench_test.go" "$source_dir/internal/vp8/dsp/dc_pair_bench_test.go"
	cp "$root/internal/vp8/dsp/six_phases_bench_test.go" "$source_dir/internal/vp8/dsp/six_phases_bench_test.go"
	cp "$root/internal/vp8/dsp/variance_native_bench_test.go" "$source_dir/internal/vp8/dsp/variance_native_bench_test.go"
	cp "$root/internal/vp8/dsp/subpel_split_bench_test.go" "$source_dir/internal/vp8/dsp/subpel_split_bench_test.go"
	cp "$root/internal/vp9/dsp/projection_average_native_test.go" "$source_dir/internal/vp9/dsp/projection_average_native_test.go"
	cp "$root/internal/vp9/encoder/dct32_bench_test.go" "$source_dir/internal/vp9/encoder/dct32_bench_test.go"
	cp "$root/internal/vp9/encoder/quant_fp_classes_native_bench_test.go" "$source_dir/internal/vp9/encoder/quant_fp_classes_native_bench_test.go"
	cp "$root/internal/vp9/dsp/subpel_pointer_native_test.go" "$source_dir/internal/vp9/dsp/subpel_pointer_native_test.go"
	cp "$root/internal/vp9/dsp/subtract_rows_native_test.go" "$source_dir/internal/vp9/dsp/subtract_rows_native_test.go"
done

export GOTOOLCHAIN=local GOEXPERIMENT=simd GOAMD64=v1 GOMAXPROCS=1
{
	printf 'assembly commit: %s\n' "$baseline_commit"
	if [ -n "$previous_commit" ]; then printf 'previous SIMD commit: %s\n' "$previous_commit"; fi
	printf 'SIMD commit: %s\n' "$(git rev-parse HEAD)"
	printf 'variants: %s\n' "$variants"
	printf '%s\n' 'GOMAXPROCS=1 PGO=off rounds=6 kernel-benchtime=150ms'
	sha256sum "$root/internal/vp9/encoder/quant_fp_classes_native_bench_test.go"
	sha256sum "$root/internal/vp8/encoder/quant_batch_native_test.go"
	sha256sum "$root/internal/vp8/encoder/denoiser_native_bench_test.go"
	sha256sum "$root/internal/vp8/dsp/gather_pointer_native_test.go"
	sha256sum "$root/internal/pixel/gather_pointer_simd.go"
	sha256sum "$root/internal/pixel/subpel16_amd64.go"
	sha256sum "$root/internal/pixel/subpel_axis_amd64.go"
	sha256sum "$root/internal/pixel/kernels_simd.go"
	sha256sum "$root/internal/pixel/subtract_rows_simd.go"
	sha256sum "$root/internal/pixel/subtract_rows_amd64.go"
	sha256sum "$root/internal/vp9/dsp/subtract_rows_native_test.go"
	sha256sum "$root/internal/pixel/sse_simd.go"
	sha256sum "$root/internal/vp8/dsp/sse8_rows_native_test.go"
	sha256sum "$root/internal/vp9/dsp/subpel_pointer_native_test.go"
	scripts/go.sh version
	scripts/go.sh env GOOS GOARCH GOAMD64 GOEXPERIMENT GOTOOLCHAIN GOCACHE
	lscpu
} > "$output_dir/environment.txt"

# This test fails if runtime feature detection selected the scalar backend.
GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestBackendSelected$' -count=1 -v > "$output_dir/dispatch.txt" 2>&1
# The current batch implementation must accept eligible native work and match
# the scalar oracle over signed inputs before collecting any timing samples.
# Older references keep their own production tests and identical bench fixture.
{
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestVP8QuantBatch(DeclinesBeforeWriting|PreservesOverlappingInputOrder)$' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/encoder -run '^TestQuantBatch(NativeSelected|ExtremesScalar)$' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(SixTap|RollingSixTap)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestSixTap' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestDenoiseBytes' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(Stats16|DiffMetricsAndStats)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestVariance' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/encoder -run '^TestDenoiser' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestSSEPlane16' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestSubtractRows' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSubtractRows' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(SSE8ExactRows|SquaredError)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSSE8ExactRows' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/encoder -run '^TestLoopFilterLumaSSE' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestMacroInternal' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestGatherPointer' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(SubpelPointer|SubpelIdentity|SubpelAxis|SubpelSixteenLaneExtremes)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp9/dsp -run '^TestVP9SubPixelVarianceSimd' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSubpel(Pointer|Identity|Axis)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestResidualGather' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestMacroInternal' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestSADLimit(PointerRows|SliceRows|SliceVisibleSpan|X4ExactRows|X4AliasedStoreOrder|X4FullWindowGuard)$' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestSADLimit16Pointer' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestSADLimitPointerAMDDisabled$' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/decoder -run '^Test.*LoopFilter' -count=1 -v
} > "$output_dir/quant-batch-validation.txt" 2>&1
if [ -n "$previous_dir" ]; then
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$previous_dir" test -pgo=off ./internal/pixel -run '^TestBackendSelected$' -count=1 -v > "$output_dir/previous-dispatch.txt" 2>&1
fi

packages='internal/vp9/dsp internal/vp8/dsp internal/vp9/encoder internal/vp8/encoder internal/vp8/decoder'
benchmarks='Benchmark(Avg8x8Quad|ProjectionColumnsNative|Variance16x16Native)$|BenchmarkVP9(FusedFPClassesNative|FPNoClassesNative|Idct4x4_16Add|Idct8x8_64Add|Convolve8Horiz16x16|Convolve8Vert16x16|SubPixelVariance(8x8|16x16|32x32|64x64)|SubpelPointerNative|LoopFilter(Horizontal|Vertical)8(Dual)?NonFlat)$|BenchmarkSAD16x16$|BenchmarkForwardDCT(16x16|32x32Native)$|Benchmark(SADLimitNative|SADLimitSliceNative|SADLimitX4Native|UVRoundedZeroNative|GatherPointerNative|GatherPackedPredictionNative|ResidualGatherNative|SixTapPhases|SplitSubpelPhases|DenoiserFilterYNative|LoopFilterLumaSSENative|VP8PickerFilterNative|FastQuantizeBatchNative|FastQuantizeBlockSIMD|ForwardDCT4x4SIMD|ForwardDCTBatchResidual25|DCOnlyIDCT4x4AddPairInt32|DCOnlyPairInPlace|ResidualGather16x16|ResidualGather8x8|SSE16x720PtrFast|SSE16x16|SSE8x8|SSE8RowsNative|SubtractRowsNative|LoopFilterHorizontalEdgesY|LoopFilterVerticalEdgesY|(MB)?LoopFilter(Horizontal|Vertical)EdgeUV|SAD16x16LimitEarly|SAD16x16x4LimitPtrFastFull)$'
for variant in $variants; do
	case "$variant" in
		assembly) source_dir=$reference_dir ;;
		previous) source_dir=$previous_dir ;;
		simd) source_dir=$root ;;
	esac
	for package in $packages; do
		name=$(printf '%s' "$package" | tr / -)
		scripts/go.sh -C "$source_dir" test -pgo=off -c -o "$output_dir/$variant-$name.test" "./$package"
	done
	scripts/go.sh -C "$source_dir" build -pgo=off -o "$output_dir/$variant-govpx-bench" ./cmd/govpx-bench
done

for round in 1 2 3 4 5 6; do
	order='assembly simd'
	case "$round" in 2|4|6) order='simd assembly' ;; esac
	if [ -n "$previous_dir" ]; then
		case "$round" in
			1) order='assembly previous simd' ;;
			2) order='previous simd assembly' ;;
			3) order='simd assembly previous' ;;
			4) order='assembly simd previous' ;;
			5) order='simd previous assembly' ;;
			6) order='previous assembly simd' ;;
		esac
	fi
	printf 'round %s: %s\n' "$round" "$order" >> "$output_dir/orders.txt"
	for variant in $order; do
		printf 'round %s: %s\n' "$round" "$variant"
		for package in $packages; do
			name=$(printf '%s' "$package" | tr / -)
			{
				printf '# round %s, %s\n' "$round" "$package"
				GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-$name.test" -test.run '^$' -test.bench "$benchmarks" -test.benchtime=150ms -test.count=1 -test.cpu=1 -test.benchmem
			} >> "$output_dir/$variant-kernels.txt" 2>&1
		done
		"$output_dir/$variant-govpx-bench" -codec=vp9 -width=320 -height=180 -frames=120 -fps=30 -bitrate=500 -mode=realtime -cpu-used=8 -threads=1 -encode-only -auto-libvpx=false -format=json > "$output_dir/$variant-encoder-$round.json"
		for codec in vp8 vp9; do
			"$output_dir/$variant-govpx-bench" -codec="$codec" -width=1280 -height=720 -frames=60 -fps=30 -bitrate=2500 -mode=realtime -cpu-used=8 -threads=1 -encode-only -auto-libvpx=false -format=json > "$output_dir/$variant-$codec-720p-encoder-$round.json"
		done
	done
done

# Profile separately after all timing rounds have finished. Retain both flat
# and cumulative views so a native encoder regression can be traced through
# its public callers without contaminating the benchmark samples.
for variant in $variants; do
	for codec in vp8 vp9; do
		profile="$output_dir/$variant-$codec-720p-profile.pgo"
		"$output_dir/$variant-govpx-bench" -codec="$codec" -width=1280 -height=720 -frames=120 -fps=30 -bitrate=2500 -mode=realtime -cpu-used=8 -threads=1 -encode-only -auto-libvpx=false -format=json -cpuprofile="$profile" > "$output_dir/$variant-$codec-720p-profile.json"
		scripts/go.sh tool pprof -top -nodecount=40 "$output_dir/$variant-govpx-bench" "$profile" > "$output_dir/$variant-$codec-720p-profile-top.txt"
		scripts/go.sh tool pprof -top -cum -nodecount=40 "$output_dir/$variant-govpx-bench" "$profile" > "$output_dir/$variant-$codec-720p-profile-cumulative.txt"
	done
done

# Keep artifacts compact; raw text/JSON and environment details are sufficient
# to reproduce and analyze these measurements.
rm -f "$output_dir/"*.test "$output_dir/"*-govpx-bench
