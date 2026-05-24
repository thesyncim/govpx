package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestDecodeIntoRejectsNilImage(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_, err = d.DecodeInto(vp8test.KeyFramePacket(16, 16, 0, 0, true), nil)
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeIntoCopiesSupportedKeyFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newVP8DecodeIntoImage(16, 16)

	info, err := d.DecodeIntoWithPTS(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst, 88)
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
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newVP8DecodeIntoImage(16, 16)
	keyPacket := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithBaseQIndex(20))

	key, err := d.DecodeIntoWithPTS(keyPacket, &dst, 100)
	if err != nil {
		t.Fatalf("key DecodeIntoWithPTS error = %v, want nil", err)
	}
	if key.InternalQuantizer != 20 || key.Quantizer != vp8common.QIndexToPublicQuantizer(20) {
		t.Fatalf("key quantizer = public:%d internal:%d, want public %d / internal 20", key.Quantizer, key.InternalQuantizer, vp8common.QIndexToPublicQuantizer(20))
	}
	if key.RefUpdates != govpx.ReferenceFlagLast|govpx.ReferenceFlagGolden|govpx.ReferenceFlagAltRef || key.RefUsed != 0 {
		t.Fatalf("key refs = updates:%03b used:%03b, want all updates / no inter refs", key.RefUpdates, key.RefUsed)
	}

	first := vp8test.InterFirstPartitionLastZeroMVWithConfig(vp8common.OnePartition, false, 36)
	inter, err := d.DecodeIntoWithPTS(vp8test.InterFramePacketWithFirstPartition(first), &dst, 101)
	if err != nil {
		t.Fatalf("inter DecodeIntoWithPTS error = %v, want nil", err)
	}
	if inter.InternalQuantizer != 36 || inter.Quantizer != vp8common.QIndexToPublicQuantizer(36) {
		t.Fatalf("inter quantizer = public:%d internal:%d, want public %d / internal 36", inter.Quantizer, inter.InternalQuantizer, vp8common.QIndexToPublicQuantizer(36))
	}
	if inter.RefUpdates != 0 || inter.RefUsed != govpx.ReferenceFlagLast {
		t.Fatalf("inter refs = updates:%03b used:%03b, want no updates / LAST used", inter.RefUpdates, inter.RefUsed)
	}
}

func TestDecodeLastFrameInfoReportsMostRecentFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatalf("LastFrameInfo before decode returned ok")
	}
	keyPacket := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithBaseQIndex(18))
	if err := d.DecodeWithPTS(keyPacket, 200); err != nil {
		t.Fatalf("key DecodeWithPTS error = %v, want nil", err)
	}
	key, ok := d.LastFrameInfo()
	if !ok {
		t.Fatalf("LastFrameInfo after key decode returned !ok")
	}
	if key.PTS != 200 || !key.KeyFrame || key.InternalQuantizer != 18 || key.RefUpdates != govpx.ReferenceFlagLast|govpx.ReferenceFlagGolden|govpx.ReferenceFlagAltRef {
		t.Fatalf("key LastFrameInfo = %+v, want PTS 200 key q18 all ref updates", key)
	}
	d.Reset()
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatalf("LastFrameInfo after Reset returned ok")
	}
}

func TestDecodeIntoInvisibleFrameDoesNotCopyOutput(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newVP8DecodeIntoImage(16, 16)
	fillVP8DecodeIntoImage(dst, 7, 8, 9)

	info, err := d.DecodeIntoWithPTS(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, false), &dst, 88)
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
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := govpx.Image{Width: 16, Height: 16}

	_, err = d.DecodeInto(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst)
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("DecodeInto error = %v, want ErrInvalidConfig", err)
	}
}

func newVP8DecodeIntoImage(width int, height int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func fillVP8DecodeIntoImage(img govpx.Image, y byte, u byte, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
	}
	for i := range img.V {
		img.V[i] = v
	}
}
