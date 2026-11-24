package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderLosslessKeyframeRoundTripExact(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := vp9test.NewCheckerYCbCr(width, height, 16, 240, 32, 224)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(packet)
	h, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if h.Quant.BaseQindex != 0 || !h.Quant.Lossless {
		t.Fatalf("quantization = %+v, want lossless qindex 0", h.Quant)
	}
	if h.Loopfilter.FilterLevel != 0 {
		t.Fatalf("loop filter level = %d, want 0 for lossless", h.Loopfilter.FilterLevel)
	}
	uncSize := br.BytesRead()
	compEnd := uncSize + int(h.FirstPartitionSize)
	if compEnd > len(packet) {
		t.Fatalf("compressed header end %d past packet len %d", compEnd, len(packet))
	}
	var cr bitstream.Reader
	if err := cr.Init(packet[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     h.Quant.Lossless,
		IntraOnly:    true,
		KeyFrame:     true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Only4x4 {
		t.Fatalf("TxMode = %d, want Only4x4 for lossless", out.TxMode)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode lossless keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless keyframe")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless keyframe", frame, img)
}

func TestVP9EncoderLosslessInterRoundTripExact(t *testing.T) {
	const width, height = 32, 32
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}
	interSrc := vp9test.NewCheckerYCbCr(width, height, 16, 240, 32, 224)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode lossless inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
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
	assertVP9ImageMatchesYCbCr(t, "lossless keyframe", keyFrame, keySrc)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode lossless inter: %v", err)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless inter frame")
	}
	assertVP9ImageMatchesYCbCr(t, "lossless inter frame", interFrame, interSrc)
}
