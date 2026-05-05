package libgopx

import "testing"

func FuzzDecoderMalformedPackets(f *testing.F) {
	seeds := [][]byte{
		{},
		{0},
		{0, 0, 0},
		vp8InterFramePacket(0, 0, true),
		vp8KeyFramePacket(0, 16, 0, 0, true),
		vp8KeyFramePacket(16, 16, 200, 0, true),
		vp8KeyFramePacketWithPayload(16, 16, 200, 0, true),
		vp8KeyFramePacketWithPayload(16, 16, 200, 4, true),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		d, err := NewVP8Decoder(DecoderOptions{MaxWidth: 64, MaxHeight: 64})
		if err != nil {
			t.Fatalf("NewVP8Decoder returned error: %v", err)
		}

		_, _ = PeekVP8StreamInfo(packet)
		_ = d.Decode(packet)

		dst := fuzzDecodeTarget(packet)
		_, _ = d.DecodeInto(packet, &dst)
		d.Reset()
		_, _ = d.NextFrame()
	})
}

func fuzzDecodeTarget(packet []byte) Image {
	info, err := PeekVP8StreamInfo(packet)
	if err == nil && info.KeyFrame && info.Width > 0 && info.Width <= 64 && info.Height > 0 && info.Height <= 64 {
		return newTestImage(info.Width, info.Height)
	}
	return newTestImage(64, 64)
}

func TestDecoderResetKeepsDecodeHotPathAllocationFree(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(64, 64, 200, 0, true)
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	d.Reset()

	allocs := testing.AllocsPerRun(1000, func() {
		d.Reset()
		_ = d.Decode(packet)
	})
	if allocs != 0 {
		t.Fatalf("Decode after reset allocs = %v, want 0", allocs)
	}
}
