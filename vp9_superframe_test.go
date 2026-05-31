package govpx_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

func TestPackVP9SuperframeIntoRoundTrip(t *testing.T) {
	frames := [][]byte{
		{0x82, 0x49, 0x83},
		bytes.Repeat([]byte{0x55}, 256),
		{0x08},
	}
	need, err := govpx.VP9SuperframeSize(frames...)
	if err != nil {
		t.Fatalf("VP9SuperframeSize: %v", err)
	}
	dst := make([]byte, need)
	n, err := govpx.PackVP9SuperframeInto(dst, frames...)
	if err != nil {
		t.Fatalf("PackVP9SuperframeInto: %v", err)
	}
	if n != need {
		t.Fatalf("n = %d, want %d", n, need)
	}
	marker := byte(0xc0 | 1<<3 | (len(frames) - 1))
	if dst[len(dst)-1] != marker {
		t.Fatalf("marker = %#x, want %#x", dst[len(dst)-1], marker)
	}
	sf, err := bitstream.ParseSuperframe(dst)
	if err != nil {
		t.Fatalf("bitstream.ParseSuperframe: %v", err)
	}
	if sf.Count != len(frames) {
		t.Fatalf("superframe count = %d, want %d", sf.Count, len(frames))
	}
	for i := range frames {
		if !bytes.Equal(sf.Frames[i], frames[i]) {
			t.Fatalf("frame %d round-trip mismatch", i)
		}
	}
}

func TestPackVP9SuperframeRejectsInvalidInput(t *testing.T) {
	if _, err := govpx.VP9SuperframeSize(); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("empty frame list error = %v, want ErrInvalidConfig", err)
	}
	tooMany := make([][]byte, 9)
	for i := range tooMany {
		tooMany[i] = []byte{byte(i + 1)}
	}
	if _, err := govpx.VP9SuperframeSize(tooMany...); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("too many frames error = %v, want ErrInvalidConfig", err)
	}
	if _, err := govpx.VP9SuperframeSize([]byte{1}, nil); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("empty frame error = %v, want ErrInvalidConfig", err)
	}
	need, err := govpx.PackVP9SuperframeInto(make([]byte, 1), []byte{1}, []byte{2})
	if !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("short dst error = %v, want ErrBufferTooSmall", err)
	}
	if need != 6 {
		t.Fatalf("short dst returned need %d, want 6", need)
	}
}

func TestPackVP9SuperframeDecode(t *testing.T) {
	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 32, 128, 128)
	interSrc := vp9test.NewYCbCr(width, height, 144, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	packet := vp9test.SuperframePacket(t, key, inter)
	info, err := govpx.PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if !info.Superframe || info.SuperframeFrames != 2 || !info.KeyFrame {
		t.Fatalf("stream info = %+v, want two-frame superframe starting with keyframe", info)
	}
	d, _ := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode packed superframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after packed superframe")
	}
	assertVP9FilledFrameWithinForTest(t, frame, width, height, 144, 96, 224, 32)
}
