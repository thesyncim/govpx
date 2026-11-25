//go:build govpx_oracle_trace

package codec_test

import (
	"bytes"
	"fmt"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strconv"
	"testing"
)

const vp9OracleLastRefSlot = 0

func TestVP9EncoderVpxdecOracleMatchesInvisibleAltRefRefresh(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	altSrc := vp9test.NewYCbCr(width, height, 188, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeWithFlags(altSrc,
		govpx.EncodeInvisibleFrame|govpx.EncodeForceAltRefFrame|govpx.EncodeNoUpdateLast|
			govpx.EncodeNoUpdateGolden|govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode hidden ALTREF refresh: %v", err)
	}
	inter, err := e.EncodeWithFlags(altSrc,
		govpx.EncodeNoReferenceLast|govpx.EncodeNoReferenceGolden|govpx.EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode visible ALTREF-only inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, hidden, inter)
}

func TestVP9EncoderVpxdecOracleMatchesShowExistingFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 91, 143, 37)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	show, err := e.EncodeShowExistingFrame(5)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, show)
}

func TestVP9EncoderVpxdecOracleAcceptsRuntimeResize(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const (
		w1 = 64
		h1 = 64
		w2 = 96
		h2 = 80
	)
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: w1, Height: h1})
	key, err := e.Encode(vp9test.NewYCbCr(w1, h1, 72, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(vp9test.NewYCbCr(w1, h1, 92, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if err := e.SetRealtimeTarget(govpx.RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	resized, err := e.Encode(vp9test.NewYCbCr(w2, h2, 111, 123, 211))
	if err != nil {
		t.Fatalf("Encode resized keyframe: %v", err)
	}

	vp9test.VpxdecAccepts(t, "runtime resize stream", w1, h1, key, inter, resized)
}

func TestVP9EncoderVpxdecOracleMatchesIntraOnlyShowExisting(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 16, 128, 128)
	src := vp9test.NewYCbCr(width, height, 83, 141, 209)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeIntraOnlyFrame(src, 0)
	if err != nil {
		t.Fatalf("EncodeIntraOnlyFrame: %v", err)
	}
	show, err := e.EncodeShowExistingFrame(vp9OracleLastRefSlot)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame LAST: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, hidden, show)
}

func TestVP9EncoderVpxdecOracleAcceptsPackedSuperframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 32, 128, 128)
	interSrc := vp9test.NewYCbCr(width, height, 144, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	packet := vp9test.SuperframePacket(t, key, inter)

	vp9test.VpxdecAccepts(t, "packed superframe", width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesOddIntegerMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.ShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 7, 0)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatches16x8InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 32, 8
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.ShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, 0)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitXShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, -8)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert32x32InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 32, 32
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitXShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, -8)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert16x16InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 16, 16
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitXShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 4, -4)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesHorz64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitYShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, -8)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesSplit64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.QuadrantShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride,
				image.Point{X: 8}, image.Point{X: -8},
				image.Point{Y: 8}, image.Point{Y: -8})
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func encodeVP9IntegerMotionPair(t testing.TB, width, height int,
	makeInter func(govpx.Image) *image.YCbCr,
) ([]byte, []byte) {
	t.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	key, err := e.Encode(vp9test.NewMotionYCbCr(width, height))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	ref := vp9oracle.DecodeLastVisibleFrame(t, key)
	inter, err := e.Encode(makeInter(ref))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	return key, inter
}

// TestVP9EncoderVpxdecOracleAcceptsKeyframe pipes a govpx-emitted
// VP9 keyframe through the libvpx vpxdec binary (built via
// internal/coracle/build_vpxdec_vp9.sh). This is a structural acceptance
// gate: vpxdec parses the frame without error.
//
// The vp9test oracle resolver skips on CI hosts that have not built the pinned
// libvpx VP9 decoder yet.
func TestVP9EncoderVpxdecOracleAcceptsKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the encoder output", 64, 64, payload)
}

func TestVP9EncoderVpxdecOracleMatchesACKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesChromaDirectionalKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewChromaHorizontalBandsYCbCr(width, height)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesTx16DirectionalKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 16
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewHorizontalBandsYCbCr(width, height, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesInvisibleKeyFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 91, 143, 37)
	hidden, err := e.EncodeWithFlags(src, govpx.EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("Encode hidden keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode visible inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, hidden, inter)
}

// TestVP9EncoderVpxdecOracleAcceptsMultiSbKeyframe runs the structural
// oracle gate against a 128x64 frame: two side-by-side 64x64 SBs. The
// encoder's WriteModesTile dispatches per SB; libvpx must accept the
// resulting multi-SB tile body.
func TestVP9EncoderVpxdecOracleAcceptsMultiSbKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 128, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 128, 64), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the multi-SB keyframe", 128, 64, payload)
}

