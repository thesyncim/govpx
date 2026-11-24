package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderConstantKeyframeProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Encode returned empty bytes")
	}

	// Layer 1: uncompressed header.
	var br vp9dec.BitReader
	br.Init(got)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader: %v", perr)
	}
	if h.Width != 64 || h.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", h.Width, h.Height)
	}
	if h.FrameType != common.KeyFrame {
		t.Errorf("FrameType = %d, want KeyFrame", h.FrameType)
	}
	if h.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0 (compressed header empty)")
	}
	uncSize := br.BytesRead()

	// Layer 2: compressed header. The encoder may emit counts-driven
	// probability updates, so the parsed frame context is the one the
	// tile body must use.
	compEnd := uncSize + int(h.FirstPartitionSize)
	if compEnd > len(got) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(got))
	}
	var cr bitstream.Reader
	if err := cr.Init(got[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     false,
		IntraOnly:    true,
		KeyFrame:     true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Allow16x16 {
		t.Errorf("TxMode = %d, want Allow16x16", out.TxMode)
	}

	// Layer 3: tile body via full packet decode.
	grid := decodeVP9PacketMiGridForOracleTest(t, got)
	if len(grid) != 64 {
		t.Fatalf("decoded mi grid len = %d, want 64", len(grid))
	}
	for i, mi := range grid {
		if mi.SbType != common.Block32x32 || mi.TxSize != common.Tx16x16 ||
			mi.Mode != common.DcPred || mi.Skip != 1 ||
			mi.RefFrame[0] != vp9dec.IntraFrame {
			t.Fatalf("mi[%d] = %+v, want Block32x32/Tx16/DcPred/skip intra", i, mi)
		}
	}
}

func TestVP9EncoderKeyframeConstantSourceRoundTrip(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 80})
	img := vp9test.NewYCbCr(96, 80, 91, 143, 37)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after source-backed keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, 96, 80, 91, 143, 37, 1)
}

func TestVP9EncoderKeyframeACResiduePreservesCheckerSource(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	img := vp9test.NewCheckerYCbCr(32, 32, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after checker keyframe")
	}
	assertVP9VisibleYContrast(t, frame, 32, 32, 40)
}

func TestVP9EncoderACKeyframeUsesOracleNoUpdateCompressedHeader(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewCheckerYCbCr(64, 64, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(packet)
	h, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if h.FirstPartitionSize != 2 {
		t.Fatalf("FirstPartitionSize = %d, want oracle no-update compressed header size 2", h.FirstPartitionSize)
	}
}
