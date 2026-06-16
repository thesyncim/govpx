package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestNewVP8DecoderRejectsInvalidOptions(t *testing.T) {
	cases := []govpx.DecoderOptions{
		{Threads: -1},
		{PostProcessFlags: govpx.PostProcessAddNoise, PostProcessNoiseLevel: -1},
		{PostProcessFlags: govpx.PostProcessAddNoise, PostProcessNoiseLevel: 17},
		{PostProcessDeblockingLevel: -1},
		{PostProcessDeblockingLevel: 17},
		{PostProcessNoiseLevel: 4},
		{PostProcessFlags: govpx.PostProcessDeblock, PostProcessNoiseLevel: 4},
		{PostProcessFlags: govpx.PostProcessFlag(1 << 12)},
	}
	for i, opts := range cases {
		if _, err := govpx.NewVP8Decoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("case %d: NewVP8Decoder error = %v, want ErrInvalidConfig", i, err)
		}
	}
}

func TestNewVP8DecoderAcceptsPostProcessNoise(t *testing.T) {
	_, err := govpx.NewVP8Decoder(govpx.DecoderOptions{
		PostProcessFlags:      govpx.PostProcessAddNoise,
		PostProcessNoiseLevel: 4,
	})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v, want nil", err)
	}
}

func TestVP8DecoderRequiresInitialKeyFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8test.InterFramePacket(0, 0, true)); !errors.Is(err, govpx.ErrNeedKeyFrame) {
		t.Fatalf("Decode interframe error = %v, want ErrNeedKeyFrame", err)
	}
}

func TestVP8DecoderPublishesValidatedKeyFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{MaxWidth: 640, MaxHeight: 480})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	if err := d.DecodeWithPTS(vp8test.KeyFramePacketWithPayload(320, 240, 200, 0, true), 44); err != nil {
		t.Fatalf("DecodeWithPTS error = %v, want nil", err)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo returned !ok after visible keyframe")
	}
	if info.Width != 320 || info.Height != 240 || info.PTS != 44 || !info.ShowFrame {
		t.Fatalf("LastFrameInfo = %+v, want visible 320x240 frame at PTS 44", info)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no frame")
	}
	if frame.Width != 320 || frame.Height != 240 || frame.YStride == 0 {
		t.Fatalf("NextFrame = %+v, want decoded 320x240 frame", frame)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned the same frame twice")
	}
}

func TestVP8DecoderHiddenKeyFrameUpdatesLastFrameInfo(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	if err := d.DecodeWithPTS(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, false), 44); err != nil {
		t.Fatalf("DecodeWithPTS error = %v, want nil", err)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo returned !ok after hidden keyframe")
	}
	if info.ShowFrame || info.Width != 16 || info.Height != 16 || info.PTS != 44 {
		t.Fatalf("LastFrameInfo = %+v, want hidden 16x16 frame at PTS 44", info)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned hidden frame")
	}
}

func TestNewVP9DecoderZeroValueOptions(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if d == nil {
		t.Fatal("NewVP9Decoder returned nil")
	}
	if got := d.Codec(); got != govpx.CodecVP9 {
		t.Errorf("Codec() = %v, want CodecVP9", got)
	}
}

func TestNewVP9DecoderRejectsInvalidOptions(t *testing.T) {
	cases := []govpx.VP9DecoderOptions{
		{Threads: -1},
		{SVCSpatialLayerSet: true, SVCSpatialLayer: uint8(govpx.VP9RTPMaxSpatialLayers)},
		{PostProcessFlags: govpx.PostProcessAddNoise, PostProcessNoiseLevel: -1},
		{PostProcessFlags: govpx.PostProcessAddNoise, PostProcessNoiseLevel: 17},
		{PostProcessNoiseLevel: 4},
		{PostProcessFlags: govpx.PostProcessDeblock, PostProcessNoiseLevel: 4},
		{PostProcessFlags: govpx.PostProcessFlag(1 << 12)},
		{MaxWidth: -1},
		{MaxHeight: -1},
		{DecoderRowMT: true},
		{DecoderRowMT: true, Threads: 1},
		{DecoderLoopFilterOpt: true},
		{DecoderLoopFilterOpt: true, Threads: 1},
	}
	for i, opts := range cases {
		if _, err := govpx.NewVP9Decoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Errorf("case %d: NewVP9Decoder error = %v, want ErrInvalidConfig", i, err)
		}
	}
	if _, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		Threads: 2, DecoderRowMT: true, DecoderLoopFilterOpt: true,
	}); err != nil {
		t.Errorf("threaded constructor with row-MT + lpf-opt error = %v, want nil", err)
	}
}

func TestVP9DecoderDecodeRejectsMalformedHeader(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if err := d.Decode([]byte{0x82, 0x49}); !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Errorf("Decode malformed header error = %v, want ErrInvalidVP9Data", err)
	}
}

func TestVP9DecoderDecodeRejectsEmptyPacket(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if err := d.Decode(nil); !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Errorf("nil packet error = %v, want ErrInvalidVP9Data", err)
	}
	if err := d.Decode([]byte{}); !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Errorf("empty packet error = %v, want ErrInvalidVP9Data", err)
	}
}

