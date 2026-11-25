//go:build govpx_oracle_trace

package codec

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestVP9DecoderVpxdecOracleMatchesCompoundGoldenAltrefNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
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

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
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

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
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

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
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

func TestVP9DecoderVpxdecOracleMatchesSegmentedAltrefInterMapReuseStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, 64, 64,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	seed := vp9SegmentedAltrefInterSkipFrameForTest(t)
	inter := vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, hidden, seed, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, hidden, seed, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for segmented altref inter map-reuse stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntraSkipStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterIntraFrameForTest(t, common.VPred, common.DcPred, true, 0)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter-intra skip stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntraResidualStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterIntraFrameForTest(t, common.DcPred, common.DcPred, false, 32)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter-intra residual stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesInterIntegerTopRightBorderNewMvStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	inter := vp9InterIntegerTopRightBorderNewMvFrameForTest(t)
	ivf := vp9IVFForTest(64, 64, key, inter)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, key, inter)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inter integer top-right border newmv stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func vp9IVFForTest(width, height int, packets ...[]byte) []byte {
	return vp9test.BuildVP9IVF(width, height, packets...)
}

func vp9DecodeVisibleI420ForTest(t *testing.T, packets ...[]byte) []byte {
	t.Helper()
	return vp9DecodeVisibleI420WithOptionsForTest(t, VP9DecoderOptions{},
		packets...)
}

func vp9DecodeVisibleI420WithOptionsForTest(t *testing.T,
	opts VP9DecoderOptions, packets ...[]byte,
) []byte {
	t.Helper()
	d, err := NewVP9Decoder(opts)
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

func TestVP9DecoderVpxdecOracleMatchesIntraResidualKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9test.SkipResidueKeyframe(t, 64, 64, true, 32)
	ivf := vp9IVFForTest(64, 64, packet)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for nonzero-residue keyframe\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesSegmentedAltQKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9SegmentedAltQKeyframeForTest(t)
	ivf := vp9IVFForTest(64, 64, packet)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for segmented alt-q keyframe\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesLoopFilteredKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9test.ColumnResidueKeyframe(t, 64, 64, 32, 32)
	ivf := vp9IVFForTest(64, 64, packet)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for loop-filtered keyframe\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}
