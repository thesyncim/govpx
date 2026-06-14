//go:build govpx_oracle_trace

package govpx_test

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// fullRDFrame1Block is one committed block of frame 1, SB0, in encode order, as
// emitted by libvpx at the encode_b commit point (vp9_encodeframe.c:2226-2251,
// after update_state finalises xd->mi[0]). Field semantics mirror MODE_INFO
// (vp9/common/vp9_blockd.h): Bsize is BLOCK_SIZE (0=4x4 1=4x8 2=8x4 3=8x8),
// Mode is PREDICTION_MODE (0..9 intra DC..TM; 10 NEARESTMV 11 NEARMV 12 ZEROMV
// 13 NEWMV), Ref0/Ref1 are ref_frame[0]/[1] (0 INTRA 1 LAST 2 GOLDEN 3 ALTREF;
// -1 == NONE), Interp is interp_filter (0 EIGHTTAP 1 SMOOTH 2 SHARP; 3 ==
// SWITCHABLE_FILTERS default left on intra blocks).
type fullRDFrame1Block struct {
	MiRow, MiCol int
	Bsize        int
	Mode         int
	Ref0, Ref1   int
	Interp       int
}

// vp9FullRDSeed0_2_0_0_2Frame1SB0 is the COMPLETE libvpx ground-truth block
// decomposition of frame 1 (first inter frame), superblock 0, for the
// {0,2,0,0,2} long-fixture parity-gap seed (CBR 1200kbps kf=999 realtime cpu0).
//
// Captured 2026-06-05 from a private $TMPDIR-built libvpx v1.16.0 vpxenc
// (md5 of the produced two-frame IVF == c41fc299791d7f2a04312f5e2d55eb3c,
// byte-identical to the unmodified pinned vpxenc-vp9 oracle md5
// 758eb78456b3a300de053d9217728dfc, proving the capture probe is
// non-mutating). Source = vp9test.NewPanningSources(64,64,256); vpxenc args =
// the exact set newVP9LongFixtureFuzzCase emits for this seed, including
// --timebase=1/30.
//
// PATH CONFIRMATION (vp9_encode_sb_row dispatch, vp9_encodeframe.c:5494):
// use_nonrd_pick_mode==0 (full-RD), partition_search_type==SEARCH_PARTITION(0).
// rd_pick_partition (vp9_encodeframe.c:4288) split 64->32->16->8 over the whole
// superblock; every leaf is an 8x8 NONE block or a sub-8x8 SPLIT (no 16x16+
// inter block survived). base_qindex==145 (matches the deferred-seed note).
//
// DISTINCT REMAINING CODE PATHS this map requires (and no more):
//   - full-RD single-ref (LAST) inter mode/MV RD at 8x8: NEWMV/NEARESTMV/NEARMV
//   - sub-8x8 SPLIT inter RD with per-bmi NEW/NEAREST/NEAR MVs (56/64 leaves)
//   - sub-8x8 intra in an inter frame: mi(1,0) is 8x4 intra DC_PRED, uv D63
//   - SWITCHABLE interp filter RD: EIGHTTAP + EIGHTTAP_SMOOTH + EIGHTTAP_SHARP
//   - rd_pick_partition's recursive square SPLIT search down to BLOCK_8X8
//
// NOT required: compound prediction (every Ref1==-1), GOLDEN/ALTREF references
// (every inter Ref0==LAST), ZEROMV, 16x16/32x32/64x64 inter blocks.
var vp9FullRDSeed0_2_0_0_2Frame1SB0 = []fullRDFrame1Block{
	{0, 0, 3, 13, 1, -1, 0}, {0, 1, 0, 10, 1, -1, 0}, {1, 0, 2, 0, 0, -1, 3},
	{1, 1, 1, 13, 1, -1, 1}, {0, 2, 0, 11, 1, -1, 0}, {0, 3, 2, 13, 1, -1, 0},
	{1, 2, 1, 13, 1, -1, 0}, {1, 3, 3, 13, 1, -1, 0}, {2, 0, 1, 13, 1, -1, 0},
	{2, 1, 1, 13, 1, -1, 0}, {3, 0, 2, 13, 1, -1, 0}, {3, 1, 0, 11, 1, -1, 0},
	{2, 2, 1, 13, 1, -1, 0}, {2, 3, 0, 13, 1, -1, 0}, {3, 2, 1, 13, 1, -1, 0},
	{3, 3, 1, 13, 1, -1, 0}, {0, 4, 0, 11, 1, -1, 0}, {0, 5, 1, 13, 1, -1, 0},
	{1, 4, 0, 10, 1, -1, 0}, {1, 5, 2, 13, 1, -1, 0}, {0, 6, 3, 10, 1, -1, 0},
	{0, 7, 2, 10, 1, -1, 1}, {1, 6, 0, 10, 1, -1, 0}, {1, 7, 1, 13, 1, -1, 0},
	{2, 4, 1, 13, 1, -1, 0}, {2, 5, 0, 13, 1, -1, 1}, {3, 4, 2, 13, 1, -1, 0},
	{3, 5, 1, 13, 1, -1, 1}, {2, 6, 1, 13, 1, -1, 1}, {2, 7, 2, 10, 1, -1, 0},
	{3, 6, 0, 13, 1, -1, 0}, {3, 7, 3, 13, 1, -1, 0}, {4, 0, 2, 13, 1, -1, 0},
	{4, 1, 2, 13, 1, -1, 0}, {5, 0, 2, 13, 1, -1, 0}, {5, 1, 2, 13, 1, -1, 1},
	{4, 2, 0, 13, 1, -1, 2}, {4, 3, 0, 13, 1, -1, 0}, {5, 2, 1, 13, 1, -1, 0},
	{5, 3, 2, 13, 1, -1, 0}, {6, 0, 0, 13, 1, -1, 0}, {6, 1, 1, 13, 1, -1, 0},
	{7, 0, 1, 11, 1, -1, 0}, {7, 1, 2, 13, 1, -1, 0}, {6, 2, 2, 11, 1, -1, 0},
	{6, 3, 0, 13, 1, -1, 0}, {7, 2, 0, 11, 1, -1, 0}, {7, 3, 2, 13, 1, -1, 0},
	{4, 4, 2, 11, 1, -1, 1}, {4, 5, 2, 10, 1, -1, 1}, {5, 4, 2, 13, 1, -1, 0},
	{5, 5, 0, 13, 1, -1, 1}, {4, 6, 0, 13, 1, -1, 0}, {4, 7, 0, 13, 1, -1, 0},
	{5, 6, 3, 13, 1, -1, 1}, {5, 7, 0, 13, 1, -1, 0}, {6, 4, 3, 13, 1, -1, 0},
	{6, 5, 3, 13, 1, -1, 0}, {7, 4, 1, 13, 1, -1, 1}, {7, 5, 0, 13, 1, -1, 0},
	{6, 6, 3, 11, 1, -1, 1}, {6, 7, 0, 11, 1, -1, 1}, {7, 6, 2, 13, 1, -1, 0},
	{7, 7, 1, 11, 1, -1, 0},
}

