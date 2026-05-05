package encoder_test

import (
	"errors"
	"testing"

	libgopx "github.com/thesyncim/libgopx"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
)

func TestWriteZeroKeyFrameDecodesWithPublicDecoder(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}

	n, err := vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
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

func TestWriteNeutralKeyFrameDecodesWithPublicDecoder(t *testing.T) {
	packet := make([]byte, 4096)

	n, err := vp8enc.WriteNeutralKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{})
	if err != nil {
		t.Fatalf("WriteNeutralKeyFrame returned error: %v", err)
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
	if frame.Width != 16 || frame.Height != 16 || frame.Y[0] != 128 {
		t.Fatalf("frame = %dx%d Y0=%d, want 16x16 Y0 128", frame.Width, frame.Height, frame.Y[0])
	}
}

func TestWriteZeroKeyFrameHandlesMacroblockPadding(t *testing.T) {
	packet := make([]byte, 8192)
	modes := []vp8enc.KeyFrameMacroblockMode{
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
		{YMode: common.DCPred, UVMode: common.DCPred},
	}

	n, err := vp8enc.WriteZeroKeyFrame(packet, 17, 17, vp8enc.KeyFrameStateConfig{}, modes)
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
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}

	_, err := vp8enc.WriteZeroKeyFrame(packet[:2], 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
	if !errors.Is(err, vp8enc.ErrBufferTooSmall) {
		t.Fatalf("small buffer error = %v, want ErrBufferTooSmall", err)
	}
	_, err = vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{TokenPartition: common.TwoPartition}, modes)
	if !errors.Is(err, vp8enc.ErrInvalidPacketConfig) {
		t.Fatalf("token partition error = %v, want ErrInvalidPacketConfig", err)
	}
	_, err = vp8enc.WriteZeroKeyFrame(packet, 17, 17, vp8enc.KeyFrameStateConfig{}, modes)
	if !errors.Is(err, vp8enc.ErrModeBufferTooSmall) {
		t.Fatalf("short mode grid error = %v, want ErrModeBufferTooSmall", err)
	}
}

func TestWriteZeroKeyFrameAllocatesZero(t *testing.T) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteZeroKeyFrame(b *testing.B) {
	packet := make([]byte, 4096)
	modes := []vp8enc.KeyFrameMacroblockMode{{YMode: common.DCPred, UVMode: common.DCPred}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = vp8enc.WriteZeroKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{}, modes)
	}
}

func BenchmarkWriteNeutralKeyFrame(b *testing.B) {
	packet := make([]byte, 4096)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = vp8enc.WriteNeutralKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{})
	}
}