func TestVP9DecoderSVCSpatialLayerSelectsSuperframePrefix(t *testing.T) {
	packet := vp9SVCStyleSuperframeForFacadeTest(t)

	all, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder all: %v", err)
	}
	if err := all.DecodeWithPTS(packet, 10); err != nil {
		t.Fatalf("Decode all layers: %v", err)
	}
	info, ok := all.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo all layers returned !ok")
	}
	if info.Width != 64 || info.Height != 64 || info.PTS != 10 {
		t.Fatalf("all-layers info = %+v, want top 64x64 layer", info)
	}

	base, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		SVCSpatialLayerSet: true,
		SVCSpatialLayer:    0,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder base: %v", err)
	}
	if err := base.DecodeWithPTS(packet, 11); err != nil {
		t.Fatalf("Decode base layer: %v", err)
	}
	info, ok = base.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo base layer returned !ok")
	}
	if info.Width != 32 || info.Height != 32 || info.PTS != 11 {
		t.Fatalf("base-layer info = %+v, want 32x32 layer", info)
	}
	img, ok := base.NextFrame()
	if !ok {
		t.Fatal("base layer NextFrame returned !ok")
	}
	if img.Width != 32 || img.Height != 32 {
		t.Fatalf("base layer image = %dx%d, want 32x32", img.Width, img.Height)
	}

	dst := newFacadeImage(32, 32)
	into, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder into: %v", err)
	}
	if err := into.SetSVCSpatialLayer(0); err != nil {
		t.Fatalf("SetSVCSpatialLayer(0): %v", err)
	}
	info, err = into.DecodeIntoWithPTS(packet, &dst, 12)
	if err != nil {
		t.Fatalf("DecodeInto base layer: %v", err)
	}
	if info.Width != 32 || info.Height != 32 || info.PTS != 12 {
		t.Fatalf("DecodeInto base-layer info = %+v, want 32x32", info)
	}
	if err := into.SetSVCSpatialLayer(1); err != nil {
		t.Fatalf("SetSVCSpatialLayer(1): %v", err)
	}
	dst = newFacadeImage(64, 64)
	info, err = into.DecodeIntoWithPTS(packet, &dst, 13)
	if err != nil {
		t.Fatalf("DecodeInto top layer: %v", err)
	}
	if info.Width != 64 || info.Height != 64 || info.PTS != 13 {
		t.Fatalf("DecodeInto top-layer info = %+v, want 64x64", info)
	}
	if err := into.ClearSVCSpatialLayer(); err != nil {
		t.Fatalf("ClearSVCSpatialLayer: %v", err)
	}
	info, err = into.DecodeIntoWithPTS(packet, &dst, 14)
	if err != nil {
		t.Fatalf("DecodeInto cleared layer filter: %v", err)
	}
	if info.Width != 64 || info.Height != 64 || info.PTS != 14 {
		t.Fatalf("DecodeInto cleared info = %+v, want 64x64", info)
	}
}

func TestVP9DecoderSVCSpatialLayerControlValidation(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.SetSVCSpatialLayer(uint8(govpx.VP9RTPMaxSpatialLayers)); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetSVCSpatialLayer invalid error = %v, want ErrInvalidConfig", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.SetSVCSpatialLayer(0); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("SetSVCSpatialLayer closed error = %v, want ErrClosed", err)
	}
	if err := d.ClearSVCSpatialLayer(); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("ClearSVCSpatialLayer closed error = %v, want ErrClosed", err)
	}
	var nilDecoder *govpx.VP9Decoder
	if err := nilDecoder.SetSVCSpatialLayer(0); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("SetSVCSpatialLayer nil error = %v, want ErrClosed", err)
	}
	if err := nilDecoder.ClearSVCSpatialLayer(); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("ClearSVCSpatialLayer nil error = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderMaxWidthRejectsLargerKeyFrame(t *testing.T) {
	packet := encodeVP9KeyframeForFacadeTest(t, 320, 240, 128)
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{MaxWidth: 160})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); !errors.Is(err, govpx.ErrFrameRejected) {
		t.Errorf("Decode error = %v, want ErrFrameRejected", err)
	}
}

func TestVP9DecoderClose(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.Decode([]byte{0x82}); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("after Close, Decode error = %v, want ErrClosed", err)
	}
	if err := d.Close(); err != nil && !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("second Close error = %v", err)
	}
}

func newFacadeImage(width, height int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		YStride: width,
		U:       make([]byte, uvWidth*uvHeight),
		UStride: uvWidth,
		V:       make([]byte, uvWidth*uvHeight),
		VStride: uvWidth,
	}
}

func vp9SVCStyleSuperframeForFacadeTest(t *testing.T) []byte {
	t.Helper()
	return packVP9SuperframeForFacadeTest(t,
		encodeVP9KeyframeForFacadeTest(t, 32, 32, 80),
		encodeVP9KeyframeForFacadeTest(t, 64, 64, 160),
	)
}

func encodeVP9KeyframeForFacadeTest(t *testing.T, width, height int, y byte) []byte {
	t.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 37,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder %dx%d: %v", width, height, err)
	}
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, y, 128, 128))
	if err != nil {
		t.Fatalf("Encode %dx%d: %v", width, height, err)
	}
	if len(packet) == 0 {
		t.Fatalf("Encode %dx%d returned an empty packet", width, height)
	}
	return packet
}

func packVP9SuperframeForFacadeTest(t *testing.T, frames ...[]byte) []byte {
	t.Helper()
	need, err := govpx.VP9SuperframeSize(frames...)
	if err != nil {
		t.Fatalf("VP9SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := govpx.PackVP9SuperframeInto(packet, frames...)
	if err != nil {
		t.Fatalf("PackVP9SuperframeInto: %v", err)
	}
	return packet[:n]
}
