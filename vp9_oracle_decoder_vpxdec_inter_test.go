//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestVP9DecoderVpxdecOracleMatchesInterSkipStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for skipped inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledZeroMvInterStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9SegmentedAltQKeyframeForTest(t)
	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled zero-mv inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledNewMvInterStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9SegmentedAltQKeyframeForTest(t)
	inter := vp9ScaledNewMvInterFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled newmv inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledNearestMvInterStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 128, 128, 0, 32)
	inter := vp9ScaledInterNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(128, 128, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled nearestmv inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledNearMvInterStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 128, 128, 0, 32)
	inter := vp9ScaledInterNearMvFrameForTest(t)
	ivf := vp9IVFForTest(128, 128, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled nearmv inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesSegmentedAltrefInterSkipStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9SegmentedAltrefInterSkipFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for segmented altref inter skip stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSkipStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9CompoundInterSkipFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound skipped inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterNearestMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter nearestmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterNearMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterNearMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter nearmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSubpelNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 96, 96,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterSubpelNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter subpel newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledCompoundInterNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9ScaledCompoundInterNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled compound inter newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledCompoundInterNearestMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 128, 128, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 128, 128,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9ScaledCompoundInterNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(128, 128, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled compound inter nearestmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesScaledCompoundInterNearMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 128, 128, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 128, 128,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9ScaledCompoundInterNearMvFrameForTest(t)
	ivf := vp9IVFForTest(128, 128, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for scaled compound inter nearmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSubpelBilinearNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 96, 96,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterSubpelBilinearNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter subpel bilinear newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterSubpelSwitchableSmoothNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 96, 96,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterSubpelSwitchableSmoothNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound inter subpel switchable smooth newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSkipEdgeStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 96, 96, 0, common.DcPred)
	inter := vp9InterSkipFrameForTest(t, 96, 96)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for edge skipped inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterResidualStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterResidueFrameForTest(t, 64, 64, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter residual stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesLoopFilteredInterResidualStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterResidueFrameLoopFilterForTest(t, 64, 64, 32, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for loop-filtered inter residual stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesLoopFilteredInterNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for loop-filtered inter newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterResidualEdgeStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

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
			key := vp9test.StubPacket(t, tc.width, tc.height, 0, common.DcPred)
			inter := vp9InterResidueFrameForTest(t, tc.width, tc.height, 32)
			ivf := vp9IVFForTest(tc.width, tc.height, key, inter)
			want := vp9test.VpxdecI420(t, ivf)

			got := vp9DecodeVisibleI420ForTest(t, key, inter)
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for edge inter residual stream\nlibvpx=%s\ngovpx=%s",
					vp9test.MD5Hex(want),
					vp9test.MD5Hex(got))
			}
		})
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9InterNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterNearestMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9InterNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter nearestmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterNearMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9InterNearMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter nearmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	inter := vp9InterSubpelNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelNearestMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	inter := vp9InterSubpelNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel nearestmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelBilinearNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	inter := vp9InterSubpelBilinearNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel bilinear newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelSwitchableSmoothNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	inter := vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel switchable smooth newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelSwitchableSharpNearestMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 96, 96, 0, 32)
	inter := vp9InterSubpelSwitchableSharpNearestMvFrameForTest(t)
	ivf := vp9IVFForTest(96, 96, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel switchable sharp nearestmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterSubpelTopRightBorderNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9InterSubpelTopRightBorderNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter subpel top-right border newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesTiledInterSkipStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.MultiTileStubPacket(t, 1024, 64, 1)
	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 1)
	ivf := vp9IVFForTest(1024, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for tiled skipped inter stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}
