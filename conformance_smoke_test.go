package libgopx

import (
	"encoding/hex"
	"testing"

	"github.com/thesyncim/libgopx/internal/testutil"
)

func TestSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	ivf := mustDecodeHex(t, libvpxSmokeIVFHex)
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	if header.Width != 32 || header.Height != 16 || header.FrameCount != uint32(len(libvpxSmokeChecksums)) {
		t.Fatalf("header = %+v, want 32x16 with %d frames", header, len(libvpxSmokeChecksums))
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	for i, want := range libvpxSmokeChecksums {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", i, err)
		}
		img, ok := d.NextFrame()
		if !ok {
			t.Fatalf("NextFrame frame %d returned no frame", i)
		}
		got := checksumFrame(i, want.KeyFrame, want.ShowFrame, img)
		if !testutil.SameFrameChecksum(got, want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\nlibgopx: %s", i, formatChecksum(want), formatChecksum(got))
		}
		offset = next
	}
	if offset != len(ivf) {
		t.Fatalf("final IVF offset = %d, want %d", offset, len(ivf))
	}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	out, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	return out
}

// Generated from libgopx encoder output and verified with the libvpx v1.16.0
// checksum oracle in internal/coracle.
const libvpxSmokeIVFHex = "444b49460000200056503830200010001e0000000100000002000000000000005f00000000000000000000001001009d012a2000100000002800000f0400fef6507ffdfa69ff39ffff26c9725c9724e2c6abb51e9788e49c58d57ffff295ffc6eff765c16ffff99a3ff49bfec37901fe81f697ffbf4d3fe73ff4fd3f4fd3c43cb5ada69e9788796b5b1e00120000000100000000000000d101000000a03100048981818043a46b0000"

var libvpxSmokeChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     32,
		Height:    16,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0x03, 0x41, 0x47, 0xb2, 0x1e, 0x0f, 0x49, 0x8a, 0xe3, 0x46, 0x0e, 0x8b, 0x1d, 0xb4, 0x3c, 0x98},
			U:    [16]byte{0xbe, 0x62, 0x8b, 0x38, 0x9b, 0x8b, 0x01, 0xce, 0xaf, 0x4e, 0x20, 0x29, 0xbb, 0x59, 0xa9, 0xf3},
			V:    [16]byte{0xeb, 0x7a, 0x49, 0x1f, 0x09, 0xf6, 0x1a, 0x33, 0x8a, 0x2b, 0x9f, 0xc2, 0xdf, 0xdf, 0x00, 0x40},
			Full: [16]byte{0xc5, 0x81, 0x68, 0xcc, 0xe8, 0x55, 0x5e, 0x8c, 0x60, 0xab, 0xdf, 0x91, 0x6b, 0xc6, 0x3f, 0x86},
		},
	},
	{
		Index:     1,
		Width:     32,
		Height:    16,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0xb6, 0x69, 0x60, 0x72, 0xd7, 0xb2, 0xeb, 0x1f, 0xf9, 0xe7, 0xe6, 0xdf, 0xb4, 0x70, 0x6c, 0xe2},
			U:    [16]byte{0xbe, 0x62, 0x8b, 0x38, 0x9b, 0x8b, 0x01, 0xce, 0xaf, 0x4e, 0x20, 0x29, 0xbb, 0x59, 0xa9, 0xf3},
			V:    [16]byte{0xeb, 0x7a, 0x49, 0x1f, 0x09, 0xf6, 0x1a, 0x33, 0x8a, 0x2b, 0x9f, 0xc2, 0xdf, 0xdf, 0x00, 0x40},
			Full: [16]byte{0x5a, 0xc3, 0xb7, 0x65, 0x20, 0x53, 0x81, 0x48, 0xbc, 0x83, 0x16, 0x72, 0x37, 0x24, 0x61, 0x1d},
		},
	},
}
