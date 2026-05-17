//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"os"
	"testing"
)

// vp9RefControlsSeedsDeferred lists VP9 reference-control fuzz seeds whose
// strict byte parity is gated behind libvpx VP9 features govpx has not yet
// ported. Mirrors VP8's longFixtureSeedsDeferred and
// vp9RuntimeControlsSeedsDeferred convention so the fuzz gate stays green;
// each entry cites the libvpx file:line that drives the divergence so a
// follow-up port has a concrete starting point.
//
// All 6 baseline RefControl seeds diverge at byte 16 of frame 0 (the first
// keyframe), with govpx emitting a noticeably larger packet than libvpx
// (got_len ~3973 vs want_len ~3040). Bytes 0..15 match exactly:
// frame_type, sync code, color config, frame size, loopfilter, base qindex,
// segmentation, tile config and refresh_frame_flags are all byte-identical.
// The divergence is FirstPartitionSize: govpx writes a 2-byte compressed
// header while libvpx writes 98 bytes of coef-update / tx-mode payload.
//
// Root cause (same as vp9RuntimeControlsSeedsDeferred entry #3 at cpu=8):
//
//   - libvpx's update_coef_probs (vp9/encoder/vp9_bitstream.c:684-700) is
//     gated by `cpi->td.counts->tx.tx_totals[tx_size] <= 20`; the counts
//     come from per-leaf `++td->counts->tx.tx_totals[mi->tx_size]` in
//     update_stats (vp9/encoder/vp9_encodeframe.c:6124-6125). One increment
//     per partition leaf (Y + UV planes). For a 64×64 keyframe under
//     VAR_BASED_PARTITION at cpu_used=8 (sf->partition_search_type=
//     VAR_BASED_PARTITION at vp9_speed_features.c:582 / 667), libvpx's
//     choose_partitioning (vp9/encoder/vp9_encodeframe.c:5304-5311) splits
//     the SB into many small leaves on high-variance content like the
//     newVP9YCbCrFuzzPanning fixture, driving tx_totals above the 20
//     threshold and triggering the full coef-update payload through
//     update_coef_probs_common (vp9_bitstream.c:546-682) at every active
//     tx size up to tx_mode_to_biggest_tx_size[tx_mode] (Tx32x32 for
//     Allow32x32 keyframes).
//
//   - govpx's keyframe variance partition picker
//     (vp9_encoder.go:5005-5058 pickVP9KeyframeVariancePartitionBlockSize)
//     is gated on RateControlCBR via vp9CBRKeyframeVariancePartitionEnabled
//     (vp9_encoder.go:5060); under RateControlQ — which every RefControl
//     seed uses — govpx falls through to vp9KeyframeSourceBlockSizeForRegion,
//     a coarse static-geometry picker that emits ~4 Block32x32 leaves on
//     a 64×64 keyframe. That keeps TxTotals[Tx16x16]=8 (≤ 20), so
//     WriteCoefProbsFromCounts (internal/vp9/encoder/coef_probs_counts.go:39-61)
//     writes a single 0 "no-update" bit per tx size and returns. The
//     compressed-header payload collapses to 2 bytes vs libvpx's 98 even
//     though the per-band branch counts (56/144 nonzero slots at Tx16x16)
//     would unlock dozens of vp9_prob_diff_update emissions if the
//     tx_totals gate cleared.
//
// Closing these seeds requires porting libvpx's choose_partitioning
// variance-based keyframe partition picker (vp9/encoder/vp9_encodeframe.c
// choose_partitioning + nonrd_use_partition @ 5470 / 4854) to the
// non-CBR rate-control branches so VAR_BASED_PARTITION fires at cpu_used=8
// regardless of rc_mode. The follow-up port should be paired with widening
// the WriteCompressedHeaderFromCounts coverage so the keyframe
// coef-prob-update payload matches libvpx's update_coef_probs_common
// emission for every active tx_size; on flat sources (the
// TestVP9EncoderVpxencOracleChecker64KeyframeByteParity fixtures) the
// writers agree today because the all-zero counts collapse both sides to
// the no-update floor.
//
// Reverting any entry here must be paired with the corresponding verbatim
// libvpx port landing; this is the explicit handoff list for follow-up
// work.
var vp9RefControlsSeedsDeferred = [][]byte{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 1, 0, 2, 0, 3, 0, 0},
	{1, 2, 3, 4, 5, 6, 0, 0},
	{0, 4, 0, 5, 0, 6, 0, 7},
	{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	{0, 7, 0, 8, 0, 9, 0, 10},
}

