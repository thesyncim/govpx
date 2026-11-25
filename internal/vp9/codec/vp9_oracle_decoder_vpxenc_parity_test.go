//go:build govpx_oracle_trace

package codec_test

import (
	"bytes"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9corpus"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"image"
	"os"
	"path/filepath"
	"testing"
)

func TestVP9DecoderOfficialIVFTestDataMatchesLibvpx(t *testing.T) {
	root, ok := vp9corpus.IVFRoot(t)
	if !ok {
		return
	}
	vp9test.RequireVpxdec(t)
	paths := vp9corpus.FindIVF(t, root, false)
	if len(paths) == 0 {
		t.Fatalf("no VP90 IVF files found under %s", root)
	}
	vp9corpus.AssertIVFMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := vp9test.VpxdecI420(t, ivf)
			got, err := vp9oracle.DecodeIVFVisibleI420(ivf)
			if err != nil {
				t.Fatalf("Decode VP90 IVF returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for official VP90 IVF %s\nlibvpx=%s\ngovpx=%s",
					filepath.Base(path),
					vp9test.MD5Hex(want),
					vp9test.MD5Hex(got))
			}
		})
	}
}

func TestVP9DecoderOfficialProfile0WebMTestDataMatchesLibvpx(t *testing.T) {
	root, ok := vp9corpus.Profile0WebMRoot(t)
	if !ok {
		return
	}
	vp9test.RequireVpxdec(t)
	paths := vp9corpus.FindProfile0WebM(t, root)
	vp9corpus.RequireProfile0WebMFiles(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := vp9test.VpxdecWebMI420(t, webm)
			got, err := vp9oracle.DecodeWebMVisibleI420(webm)
			if err != nil {
				t.Fatalf("Decode VP9 Profile 0 WebM returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("I420 mismatch for official VP9 Profile 0 WebM %s\nlibvpx=%s\ngovpx=%s",
					filepath.Base(path),
					vp9test.MD5Hex(want),
					vp9test.MD5Hex(got))
			}
		})
	}
}

func TestVP9DecoderOfficialInvalidIVFTestDataRejectedLikeLibvpx(t *testing.T) {
	root, ok := vp9corpus.InvalidIVFRoot(t)
	if !ok {
		return
	}
	vp9test.RequireVpxdec(t)
	paths := vp9corpus.FindIVF(t, root, true)
	vp9corpus.RequireInvalidIVFFiles(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			vp9test.VpxdecRejectsI420(t, ivf)
			if err := vp9oracle.DecodeIVFExpectError(ivf); err == nil {
				t.Fatalf("Decode accepted invalid VP90 IVF that libvpx rejected")
			}
		})
	}
}

