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
	cp "$root/internal/vp8/encoder/token_count_macro_test.go" "$source_dir/internal/vp8/encoder/token_count_macro_test.go"
	cp "$root/internal/vp8/encoder/token_eob_native_test.go" "$source_dir/internal/vp8/encoder/token_eob_native_test.go"
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
	cp "$root/internal/vp8/dsp/copy8_rows_native_test.go" "$source_dir/internal/vp8/dsp/copy8_rows_native_test.go"
	cp "$root/internal/vp8/dsp/fill_exact_native_test.go" "$source_dir/internal/vp8/dsp/fill_exact_native_test.go"
	cp "$root/internal/vp8/dsp/fused_dc_supplement_test.go" "$source_dir/internal/vp8/dsp/fused_dc_supplement_test.go"
	cp "$root/internal/vp8/dsp/copy16_rows_native_test.go" "$source_dir/internal/vp8/dsp/copy16_rows_native_test.go"
	cp "$root/internal/vp8/dsp/residual_gather_native_test.go" "$source_dir/internal/vp8/dsp/residual_gather_native_test.go"
	cp "$root/internal/vp8/dsp/dc_pair_bench_test.go" "$source_dir/internal/vp8/dsp/dc_pair_bench_test.go"
	cp "$root/internal/vp8/dsp/six_phases_bench_test.go" "$source_dir/internal/vp8/dsp/six_phases_bench_test.go"
	cp "$root/internal/vp8/dsp/variance_native_bench_test.go" "$source_dir/internal/vp8/dsp/variance_native_bench_test.go"
	cp "$root/internal/vp8/dsp/subpel_split_bench_test.go" "$source_dir/internal/vp8/dsp/subpel_split_bench_test.go"
	cp "$root/internal/vp9/dsp/projection_average_native_test.go" "$source_dir/internal/vp9/dsp/projection_average_native_test.go"
	cp "$root/internal/vp9/encoder/dct32_bench_test.go" "$source_dir/internal/vp9/encoder/dct32_bench_test.go"
	cp "$root/internal/vp9/encoder/quant_fp_classes_native_bench_test.go" "$source_dir/internal/vp9/encoder/quant_fp_classes_native_bench_test.go"
	cp "$root/internal/vp9/encoder/quant_fp16_native_test.go" "$source_dir/internal/vp9/encoder/quant_fp16_native_test.go"
	cp "$root/internal/vp9/dsp/subpel_pointer_native_test.go" "$source_dir/internal/vp9/dsp/subpel_pointer_native_test.go"
	cp "$root/internal/vp9/dsp/subtract_rows_native_test.go" "$source_dir/internal/vp9/dsp/subtract_rows_native_test.go"
	cp "$root/internal/vp9/dsp/sad_wide_native_test.go" "$source_dir/internal/vp9/dsp/sad_wide_native_test.go"
	cp "$root/internal/vp8/dsp/tmpredict_bytes_native_test.go" "$source_dir/internal/vp8/dsp/tmpredict_bytes_native_test.go"
	cp "$root/internal/vp8/dsp/tmpredict_rows_common_state_test.go" "$source_dir/internal/vp8/dsp/tmpredict_rows_common_state_test.go"
	cp "$root/copy16_rows_native_test.go" "$source_dir/copy16_rows_native_test.go"
	cp "$root/internal/vp8/dsp/dc_pair_native_bench_test.go" "$source_dir/internal/vp8/dsp/dc_pair_native_bench_test.go"
	cp "$root/internal/vp8/dsp/dc_pair_inplace_native_bench_test.go" "$source_dir/internal/vp8/dsp/dc_pair_inplace_native_bench_test.go"
	# Reference-only adapter inlines into B.Loop; original public operation stays live.
	if ! grep -q '^func DCOnlyIDCT4x4AddPairInPlaceInt32(' "$source_dir/internal/vp8/dsp/idct.go"; then
		cp "$root/scripts/benchmark-fixtures/dc_pair_inplace_reference_shim.go.txt" "$source_dir/internal/vp8/dsp/dc_pair_inplace_reference_shim_test.go"
	fi
done

