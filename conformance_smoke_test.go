package libgopx

import (
	"encoding/hex"
	"testing"

	"github.com/thesyncim/libgopx/internal/testutil"
)

func TestSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxSmokeIVFHex, libvpxSmokeChecksums[:])
}

func TestLibvpxEncodedSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxEncodedSmokeIVFHex, libvpxEncodedSmokeChecksums[:])
}

func TestNewMVSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxNewMVIVFHex, libvpxNewMVChecksums[:])
}

func TestSubpixelNewMVSmokeIVFMatchesLibvpxChecksums(t *testing.T) {
	assertSmokeIVFMatchesLibvpxChecksums(t, libvpxSubpixelNewMVIVFHex, libvpxSubpixelNewMVChecksums[:])
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

func checksumMD5(y, u, v, full string) testutil.PlaneMD5 {
	return testutil.PlaneMD5{
		Y:    md5Hex(y),
		U:    md5Hex(u),
		V:    md5Hex(v),
		Full: md5Hex(full),
	}
}

func md5Hex(s string) [16]byte {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 16 {
		panic("invalid test MD5")
	}
	var out [16]byte
	copy(out[:], raw)
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

// Encoded from deterministic 32x32 I420 input by the libvpx v1.16.0
// simple_encoder example, then checksummed with the libvpx v1.16.0 oracle in
// internal/coracle. This is a libvpx-authored VP8 reference stream.
const libvpxEncodedSmokeIVFHex = "444b49460000200056503830200020001e000000010000000200000000000000560100000000000000000000900c009d012a2000200001470885858885848802020275ba24c1be2bf8e9f901cf9fb25de2c907f01fc8bfb67f01fdbcfe8dbc03fc2bf35ffb16f80ff01f6e6e701e001fd15f340f52bfd01f801fe15fc5ff437f7b6f92fe035807c41abfe25b376ecbb4f18481b2b0572bcc3f80feffff24af5679a4cd69bcc3eef32163ffca80cf151c31c16fdcffe4aa4bbbc14cd15a7ff91efeb83f702e377acb525f3a190cbffb05fb13a27bf99de91586ff8d8ab1e89e4222377181ceea56d3e8fe239d66b9b71b8fdffe23f9ebb2e4658a14d80becf046b103005c9d510cabbbe413ad53add4496dceedf1e90cfbd6535d0ecc1623362de9dccb4b7fcd8321ee9554201de7fe44f87a72dae52a96c13edac721af6f726a3ae6066ef6339ad4feef767a45a62cf39f53bf0e71987acdb29ad4545ff0ebc3d390fcd4d64b61bea3de2cf532110e753131aa4a861d0a19ef4ac90d4fe9047a60c0520100000100000000000000910500051010001804c04ee03ba03e481bc03ec39f89284032a03f803ce73f893b15dff9406e4e789d7710f8deea80fef6b484ffc05046db2a8c911247b101eae3ab788db0fff1eda7f9e42ddfb2f8fea5e18d7c1f63f1d4d688003008177c3976ddf61938e718ca9e71ebf27bfb88645d425d57f92ba2a7e49ef6739741ae374ffb5a72ef6f0e31e648474f216c74e4b4e2fa25bb6f8e24ef566e81d4ffd7d0d3cea9394237f617cb47b4d85dfb632fa66603080d2914b86db70f7fef5bb9c276abfa5d7560ca6016818fce9e32ab039b05dd2d58664deb92d50d26319e09bdfa0fff0bf7fa90e4effdf1bfff3968200d6fafb348b7f45a7dc3fff5f0bfe7d3ffb2e75fe1afffdfd8fb0a9a557ff989cd5f87f55a623a50993b47587ff38042ab96ea2dd122b90ff52050f92331317832e065fd9595fb6082d81b321f4c833211fd56d0277030261f1131c39c559e224700"

var libvpxEncodedSmokeChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     32,
		Height:    32,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: checksumMD5(
			"619b9320d46f28592c05cc7fdbc932ca",
			"4da7bc1de91fa1f67109859408a22547",
			"2f5b346c77bac5c09d6c1adb71e2ef75",
			"5295d8fea89ac706ed916258d03eb846",
		),
	},
	{
		Index:     1,
		Width:     32,
		Height:    32,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: checksumMD5(
			"625598c470bc09b7b809c2c154a0e8a4",
			"09bb44a2dc7d7b87321e36ab814e7b91",
			"a4516f5667d7d81d171d5744fa6bf6e4",
			"f9aa515f6ed3ef5df4fd9f1ce329d5a9",
		),
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
// checksum oracle in internal/coracle. This vector exercises a subpixel NEWMV
// interframe.
const libvpxSubpixelNewMVIVFHex = "444b49460000200056503830100010001e000000010000000200000000000000830100000000000000000000f000009d012a10001000000008000011d0fefa9fa07ffd455c3cf6ffe3b7f929fd3fa1bffffcabfff453b569f4fcdc7fa2ebff533ff9727e973ecbfff601f3bca7ffffc1afa57d807ecc6ff40359cbf37aff3fff68ff277effff7fff75ffffe55fffa29dacdfffc1affea34fb47fd69ffd00d67dfffdffbaffe4dbf5ffdfdebfdff7fff3ff99fd807e4dbfe8058bef5ff7af4df33bf601f33bbff07c7fe7ff4ffb47fcabffd47b17debfef5e9be7ffed1fe7ff7fe0f9ffffc1affea34fb00fd5e9fd47d67dfffdffbaffe55ff68aafdebfdff7affffe55fffa29dacdffc1afa57da3fed08ff51f59cbf37aff33bf601f33bf7fffbfffb6c547e68bfd081ffa99ffcb93f4b9f5d1ffb47f9de57ffff2afffd14ed66fffea34ff9c4bff9adddff319fe61c97fccdbff33bf33bf371fff957ffe8a76a5347e6e3fd175ffa99ffcb93f4b9f65fffb00f9de53ffff06be95f601fb31bfd00d672fcdebfcfffda3fc9dfbfffdfffdd7fffff1cdbfff1cdbfff1cdbfe39b7fffe65e7fff32f3fff9979ff31300750000000100000000000000b101000000203100048981818017fe809bffffacdbffee827fd611c7ff0eb7fff336fffba09ff58471ffc3adfffe8a3bffe5fbaa7fcde9ffe8a3bffe5fbaa7fcde9fffff3633ffed5c7fe6976bff5b0bfffcd8cfffb571ff9a5daffd6c2ffffa6677ffcd00b97fbe51fff4ccefff9a0172ff7c7000"

var libvpxSubpixelNewMVChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     16,
		Height:    16,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: checksumMD5(
			"add56b48e2cfcf4af5d36380df2b3c23",
			"ce7b785b1be7ad4f72773217db8c5d3e",
			"18605d3935338c5bcb1b1c6a47ece81a",
			"00d91d994f5c48c49601e204ba7b6364",
		),
	},
	{
		Index:     1,
		Width:     16,
		Height:    16,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: checksumMD5(
			"0e83ed227561506856c561fc52dd36f4",
			"ca8c302f6aa83ab653b65a3b62abd855",
			"d6a2ea34758f123bad77a0cefe135c78",
			"2047d065c8d6d4fa2e6078ee01c8aed8",
		),
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