// TestVP9EncoderVpxdecOracleAcceptsVerticalSBStack runs the structural
// gate against a 64x128 frame: two stacked 64x64 SBs. The encoder's SB row
// loop in WriteModesTile steps mi_row by MiBlockSize across the two rows;
// libvpx must accept the per-row left_seg_context reset.
func TestVP9EncoderVpxdecOracleAcceptsVerticalSBStack(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 128})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 128), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the vertical-SB stack", 64, 128, payload)
}

// TestVP9EncoderVpxdecOracleAcceptsLargeFrame runs the structural gate
// against a 256x192 keyframe: a 4x3 SB grid. This exercises the SB walker
// against a fuller mi grid and entropy-context propagation across both axes.
func TestVP9EncoderVpxdecOracleAcceptsLargeFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 256, Height: 192})
	img := image.NewYCbCr(image.Rect(0, 0, 256, 192), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the large keyframe", 256, 192, payload)
}

// TestVP9EncoderVpxdecOracleAcceptsEdgeClippedKeyframes expands structural
// coverage beyond complete 64x64 SBs. These sizes force the
// partition writer into libvpx's frame-edge branches where the
// decoder may force SPLIT/HORZ/VERT decisions from has_rows /
// has_cols instead of reading the full tree.
func TestVP9EncoderVpxdecOracleAcceptsEdgeClippedKeyframes(t *testing.T) {
	vp9test.RequireVpxdec(t)

	cases := []struct {
		name          string
		width, height int
	}{
		{"right-edge", 96, 64},
		{"bottom-edge", 64, 96},
		{"corner-edge", 96, 96},
		{"sub-sb", 32, 32},
		{"odd-visible", 70, 70},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
				Width:  tc.width,
				Height: tc.height,
			})
			img := image.NewYCbCr(image.Rect(0, 0, tc.width, tc.height),
				image.YCbCrSubsampleRatio420)
			payload, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			vp9test.VpxdecAccepts(t, "edge-clipped keyframe", tc.width,
				tc.height, payload)
		})
	}
}

func TestVP9EncoderVpxdecOracleMatchesLosslessKeyAndInter(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 70, 70
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	keySrc := vp9test.NewCheckerYCbCr(width, height, 16, 240, 32, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}
	ref := vp9oracle.DecodeLastVisibleFrame(t, key)
	interSrc := vp9test.ShiftedI420(ref.Width, ref.Height,
		ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 4, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode lossless inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode lossless keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless keyframe")
	}
	vp9oracle.AssertImageMatchesYCbCr(t, "lossless keyframe", keyFrame, keySrc)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode lossless inter: %v", err)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless inter")
	}
	vp9oracle.AssertImageMatchesYCbCr(t, "lossless inter", interFrame, interSrc)
}

// TestVP9EncoderVpxencFrameFlagsDoubleKeyframeNonRDByteParity exercises the
// non-RD (cpu_used=8 realtime CBR) double-keyframe path: an initial keyframe at
// frame 0 plus a FORCED keyframe at frame 1 (two adjacent keyframes), then plain
// inter frames over a panning clip. The first inter frame after the double KF
// (frame 2) runs libvpx's nonrd NEWMV subpel search with a zero MV-entropy cost
// table: vp9_build_nmv_cost_table / vp9_build_inter_mode_cost
// (vp9_rd.c:439-444) only rebuild x->nmvcost and cpi->inter_mode_cost on
// non-intra frames satisfying (!use_nonrd_pick_mode || current_video_frame&7 ==
// 1), and both arrays are vpx_calloc'd to zero. Two adjacent keyframes never
// build them and frame 2 (&7 != 1) does not either, so the NEWMV refinement
// minimises pure variance (no MV cost), which previously diverged from govpx's
// always-on MV cost. Regression for that fix.
func TestVP9EncoderVpxencFrameFlagsDoubleKeyframeNonRDByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	const frames = 5
	const targetKbps = 300

	sources := vp9test.NewPanningSources(width, height, frames)

	opts := govpx.VP9EncoderOptions{
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
	}

	flags := make([]govpx.EncodeFlags, frames)
	flags[1] = govpx.EncodeForceKeyFrame

	extraArgs := []string{
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--cpu-used=8",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--exact-fps-timebase",
	}

	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, sources, flags, opts,
		extraArgs)
}