export GOTOOLCHAIN=local GOEXPERIMENT=simd GOAMD64=v1 GOMAXPROCS=1
{
	printf 'assembly commit: %s\n' "$baseline_commit"
	if [ -n "$previous_commit" ]; then printf 'previous SIMD commit: %s\n' "$previous_commit"; fi
	printf 'SIMD commit: %s\n' "$(git rev-parse HEAD)"
	printf 'variants: %s\n' "$variants"
	for source_dir in "$reference_dir" "$previous_dir"; do
		[ -n "$source_dir" ] || continue
		printf 'reference DC inplace API commit: %s\n' "$(git -C "$source_dir" rev-parse HEAD)"
		if [ -f "$source_dir/internal/vp8/dsp/dc_pair_inplace_reference_shim_test.go" ]; then
			printf '%s\n' 'reference DC inplace API: test-only forwarding adapter'
			sha256sum "$source_dir/internal/vp8/dsp/dc_pair_inplace_reference_shim_test.go"
		else
			printf '%s\n' 'reference DC inplace API: original production function'
		fi
	done
	printf '%s\n' 'GOMAXPROCS=1 PGO=off rounds=6 kernel-benchtime=150ms'
	sha256sum "$root/scripts/capture-stats-codegen.py"
	sha256sum "$root/internal/vp8/encoder/token_zero_y1_arm64.go"
	sha256sum "$root/internal/vp8/encoder/token_zero_y1_other.go"
	sha256sum "$root/internal/vp8/encoder/token_zero_y1_alias_acceptance_arm64_test.go"
	sha256sum "$root/internal/vp8/encoder/token_zero_y1_private_test.go"
	sha256sum "$root/internal/pixel/loopfilter_rowtail_native_test.go"
	sha256sum "$root/internal/pixel/loopfilter_columns_wide20_arm64.go"
	sha256sum "$root/internal/pixel/loopfilter_columns_reuse_arm64_test.go"
	sha256sum "$root/internal/vp8/dsp/loopfilter_macro_axis_native_test.go"
	sha256sum "$root/internal/pixel/quantize_fp_packets_test.go"
	sha256sum "$root/internal/pixel/fp32_optional_arm_native_test.go"
	sha256sum "$root/internal/pixel/stats_odd_row_frozen_test.go"
	sha256sum "$root/internal/pixel/stats_odd_row_state_test.go"
	sha256sum "$root/internal/vp8/encoder/probability_tokens.go"
	sha256sum "$root/internal/vp8/encoder/token_count_macro_test.go"
	sha256sum "$root/internal/pixel/dc_pair_coeff_amd64_test.go"
	sha256sum "$root/internal/pixel/dc_pair_coeff_arm64_test.go"
	sha256sum "$root/internal/pixel/dc_pair_inplace_amd64.go"
	sha256sum "$root/internal/pixel/dc_pair_native_amd64.go"
	sha256sum "$root/internal/pixel/loopfilter_macro_internal_fused_arm64.go"
	sha256sum "$root/internal/pixel/loopfilter_rowvalues_arm64.go"
	sha256sum "$root/internal/vp8/encoder/token_eob_native_test.go"
	sha256sum "$root/internal/vp8/encoder/tokenpack.go"
	sha256sum "$root/scripts/gensimd.go"
	sha256sum "$root/internal/vp9/encoder/quant_fp_classes_native_bench_test.go"
	sha256sum "$root/internal/pixel/quantize_fp_simd.go"
	sha256sum "$root/internal/pixel/quantize_fp_amd64.go"
	sha256sum "$root/internal/pixel/quantize_fp32_avx512_amd64.go"
	sha256sum "$root/internal/pixel/quantize_fp32_avx512_amd64_test.go"
	sha256sum "$root/internal/pixel/quantize_fp32_bounds_amd64_test.go"
	sha256sum "$root/internal/pixel/fp32_optional_native_test.go"
	sha256sum "$root/internal/pixel/stats_reduction_native_test.go"
	sha256sum "$root/internal/pixel/goav1_pixelstats_simd.go"
	sha256sum "$root/internal/pixel/stats16_amd64.go"
	sha256sum "$root/internal/pixel/stats16_arm64.go"
	sha256sum "$root/scripts/benchmark-pixel-optional.sh"
	sha256sum "$root/internal/pixel/fp16_guard_component_native_test.go"
	sha256sum "$root/scripts/benchmark-fp16-guard-diagnostic.sh"
	sha256sum "$root/internal/pixel/quantize_fp16_amd64_test.go"
	sha256sum "$root/internal/vp9/encoder/quant_fp16_native_test.go"
	sha256sum "$root/internal/vp8/encoder/quant_batch_native_test.go"
	sha256sum "$root/internal/vp8/encoder/denoiser_native_bench_test.go"
	sha256sum "$root/internal/vp8/dsp/gather_pointer_native_test.go"
	sha256sum "$root/internal/pixel/gather_pointer_simd.go"
	sha256sum "$root/internal/pixel/subpel16_amd64.go"
	sha256sum "$root/internal/pixel/subpel_axis_amd64.go"
	sha256sum "$root/internal/pixel/kernels_simd.go"
	sha256sum "$root/internal/pixel/fill_fixed_arm64.go"
	sha256sum "$root/internal/pixel/fill_fixed_other.go"
	sha256sum "$root/internal/pixel/fill_exact_native_test.go"
	sha256sum "$root/internal/vp8/dsp/fill_exact_native_test.go"
	sha256sum "$root/internal/pixel/intra_dc_fill_arm64.go"
	sha256sum "$root/internal/pixel/intra_dc_fill_other.go"
	sha256sum "$root/internal/pixel/intra_dc_fill_probe_test.go"
	sha256sum "$root/internal/vp8/dsp/intra_dispatch_other.go"
	sha256sum "$root/internal/vp8/dsp/fused_dc_probe_test.go"
	sha256sum "$root/internal/vp8/dsp/fused_dc_supplement_test.go"
	sha256sum "$root/internal/pixel/intra4x4_dc_fill_arm64.go"
	sha256sum "$root/internal/pixel/intra4x4_dc_fill_other.go"
	sha256sum "$root/internal/pixel/intra4x4_dc_fill_probe_test.go"
	sha256sum "$root/internal/vp8/dsp/intra4x4_other.go"
	sha256sum "$root/internal/vp8/dsp/fused_dc4_probe_test.go"
	sha256sum "$root/internal/pixel/sad_amd64.go"
	sha256sum "$root/internal/pixel/sad_wide_amd64_test.go"
	sha256sum "$root/internal/vp9/dsp/sad_wide_native_test.go"
	sha256sum "$root/internal/pixel/subtract_rows_simd.go"
	sha256sum "$root/internal/pixel/subtract_rows_amd64.go"
	sha256sum "$root/internal/vp9/dsp/subtract_rows_native_test.go"
	sha256sum "$root/internal/pixel/sse_simd.go"
	sha256sum "$root/internal/pixel/sse8_rows_wide_amd64.go"
	sha256sum "$root/internal/pixel/sse8_rows_arm64.go"
	sha256sum "$root/internal/pixel/sse8_entry_fallback_amd64_test.go"
	sha256sum "$root/internal/pixel/sse8_rows_wide_amd64_test.go"
	sha256sum "$root/internal/pixel/sse8_ptr_amd64.go"
	sha256sum "$root/internal/pixel/sse8_ptr_other.go"
	sha256sum "$root/internal/pixel/sse8_ptr_amd64_test.go"
	sha256sum "$root/internal/vp8/encoder/token_dense_arm64.go"
	sha256sum "$root/internal/vp8/encoder/token_dense_bench_test.go"
	sha256sum "$root/internal/vp8/encoder/token_dense_other.go"
	sha256sum "$root/internal/vp8/encoder/token_dense_overlap.go"
	sha256sum "$root/internal/vp8/encoder/token_dense_test.go"
	sha256sum "$root/internal/vp8/dsp/variance_block_sized_other.go"
	sha256sum "$root/internal/pixel/sse_plane_amd64.go"
	sha256sum "$root/internal/pixel/sse_plane_avx512_amd64_test.go"
	sha256sum "$root/internal/pixel/copy8_rows_arm64.go"
	sha256sum "$root/internal/pixel/copy8_rows_other.go"
	sha256sum "$root/internal/pixel/copy8_rows_arm64_test.go"
	sha256sum "$root/copy8_rows_native_test.go"
	sha256sum "$root/internal/vp8/dsp/copy8_rows_native_test.go"
	sha256sum "$root/internal/pixel/copy16_rows_arm64.go"
	sha256sum "$root/internal/pixel/copy16_rows_other.go"
	sha256sum "$root/internal/pixel/copy16_rows_arm64_test.go"
	sha256sum "$root/internal/vp8/dsp/recon.go"
	sha256sum "$root/internal/vp8/dsp/copy16_rows_native_test.go"
	sha256sum "$root/vp8_encoder_denoiser.go"
	sha256sum "$root/copy16_rows_native_test.go"
	sha256sum "$root/internal/vp8/dsp/sse8_rows_native_test.go"
	sha256sum "$root/internal/vp9/dsp/subpel_pointer_native_test.go"
	sha256sum "$root/internal/pixel/copy16_rows_amd64.go"
	sha256sum "$root/internal/pixel/copy16_amd64_probe_test.go"
	sha256sum "$root/copy16_frozen_probe_test.go"
	sha256sum "$root/internal/vp8/dsp/copy16_frozen_probe_test.go"
	sha256sum "$root/internal/pixel/tmpredict_bytes_arm64.go"
	sha256sum "$root/internal/pixel/tmpredict_bytes_other.go"
	sha256sum "$root/internal/pixel/tmpredict_bytes_test.go"
	sha256sum "$root/internal/pixel/tmpredict_bytes_guards_test.go"
	sha256sum "$root/internal/pixel/tmpredict_bytes_simd_reference_test.go"
	sha256sum "$root/internal/pixel/tmpredict_bytes_generic_reference_test.go"
	sha256sum "$root/internal/pixel/tmpredict_left_packet_arm64_test.go"
	sha256sum "$root/internal/vp8/dsp/tmpredict_bytes_state_test.go"
	sha256sum "$root/internal/vp8/dsp/tmpredict_bytes_native_test.go"
	sha256sum "$root/internal/vp8/dsp/tmpredict_rows_common_state_test.go"
	sha256sum "$root/internal/pixel/tmpredict_native_arm64.go"
	sha256sum "$root/internal/pixel/tmpredict_native_other.go"
	sha256sum "$root/internal/pixel/tmpredict_native_arm64_test.go"
	sha256sum "$root/internal/pixel/tmpredict_disabled_scalar_test.go"
	sha256sum "$root/internal/pixel/tmpredict_disabled_scalar_arm64_test.go"
	sha256sum "$root/internal/pixel/tmpredict_disabled_scalar_amd64_test.go"
	sha256sum "$root/internal/pixel/tmpredict_disabled_scalar_other_test.go"
	sha256sum "$root/internal/pixel/loopfilter_macro_axis_arm64.go"
	sha256sum "$root/internal/pixel/loopfilter_macro_axis_arm64_test.go"
	sha256sum "$root/internal/pixel/loopfilter_macro_axis_other.go"
	sha256sum "$root/internal/pixel/tmpredict4_native_arm64.go"
	sha256sum "$root/internal/pixel/tmpredict4_native_other.go"
	sha256sum "$root/internal/pixel/tmpredict4_native_test.go"
	sha256sum "$root/internal/vp8/decoder/loopfilter.go"
	sha256sum "$root/internal/vp8/decoder/loopfilter_axis_frozen_test.go"
	sha256sum "$root/internal/vp8/dsp/intra4x4.go"
	sha256sum "$root/internal/pixel/dc_pair_native_arm64.go"
	sha256sum "$root/internal/pixel/dc_pair_native_fallback.go"
	sha256sum "$root/internal/pixel/dc_pair_native_other.go"
	sha256sum "$root/internal/pixel/dc_pair_simd.go"
	sha256sum "$root/internal/vp8/dsp/idct.go"
	sha256sum "$root/internal/vp8/dsp/dc_pair_native_state_test.go"
	sha256sum "$root/internal/vp8/dsp/dc_pair_native_disabled_amd64_test.go"
	sha256sum "$root/internal/vp8/dsp/dc_pair_native_bench_test.go"
	sha256sum "$root/internal/pixel/dc_pair_inplace_arm64.go"
	sha256sum "$root/internal/vp8/decoder/reconstruct.go"
	sha256sum "$root/internal/vp8/dsp/dc_pair_inplace_state_test.go"
	sha256sum "$root/internal/vp8/dsp/dc_pair_inplace_disabled_amd64_test.go"
	sha256sum "$root/internal/vp8/dsp/dc_pair_inplace_native_bench_test.go"
	sha256sum "$root/scripts/benchmark-fixtures/dc_pair_inplace_reference_shim.go.txt"
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
	# Current-only odd-row/tall Stats correctness preserves exact backing and
	# malformed-window behavior on both actual SIMD and disabled fallback.
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestStatsOddRow' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestStatsOddRow' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestStatsOddRow' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestStatsOddRow' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestVariance' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/encoder -run '^TestDenoiser' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/encoder -run '^TestDenseToken' -count=1 -v
	# Current-only zero-Y1 full-state oracle; AMD declines to the original scalar walk.
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/encoder -run '^TestTokenZeroY1' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp8/encoder -run '^TestTokenZeroY1' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off ./internal/vp8/encoder -run '^TestTokenZeroY1' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp8/encoder -run '^TestTokenZeroY1' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp8/encoder -run '^TestDenseToken' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./ ./internal/vp8/dsp -run '^TestCopy(8|16)RowsPublicState$' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./ ./internal/vp8/dsp -run '^TestCopy(8|16)RowsPublicState$' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestSSEPlane16' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSSEPlane16' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestSubtractRows' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSubtractRows' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(SSE8|SquaredError)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSSE8' -count=1 -v
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
	# Wide SAD requires the actual AVX2 feature and retains exact scalar/old
	# behavior for disabled CPUs and all unsupported or incomplete windows.
	# ARM-only DC helpers decline on this native x64 job; public fallback,
	# alias and partial-panic behavior is checked in ordinary/race runs.
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^Test(IntraDCFill|Intra4x4DCFill|FusedDC)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^Test(IntraDCFill|Intra4x4DCFill|FusedDC)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^Test(FillExact|IntraDCFillExactState)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^Test(FillExact|IntraDCFillExactState)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestSADWideAVX2' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSADWideAVX2' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp9/dsp -run '^TestVP9Sad(Simd|4D)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp9/dsp -run '^TestVP9Sad(Simd|4D)' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SAD_AVX2_OFF=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestSADWideAVX2' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SAD_AVX2_OFF=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestSADWideAVX2' -count=1 -v

	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/dsp -run '^TestSADLimitPointerAMDDisabled$' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp8/decoder -run '^Test.*LoopFilter' -count=1 -v

	# AMD Copy16 is current-only. Require the actual selected backend and the
	# actual AVX-off fallback, preserving frozen partial-panic/backing checks.
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel ./ ./internal/vp8/dsp -run '^TestCopy16(AMDProbe|FrozenPartialPanic)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./ ./internal/vp8/dsp -run '^TestCopy16(AMDProbe|FrozenPartialPanic)' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SCALAR=1 scripts/go.sh test -pgo=off ./internal/pixel ./ ./internal/vp8/dsp -run '^TestCopy16(AMDProbe|FrozenPartialPanic)' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SCALAR=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./ ./internal/vp8/dsp -run '^TestCopy16(AMDProbe|FrozenPartialPanic)' -count=1 -v
	# Both actual callers get their existing eight live fixtures. These are
	# one-shot oracle/allocation preflights, outside the timed kernel inventory.
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./ ./internal/vp8/dsp -run '^$' -bench '^BenchmarkCopy16RowsNative$' -benchtime=1x -count=1 -cpu=1 -benchmem
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off ./ ./internal/vp8/dsp -run '^$' -bench '^BenchmarkCopy16RowsNative$' -benchtime=1x -count=1 -cpu=1 -benchmem
	# TM's ARM-only arithmetic/lane tests retain their architecture guards;
	# x64 validates the declining entry and exact current fallback state.
	# The frozen state imports pixel and is deliberately never copied to old
	# references with different malformed-window/alias contracts.
	# This current-only prefix includes TestTMPredictDisabledScalarContract in
	# selected/off ordinary+race runs below; its defer restores Enabled. No test
	# is overlaid onto old references. ARM native-only tests are build-excluded here.
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^TestTMPredict' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^TestTMPredict' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^TestTMPredict' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^TestTMPredict' -count=1 -v
	# Current-only DC-pair tests exercise the real x64 paired owner and its
	# disabled/unsupported fallback, alias and partial-panic contracts. Axis
	# dispatch remains ARM-only on this native x64 job. Never overlay
	# these state tests onto old references with different invalid contracts.
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SCALAR=1 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SCALAR=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/dsp -run '^Test(DCOnly|DCPair|AddConstantPair)' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/decoder -run '^TestMacroAxis' -count=1 -v
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/decoder -run '^TestMacroAxis' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SCALAR=1 scripts/go.sh test -pgo=off ./internal/pixel ./internal/vp8/decoder -run '^TestMacroAxis' -count=1 -v
	GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_SCALAR=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel ./internal/vp8/decoder -run '^TestMacroAxis' -count=1 -v
	# Existing ^TestTMPredict selected/off ordinary+race calls above already
	# include all four new TestTMPredict4Tile* states and arithmetic tests.
} > "$output_dir/quant-batch-validation.txt" 2>&1
# The new FP16 specialization is current-only; references receive the same
# public fixture, retaining their own production backends and class callers.
{
 GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestFP16' -count=1 -v
 GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestFP16' -count=1 -v
 GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/vp9/encoder -run '^TestQuantizeFP' -count=1 -v
 GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp9/encoder -run '^TestQuantizeFP' -count=1 -v
 GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_FP16_AVX_OFF=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^TestFP16' -count=1 -v
 GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_FP16_AVX_OFF=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^TestFP16' -count=1 -v
 GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off ./internal/vp9/encoder -run '^TestQuantizeFP' -count=1 -v
 GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp9/encoder -run '^TestQuantizeFP' -count=1 -v
} > "$output_dir/fp16-validation.txt" 2>&1
# FP32 tests require actual features; unsupported selected tests explicitly skip.
# No fake capability or selected-AVX512 claim from fallback runs.
{
 GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(FP32|FPPacket)' -count=1 -v
 GOVPX_REQUIRE_SIMD=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^Test(FP32|FPPacket)' -count=1 -v
 GODEBUG=cpu.avx512f=off GOVPX_REQUIRE_SIMD=1 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(FP32|FPPacket)' -count=1 -v
 GODEBUG=cpu.avx512f=off GOVPX_REQUIRE_SIMD=1 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^Test(FP32|FPPacket)' -count=1 -v
 GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh test -pgo=off ./internal/pixel -run '^Test(FP32|FPPacket)' -count=1 -v
 GODEBUG=cpu.avx=off,cpu.avx2=off,cpu.avx512f=off GOVPX_REQUIRE_SIMD=0 GOVPX_REQUIRE_FP32_OFF=1 scripts/go.sh test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/pixel -run '^Test(FP32|FPPacket)' -count=1 -v
} >> "$output_dir/fp16-validation.txt" 2>&1

