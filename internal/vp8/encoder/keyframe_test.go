package encoder

import (
	"errors"
	"testing"

	libgopx "github.com/thesyncim/libgopx"
	"github.com/thesyncim/libgopx/internal/vp8/common"
)

func TestWriteZeroKeyFrameDecodesWithPublicDecoder(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}

	n, err := WriteZeroKeyFrame(packet, 16, 16, KeyFrameStateConfig{}, modes)
	if err != nil {
		t.Fatalf("WriteZeroKeyFrame returned error: %v", err)
	}

	d, err := libgopx.NewVP8Decoder(libgopx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet[:n]); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 16 || frame.Height != 16 {
		t.Fatalf("frame dimensions = %dx%d, want 16x16", frame.Width, frame.Height)
	}
	if frame.Y[0] != 128 || frame.U[0] != 128 || frame.V[0] != 128 {
		t.Fatalf("frame samples = %d/%d/%d, want 128/128/128", frame.Y[0], frame.U[0], frame.V[0])
	}
}

func TestWriteZeroKeyFrameHandlesMacroblockPadding(t *testing.T) {
	packet := make([]byte, 8192)
	modes := []KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
	}

	n, err := WriteZeroKeyFrame(packet, 17, 17, KeyFrameStateConfig{}, modes)
	if err != nil {
		t.Fatalf("WriteZeroKeyFrame returned error: %v", err)
	}

	d, err := libgopx.NewVP8Decoder(libgopx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet[:n]); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 17 || frame.Height != 17 {
		t.Fatalf("frame dimensions = %dx%d, want 17x17", frame.Width, frame.Height)
	}
}

func TestWriteZeroKeyFrameRejectsInvalidInput(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}

	_, err := WriteZeroKeyFrame(packet[:2], 16, 16, KeyFrameStateConfig{}, modes)
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("small buffer error = %v, want ErrBufferTooSmall", err)
	}
	_, err = WriteZeroKeyFrame(packet, 16, 16, KeyFrameStateConfig{TokenPartition: common.TwoPartition}, modes)
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("token partition error = %v, want ErrInvalidPacketConfig", err)
	}
	_, err = WriteZeroKeyFrame(packet, 17, 17, KeyFrameStateConfig{}, modes)
	if !errors.Is(err, ErrModeBufferTooSmall) {
		t.Fatalf("short mode grid error = %v, want ErrModeBufferTooSmall", err)
	}
}

func TestWriteZeroKeyFrameAllocatesZero(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = WriteZeroKeyFrame(packet, 16, 16, KeyFrameStateConfig{}, modes)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteZeroKeyFrame(b *testing.B) {
	packet := make([]byte, 4096)
	modes := []KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = WriteZeroKeyFrame(packet, 16, 16, KeyFrameStateConfig{}, modes)
	}
}
