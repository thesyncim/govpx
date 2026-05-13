package govpx

import (
	"bytes"
	"crypto/md5"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderVpxdecOracleMatchesIntraResidualKeyframe(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packet := vp9SkipResidueKeyframeForTest(t, 64, 64, true, 32)
	ivf := vp9IVFForTest(64, 64, packet)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for nonzero-residue keyframe\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesSegmentedAltQKeyframe(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packet := vp9SegmentedAltQKeyframeForTest(t)
	ivf := vp9IVFForTest(64, 64, packet)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for segmented alt-q keyframe\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSkipStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for skipped inter stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledZeroMvInterStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9SegmentedAltQKeyframeForTest(t)
	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled zero-mv inter stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesSegmentedAltrefInterSkipStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9SegmentedAltrefInterSkipFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for segmented altref inter skip stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesSegmentedAltrefInterMapReuseStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	seed := vp9SegmentedAltrefInterSkipFrameForTest(t)
	inter := vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, seed, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, seed, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for segmented altref inter map-reuse stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSkipStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9CompoundInterSkipFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound skipped inter stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterReferenceModeSelectNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterReferenceModeSelectNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for reference-mode-select compound inter newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterNearestMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter nearestmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterNearMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterNearMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter nearmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSubpelNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 96, 96,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterSubpelNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter subpel newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSubpelBilinearNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 96, 96,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterSubpelBilinearNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter subpel bilinear newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSubpelSwitchableSmoothNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 96, 96,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterSubpelSwitchableSmoothNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, hidden, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter subpel switchable smooth newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntraSkipStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	inter := vp9InterIntraFrameForTest(t, common.VPred, common.DcPred, true, 0)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter-intra skip stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntraResidualStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	inter := vp9InterIntraFrameForTest(t, common.DcPred, common.DcPred, false, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter-intra residual stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSkipEdgeStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9StubPacketForTest(t, 96, 96, 0, common.DcPred)
	inter := vp9InterSkipFrameForTest(t, 96, 96)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for edge skipped inter stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterResidualStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	inter := vp9InterResidueFrameForTest(t, 64, 64, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter residual stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesLoopFilteredKeyframe(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	ivf := vp9IVFForTest(64, 64, packet)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for loop-filtered keyframe\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesLoopFilteredInterResidualStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	inter := vp9InterResidueFrameLoopFilterForTest(t, 64, 64, 32, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for loop-filtered inter residual stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesLoopFilteredInterNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for loop-filtered inter newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterResidualEdgeStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	cases := []struct {
		name          string
		width, height int
	}{
		{"sub-sb", 32, 32},
		{"right-edge", 96, 64},
		{"bottom-edge", 64, 96},
		{"corner-edge", 96, 96},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := vp9StubPacketForTest(t, tc.width, tc.height, 0, common.DcPred)
			inter := vp9InterResidueFrameForTest(t, tc.width, tc.height, 32)
			ivf := vp9IVFForTest(tc.width, tc.height, key, inter)
			want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
			if err != nil {
				t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
			}

			got := vp9DecodeVisibleI420ForTest(t, key, inter)
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for edge inter residual stream\nlibvpx=%s\ngovpx=%s",
					testutil.MD5Hex(md5.Sum(want)),
					testutil.MD5Hex(md5.Sum(got)))
			}
		})
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterNearestMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter nearestmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	inter := vp9InterSubpelNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelNearestMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	inter := vp9InterSubpelNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel nearestmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelBilinearNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	inter := vp9InterSubpelBilinearNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel bilinear newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelSwitchableSmoothNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	inter := vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel switchable smooth newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelSwitchableSharpNearestMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	inter := vp9InterSubpelSwitchableSharpNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel switchable sharp nearestmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelTopRightBorderNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterSubpelTopRightBorderNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel top-right border newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntegerTopRightBorderNewMvStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterIntegerTopRightBorderNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter integer top-right border newmv stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesTiledInterSkipStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	key := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)
	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 1)
	ivf := vp9IVFForTest(1024, 64, key, inter)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for tiled skipped inter stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesShowExistingStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packets, ivf := vp9ShowExistingOracleStreamForTest(t, 96, 96)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeVisibleI420ForTest(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for key/hidden/show-existing stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func TestVP9DecoderVpxdecOracleMatchesDecodeIntoShowExistingStream(t *testing.T) {
	requireVP9VpxdecOracle(t)

	packets, ivf := vp9ShowExistingOracleStreamForTest(t, 96, 96)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}

	got := vp9DecodeIntoVisibleI420ForTest(t, 96, 96, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("DecodeInto I420 mismatch for key/hidden/show-existing stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

func requireVP9VpxdecOracle(t *testing.T) {
	t.Helper()
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}
}

func vp9ShowExistingOracleStreamForTest(t *testing.T, width, height int) ([][]byte, []byte) {
	t.Helper()
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode hidden intra-only: %v", err)
	}
	packets := [][]byte{
		key,
		hidden,
		vp9ShowExistingFramePacketForTest(5),
	}
	return packets, vp9IVFForTest(width, height, packets...)
}

func vp9IVFForTest(width, height int, packets ...[]byte) []byte {
	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               width,
		Height:              height,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          uint32(len(packets)),
	}
	out := testutil.WriteIVFHeader(header)
	for i, packet := range packets {
		out = append(out, testutil.WriteIVFFrame(packet, uint64(i))...)
	}
	return out
}

func vp9DecodeVisibleI420ForTest(t *testing.T, packets ...[]byte) []byte {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	var out []byte
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, ok := d.NextFrame(); ok {
			out = appendVP9I420(out, frame)
		}
	}
	return out
}

func vp9DecodeIntoVisibleI420ForTest(t *testing.T, width, height int, packets ...[]byte) []byte {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(width, height)
	var out []byte
	for i, packet := range packets {
		info, err := d.DecodeInto(packet, &dst)
		if err != nil {
			t.Fatalf("DecodeInto packet %d: %v", i, err)
		}
		if info.ShowFrame {
			out = appendVP9I420(out, dst)
		}
	}
	return out
}

func appendVP9I420(out []byte, img Image) []byte {
	for row := range img.Height {
		start := row * img.YStride
		out = append(out, img.Y[start:start+img.Width]...)
	}
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	for row := range uvHeight {
		start := row * img.UStride
		out = append(out, img.U[start:start+uvWidth]...)
	}
	for row := range uvHeight {
		start := row * img.VStride
		out = append(out, img.V[start:start+uvWidth]...)
	}
	return out
}
