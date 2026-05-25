package govpx

import "testing"

func TestLibvpxEncodedDecodeHasNoHotPathAllocs(t *testing.T) {
	frames := mustDecodeIVFFrames(t, libvpxEncodedBaselineIVFHex, len(libvpxEncodedBaselineChecksums))
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	decodeFrames(t, d, frames)

	allocs := testing.AllocsPerRun(1000, func() {
		d.Reset()
		for i := range frames {
			_ = d.Decode(frames[i])
			_, _ = d.NextFrame()
		}
	})
	if allocs != 0 {
		t.Fatalf("Decode/NextFrame libvpx smoke allocs = %v, want 0", allocs)
	}
}

func TestLibvpxAuthoredDecodeIntoHasNoHotPathAllocs(t *testing.T) {
	for _, tc := range libvpxAuthoredDecodeCases() {
		t.Run(tc.name, func(t *testing.T) {
			frames := mustDecodeIVFFrames(t, tc.ivfHex, len(tc.checksums))
			d, err := NewVP8Decoder(DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP8Decoder returned error: %v", err)
			}
			dst := testImage(tc.checksums[0].Width, tc.checksums[0].Height)
			decodeFramesInto(t, d, frames, &dst)

			allocs := testing.AllocsPerRun(1000, func() {
				d.Reset()
				for i := range frames {
					_, _ = d.DecodeInto(frames[i], &dst)
				}
			})
			if allocs != 0 {
				t.Fatalf("DecodeInto libvpx smoke allocs = %v, want 0", allocs)
			}
		})
	}
}
