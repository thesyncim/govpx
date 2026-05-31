//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestVP9DecoderVpxdecOracleMatchesSkipLoopFilterControl(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	ivf := vp9IVFForTest(64, 64, packet)
	want := vp9test.VpxdecI420WithOptions(t, ivf,
		vp9test.VpxdecOptions{SkipLoopFilter: true})

	got := vp9DecodeVisibleI420WithOptionsForTest(t,
		VP9DecoderOptions{SkipLoopFilter: true}, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for skip-loop-filter control\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
	if filtered := vp9DecodeVisibleI420ForTest(t, packet); bytes.Equal(got, filtered) {
		t.Fatal("skip-loop-filter oracle output unexpectedly matched filtered decode")
	}
}

func TestVP9DecoderVpxdecOracleMatchesPostProcessControls(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	ivf := vp9IVFForTest(64, 64, packet)
	for _, tc := range []struct {
		name string
		lib  vp9test.VpxdecOptions
		gov  VP9DecoderOptions
	}{
		{
			name: "default postprocess",
			lib:  vp9test.VpxdecOptions{PostProcess: true},
			gov:  VP9DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock},
		},
		{
			name: "explicit deblock demacroblock",
			lib: vp9test.VpxdecOptions{
				PostProcessFlags:           int(PostProcessDeblock | PostProcessDemacroblock),
				PostProcessDeblockingLevel: 4,
			},
			gov: VP9DecoderOptions{
				PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock,
			},
		},
		{
			name: "add noise",
			lib: vp9test.VpxdecOptions{
				PostProcessFlags:      int(PostProcessAddNoise),
				PostProcessNoiseLevel: 4,
			},
			gov: VP9DecoderOptions{
				PostProcessFlags:      PostProcessAddNoise,
				PostProcessNoiseLevel: 4,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := vp9test.VpxdecI420WithOptions(t, ivf, tc.lib)

			got := vp9DecodeVisibleI420WithOptionsForTest(t, tc.gov, packet)
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for VP9 postprocess %s\nlibvpx=%s\ngovpx=%s",
					tc.name,
					vp9test.MD5Hex(want),
					vp9test.MD5Hex(got))
			}
		})
	}
}

func TestVP9DecoderVpxdecOracleMatchesInvertTileDecodeOrderControl(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9test.MultiTileModePacket(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})
	ivf := vp9IVFForTest(1024, 64, packet)
	want := vp9test.VpxdecI420WithOptions(t, ivf,
		vp9test.VpxdecOptions{InvertTileDecodeOrder: true})

	got := vp9DecodeVisibleI420WithOptionsForTest(t,
		VP9DecoderOptions{InvertTileDecodeOrder: true}, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inverted tile decode order control\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesShowExistingStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets, ivf := vp9ShowExistingOracleStreamForTest(t, 96, 96)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeVisibleI420ForTest(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for key/inter/show-existing stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesDecodeIntoShowExistingStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets, ivf := vp9ShowExistingOracleStreamForTest(t, 96, 96)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9DecodeIntoVisibleI420ForTest(t, 96, 96, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("DecodeInto I420 mismatch for key/inter/show-existing stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesSVCSpatialLayerSuperframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9SVCStyleSuperframeForTest(t)
	ivf := vp9IVFForTest(64, 64, packet)
	for _, layer := range []int{0, 1} {
		want := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})

		got := vp9DecodeVisibleI420WithOptionsForTest(t,
			VP9DecoderOptions{
				SVCSpatialLayerSet: true,
				SVCSpatialLayer:    uint8(layer),
			}, packet)
		if !bytes.Equal(got, want) {
			t.Fatalf("I420 mismatch for SVC spatial layer %d superframe\nlibvpx=%s len=%d\ngovpx=%s len=%d",
				layer,
				vp9test.MD5Hex(want), len(want),
				vp9test.MD5Hex(got), len(got))
		}
	}
}
