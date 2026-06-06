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
// fixture and reproduces frames 0..9 byte-identically to the pinned libvpx
// v1.16.0 vpxenc-vp9 oracle — advancing the seed's matched-frame prefix from 1
// (keyframe only) to >= 10 (keyframe + the first nine inter frames). This proves
// the genuine full-RD inter engine GENERALIZES across frames: frame 2 references
// frame 1's now-byte-exact reconstruction, and frames 3..9 exercise the GOLDEN
// refresh cadence, changing q, and the accumulated frame-context probability
// adaptation. The decoder-side FrameContext entering frame 10 is byte-identical
// between the govpx and libvpx streams (backward adaptation across 1..9 is
// correct).
//
// Frame 8 (the prior frontier, SB(0,0) mi(0,4) full-RD intra-vs-NEWMV-LAST) is
// now closed by the x->skip_encode search-context freeze (vp9_encoder_skip_-
// encode_search_ctx.go): when sf->skip_encode_frame is set (computed per frame
// from the previous frame's intra/inter counts, get_skip_encode_frame,
// vp9/encoder/vp9_encodeframe.c:5380-5391) and base_qindex < QIDX_SKIP_THRESH,
// libvpx's RD-search-phase intermediate encode_superblock (output_enabled==0)
// early-returns BEFORE advancing the entropy context (vp9_encodeframe.c:6112-
// 6115), so every leaf in the SB runs its RD search against the SB-ENTRY entropy
// context, not the running committed context. For frame 8 sf->skip_encode_frame
// flips to 1 (qidx=112), so libvpx searched mi(0,4) with left_context=0 and
// NEWMV-LAST mv=(-12,52) won; govpx had threaded mi(0,3)'s committed
// left_context=1 into the search, inflating the NEWMV coefficient cost so DC
// intra won. The freeze decouples the per-leaf search context (frozen) from the
// commit context (still threaded for the bitstream + next SB), matching libvpx
// exactly. Frame 1 (sf->skip_encode_frame==0, qidx=145, previous frame is the
// all-intra keyframe) is unaffected — its leaves still search the running
// threaded context, byte-for-byte as before.
//
// Frame 10 is the NEW first divergence (SB(0,0) mi(0,4) full-RD intra-vs-NEWMV-
// LAST again, but a DISTINCT root cause): the entropy context is now correct
// (govpx searches mi(0,4) with left=[0,0], matching libvpx) and govpx's
// NEWMV-LAST mv=(2,18) this_rd is byte-identical to libvpx (4883058), but
// govpx's intra producer picks H_PRED with this_rd=4845079 (dist=4587) while
// libvpx's best intra is DC_PRED with this_rd=5058519 (dist=5359) — so govpx's
// intra wins (4845079 < 4883058) where libvpx's loses (5058519 > 4883058). The
// frontier is the inter-frame intra Y-mode RD (the H_PRED distortion under
// x->skip_encode source-prediction), not the partition / entropy-context layer.
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
//  6. The x->skip_encode SEARCH-context freeze (encode_superblock early-return
//     at vp9_encodeframe.c:6112-6115): the per-leaf RD-search-phase intermediate
//     encode never advances pd->above_context / pd->left_context when
//     sf->skip_encode_frame is armed, so every leaf in the SB searches against
//     the SB-entry entropy context, decoupled from the (still-threaded) commit
//     context. This closed frame 8 (mi(0,4): NEWMV-LAST mv=(-12,52) over DC
//     intra). vp9_encoder_skip_encode_search_ctx.go.
//
// All six are gated behind the deep flags (vp9InterUseDeepRDUsePartition,
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
	// frames 0..9 byte-for-byte; frame 10 is the first divergence (mi(0,4) intra
	// H_PRED vs NEWMV-LAST — see the header), so the prefix is exactly 10.
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

	// The seed's matched-frame prefix must reach >= 10: frames 0 (keyframe) and
	// 1..9 (inter frames referencing the now-byte-exact reconstructions) all
	// serialize byte-for-byte. Frame 8 closed via the x->skip_encode search-
	// context freeze (the per-leaf RD search reads the SB-entry entropy context,
	// not the running committed context, when sf->skip_encode_frame is armed —
	// vp9_encoder_skip_encode_search_ctx.go). Frame 10 is the new first
	// divergence (mi(0,4) full-RD intra H_PRED beats NEWMV-LAST mv=(2,18) in
	// govpx by ~0.8% — the inter NEWMV this_rd is byte-identical to libvpx, the
	// gap is the inter-frame intra Y-mode RD). Asserting >= 10 (was 8) proves the
	// genuine full-RD inter engine GENERALIZES across the GOLDEN-refresh cadence
	// and the accumulated entropy-context adaptation, not just the first inter
	// frame.
	prefix := testutil.MatchedFramePrefixLength(govpxFrames, libvpxFrames)
	if prefix < 10 {
		t.Fatalf("matched-frame prefix = %d, want >= 10 (frame 0 keyframe + "+
			"frames 1..9 inter all byte-exact)", prefix)
	}
	t.Logf("{0,1,1,0,1} full-RD inter engine generalizes; "+
		"matched-frame prefix = %d (frame0=%d bytes frame1=%d bytes)",
		prefix, len(govpxFrames[0]), len(govpxFrames[1]))
}
