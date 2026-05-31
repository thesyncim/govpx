package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderForceKeyFrameIsStickyUntilCommitted(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode initial keyframe: %v", err)
	}
	if e.IsKeyFrameNext() {
		t.Fatal("IsKeyFrameNext = true after initial keyframe, want false")
	}

	e.ForceKeyFrame()
	if !e.IsKeyFrameNext() {
		t.Fatal("IsKeyFrameNext = false after ForceKeyFrame, want true")
	}
	if _, err := e.EncodeInto(src, nil); !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("EncodeInto nil err = %v, want ErrBufferTooSmall", err)
	}
	if !e.IsKeyFrameNext() {
		t.Fatal("ForceKeyFrame was consumed by failed EncodeInto")
	}

	forced, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode forced keyframe: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(forced)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader forced keyframe: %v", perr)
	}
	if h.FrameType != common.KeyFrame {
		t.Fatalf("forced frame type = %d, want KeyFrame", h.FrameType)
	}
	if e.IsKeyFrameNext() {
		t.Fatal("IsKeyFrameNext still true after forced keyframe commit")
	}
}

func TestVP9EncoderEncodeIntoWithFlagsForceKeyFrameOneShot(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode initial keyframe: %v", err)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeIntoWithFlags(src, dst, govpx.EncodeForceKeyFrame)
	if err != nil {
		t.Fatalf("EncodeIntoWithFlags force keyframe: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(dst[:n])
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader forced keyframe: %v", perr)
	}
	if h.FrameType != common.KeyFrame {
		t.Fatalf("forced frame type = %d, want KeyFrame", h.FrameType)
	}
	if e.IsKeyFrameNext() {
		t.Fatal("EncodeForceKeyFrame acted sticky; next frame should be inter")
	}
}

func TestVP9EncoderAdaptiveKeyFramesDisabledByDefault(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		MaxKeyframeInterval: 999,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 16, 128, 128), dst); err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	inter, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 240, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if inter.KeyFrame {
		t.Fatal("default VP9 scene-cut frame became keyframe")
	}
}

func TestVP9EncoderAdaptiveKeyFramesHonorMinDistance(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		MinKeyframeInterval: 2,
		MaxKeyframeInterval: 999,
		AdaptiveKeyFrames:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 16, 128, 128), dst); err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	blocked, err := e.EncodeIntoWithFlagsResult(
		vp9test.NewYCbCr(width, height, 240, 128, 128), dst,
		govpx.EncodeForceGoldenFrame|govpx.EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("Encode min-distance blocked scene cut: %v", err)
	}
	if blocked.KeyFrame {
		t.Fatal("adaptive scene cut ignored MinKeyframeInterval")
	}
	allowed, err := e.EncodeIntoWithResult(
		vp9test.NewYCbCr(width, height, 16, 128, 128), dst)
	if err != nil {
		t.Fatalf("Encode min-distance allowed scene cut: %v", err)
	}
	if !allowed.KeyFrame {
		t.Fatal("adaptive scene cut did not fire after MinKeyframeInterval elapsed")
	}
}

func TestVP9EncoderAdaptiveKeyFramesSteadyStateNoAlloc(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		MaxKeyframeInterval: 999,
		AdaptiveKeyFrames:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	dst := make([]byte, 65536)
	for i := range 3 {
		if _, err := e.EncodeIntoWithResult(src, dst); err != nil {
			t.Fatalf("warm EncodeIntoWithResult[%d]: %v", i, err)
		}
	}
	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRunsForTest, func() {
		if _, err := e.EncodeIntoWithResult(src, dst); err != nil {
			t.Fatalf("adaptive EncodeIntoWithResult: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("adaptive keyframe steady state allocs = %f, want 0", allocs)
	}
}
