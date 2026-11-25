package codec

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"testing"
)

func decodeIVFIntoChecksums(t *testing.T, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := testImage(header.Width, header.Height)

	var frames []testutil.FrameChecksum
	outputIndex := 0
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", inputIndex, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo[%d] returned error: %v", inputIndex, err)
		}
		if info.KeyFrame && (dst.Width != info.Width || dst.Height != info.Height) {
			dst = testImage(info.Width, info.Height)
		}
		frameInfo, err := d.DecodeInto(frame.Data, &dst)
		if err != nil {
			t.Fatalf("DecodeInto frame %d returned error: %v", inputIndex, err)
		}
		if _, ok := d.NextFrame(); ok {
			t.Fatalf("DecodeInto frame %d queued a NextFrame output", inputIndex)
		}
		if frameInfo.ShowFrame {
			frames = append(frames, checksumFrame(outputIndex, frameInfo.KeyFrame, frameInfo.ShowFrame, dst))
			outputIndex++
		}
		offset = next
	}
	return frames
}

func decodeIVFIntoExpectError(t *testing.T, ivf []byte) error {
	t.Helper()
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		return err
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return err
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		return err
	}
	dst := testImage(header.Width, header.Height)
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			return err
		}
		if info.KeyFrame && (dst.Width != info.Width || dst.Height != info.Height) {
			dst = testImage(info.Width, info.Height)
		}
		if _, err := d.DecodeInto(frame.Data, &dst); err != nil {
			return err
		}
		offset = next
	}
	return nil
}

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
	packet := vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)
	_ = d.Decode(packet)
	var poolY [4]*byte
	for i := range d.refPool {
		poolY[i] = &d.refPool[i].Img.Y[0]
	}
	firstModes := &d.modes[0]
	firstTokens := &d.tokens[0]

	_ = d.Decode(packet)

	for i := range d.refPool {
		if &d.refPool[i].Img.Y[0] != poolY[i] {
			t.Fatalf("reference pool slot %d was reallocated for same resolution", i)
		}
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

	_ = d.Decode(vp8test.KeyFramePacketWithPayload(17, 17, 200, 0, true))

	if d.mbRows != 2 || d.mbCols != 2 {
		t.Fatalf("workspace grid = %dx%d, want 2x2", d.mbRows, d.mbCols)
	}
	if len(d.modes) != 4 || len(d.tokens) != 4 || len(d.tokenAbove) != 2 {
		t.Fatalf("workspace lengths = %d/%d/%d, want 4/4/2", len(d.modes), len(d.tokens), len(d.tokenAbove))
	}
}

func TestDecoderHotPathAllocs(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithPayload(64, 64, 200, 0, true)
	dst := newTestImage(64, 64)
	rtpPayloads, err := PacketizeVP8RTPFrame(VP8RTPPayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        7,
	}, packet, 32)
	if err != nil {
		t.Fatalf("PacketizeVP8RTPFrame returned error: %v", err)
	}
	rtpFrameBuf := make([]byte, len(packet))

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "Decode", fn: func() { _ = d.Decode(packet) }},
		{name: "DecodeWithPTS", fn: func() { _ = d.DecodeWithPTS(packet, 123) }},
		{name: "DecodeInto", fn: func() { _, _ = d.DecodeInto(packet, &dst) }},
		{name: "DecodeIntoWithPTS", fn: func() { _, _ = d.DecodeIntoWithPTS(packet, &dst, 123) }},
		{name: "DecodeRTPInto", fn: func() { _, _ = d.DecodeRTPInto(rtpFrameBuf, rtpPayloads) }},
		{name: "DecodeRTPIntoWithPTS", fn: func() { _, _ = d.DecodeRTPIntoWithPTS(rtpFrameBuf, rtpPayloads, 123) }},
		{name: "LastQuantizer", fn: func() { _, _, _ = d.LastQuantizer() }},
		{name: "NextFrame", fn: func() { _, _ = d.NextFrame() }},
		{name: "SetPostProcess", fn: func() { _ = d.SetPostProcess(0, 0) }},
		{name: "SetPostProcessConfig", fn: func() { _ = d.SetPostProcessConfig(0, 4, 0) }},
		{name: "SetDecryptor", fn: func() { _ = d.SetDecryptor(nil, nil) }},
		{name: "Reset", fn: func() { d.Reset() }},
	}

	for _, tt := range tests {
		allocs := testing.AllocsPerRun(1000, tt.fn)
		if allocs != 0 {
			t.Fatalf("%s allocs = %v, want 0", tt.name, allocs)
		}
	}

	d.lifecycle = vp8DecoderNeedKey
	allocs := testing.AllocsPerRun(1000, func() {
		d.lifecycle = vp8DecoderNeedKey
		_ = d.Close()
	})
	if allocs != 0 {
		t.Fatalf("Close allocs = %v, want 0", allocs)
	}
}
