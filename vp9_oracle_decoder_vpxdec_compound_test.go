//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9DecoderVpxdecOracleMatchesCompoundGoldenAltrefNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	golden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundGoldenSlotForTest), 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterGoldenAltrefNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, golden, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, golden, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound golden/altref newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundFixedGoldenSignBiasNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	golden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundGoldenSlotForTest), 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundFixedGoldenSignBiasNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, golden, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, golden, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound fixed-GOLDEN sign-bias newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundFixedLastSignBiasNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	golden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundGoldenSlotForTest), 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundFixedLastSignBiasNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, golden, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, golden, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for compound fixed-LAST sign-bias newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesCompoundInterReferenceModeSelectNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	inter := vp9CompoundInterReferenceModeSelectNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for reference-mode-select compound inter newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}
