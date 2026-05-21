package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

func TestReconstructFrameInvalidInterModeReturnsInvalidData(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.ensureFrameBuffers(StreamInfo{Width: 16, Height: 16, KeyFrame: true}); err != nil {
		t.Fatalf("ensureFrameBuffers returned error: %v", err)
	}
	d.modes[0] = vp8dec.MacroblockMode{
		RefFrame: vp8common.LastFrame,
		Mode:     vp8common.MBPredictionMode(99),
	}

	err = d.reconstructFrame(StreamInfo{Profile: 0})
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("reconstructFrame error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeReusesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	_ = d.Decode(packet)
	firstY := &d.current.Img.Y[0]
	firstLastY := &d.lastRef.Img.Y[0]
	firstModes := &d.modes[0]
	firstTokens := &d.tokens[0]

	_ = d.Decode(packet)

	if &d.current.Img.Y[0] != firstY {
		t.Fatalf("current frame buffer was reallocated for same resolution")
	}
	if &d.lastRef.Img.Y[0] != firstLastY {
		t.Fatalf("last reference buffer was reallocated for same resolution")
	}
	if &d.modes[0] != firstModes || &d.tokens[0] != firstTokens {
		t.Fatalf("macroblock workspace was reallocated for same resolution")
	}
}

func TestDecodeWorkspaceTracksMacroblockGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_ = d.Decode(vp8KeyFramePacketWithPayload(17, 17, 200, 0, true))

	if d.mbRows != 2 || d.mbCols != 2 {
		t.Fatalf("workspace grid = %dx%d, want 2x2", d.mbRows, d.mbCols)
	}
	if len(d.modes) != 4 || len(d.tokens) != 4 || len(d.tokenAbove) != 2 {
		t.Fatalf("workspace lengths = %d/%d/%d, want 4/4/2", len(d.modes), len(d.tokens), len(d.tokenAbove))
	}
}

func TestDecodeIntoRejectsNilImage(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_, err = d.DecodeInto(vp8KeyFramePacket(16, 16, 0, 0, true), nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeIntoCopiesSupportedKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)

	info, err := d.DecodeIntoWithPTS(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst, 88)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS error = %v, want nil", err)
	}
	if info.Width != 16 || info.Height != 16 || !info.KeyFrame || info.PTS != 88 {
		t.Fatalf("FrameInfo = %+v, want 16x16 keyframe PTS 88", info)
	}
	if got := dst.Y[0]; got != 128 {
		t.Fatalf("dst Y[0] = %d, want 128", got)
	}
	if got := dst.U[0]; got != 128 {
		t.Fatalf("dst U[0] = %d, want 128", got)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued a frame for NextFrame")
	}
}

func TestDecodeIntoFrameInfoReportsQuantizerAndReferences(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)
	keyPacket := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithBaseQIndex(20))

	key, err := d.DecodeIntoWithPTS(keyPacket, &dst, 100)
	if err != nil {
		t.Fatalf("key DecodeIntoWithPTS error = %v, want nil", err)
	}
	if key.InternalQuantizer != 20 || key.Quantizer != vp8common.QIndexToPublicQuantizer(20) {
		t.Fatalf("key quantizer = public:%d internal:%d, want public %d / internal 20", key.Quantizer, key.InternalQuantizer, vp8common.QIndexToPublicQuantizer(20))
	}
	if key.RefUpdates != ReferenceFlagLast|ReferenceFlagGolden|ReferenceFlagAltRef || key.RefUsed != 0 {
		t.Fatalf("key refs = updates:%03b used:%03b, want all updates / no inter refs", key.RefUpdates, key.RefUsed)
	}

	first := vp8InterFirstPartitionLastZeroMVWithConfig(vp8common.OnePartition, false, 36)
	inter, err := d.DecodeIntoWithPTS(vp8InterFramePacketWithFirstPartition(first), &dst, 101)
	if err != nil {
		t.Fatalf("inter DecodeIntoWithPTS error = %v, want nil", err)
	}
	if inter.InternalQuantizer != 36 || inter.Quantizer != vp8common.QIndexToPublicQuantizer(36) {
		t.Fatalf("inter quantizer = public:%d internal:%d, want public %d / internal 36", inter.Quantizer, inter.InternalQuantizer, vp8common.QIndexToPublicQuantizer(36))
	}
	if inter.RefUpdates != 0 || inter.RefUsed != ReferenceFlagLast {
		t.Fatalf("inter refs = updates:%03b used:%03b, want no updates / LAST used", inter.RefUpdates, inter.RefUsed)
	}
}

func TestDecodeLastFrameInfoReportsMostRecentFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatalf("LastFrameInfo before decode returned ok")
	}
	keyPacket := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithBaseQIndex(18))
	if err := d.DecodeWithPTS(keyPacket, 200); err != nil {
		t.Fatalf("key DecodeWithPTS error = %v, want nil", err)
	}
	key, ok := d.LastFrameInfo()
	if !ok {
		t.Fatalf("LastFrameInfo after key decode returned !ok")
	}
	if key.PTS != 200 || !key.KeyFrame || key.InternalQuantizer != 18 || key.RefUpdates != ReferenceFlagLast|ReferenceFlagGolden|ReferenceFlagAltRef {
		t.Fatalf("key LastFrameInfo = %+v, want PTS 200 key q18 all ref updates", key)
	}
	d.Reset()
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatalf("LastFrameInfo after Reset returned ok")
	}
}

func TestDecodeIntoInvisibleFrameDoesNotCopyOutput(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)
	fillImage(dst, 7, 8, 9)

	info, err := d.DecodeIntoWithPTS(vp8KeyFramePacketWithPayload(16, 16, 200, 0, false), &dst, 88)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS error = %v, want nil", err)
	}
	if info.ShowFrame || info.PTS != 88 {
		t.Fatalf("FrameInfo = %+v, want invisible PTS 88", info)
	}
	if dst.Y[0] != 7 || dst.U[0] != 8 || dst.V[0] != 9 {
		t.Fatalf("dst samples = %d/%d/%d, want unchanged 7/8/9", dst.Y[0], dst.U[0], dst.V[0])
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued invisible frame")
	}
}

func TestDecodeIntoRejectsInvalidImage(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := Image{Width: 16, Height: 16}

	_, err = d.DecodeInto(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("DecodeInto error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecoderHotPathAllocs(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(64, 64, 200, 0, true)
	dst := newTestImage(64, 64)

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "Decode", fn: func() { _ = d.Decode(packet) }},
		{name: "DecodeWithPTS", fn: func() { _ = d.DecodeWithPTS(packet, 123) }},
		{name: "DecodeInto", fn: func() { _, _ = d.DecodeInto(packet, &dst) }},
		{name: "DecodeIntoWithPTS", fn: func() { _, _ = d.DecodeIntoWithPTS(packet, &dst, 123) }},
		{name: "NextFrame", fn: func() { _, _ = d.NextFrame() }},
		{name: "Reset", fn: func() { d.Reset() }},
	}

	for _, tt := range tests {
		allocs := testing.AllocsPerRun(1000, tt.fn)
		if allocs != 0 {
			t.Fatalf("%s allocs = %v, want 0", tt.name, allocs)
		}
	}

	d.closed = false
	allocs := testing.AllocsPerRun(1000, func() {
		d.closed = false
		_ = d.Close()
	})
	if allocs != 0 {
		t.Fatalf("Close allocs = %v, want 0", allocs)
	}
}