func TestVP9EncoderVpxencFrameFlagsForceKeyFrameByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 128, 128, 128),
	}
	flags := []govpx.EncodeFlags{0, govpx.EncodeForceKeyFrame}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencForceKeyFrameAPIByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 144, 128, 128),
	}
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:  width,
		Height: height,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dstSize, err := vp9oracle.EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	dst := make([]byte, dstSize)
	govpxPackets := make([][]byte, len(frames))
	for i, frame := range frames {
		if i == 1 {
			e.ForceKeyFrame()
		}
		result, err := e.EncodeIntoWithResult(frame, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeIntoWithResult frame %d unexpectedly dropped", i)
		}
		govpxPackets[i] = append([]byte(nil), result.Data...)
	}

	libvpxPackets := vp9test.VpxencFrameFlagPackets(t, frames,
		[]uint32{0, vp9oracle.FrameFlagsForLibvpx(govpx.EncodeForceKeyFrame)})
	for i, got := range govpxPackets {
		vp9test.AssertPacketByteParity(t, fmt.Sprintf("frame %d", i), got,
			libvpxPackets[i])
	}
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateAllByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeNoUpdateLast | govpx.EncodeNoUpdateGolden |
			govpx.EncodeNoUpdateAltRef,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoReferenceGoldenAltRefByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeNoReferenceGolden | govpx.EncodeNoReferenceAltRef,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateLastByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{0, govpx.EncodeNoUpdateLast}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsForceGoldenNoUpdateLastByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeForceGoldenFrame | govpx.EncodeNoUpdateLast,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsForceAltRefNoUpdateGoldenByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{
		0,
		govpx.EncodeForceAltRefFrame | govpx.EncodeNoUpdateGolden,
	}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencFrameFlagsNoUpdateEntropyByteParity(t *testing.T) {
	vp9test.RequireVpxencFrameFlags(t)

	const width, height = 64, 64
	frames := []*image.YCbCr{
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
	}
	flags := []govpx.EncodeFlags{0, govpx.EncodeNoUpdateEntropy}
	vp9oracle.AssertFrameFlagsByteParityWithOptions(t, frames, flags,
		govpx.VP9EncoderOptions{}, nil)
}

func TestVP9EncoderVpxencOracleLookaheadNoAltRefParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height, frames = 64, 64, 4
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewYCbCr(width, height, byte(80+i*24), 128, 128)
	}

	govpxPackets := vp9oracle.CaptureLookaheadPackets(t,
		govpx.VP9EncoderOptions{LookaheadFrames: 2}, sources)

	libvpxPackets := vp9test.VpxencPackets(t, sources,
		"--lag-in-frames=2", "--auto-alt-ref=0")
	matches := 0
	for i, got := range govpxPackets {
		if bytes.Equal(got, libvpxPackets[i]) {
			matches++
			continue
		}
		gotHeader, _ := vp9test.ParseHeader(t, got)
		wantHeader, _ := vp9test.ParseHeader(t, libvpxPackets[i])
		t.Logf("lookahead row %d drift: govpx bytes=%d q=%d refresh=%#x first_partition=%d libvpx bytes=%d q=%d refresh=%#x first_partition=%d",
			i, len(got), gotHeader.Quant.BaseQindex, gotHeader.RefreshFrameFlags,
			gotHeader.FirstPartitionSize, len(libvpxPackets[i]),
			wantHeader.Quant.BaseQindex, wantHeader.RefreshFrameFlags,
			wantHeader.FirstPartitionSize)
	}
	t.Logf("VP9 lookahead no-alt-ref oracle: byte_matches=%d/%d", matches, frames)
	if matches != frames {
		t.Fatalf("VP9 lookahead byte parity matched %d/%d packets", matches, frames)
	}
}

func TestVP9EncoderVpxencOracleLookaheadNoAltRefMatrixParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	cases := []struct {
		name   string
		lag    int
		frames int
	}{
		{name: "lag1", lag: 1, frames: 4},
		{name: "lag2", lag: 2, frames: 5},
		{name: "lag4", lag: 4, frames: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]*image.YCbCr, tc.frames)
			for i := range sources {
				sources[i] = vp9test.NewYCbCr(width, height,
					byte(72+i*19), 128, 128)
			}

			govpxPackets := vp9oracle.CaptureLookaheadPackets(t,
				govpx.VP9EncoderOptions{LookaheadFrames: tc.lag}, sources)
			libvpxPackets := vp9test.VpxencPackets(t, sources,
				fmt.Sprintf("--lag-in-frames=%d", tc.lag),
				"--auto-alt-ref=0")

			matches := 0
			firstMismatch := -1
			for i, got := range govpxPackets {
				if bytes.Equal(got, libvpxPackets[i]) {
					matches++
					continue
				}
				if firstMismatch < 0 {
					firstMismatch = i
				}
				gotHeader, _ := vp9test.ParseHeader(t, got)
				wantHeader, _ := vp9test.ParseHeader(t, libvpxPackets[i])
				t.Logf("lookahead %s row %d drift: govpx bytes=%d q=%d refresh=%#x first_partition=%d libvpx bytes=%d q=%d refresh=%#x first_partition=%d",
					tc.name, i, len(got), gotHeader.Quant.BaseQindex,
					gotHeader.RefreshFrameFlags, gotHeader.FirstPartitionSize,
					len(libvpxPackets[i]), wantHeader.Quant.BaseQindex,
					wantHeader.RefreshFrameFlags,
					wantHeader.FirstPartitionSize)
			}
			t.Logf("VP9 lookahead no-alt-ref matrix %s: byte_matches=%d/%d first_mismatch=%d",
				tc.name, matches, tc.frames, firstMismatch)
			if vp9test.StrictEnv("GOVPX_VP9_LOOKAHEAD_MATRIX_STRICT") &&
				matches != tc.frames {
				t.Fatalf("strict VP9 lookahead no-alt-ref matrix %s matched %d/%d packets",
					tc.name, matches, tc.frames)
			}
		})
	}
}
