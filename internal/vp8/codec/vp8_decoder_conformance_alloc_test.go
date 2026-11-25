package codec

import (
	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

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

func TestGovpxEncodedIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFMatchesLibvpxChecksums(t, govpxBaselineIVFHex, govpxBaselineChecksums[:])
}

func TestLibvpxEncodedIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFTokenPartition(t, libvpxEncodedBaselineIVFHex, vp8common.OnePartition)
	assertIVFHasMacroblockMode(t, libvpxEncodedBaselineIVFHex, vp8common.KeyFrame, vp8common.BPred)
	assertIVFHasMacroblockMode(t, libvpxEncodedBaselineIVFHex, vp8common.InterFrame, vp8common.NewMV)
	assertIVFMatchesLibvpxChecksums(t, libvpxEncodedBaselineIVFHex, libvpxEncodedBaselineChecksums[:])
}

func TestLibvpxAuthoredDecodeIntoMatchesLibvpxChecksums(t *testing.T) {
	for _, tc := range libvpxAuthoredDecodeCases() {
		t.Run(tc.name, func(t *testing.T) {
			assertIVFDecodeIntoMatchesLibvpxChecksums(t, tc.ivfHex, tc.checksums)
		})
	}
}

func TestTokenPartitionIVFMatchesLibvpxChecksums(t *testing.T) {
	cases := []struct {
		name      string
		ivfHex    string
		partition vp8common.TokenPartition
	}{
		{name: "two", ivfHex: libvpxTwoTokenPartitionIVFHex, partition: vp8common.TwoPartition},
		{name: "four", ivfHex: libvpxFourTokenPartitionIVFHex, partition: vp8common.FourPartition},
		{name: "eight", ivfHex: libvpxEightTokenPartitionIVFHex, partition: vp8common.EightPartition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertIVFTokenPartition(t, tc.ivfHex, tc.partition)
			assertIVFHasMacroblockMode(t, tc.ivfHex, vp8common.InterFrame, vp8common.SplitMV)
			assertIVFMatchesLibvpxChecksums(t, tc.ivfHex, libvpxTokenPartitionChecksums[:])
		})
	}
}

func TestSupportedProfileIVFMatchesLibvpxChecksums(t *testing.T) {
	cases := []struct {
		name      string
		ivfHex    string
		profile   int
		checksums []testutil.FrameChecksum
	}{
		{name: "profile1", ivfHex: libvpxProfile1IVFHex, profile: 1, checksums: libvpxProfile1Checksums[:]},
		{name: "profile2", ivfHex: libvpxProfile2IVFHex, profile: 2, checksums: libvpxProfile2Checksums[:]},
		{name: "profile3", ivfHex: libvpxProfile3IVFHex, profile: 3, checksums: libvpxProfile3Checksums[:]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertIVFProfile(t, tc.ivfHex, tc.profile)
			assertIVFMatchesLibvpxChecksums(t, tc.ivfHex, tc.checksums)
		})
	}
}

func TestLoopFilterSharpnessIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFLoopFilterSharpness(t, libvpxSharpness7IVFHex, vp8common.InterFrame, 7)
	assertIVFMatchesLibvpxChecksums(t, libvpxSharpness7IVFHex, libvpxSharpness7Checksums[:])
}

func TestErrorResilientIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFMatchesLibvpxChecksums(t, libvpxErrorResilientIVFHex, libvpxErrorResilientChecksums[:])
}

func TestNewMVIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFMatchesLibvpxChecksums(t, govpxNewMVIVFHex, govpxNewMVChecksums[:])
}

func TestSubpixelNewMVIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFMatchesLibvpxChecksums(t, govpxSubpixelNewMVIVFHex, govpxSubpixelNewMVChecksums[:])
}

func TestIntraInterIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFMatchesLibvpxChecksums(t, govpxIntraInterIVFHex, govpxIntraInterChecksums[:])
}

func TestIntraModeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFMatchesLibvpxChecksums(t, govpxIntraModeIVFHex, govpxIntraModeChecksums[:])
}

func TestChromaModeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertIVFMatchesLibvpxChecksums(t, govpxChromaModeIVFHex, govpxChromaModeChecksums[:])
}
