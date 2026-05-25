package govpx

import "testing"

func BenchmarkLibvpxEncodedDecode(b *testing.B) {
	frames := mustDecodeIVFFrames(b, libvpxEncodedBaselineIVFHex, len(libvpxEncodedBaselineChecksums))
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		b.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	decodeFrames(b, d, frames)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Reset()
		for j := range frames {
			if err := d.Decode(frames[j]); err != nil {
				b.Fatalf("Decode frame %d returned error: %v", j, err)
			}
			if _, ok := d.NextFrame(); !ok {
				b.Fatalf("NextFrame frame %d returned no frame", j)
			}
		}
	}
}

func BenchmarkLibvpxEncodedDecodeInto(b *testing.B) {
	frames := mustDecodeIVFFrames(b, libvpxEncodedBaselineIVFHex, len(libvpxEncodedBaselineChecksums))
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		b.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := testImage(libvpxEncodedBaselineChecksums[0].Width, libvpxEncodedBaselineChecksums[0].Height)
	for i := range frames {
		if _, err := d.DecodeInto(frames[i], &dst); err != nil {
			b.Fatalf("warm DecodeInto frame %d returned error: %v", i, err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Reset()
		for j := range frames {
			if _, err := d.DecodeInto(frames[j], &dst); err != nil {
				b.Fatalf("DecodeInto frame %d returned error: %v", j, err)
			}
		}
	}
}
