//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

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