if [ -n "$previous_dir" ]; then
	GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$previous_dir" test -pgo=off ./internal/pixel -run '^TestBackendSelected$' -count=1 -v > "$output_dir/previous-dispatch.txt" 2>&1
fi

packages='internal/vp9/dsp internal/vp8/dsp internal/vp9/encoder internal/vp8/encoder internal/vp8/decoder'
benchmarks='Benchmark(Avg8x8Quad|ProjectionColumnsNative|Variance16x16Native)$|BenchmarkVP9(FP16Native|WideSADNative|FusedFPClassesNative|FPNoClassesNative|Idct4x4_16Add|Idct8x8_64Add|Convolve8Horiz16x16|Convolve8Vert16x16|SubPixelVariance(8x8|16x16|32x32|64x64)|SubpelPointerNative|LoopFilter(Horizontal|Vertical)8(Dual)?NonFlat)$|BenchmarkSAD16x16$|BenchmarkForwardDCT(16x16|32x32Native)$|Benchmark(SADLimitNative|SADLimitSliceNative|SADLimitX4Native|UVRoundedZeroNative|GatherPointerNative|GatherPackedPredictionNative|ResidualGatherNative|PreparedTokenEOBNative|TokenCountMacroNative|DCPairRowsNative|DCPairInPlaceRowsNative|SixTapPhases|SplitSubpelPhases|DenoiserFilterYNative|LoopFilterLumaSSENative|VP8PickerFilterNative|FastQuantizeBatchNative|FastQuantizeBlockSIMD|ForwardDCT4x4SIMD|ForwardDCTBatchResidual25|DCOnlyIDCT4x4AddPairInt32|DCOnlyPairInPlace|ResidualGather16x16|ResidualGather8x8|SSE16x720PtrFast|SSE16x16|SSE8x8|SSE8RowsNative|TMPredictRowsNative|Copy(8|16)RowsNative|IntraDCFill(Exact|Supplement)|SubtractRowsNative|LoopFilterHorizontalEdgesY|LoopFilterVerticalEdgesY|(MB)?LoopFilter(Horizontal|Vertical)EdgeUV|SAD16x16LimitEarly|SAD16x16x4LimitPtrFastFull)$'
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
	# Time the actual root denoiser caller as well as the DSP caller. Its
	# larger dispatch body can change inlining independently of the DSP path.
	scripts/go.sh -C "$source_dir" test -pgo=off -c -o "$output_dir/$variant-root-copy16.test" .
	# Validate all DC callers on every reference before its timed rounds.
	{
		printf '# DC preflight: %s\n' "$variant"
		GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-internal-vp8-dsp.test" -test.run '^TestFusedDCSupplementState$' -test.bench '^BenchmarkIntraDCFill(Exact|Supplement)$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem
	} >> "$output_dir/quant-batch-validation.txt" 2>&1
	# Same standalone state oracle and untouched 27-case public TM fixture on
	# all three references; no pixel import or cross-era alias assumptions.
	{
		printf '# TM preflight: %s\n' "$variant"
		GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-internal-vp8-dsp.test" -test.run '^TestTMPredictRowsNativeState$' -test.bench '^BenchmarkTMPredictRowsNative$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem -test.v
	} >> "$output_dir/quant-batch-validation.txt" 2>&1
	# Standalone public DC pair fixture has its own clipped scalar equation,
	# full backing/input canaries and live output sink. All 18 one-shot cases
	# are oracle-only, excluded from timed inventories on every reference.
	{
		printf '# DC pair preflight: %s\n' "$variant"
		GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-internal-vp8-dsp.test" -test.run '^$' -test.bench '^BenchmarkDCPairRowsNative$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem
	} >> "$output_dir/quant-batch-validation.txt" 2>&1
	# New paired-in-place API: identical standalone18 fixture on each reference.
	# Base/old receive only the inlined forwarding adapter to their original API.
	# Full-state/current-only tests already match existing ^TestDCPair selectors.
	{
		printf '# DC inplace preflight: %s\n' "$variant"
		GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-internal-vp8-dsp.test" -test.run '^$' -test.bench '^BenchmarkDCPairInPlaceRowsNative$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem
	} >> "$output_dir/quant-batch-validation.txt" 2>&1
	# Identical token fixture exercises the original prepared writer and its
	# independent scalar/full-state oracle on all pinned production sources.
	# These 15 one-shot cases and ordinary/race state checks are untimed.
	{
		printf '# token EOB preflight: %s\n' "$variant"
		GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-internal-vp8-encoder.test" -test.run '^TestPreparedEOBNoShiftState$' -test.bench '^BenchmarkPreparedTokenEOBNative$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem -test.v
		GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$source_dir" test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp8/encoder -run '^TestPreparedEOBNoShiftState$' -count=1 -v
	} > "$output_dir/$variant-token-eob-fixture-validation.txt" 2>&1
	# Keep count-only and prepared-record macroblock controls independent of
	# the existing prepared-token writer fixture. All12 live cases and the
	# frozen full-state/error/panic oracle run on every pinned reference.
	{
		printf '# token macro preflight: %s\n' "$variant"
		GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-internal-vp8-encoder.test" -test.run '^TestTokenCountMacroFrozenState$' -test.bench '^BenchmarkTokenCountMacroNative$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem -test.v
		GOVPX_REQUIRE_SIMD=1 scripts/go.sh -C "$source_dir" test -pgo=off -race -gcflags=all=-d=checkptr=2 ./internal/vp8/encoder -run '^TestTokenCountMacroFrozenState$' -count=1 -v
	} > "$output_dir/$variant-token-count-macro-validation.txt" 2>&1
	# Oracle work is untimed and identical on every pinned production source.
	GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-internal-vp9-encoder.test" -test.run '^$' -test.bench '^BenchmarkVP9FP16Native$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem > "$output_dir/$variant-fp16-fixture-validation.txt" 2>&1
	if [ "$variant" = simd ]; then
		GODEBUG=cpu.avx=off,cpu.avx2=off GOVPX_REQUIRE_SIMD=0 "$output_dir/$variant-internal-vp9-encoder.test" -test.run '^$' -test.bench '^BenchmarkVP9FP16Native$' -test.benchtime=1x -test.count=1 -test.cpu=1 -test.benchmem > "$output_dir/simd-fp16-cpuoff-fixture-validation.txt" 2>&1
	fi
	scripts/go.sh -C "$source_dir" build -pgo=off -o "$output_dir/$variant-govpx-bench" ./cmd/govpx-bench