func TestVP9DecoderVpxdecOracleMatchesSkipLoopFilterControl(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9test.ColumnResidueKeyframe(t, 64, 64, 32, 32)
	ivf := vp9test.BuildVP9IVF(64, 64, packet)
	want := vp9test.VpxdecI420WithOptions(t, ivf,
		vp9test.VpxdecOptions{SkipLoopFilter: true})

	got := vp9oracle.DecodeVisibleI420WithOptions(t,
		govpx.VP9DecoderOptions{SkipLoopFilter: true}, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for skip-loop-filter control\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
	if filtered := vp9oracle.DecodeVisibleI420(t, packet); bytes.Equal(got, filtered) {
		t.Fatal("skip-loop-filter oracle output unexpectedly matched filtered decode")
	}
}

func TestVP9DecoderVpxdecOracleMatchesPostProcessControls(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9test.ColumnResidueKeyframe(t, 64, 64, 32, 32)
	ivf := vp9test.BuildVP9IVF(64, 64, packet)
	for _, tc := range []struct {
		name string
		lib  vp9test.VpxdecOptions
		gov  govpx.VP9DecoderOptions
	}{
		{
			name: "default postprocess",
			lib:  vp9test.VpxdecOptions{PostProcess: true},
			gov: govpx.VP9DecoderOptions{
				PostProcessFlags: govpx.PostProcessDeblock | govpx.PostProcessDemacroblock,
			},
		},
		{
			name: "explicit deblock demacroblock",
			lib: vp9test.VpxdecOptions{
				PostProcessFlags:           int(govpx.PostProcessDeblock | govpx.PostProcessDemacroblock),
				PostProcessDeblockingLevel: 4,
			},
			gov: govpx.VP9DecoderOptions{
				PostProcessFlags: govpx.PostProcessDeblock | govpx.PostProcessDemacroblock,
			},
		},
		{
			name: "add noise",
			lib: vp9test.VpxdecOptions{
				PostProcessFlags:      int(govpx.PostProcessAddNoise),
				PostProcessNoiseLevel: 4,
			},
			gov: govpx.VP9DecoderOptions{
				PostProcessFlags:      govpx.PostProcessAddNoise,
				PostProcessNoiseLevel: 4,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := vp9test.VpxdecI420WithOptions(t, ivf, tc.lib)

			got := vp9oracle.DecodeVisibleI420WithOptions(t, tc.gov, packet)
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
	ivf := vp9test.BuildVP9IVF(1024, 64, packet)
	want := vp9test.VpxdecI420WithOptions(t, ivf,
		vp9test.VpxdecOptions{InvertTileDecodeOrder: true})

	got := vp9oracle.DecodeVisibleI420WithOptions(t,
		govpx.VP9DecoderOptions{InvertTileDecodeOrder: true}, packet)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for inverted tile decode order control\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesShowExistingStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets, ivf := vp9oracle.ShowExistingStream(t, 96, 96)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9oracle.DecodeVisibleI420(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for key/inter/show-existing stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesDecodeIntoShowExistingStream(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets, ivf := vp9oracle.ShowExistingStream(t, 96, 96)
	want := vp9test.VpxdecI420(t, ivf)

	got := vp9oracle.DecodeIntoVisibleI420(t, 96, 96, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("DecodeInto I420 mismatch for key/inter/show-existing stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9DecoderVpxdecOracleMatchesSVCSpatialLayerSuperframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packet := vp9oracle.SVCStyleSuperframe(t)
	ivf := vp9test.BuildVP9IVF(64, 64, packet)
	for _, layer := range []int{0, 1} {
		want := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})

		got := vp9oracle.DecodeVisibleI420WithOptions(t,
			govpx.VP9DecoderOptions{
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

const vp9OracleDefaultBaseQIndex = 37

func TestVP9DecoderVpxencOracleProfile0StreamMatchesLibvpx(t *testing.T) {
	vp9test.RequireVpxdec(t)
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewMotionYCbCr(width, height),
		vp9test.NewCheckerYCbCr(width, height, 48, 208, 96, 160),
		vp9test.NewHorizontalBandsYCbCr(width, height, 112, 144),
		vp9test.NewChromaHorizontalBandsYCbCr(width, height),
	}
	packets := vp9test.VpxencPackets(t, frames,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
	)
	assertVpxencVP9StreamInfo(t, packets)

	ivf := vp9test.BuildVP9IVF(width, height, packets...)
	want := vp9test.VpxdecI420(t, ivf)
	got, err := vp9oracle.DecodeIVFVisibleI420(ivf)
	if err != nil {
		t.Fatalf("govpx Decode VP9 vpxenc IVF returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for vpxenc-vp9 Profile 0 stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}

func TestVP9VpxencOracleDefaultCQKeyframeBaseQIndex(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	frame := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	first := vp9test.VpxencPackets(t, []*image.YCbCr{frame})[0]
	h, _ := vp9test.ParseHeader(t, first)
	if got := int(h.Quant.BaseQindex); got != vp9OracleDefaultBaseQIndex {
		t.Fatalf("vpxenc-vp9 BaseQindex = %d, want pinned default %d",
			got, vp9OracleDefaultBaseQIndex)
	}
}

func assertVpxencVP9StreamInfo(t *testing.T, packets [][]byte) {
	t.Helper()
	seenInter := false
	for index, packet := range packets {
		info, err := govpx.PeekVP9StreamInfo(packet)
		if err != nil {
			t.Fatalf("govpx.PeekVP9StreamInfo[%d]: %v", index, err)
		}
		if info.Profile != 0 {
			t.Fatalf("frame %d profile = %d, want 0", index, info.Profile)
		}
		if index == 0 && !info.KeyFrame {
			t.Fatalf("first vpxenc-vp9 frame was not a keyframe")
		}
		if index > 0 && !info.KeyFrame {
			seenInter = true
		}
	}
	if !seenInter {
		t.Fatalf("vpxenc-vp9 corpus did not produce an inter frame")
	}
}
