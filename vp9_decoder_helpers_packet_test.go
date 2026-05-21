package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

type vp9BitPacker struct {
	buf []byte
	pos int // bit position from MSB of current byte
}

func (p *vp9BitPacker) writeBit(b uint32) {
	if p.pos == 0 {
		p.buf = append(p.buf, 0)
	}
	if b != 0 {
		p.buf[len(p.buf)-1] |= 1 << (7 - p.pos)
	}
	p.pos = (p.pos + 1) & 7
}

func (p *vp9BitPacker) writeLiteral(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		p.writeBit((v >> uint(i)) & 1)
	}
}

func (p *vp9BitPacker) flushByte() {
	if p.pos != 0 {
		p.pos = 0
	}
}

func vp9ShowExistingFramePacketForTest(slot uint8) []byte {
	var pk vp9BitPacker
	pk.writeLiteral(2, 2)              // frame_marker = 0b10
	pk.writeLiteral(0, 2)              // profile = 0
	pk.writeBit(1)                     // show_existing_frame
	pk.writeLiteral(uint32(slot&7), 3) // frame_to_show_map_idx
	pk.flushByte()
	return pk.buf
}

func vp9SuperframePacketForTest(frames ...[]byte) []byte {
	need, err := VP9SuperframeSize(frames...)
	if err != nil {
		panic(err)
	}
	packet := make([]byte, need)
	n, err := PackVP9SuperframeInto(packet, frames...)
	if err != nil {
		panic(err)
	}
	return packet[:n]
}

func vp9SVCStyleSuperframeForTest(t *testing.T) []byte {
	t.Helper()
	return vp9SuperframePacketForTest(
		vp9EncodedKeyframeForTest(t, 32, 32, 80),
		vp9EncodedKeyframeForTest(t, 64, 64, 160),
	)
}

func vp9EncodedKeyframeForTest(t *testing.T, width, height int, y byte) []byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 37,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder %dx%d: %v", width, height, err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize %dx%d: %v", width, height, err)
	}
	dst := make([]byte, dstSize)
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height, y, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult %dx%d: %v", width, height, err)
	}
	if !result.KeyFrame || !result.ShowFrame || len(result.Data) == 0 {
		t.Fatalf("encoded test frame result = %+v, want visible keyframe", result)
	}
	return append([]byte(nil), result.Data...)
}

// TestVP9DecoderClose marks the decoder as closed; subsequent Decode
// returns ErrClosed.