done

# Validate the additive preflight inventory before any timed rounds. Bare
# verbose benchmark headings are not measurements; result rows stay strict.
python3 - "$output_dir" $variants <<'TOKEN_EOB_PREFLIGHT'
from pathlib import Path
import re, sys
expected={f"BenchmarkPreparedTokenEOBNative/{p}/p{q}" for p in ("allEOB","sparse","gappy","denseSmall","denseMixed") for q in (0,128,230)}
for variant in sys.argv[2:]:
    text=(Path(sys.argv[1])/(variant+"-token-eob-fixture-validation.txt")).read_text()
    assert text.count("--- PASS: TestPreparedEOBNoShiftState ")==2, variant
    assert "--- FAIL:" not in text and not re.search(r"^FAIL(?:\s|$)",text,re.M),variant
    names=[]
    for line in text.splitlines():
        if not line.startswith("BenchmarkPreparedTokenEOBNative/"): continue
        if line in expected: continue
        m=re.fullmatch(r"(BenchmarkPreparedTokenEOBNative/\S+)\s+(\d+)\s+(?:(\S+) ns/op\s+)?(\S+) B/op\s+(\S+) allocs/op",line)
        assert m,(variant,line)
        name,it,ns,byte,alloc=m.groups()
        assert name in expected and int(it)==1 and float(byte)==float(alloc)==0,(variant,line)
        names.append(name)
    assert len(names)==len(set(names))==15 and set(names)==expected,(variant,names)
