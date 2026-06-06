//go:build govpx_oracle_trace

package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity is the headline
// milestone pin for the {0,1,1,0,1} long-fixture parity-gap seed (CBR 700 kbps
// kf=30 realtime cpu4, VAR_BASED_PARTITION, one-pass q=145). With the deep
// full-RD use-partition stack enabled, govpx serializes the FULL 256-frame
// fixture and reproduces frames 0..7 byte-identically to the pinned libvpx
// v1.16.0 vpxenc-vp9 oracle — advancing the seed's matched-frame prefix from 1
// (keyframe only) to >= 8 (keyframe + the first seven inter frames). This proves
// the genuine full-RD inter engine GENERALIZES across frames: frame 2 references
// frame 1's now-byte-exact reconstruction, and frames 3..7 exercise the GOLDEN
// refresh cadence, changing q, and the accumulated frame-context probability
// adaptation. The decoder-side FrameContext entering frame 8 is byte-identical
// between the govpx and libvpx streams (backward adaptation across 1..7 is
// correct); frame 8 is the first divergence (SB(0,0) mi(0,4) full-RD intra-vs-
// NEWMV-LAST — see vp9InterUseDeepRDIntraSkipEncode for the precise root cause).
//
// The closure required four libvpx-faithful ports on top of the already-closed
// 64/64 committed-mode decomposition (TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1):
//
//  1. The writer reads the full-RD-committed tx_size (interDecision.txSize from
//     choose_tx_size_from_rd) instead of re-deriving it with the realtime
//     pickVP9InterTxSize heuristic — libvpx encode_superblock reads mi->tx_size
//     (vp9/encoder/vp9_encodeframe.c:6100).
//  2. The genuine inter super_block_yrd / super_block_uvrd producers score in the
//     TRANSFORM domain (vp9_block_error on coeff/dqcoeff >> shift), matching
//     x->block_tx_domain == 1 for REALTIME speed >= 1 (vp9_encodeframe.c:2041-2048,
//     vp9_speed_features.c:486-489; vp9_rdopt.c:571-600). Y-only is insufficient —
//     the Y and UV producers must both use it or the NEARESTMV-vs-NEARMV tie at
//     mi(0,4) inverts.
//  3. The writer applies the per-Y-tx-block x->zcoeff_blk zero-forcing
//     (vp9/encoder/vp9_encodemb.c:580; decided by vp9_rdopt.c:835 rd1 > rd2) using
//     the per-frame rdmult, the search-time (pre-compressed-header-update) coef
//     probs (inter.selectFc), AND the same transform-domain distortion the RD
//     search used.
//  4. The committed block skip is best_skip2 || best_mode_skippable
//     (vp9_rdopt.c:4149,4173); the writer codes a skip block (no Y/UV residual,
//     recon == predictor) instead of re-deriving skip from the re-quantized
//     residual.
//  5. The inter-frame intra RD producer applies x->skip_encode (set for cpu4 at
//     base_qindex < QIDX_SKIP_THRESH, vp9_rdopt.c:3519): the intra prediction
//     reads its neighbour samples from the SOURCE plane, the inverse transform is
//     not added back into recon, and dist_block scores in the transform domain
//     with the mean_quant_error model term (vp9_encodemb.c:840-934,
//     vp9_rdopt.c:589-600). Without it govpx's intra DC residual at frame-8
//     mi(0,4) inverted (coeff[0] 412 vs 604); with it the intra coeffs and
//     distortion are byte-exact with libvpx. (See vp9InterUseDeepRDIntraSkipEncode.)
//
// All five are gated behind the deep flags (vp9InterUseDeepRDUsePartition,
// vp9InterUseDeepRDRefBestRD, vp9InterUseDeepRDTxDomainDistortion,
// vp9InterUseDeepRDIntraSkipEncode), default OFF, so production and every other
// VP9 oracle gate stay byte-identical; the seed stays in
// vp9LongFixtureParityGapSeeds (its production path is unchanged). This test
// flips them locally.
func TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64

	saved := vp9InterUseDeepRDUsePartition
	savedRBR := vp9InterUseDeepRDRefBestRD
	savedTD := vp9InterUseDeepRDTxDomainDistortion
	savedSE := vp9InterUseDeepRDIntraSkipEncode
	defer func() {
		vp9InterUseDeepRDUsePartition = saved
		vp9InterUseDeepRDRefBestRD = savedRBR
		vp9InterUseDeepRDTxDomainDistortion = savedTD
		vp9InterUseDeepRDIntraSkipEncode = savedSE
	}()
	vp9InterUseDeepRDUsePartition = true
	vp9InterUseDeepRDRefBestRD = true
	vp9InterUseDeepRDTxDomainDistortion = true
	vp9InterUseDeepRDIntraSkipEncode = true

	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 30,
		Deadline:            DeadlineRealtime,
		CpuUsed:             4,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	// Encode the FULL {0,1,1,0,1} long fixture (256 panning frames) so the pin
	// asserts the generalized matched-frame prefix, not just the first inter
	// frame. With the deep full-RD use-partition stack the engine reproduces
	// frames 0..7 byte-for-byte; frame 8 is the first divergence (mi(0,4) intra
	// vs NEWMV — see the deep-flag notes), so the prefix is exactly 8.
	const fixtureFrames = 256
	sources := vp9test.NewPanningSources(width, height, fixtureFrames)
	dst := make([]byte, 1<<20)
	govpxFrames := make([][]byte, 0, len(sources))
	for i := range sources {
		res, err := e.EncodeIntoWithResult(sources[i], dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		govpxFrames = append(govpxFrames, append([]byte(nil), res.Data...))
	}

	// --timebase=1/30 pins vpxenc-vp9's encoder framerate to exactly 30 (see the
	// vp9LongFixtureParityGapSeeds note); the rest mirror the {0,1,1,0,1} bucket.
	extraArgs := []string{
		"--end-usage=cbr",
		"--target-bitrate=700",
		"--cpu-used=4",
		"--kf-min-dist=0",
		"--kf-max-dist=30",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--timebase=1/30",
	}
	libvpxFrames := vp9test.VpxencPackets(t, sources, extraArgs...)

	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("need >= 2 frames each, got govpx=%d libvpx=%d",
			len(govpxFrames), len(libvpxFrames))
	}

	// Headline: frame 1 (the first inter frame) is byte-exact.
	g1, l1 := govpxFrames[1], libvpxFrames[1]
	if fd := testutil.FirstByteDiff(g1, l1); fd != -1 || len(g1) != len(l1) {
		t.Fatalf("frame 1 NOT byte-exact: govpx=%d bytes libvpx=%d bytes "+
			"firstByteDiff=%d (want -1). The deep full-RD use-partition "+
			"tx_size/zcoeff_blk/transform-domain-distortion/skip ports regressed.",
			len(g1), len(l1), fd)
	}

	// Frame 0 (keyframe) must also stay byte-exact (it is, independently).
	if fd := testutil.FirstByteDiff(govpxFrames[0], libvpxFrames[0]); fd != -1 {
		t.Fatalf("frame 0 (keyframe) byte mismatch at offset %d", fd)
	}

	// The seed's matched-frame prefix must reach >= 8: frames 0 (keyframe) and
	// 1..7 (inter frames referencing the now-byte-exact reconstructions) all
	// serialize byte-for-byte. Frame 8 is the first divergence (mi(0,4) full-RD
	// intra-vs-NEWMV; the intra coeffs/distortion are byte-exact with libvpx
	// after the skip_encode port, the residual gap is the rd_use_partition
	// per-leaf entropy-context reset). Asserting >= 8 (was 2) proves the genuine
	// full-RD inter engine GENERALIZES across the GOLDEN-refresh cadence and the
	// accumulated entropy-context adaptation, not just the first inter frame.
	prefix := testutil.MatchedFramePrefixLength(govpxFrames, libvpxFrames)
	if prefix < 8 {
		t.Fatalf("matched-frame prefix = %d, want >= 8 (frame 0 keyframe + "+
			"frames 1..7 inter all byte-exact)", prefix)
	}
	t.Logf("{0,1,1,0,1} full-RD inter engine generalizes; "+
		"matched-frame prefix = %d (frame0=%d bytes frame1=%d bytes)",
		prefix, len(govpxFrames[0]), len(govpxFrames[1]))
}
