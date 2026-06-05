//go:build govpx_oracle_trace

package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity is the headline
// milestone pin for the {0,1,1,0,1} long-fixture parity-gap seed (CBR 700 kbps
// kf=30 realtime cpu4, VAR_BASED_PARTITION, one-pass q=145): the FIRST byte-exact
// full-RD inter frame. With the deep full-RD use-partition stack enabled, govpx
// serializes frame 1 (the first inter frame) byte-identically to the pinned
// libvpx v1.16.0 vpxenc-vp9 oracle, advancing the seed's matched-frame prefix
// from 1 (keyframe only) to >= 2 (keyframe + first inter frame).
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
//
// All four are gated behind the deep flags (vp9InterUseDeepRDUsePartition,
// vp9InterUseDeepRDRefBestRD, vp9InterUseDeepRDTxDomainDistortion), default OFF,
// so production and every other VP9 oracle gate stay byte-identical; the seed
// stays in vp9LongFixtureParityGapSeeds (its production path is unchanged). This
// test flips them locally.
func TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64

	saved := vp9InterUseDeepRDUsePartition
	savedRBR := vp9InterUseDeepRDRefBestRD
	savedTD := vp9InterUseDeepRDTxDomainDistortion
	defer func() {
		vp9InterUseDeepRDUsePartition = saved
		vp9InterUseDeepRDRefBestRD = savedRBR
		vp9InterUseDeepRDTxDomainDistortion = savedTD
	}()
	vp9InterUseDeepRDUsePartition = true
	vp9InterUseDeepRDRefBestRD = true
	vp9InterUseDeepRDTxDomainDistortion = true

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

	sources := []*image.YCbCr{
		vp9test.NewPanningYCbCr(width, height, 0),
		vp9test.NewPanningYCbCr(width, height, 1),
	}
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

	// The seed's matched-frame prefix must reach >= 2 (was 1 = keyframe only).
	prefix := testutil.MatchedFramePrefixLength(govpxFrames, libvpxFrames)
	if prefix < 2 {
		t.Fatalf("matched-frame prefix = %d, want >= 2 (frame 0 keyframe + "+
			"frame 1 first inter frame both byte-exact)", prefix)
	}
	t.Logf("{0,1,1,0,1} frame-1 first byte-exact full-RD inter frame; "+
		"matched-frame prefix = %d (frame0=%d bytes frame1=%d bytes)",
		prefix, len(govpxFrames[0]), len(govpxFrames[1]))
}