TOKEN_EOB_PREFLIGHT

# The new macroblock one-shots are excluded from all timed counts. Require
# the complete independent state oracle twice and exactly12 zeroalloc cases.
python3 - "$output_dir" $variants <<'TOKEN_MACRO_PREFLIGHT'
from pathlib import Path
import re, sys
expected={f"BenchmarkTokenCountMacroNative/is4{a}/{p}/records{b}" for a in ("false","true") for p in ("zero","sparse","dense") for b in ("false","true")}
for variant in sys.argv[2:]:
    text=(Path(sys.argv[1])/(variant+"-token-count-macro-validation.txt")).read_text()
    assert text.count("--- PASS: TestTokenCountMacroFrozenState ")==2, variant
    assert text.count("full-state/error/panic/backing cases=12288")==2, variant
    assert "--- FAIL:" not in text and not re.search(r"^FAIL(?:\s|$)",text,re.M),variant
    names=[]
    for line in text.splitlines():
        if not line.startswith("BenchmarkTokenCountMacroNative/"): continue
        if line.strip() in expected: continue
        m=re.fullmatch(r"(BenchmarkTokenCountMacroNative/\S+)\s+(\d+)\s+(?:(\S+) ns/op\s+)?(\S+) B/op\s+(\S+) allocs/op",line.rstrip())
        assert m,(variant,line)
        name,it,ns,byte,alloc=m.groups()
        assert name in expected and int(it)==1 and float(byte)==float(alloc)==0,(variant,line)
        names.append(name)
    assert len(names)==len(set(names))==12 and set(names)==expected,(variant,names)