// TestVP9FullRDFrame1DecompositionSeed0_2_0_0_2 is the regression anchor for
// the {0,2,0,0,2} full-RD frame-1 parity gap. It (1) self-validates the
// libvpx ground-truth block map embedded above (counts + invariants the rest
// of the campaign relies on), (2) re-pins the closed keyframe milestone
// (frame 0 byte-exact between govpx and the pinned vpxenc-vp9 oracle), and
// (3) asserts the now-production-default frame-1 byte-parity milestone on the
// live oracle (base_qindex==145), so future work cannot accidentally fall back
// to the shallow SearchPartition path.
func TestVP9FullRDFrame1DecompositionSeed0_2_0_0_2(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64

	// (1) Self-validate the embedded libvpx ground-truth block map. These
	// invariants are exactly what the deferred-seed note and the campaign
	// depend on; if the table is ever edited inconsistently this fails loudly.
	if got, want := len(vp9FullRDSeed0_2_0_0_2Frame1SB0), 64; got != want {
		t.Fatalf("frame-1 SB0 block count = %d, want %d (8x8 mi grid)", got, want)
	}
	var subEight, intra, compound, nonLast, sharp, smooth, eighttap int
	seen := map[[2]int]bool{}
	for i, b := range vp9FullRDSeed0_2_0_0_2Frame1SB0 {
		key := [2]int{b.MiRow, b.MiCol}
		if seen[key] {
			t.Fatalf("duplicate mi position (%d,%d) at index %d", b.MiRow, b.MiCol, i)
		}
		seen[key] = true
		if b.MiRow < 0 || b.MiRow > 7 || b.MiCol < 0 || b.MiCol > 7 {
			t.Fatalf("block %d mi (%d,%d) outside SB0 8x8 grid", i, b.MiRow, b.MiCol)
		}
		if b.Bsize < 0 || b.Bsize > 3 {
			t.Fatalf("block %d (%d,%d) bsize=%d, want a leaf size 0..3 (<=8x8)",
				i, b.MiRow, b.MiCol, b.Bsize)
		}
		if b.Bsize < 3 {
			subEight++
		}
		if b.Ref0 == 0 { // INTRA_FRAME
			intra++
			if b.Mode > 9 {
				t.Fatalf("block %d (%d,%d) intra ref but inter mode %d",
					i, b.MiRow, b.MiCol, b.Mode)
			}
		} else {
			if b.Ref0 != 1 { // LAST_FRAME
				nonLast++
			}
			if b.Mode < 10 {
				t.Fatalf("block %d (%d,%d) inter ref but intra mode %d",
					i, b.MiRow, b.MiCol, b.Mode)
			}
		}
		if b.Ref1 != -1 {
			compound++
		}
		switch b.Interp {
		case 0:
			eighttap++
		case 1:
			smooth++
		case 2:
			sharp++
		}
	}
	if subEight != 56 {
		t.Fatalf("sub-8x8 leaf count = %d, want 56 (rd_pick_partition split the SB to sub-8x8)", subEight)
	}
	if intra != 1 {
		t.Fatalf("intra-in-inter-frame block count = %d, want 1 (mi(1,0) 8x4 DC)", intra)
	}
	if compound != 0 {
		t.Fatalf("compound block count = %d, want 0 (seed needs single-ref only)", compound)
	}
	if nonLast != 0 {
		t.Fatalf("non-LAST inter-ref block count = %d, want 0 (no GOLDEN/ALTREF)", nonLast)
	}
	// The three switchable interp filters must all appear at least once so the
	// parent knows the SWITCHABLE filter RD is genuinely exercised here.
	if eighttap == 0 || smooth == 0 || sharp == 0 {
		t.Fatalf("interp filter coverage eighttap=%d smooth=%d sharp=%d, want all > 0",
			eighttap, smooth, sharp)
	}

	// (2)+(3) Drive the live oracle and govpx over the two-frame prefix of the
	// exact seed config and pin the keyframe + frame-1 byte-parity signature.
	opts := govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             0,
	}
	args := []string{
		"--end-usage=cbr",
		"--target-bitrate=1200",
		"--cpu-used=0",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--timebase=1/30",
	}

	sources := []*image.YCbCr{
		vp9test.NewPanningYCbCr(width, height, 0),
		vp9test.NewPanningYCbCr(width, height, 1),
	}

	govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, opts, sources, nil)
	libvpxFrames := vp9test.VpxencPackets(t, sources, args...)
	if len(govpxFrames) < 2 || len(libvpxFrames) < 2 {
		t.Fatalf("expected >=2 packets each, got govpx=%d libvpx=%d",
			len(govpxFrames), len(libvpxFrames))
	}

	// Keyframe (frame 0) must stay byte-exact: the closed full-RD keyframe
	// milestone for this seed.
	vp9test.AssertPacketByteParity(t, "seed{0,2,0,0,2} keyframe",
		govpxFrames[0], libvpxFrames[0])

	// Pin the oracle's frame-1 header signature. base_qindex==145 is the
	// rate-control truth the gap note records; it MUST keep matching so any
	// future frame-1 divergence is isolated to the full-RD mode/coef/partition
	// engine and not to q selection.
	libHeader1, _ := vp9test.ParseHeader(t, libvpxFrames[1])
	if got := libHeader1.Quant.BaseQindex; got != 145 {
		t.Fatalf("libvpx frame-1 base_qindex = %d, want 145 (rate-control pin)", got)
	}
	if libHeader1.FrameType == 0 {
		t.Fatal("libvpx frame 1 parsed as KEY_FRAME, want INTER")
	}

	// Frame 1 is now production-default byte-exact on the scoped cpu0
	// SearchPartition full-RD path. Keep the header log compact so future
	// failures still show whether q/filter/FPS drifted before the packet diff.
	prefix := testutil.MatchedFramePrefixLength(govpxFrames[:2], libvpxFrames[:2])
	goHeader1, _ := vp9test.ParseHeader(t, govpxFrames[1])
	t.Logf("seed{0,2,0,0,2} frame1 govpx q=%d fps=%d filterLevel=%d | libvpx q=%d fps=%d filterLevel=%d",
		goHeader1.Quant.BaseQindex, goHeader1.FirstPartitionSize, goHeader1.Loopfilter.FilterLevel,
		libHeader1.Quant.BaseQindex, libHeader1.FirstPartitionSize, libHeader1.Loopfilter.FilterLevel)
	if prefix != 2 {
		fd := testutil.FirstByteDiff(govpxFrames[1], libvpxFrames[1])
		t.Fatalf("matched-frame-prefix=%d/2; frame1 firstByteDiff=%d govpx_len=%d libvpx_len=%d, want byte-exact",
			prefix, fd, len(govpxFrames[1]), len(libvpxFrames[1]))
	}
}
