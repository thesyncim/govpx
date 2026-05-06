package libgopx

import (
	"encoding/hex"
	"testing"

	"github.com/thesyncim/libgopx/internal/testutil"
)

func TestSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxSmokeIVFHex, libvpxSmokeChecksums[:])
}

func TestNewMVSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxNewMVIVFHex, libvpxNewMVChecksums[:])
}

func TestIntraInterSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxIntraInterIVFHex, libvpxIntraInterChecksums[:])
}

func TestIntraModeSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxIntraModeIVFHex, libvpxIntraModeChecksums[:])
}

func TestChromaModeSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxChromaModeIVFHex, libvpxChromaModeChecksums[:])
}

func assertSmokeIVFMatchesLibvpxChecksums(t *testing.T, ivfHex string, checksums []testutil.FrameChecksum) {
	t.Helper()
	if len(checksums) == 0 {
		t.Fatalf("checksums must not be empty")
	}
	ivf := mustDecodeHex(t, ivfHex)
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	if header.Width != checksums[0].Width || header.Height != checksums[0].Height || header.FrameCount != uint32(len(checksums)) {
		t.Fatalf("header = %+v, want %dx%d with %d frames", header, checksums[0].Width, checksums[0].Height, len(checksums))
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	for i, want := range checksums {
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

// Generated from libgopx encoder output and verified with the libvpx v1.16.0
// checksum oracle in internal/coracle. This vector exercises a NEWMV interframe.
const libvpxNewMVIVFHex = "444b49460000200056503830200010001e0000000100000002000000000000008300000000000000000000001001009d012a2000100000000800000f0400fefe6ebffff80d0bff6281fffe337feb0ffac3feb0ffac3f19fc67f19ffac3f19fc67f19ffac3f19fc67f19fffff1cdbfff1cdbfff1cdbfe39b7fffe65e7fff32f3fff9979ff32f3ff39effff80d0bff6281ff587fd61ff587fd61f8cfe33f8cfe33f8cfe33f8cfe33f8cfe33f8cfdec00120000000100000000000000d101000000203100048981818043a41a0000"

var libvpxNewMVChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     32,
		Height:    16,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0x83, 0xc3, 0xc6, 0x07, 0x49, 0xc1, 0x43, 0x71, 0xc1, 0x39, 0x30, 0x85, 0x3d, 0x57, 0x7a, 0x04},
			U:    [16]byte{0x5f, 0xfc, 0xfd, 0xab, 0x5e, 0x50, 0x51, 0x99, 0xde, 0x10, 0x31, 0x33, 0x46, 0xb9, 0x77, 0xe8},
			V:    [16]byte{0xeb, 0x7a, 0x49, 0x1f, 0x09, 0xf6, 0x1a, 0x33, 0x8a, 0x2b, 0x9f, 0xc2, 0xdf, 0xdf, 0x00, 0x40},
			Full: [16]byte{0x55, 0x0a, 0xb7, 0xd3, 0x59, 0xe5, 0xcd, 0xea, 0x5b, 0x18, 0xce, 0xa7, 0xd6, 0x2b, 0x90, 0xf8},
		},
	},
	{
		Index:     1,
		Width:     32,
		Height:    16,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0xa0, 0x7c, 0x26, 0xca, 0x75, 0x5c, 0x5a, 0x23, 0x13, 0x14, 0x36, 0xf8, 0x94, 0x78, 0xc8, 0xba},
			U:    [16]byte{0x5f, 0xfc, 0xfd, 0xab, 0x5e, 0x50, 0x51, 0x99, 0xde, 0x10, 0x31, 0x33, 0x46, 0xb9, 0x77, 0xe8},
			V:    [16]byte{0xeb, 0x7a, 0x49, 0x1f, 0x09, 0xf6, 0x1a, 0x33, 0x8a, 0x2b, 0x9f, 0xc2, 0xdf, 0xdf, 0x00, 0x40},
			Full: [16]byte{0x95, 0xa1, 0xcb, 0x39, 0xa7, 0x9e, 0x9b, 0x93, 0xe6, 0x9f, 0x73, 0x43, 0xa9, 0x0d, 0x7e, 0x83},
		},
	},
}

// Generated from libgopx encoder output and verified with the libvpx v1.16.0
// checksum oracle in internal/coracle. This vector exercises an intra
// macroblock inside an interframe.
const libvpxIntraInterIVFHex = "444b49460000200056503830100010001e000000010000000200000000000000320000000000000000000000f000009d012a1000100000000800000dc0feffbb029ffffe39b7ffe39b7ffe39b7fc736ffffccbcfffe65e7fff32f3fe62602d00000001000000000000009101000000203100048981818000009bffffc736fffc736fffc736ff8e6dffff9979fffccbcfffe65e7fcc4c00"

var libvpxIntraInterChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     16,
		Height:    16,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0x34, 0x8a, 0x97, 0x91, 0xdc, 0x41, 0xb8, 0x97, 0x96, 0xec, 0x38, 0x08, 0xb5, 0xb5, 0x26, 0x2f},
			U:    [16]byte{0xce, 0x7b, 0x78, 0x5b, 0x1b, 0xe7, 0xad, 0x4f, 0x72, 0x77, 0x32, 0x17, 0xdb, 0x8c, 0x5d, 0x3e},
			V:    [16]byte{0x18, 0x60, 0x5d, 0x39, 0x35, 0x33, 0x8c, 0x5b, 0xcb, 0x1b, 0x1c, 0x6a, 0x47, 0xec, 0xe8, 0x1a},
			Full: [16]byte{0xe0, 0x52, 0x8a, 0x0e, 0xcf, 0xcb, 0xc9, 0x6d, 0xe1, 0x55, 0xf7, 0x4e, 0x48, 0x79, 0xbe, 0x42},
		},
	},
	{
		Index:     1,
		Width:     16,
		Height:    16,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0xb0, 0x31, 0xe0, 0x74, 0xf5, 0x7a, 0x10, 0x5f, 0x0d, 0x91, 0xcc, 0xa3, 0x4e, 0x90, 0x2c, 0x82},
			U:    [16]byte{0xce, 0x7b, 0x78, 0x5b, 0x1b, 0xe7, 0xad, 0x4f, 0x72, 0x77, 0x32, 0x17, 0xdb, 0x8c, 0x5d, 0x3e},
			V:    [16]byte{0x18, 0x60, 0x5d, 0x39, 0x35, 0x33, 0x8c, 0x5b, 0xcb, 0x1b, 0x1c, 0x6a, 0x47, 0xec, 0xe8, 0x1a},
			Full: [16]byte{0x38, 0xfb, 0xb2, 0x15, 0x27, 0xe8, 0xa5, 0x4e, 0x8a, 0x02, 0x91, 0xd1, 0x99, 0x4e, 0xd6, 0x0f},
		},
	},
}

// Generated from libgopx encoder output and verified with the libvpx v1.16.0
// checksum oracle in internal/coracle. This vector exercises non-DC whole-block
// intra mode selection in a keyframe.
const libvpxIntraModeIVFHex = "444b49460000200056503830100020001e0000000100000001000000000000007c00000000000000000000001001009d012a10002000000008000012c080fefcf3ffffeec5dfff233bfffd8b3ffda5fff697ffda5fff697ffb167ffa2cfff459ffed2fff62cfff459ffe8b3ffda5ffec59ffe8b3ffd167ffff1cdbfff1cdbfff1cdbfe39b7fffe65e7fff32f3fff9979ff31dcdadd64ca40c7ed6eb3042a6e9a5a365f1d374b3000"

var libvpxIntraModeChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     16,
		Height:    32,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0x98, 0x93, 0xdf, 0x4c, 0x62, 0xa8, 0xd5, 0x96, 0x05, 0xac, 0xc4, 0x61, 0x33, 0xdb, 0x7c, 0x30},
			U:    [16]byte{0x5f, 0xfc, 0xfd, 0xab, 0x5e, 0x50, 0x51, 0x99, 0xde, 0x10, 0x31, 0x33, 0x46, 0xb9, 0x77, 0xe8},
			V:    [16]byte{0xeb, 0x7a, 0x49, 0x1f, 0x09, 0xf6, 0x1a, 0x33, 0x8a, 0x2b, 0x9f, 0xc2, 0xdf, 0xdf, 0x00, 0x40},
			Full: [16]byte{0xe1, 0x90, 0xe9, 0xce, 0x97, 0xc4, 0xdf, 0x13, 0xce, 0xd3, 0xb0, 0xd6, 0xd2, 0x9a, 0xa1, 0xd9},
		},
	},
}

// Generated from libgopx encoder output and verified with the libvpx v1.16.0
// checksum oracle in internal/coracle. This vector exercises non-DC chroma
// intra mode selection in a keyframe.
const libvpxChromaModeIVFHex = "444b49460000200056503830100020001e0000000100000001000000000000008d00000000000000000000001001009d012a100020000000080000137500fefcf3ffffeec5dfff233bfffd8b3ffda5fff697ffda5fff697ffb167ffa2cfff459ffed2fff62cfff459ffe8b3ffda5ffec59ffe8b3ffd167ffff7b23fef8b8e6fffd7bffef8b8e6fffef647fdf171cdfbe5ffbc5c737fffd98de50e45e50fffd98de4f5feb99023ff52d6485011ffb9ab93a809181bb2f8e98a0"

var libvpxChromaModeChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     16,
		Height:    32,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: testutil.PlaneMD5{
			Y:    [16]byte{0x98, 0x93, 0xdf, 0x4c, 0x62, 0xa8, 0xd5, 0x96, 0x05, 0xac, 0xc4, 0x61, 0x33, 0xdb, 0x7c, 0x30},
			U:    [16]byte{0x71, 0x1b, 0x3a, 0x7a, 0xca, 0xa7, 0xb9, 0xfb, 0x20, 0x8e, 0xd4, 0x5f, 0x1e, 0x65, 0x7d, 0x12},
			V:    [16]byte{0xc3, 0x89, 0x48, 0x4a, 0x39, 0x03, 0x3c, 0x63, 0xcb, 0x15, 0xc9, 0x6d, 0x53, 0x06, 0x98, 0x16},
			Full: [16]byte{0xe4, 0x8b, 0xd8, 0xf7, 0x17, 0x41, 0xbc, 0x03, 0x51, 0x19, 0xec, 0x78, 0x63, 0x59, 0x4b, 0x81},
		},
	},
}