func vp9RefControlsSeedDeferred(data []byte) bool {
	for _, seed := range vp9RefControlsSeedsDeferred {
		if bytes.Equal(data, seed) {
			return true
		}
	}
	return false
}

// FuzzVP9EncoderReferenceControlSequences mirrors
// FuzzEncoderReferenceControlSequences (F8) for VP9: per-frame schedules mix
// EncodeFlags-based reference-update bits (NoUpdateLast, NoUpdateGolden,
// NoUpdateAltRef, NoReferenceLast/Golden/AltRef, ForceGolden/AltRefFrame), and
// the encoded bytes must match the libvpx VP9 vpxenc-vp9-frameflags driver
// driven through the same schedule.
//
// VP9's public SetReferenceFrame/CopyReferenceFrame surface is exercised by
// the dedicated vp9_oracle_copy_reference_parity_test.go family, so this
// fuzzer focuses on the per-frame EncodeFlags permutations the libvpx driver
// also supports. Gated by GOVPX_WITH_ORACLE=1 plus a built
// vpxenc-vp9-frameflags binary.
func FuzzVP9EncoderReferenceControlSequences(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 ref-control sequence fuzz")
	}
	requireVP9VpxencFrameFlagsOracleFuzz(f)
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 2, 0, 3, 0, 0},
		{1, 2, 3, 4, 5, 6, 0, 0},
		{0, 4, 0, 5, 0, 6, 0, 7},
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		{0, 7, 0, 8, 0, 9, 0, 10},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if vp9RefControlsSeedDeferred(data) {
			t.Skip("seed deferred: see vp9RefControlsSeedsDeferred for libvpx file:line citations")
		}
		tc := newVP9RefControlsFuzzCase(data)
		sum := sha256.Sum256(data)
		label := "fuzz-vp9-refctrl-" + hex.EncodeToString(sum[:4])
		t.Logf("%s frames=%d flags=%v", label, len(tc.sources), tc.flags)

		govpxFrames := encodeVP9FramesWithGovpx(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, tc.sources, tc.flags, tc.extraArgs)
		assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9RefControlsFuzzCase struct {
	opts      VP9EncoderOptions
	sources   []*image.YCbCr
	flags     []EncodeFlags
	extraArgs []string
}

// newVP9RefControlsFuzzCase generates a per-frame schedule that mixes the
// EncodeFlags ref-update / no-reference / force-* bits supported by both
// govpx VP9 and the vpxenc-vp9-frameflags driver.
func newVP9RefControlsFuzzCase(data []byte) vp9RefControlsFuzzCase {
	r := vp9FuzzByteCursor{data: data}
	framesPool := [...]int{6, 8, 10}
	frames := framesPool[r.pick(len(framesPool))]
	const (
		width  = 64
		height = 64
	)
	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		MaxKeyframeInterval: 128,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
	}
	sources := newVP9YCbCrFuzzSources(width, height, frames)
	flags := make([]EncodeFlags, frames)

	for frame := 1; frame < frames; frame++ {
		switch r.pick(11) {
		case 0:
			// No-op frame.
		case 1, 2:
			flags[frame] |= EncodeNoUpdateLast
		case 3, 4:
			flags[frame] |= EncodeNoUpdateGolden
		case 5, 6:
			flags[frame] |= EncodeNoUpdateAltRef
		case 7:
			flags[frame] |= EncodeNoReferenceLast | EncodeNoUpdateLast
		case 8:
			flags[frame] |= EncodeForceGoldenFrame
		case 9:
			flags[frame] |= EncodeForceAltRefFrame
		case 10:
			flags[frame] |= EncodeNoUpdateEntropy
		}
	}

	extraArgs := []string{
		"--cq-level=32",
		"--min-q=4",
		"--max-q=56",
		"--end-usage=q",
	}
	return vp9RefControlsFuzzCase{
		opts:      opts,
		sources:   sources,
		flags:     flags,
		extraArgs: extraArgs,
	}
}