TOKEN_MACRO_PREFLIGHT

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
		# Distinct root-package header keeps all eight actual root Copy16 cases
		# separate from the existing eight DSP cases in the same raw artifact.
		{
			printf '# round %s, .\n' "$round"
			GOVPX_REQUIRE_SIMD=1 "$output_dir/$variant-root-copy16.test" -test.run '^$' -test.bench '^BenchmarkCopy16RowsNative$' -test.benchtime=150ms -test.count=1 -test.cpu=1 -test.benchmem
		} >> "$output_dir/$variant-kernels.txt" 2>&1
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

# Diagnostic guard component only, after all unchanged codec timing/profile work.
# One current-only binary; frozen helper candidates stay test-only. References,
# common overlays remain unchanged. The diagnostic times the actual selected
# trial guard and two frozen comparators; it does not select production dispatch.
scripts/go.sh test -pgo=off -c -o "$output_dir/simd-fp16-guard.test" ./internal/pixel
scripts/benchmark-fp16-guard-diagnostic.sh "$output_dir/simd-fp16-guard.test" "$output_dir/fp16-guard-component.txt"

# Additive two-way pixel API diagnostics only. Old691 has no pixel package;
# do not invent an old adapter or change any existing three-way fixture.
if [ -n "$previous_dir" ]; then
 cp "$root/internal/pixel/fp32_optional_native_test.go" "$previous_dir/internal/pixel/fp32_optional_native_test.go"
 cp "$root/internal/pixel/stats_reduction_native_test.go" "$previous_dir/internal/pixel/stats_reduction_native_test.go"
 scripts/go.sh -C "$previous_dir" test -pgo=off -c -o "$output_dir/previous-pixel-optional.test" ./internal/pixel
 scripts/benchmark-pixel-optional.sh "$output_dir/previous-pixel-optional.test" "$output_dir/simd-fp16-guard.test" "$output_dir/pixel-optional.txt" "$root" "$(git rev-parse HEAD)" "$previous_commit" "$previous_dir"
fi

# Keep artifacts compact; raw text/JSON and environment details are sufficient
# to reproduce and analyze these measurements.
rm -f "$output_dir/"*.test "$output_dir/"*-govpx-bench
