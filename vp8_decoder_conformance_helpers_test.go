package govpx

import (
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"testing"
)

func assertIVFMatchesLibvpxChecksums(t *testing.T, ivfHex string, checksums []testutil.FrameChecksum) {
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
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(want), formatChecksum(got))
		}
		offset = next
	}
	if offset != len(ivf) {
		t.Fatalf("final IVF offset = %d, want %d", offset, len(ivf))
	}
}

type decodeFixtureCase struct {
	name      string
	ivfHex    string
	checksums []testutil.FrameChecksum
}

func libvpxAuthoredDecodeCases() []decodeFixtureCase {
	return []decodeFixtureCase{
		{name: "base", ivfHex: libvpxEncodedBaselineIVFHex, checksums: libvpxEncodedBaselineChecksums[:]},
		{name: "token-two", ivfHex: libvpxTwoTokenPartitionIVFHex, checksums: libvpxTokenPartitionChecksums[:]},
		{name: "token-four", ivfHex: libvpxFourTokenPartitionIVFHex, checksums: libvpxTokenPartitionChecksums[:]},
		{name: "token-eight", ivfHex: libvpxEightTokenPartitionIVFHex, checksums: libvpxTokenPartitionChecksums[:]},
		{name: "profile1", ivfHex: libvpxProfile1IVFHex, checksums: libvpxProfile1Checksums[:]},
		{name: "profile2", ivfHex: libvpxProfile2IVFHex, checksums: libvpxProfile2Checksums[:]},
		{name: "profile3", ivfHex: libvpxProfile3IVFHex, checksums: libvpxProfile3Checksums[:]},
		{name: "sharpness7", ivfHex: libvpxSharpness7IVFHex, checksums: libvpxSharpness7Checksums[:]},
		{name: "error-resilient", ivfHex: libvpxErrorResilientIVFHex, checksums: libvpxErrorResilientChecksums[:]},
	}
}

func mustDecodeIVFFrames(t testing.TB, ivfHex string, want int) [][]byte {
	t.Helper()
	ivf := mustDecodeHex(t, ivfHex)
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	frames := make([][]byte, 0, want)
	for i := 0; offset < len(ivf); i++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		frames = append(frames, frame.Data)
		offset = next
	}
	if len(frames) != want {
		t.Fatalf("IVF frame count = %d, want %d", len(frames), want)
	}
	return frames
}

func decodeFrames(t testing.TB, d *VP8Decoder, frames [][]byte) {
	t.Helper()
	d.Reset()
	for i := range frames {
		if err := d.Decode(frames[i]); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame frame %d returned no frame", i)
		}
	}
}

func decodeFramesInto(t testing.TB, d *VP8Decoder, frames [][]byte, dst *Image) {
	t.Helper()
	d.Reset()
	for i := range frames {
		if _, err := d.DecodeInto(frames[i], dst); err != nil {
			t.Fatalf("DecodeInto frame %d returned error: %v", i, err)
		}
		if _, ok := d.NextFrame(); ok {
			t.Fatalf("DecodeInto frame %d queued a NextFrame output", i)
		}
	}
}

func assertIVFDecodeIntoMatchesLibvpxChecksums(t *testing.T, ivfHex string, checksums []testutil.FrameChecksum) {
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
	dst := testImage(int(header.Width), int(header.Height))

	for i, want := range checksums {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		info, err := d.DecodeInto(frame.Data, &dst)
		if err != nil {
			t.Fatalf("DecodeInto frame %d returned error: %v", i, err)
		}
		if info.Width != want.Width || info.Height != want.Height || info.KeyFrame != want.KeyFrame || info.ShowFrame != want.ShowFrame {
			t.Fatalf("FrameInfo[%d] = %+v, want %dx%d key=%t show=%t", i, info, want.Width, want.Height, want.KeyFrame, want.ShowFrame)
		}
		if _, ok := d.NextFrame(); ok {
			t.Fatalf("DecodeInto frame %d queued a NextFrame output", i)
		}
		got := checksumFrame(i, want.KeyFrame, want.ShowFrame, dst)
		if !testutil.SameFrameChecksum(got, want) {
			t.Fatalf("DecodeInto frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(want), formatChecksum(got))
		}
		offset = next
	}
	if offset != len(ivf) {
		t.Fatalf("final IVF offset = %d, want %d", offset, len(ivf))
	}
}

func assertIVFTokenPartition(t *testing.T, ivfHex string, want vp8common.TokenPartition) {
	t.Helper()
	ivf := mustDecodeHex(t, ivfHex)
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	var previous vp8dec.QuantHeader
	for i := 0; offset < len(ivf); i++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		_, state, _, err := vp8dec.ParseStateHeaderWithReader(frame.Data, previous)
		if err != nil {
			t.Fatalf("ParseStateHeaderWithReader[%d] returned error: %v", i, err)
		}
		if state.TokenPartition != want {
			t.Fatalf("frame %d token partition = %d, want %d", i, state.TokenPartition, want)
		}
		previous = state.Quant
		offset = next
	}
}

func assertIVFProfile(t *testing.T, ivfHex string, want int) {
	t.Helper()
	ivf := mustDecodeHex(t, ivfHex)
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	for i := 0; offset < len(ivf); i++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo[%d] returned error: %v", i, err)
		}
		if info.Profile != want {
			t.Fatalf("frame %d profile = %d, want %d", i, info.Profile, want)
		}
		offset = next
	}
}

func assertIVFLoopFilterSharpness(t *testing.T, ivfHex string, frameType vp8common.FrameType, want uint8) {
	t.Helper()
	ivf := mustDecodeHex(t, ivfHex)
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	var matched bool
	for i := 0; offset < len(ivf); i++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo[%d] returned error: %v", i, err)
		}
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", i, err)
		}
		if (info.KeyFrame && frameType != vp8common.KeyFrame) || (!info.KeyFrame && frameType != vp8common.InterFrame) {
			offset = next
			continue
		}
		matched = true
		if got := d.state.LoopFilter.SharpnessLevel; got != want {
			t.Fatalf("frame %d sharpness = %d, want %d", i, got, want)
		}
		offset = next
	}
	if !matched {
		t.Fatalf("IVF has no frame type %d to check sharpness %d", frameType, want)
	}
}

func assertIVFHasMacroblockMode(t *testing.T, ivfHex string, frameType vp8common.FrameType, mode vp8common.MBPredictionMode) {
	t.Helper()
	ivf := mustDecodeHex(t, ivfHex)
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	for i := 0; offset < len(ivf); i++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, i)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", i, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo[%d] returned error: %v", i, err)
		}
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", i, err)
		}
		if (info.KeyFrame && frameType != vp8common.KeyFrame) || (!info.KeyFrame && frameType != vp8common.InterFrame) {
			offset = next
			continue
		}
		for j := range d.modes {
			if d.modes[j].Mode == mode {
				return
			}
		}
		offset = next
	}
	t.Fatalf("IVF has no frame type %d macroblock mode %d", frameType, mode)
}

func mustDecodeHex(t testing.TB, s string) []byte {
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

// Generated from govpx encoder output and verified with the libvpx v1.16.0
// checksum oracle through the VP8 test harness.
const govpxBaselineIVFHex = "444b49460000200056503830200010001e0000000100000002000000000000005f00000000000000000000001001009d012a2000100000002800000f0400fef6507ffdfa69ff39ffff26c9725c9724e2c6abb51e9788e49c58d57ffff295ffc6eff765c16ffff99a3ff49bfec37901fe81f697ffbf4d3fe73ff4fd3f4fd3c43cb5ada69e9788796b5b1e00120000000100000000000000d101000000a03100048981818043a46b0000"

var govpxBaselineChecksums = [...]testutil.FrameChecksum{
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

const libvpxEncodedBaselineIVFHex = testutil.LibvpxEncodedSmokeIVFHex

var libvpxEncodedBaselineChecksums = [...]testutil.FrameChecksum{
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

// Generated from govpx encoder output and verified with the libvpx v1.16.0
// checksum oracle through the VP8 test harness. This vector exercises a NEWMV
// interframe.
const govpxNewMVIVFHex = "444b49460000200056503830200010001e0000000100000002000000000000008300000000000000000000001001009d012a2000100000000800000f0400fefe6ebffff80d0bff6281fffe337feb0ffac3feb0ffac3f19fc67f19ffac3f19fc67f19ffac3f19fc67f19fffff1cdbfff1cdbfff1cdbfe39b7fffe65e7fff32f3fff9979ff32f3ff39effff80d0bff6281ff587fd61ff587fd61f8cfe33f8cfe33f8cfe33f8cfe33f8cfe33f8cfdec00120000000100000000000000d101000000203100048981818043a41a0000"

var govpxNewMVChecksums = [...]testutil.FrameChecksum{
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

// Generated from govpx encoder output and verified with the libvpx v1.16.0
// checksum oracle through the VP8 test harness. This vector exercises a subpixel NEWMV
// interframe.
const govpxSubpixelNewMVIVFHex = "444b49460000200056503830100010001e000000010000000200000000000000830100000000000000000000f000009d012a10001000000008000011d0fefa9fa07ffd455c3cf6ffe3b7f929fd3fa1bffffcabfff453b569f4fcdc7fa2ebff533ff9727e973ecbfff601f3bca7ffffc1afa57d807ecc6ff40359cbf37aff3fff68ff277effff7fff75ffffe55fffa29dacdfffc1affea34fb47fd69ffd00d67dfffdffbaffe4dbf5ffdfdebfdff7fff3ff99fd807e4dbfe8058bef5ff7af4df33bf601f33bbff07c7fe7ff4ffb47fcabffd47b17debfef5e9be7ffed1fe7ff7fe0f9ffffc1affea34fb00fd5e9fd47d67dfffdffbaffe55ff68aafdebfdff7affffe55fffa29dacdffc1afa57da3fed08ff51f59cbf37aff33bf601f33bf7fffbfffb6c547e68bfd081ffa99ffcb93f4b9f5d1ffb47f9de57ffff2afffd14ed66fffea34ff9c4bff9adddff319fe61c97fccdbff33bf33bf371fff957ffe8a76a5347e6e3fd175ffa99ffcb93f4b9f65fffb00f9de53ffff06be95f601fb31bfd00d672fcdebfcfffda3fc9dfbfffdfffdd7fffff1cdbfff1cdbfff1cdbfe39b7fffe65e7fff32f3fff9979ff31300750000000100000000000000b101000000203100048981818017fe809bffffacdbffee827fd611c7ff0eb7fff336fffba09ff58471ffc3adfffe8a3bffe5fbaa7fcde9ffe8a3bffe5fbaa7fcde9fffff3633ffed5c7fe6976bff5b0bfffcd8cfffb571ff9a5daffd6c2ffffa6677ffcd00b97fbe51fff4ccefff9a0172ff7c7000"

var govpxSubpixelNewMVChecksums = [...]testutil.FrameChecksum{
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

// Generated from govpx encoder output and verified with the libvpx v1.16.0
// checksum oracle through the VP8 test harness. This vector exercises an intra
// macroblock inside an interframe.
const govpxIntraInterIVFHex = "444b49460000200056503830100010001e000000010000000200000000000000320000000000000000000000f000009d012a1000100000000800000dc0feffbb029ffffe39b7ffe39b7ffe39b7fc736ffffccbcfffe65e7fff32f3fe62602d00000001000000000000009101000000203100048981818000009bffffc736fffc736fffc736ff8e6dffff9979fffccbcfffe65e7fcc4c00"

var govpxIntraInterChecksums = [...]testutil.FrameChecksum{
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

// Generated from govpx encoder output and verified with the libvpx v1.16.0
// checksum oracle through the VP8 test harness. This vector exercises non-DC whole-block
// intra mode selection in a keyframe.
const govpxIntraModeIVFHex = "444b49460000200056503830100020001e0000000100000001000000000000007c00000000000000000000001001009d012a10002000000008000012c080fefcf3ffffeec5dfff233bfffd8b3ffda5fff697ffda5fff697ffb167ffa2cfff459ffed2fff62cfff459ffe8b3ffda5ffec59ffe8b3ffd167ffff1cdbfff1cdbfff1cdbfe39b7fffe65e7fff32f3fff9979ff31dcdadd64ca40c7ed6eb3042a6e9a5a365f1d374b3000"

var govpxIntraModeChecksums = [...]testutil.FrameChecksum{
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

// Generated from govpx encoder output and verified with the libvpx v1.16.0
// checksum oracle through the VP8 test harness. This vector exercises non-DC chroma
// intra mode selection in a keyframe.
const govpxChromaModeIVFHex = "444b49460000200056503830100020001e0000000100000001000000000000008d00000000000000000000001001009d012a100020000000080000137500fefcf3ffffeec5dfff233bfffd8b3ffda5fff697ffda5fff697ffb167ffa2cfff459ffed2fff62cfff459ffe8b3ffda5ffec59ffe8b3ffd167ffff7b23fef8b8e6fffd7bffef8b8e6fffef647fdf171cdfbe5ffbc5c737fffd98de50e45e50fffd98de4f5feb99023ff52d6485011ffb9ab93a809181bb2f8e98a0"

var govpxChromaModeChecksums = [...]testutil.FrameChecksum{
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

// Generated with libvpx v1.16.0 vpxenc --token-parts=1 and verified with the
// libvpx v1.16.0 checksum oracle through the VP8 test harness. This vector exercises
// two independent token partitions across keyframe and interframes.
const libvpxTwoTokenPartitionIVFHex = "444b4946000020005650383020002000e80300000100000003000000000000003a0200000000000000000000100e009d012a2000200000c7088585888584884c820275ba24c1bed7c677ad1de2c907e58bf25f657f5673c0f981fd01fd80f792e88cfe03d603e803e595fb57f051fe77fdf7ea4fb3d5605642b8731069e87bb46645fee3b0d7a107ea4fb61b1e4224dc6f78f88c683f39d895f11e309ea0f5e7f3104bc52000c70000fefd6e60365fae3bfa713577fc000dea7323075ac92eade10d0d7dc353fdbbbaf647041544cc2314bf909ae0119a768b157a57995e7bffa71760d7f3427039eae897ca2fa90d6bffea8ac39d80bcfd9d0ecb5a46dcff125fb29f37f4473f01ba4eaab4acd0fc23f8d223cc91340baf5a1fa957fa783da4209a6b3336d374c9c59992de10d0e5c862223ef33edddd85106f6c89ed8d59fab44b939d5ac2a4e329fa66b05a93499fb807a29b9858f971a673dc0011be375149d28f9747245d3aa856fc55caf79200f34b252c87ab873c609fe5da6f81d539d5fef149f40f63902f22593d16d1e69a02cbb13e35ce3bfc62b1ac9d9fc9bb9f06cb734efb53fde27f02195f90c5ca850b5fd5e66367ac058c9ebe8a7fd7e06025ef2e5e278025bdc269dbdd1215f6eb7524f19a37d00bb2d0f323906953ac228ef32fcdef8f1ff732bb9d9122df97d25897322998752fdcbffd101aad4a01b017da1bf1c00543d9b3ff049f3fca7268ae225e529f5f799c56123d86273b0783e55075ad1f959ec05fc02c069ea69abf0d60197c0029434b126174503ad7350a656881e5b1b357e68636b0bf3e4359ff5e4646152b2c3c435e12852da6a66ebffb6beb287800ff000000210000000000000091050003126400180530d781b723e5358dc5ebfe38a3f871e145ae562d774b2d5630cbb104f95b12d4c8c2e971e4e0610000b000e3fae0fcc3c36544b6cee9b3ee28d68393d22c8e3dadce76d0d864fabaebea2272ac1ed6e9dba2ee385b7ea7522d41b447501ff3f2bcdf5086229f7898a7a5966c79e55ffcfa22ad5c96d4103f0343960315cf04a9e87d0de9fa8dcfc8d5000b55dad20f4acb3613b179fcf807b89feddd969db85e3772b626634666ada1e4a53ec5263ac769d3fcf1805d33a800d9ea512c2d6ee6133e9a2107d6d6d1e60ac77cbbe3e2cfcbab5caa6dcb46d6cdcbae97355d9e471e4a176ec7e9cdea9f6e193bedcb352190c500007200f80000004200000000000000d1020003126400180677e8b0a8afd403d50d68378477cab6006b0000bcc5800f6261ebf6302c13602dcc84b7731ad1a44eef5858c338609c5063b6843819fca539eed25785ba59dd2d1e47774f263d6883f35d085fe1e3f49d0c939b25373d62e21a63f3f58a7db399c90c207089c92c58aaac3379bff87ce7997d7cecc88397975902d82cbe0008f4b120df47b59aa79f595fa6780efcc28d5da6e953675c654b3f649f9926223c770185f30d7224df951cfeda68e9562789524b7b7d49e9a2d3540f94850504c2b65987af1811aa01e5410b29bfe43f8c57b2060e69e00bbf9d23a5982f689610eaf5df6818416bb5d4b17705520d0000"

// Generated with libvpx v1.16.0 vpxenc --token-parts=2 and verified with the
// libvpx v1.16.0 checksum oracle through the VP8 test harness. This vector exercises
// four independent token partitions across keyframe and interframes.
const libvpxFourTokenPartitionIVFHex = "444b4946000020005650383020002000e8030000010000000300000000000000420200000000000000000000100e009d012a2000200000c7088585888584888c820275ba24c1bed7c677ad1de2c907e58bf25f657f5673c0f981fd01fd80f792e88cfe03d603e803e595fb57f051fe77fdf7ea4fb3d5605642b8731069e87bb46645fee3b0d7a107ea4fb61b1e4224dc6f78f88c683f39d895f11e309ea0f5e7f3104bc52000c70000f60000010000fefd6e60365fae3bfa713577fc000dea7323075ac92eade10d0d7dc353fdbbbaf647041544cc2314bf909ae0119a768b157a57995e7bffa71760d7f3427039eae897ca2fa90d6bffea8ac39d80bcfd9d0ecb5a46dcff125fb29f37f4473f01ba4eaab4acd0fc23f8d223cc91340baf5a1fa957fa783da4209a6b3336d374c9c59992de10d0e5c862223ef33edddd85106f6c89ed8d59fab44b939d5ac2a4e329fa66b05a93499fb807a29b9858f971a673dc0011be375149d28f9747245d3aa856fc55caf79200f34b252c87ab873c609fe5da6f81d539d5fef149f40f63902f22593d16d1e69a02cbb13e35ce3bfc62b1ac9d9fc9bb9f06cb734efb53fde27f02195f90c5ca850b5fd5e66367ac058c9ebe8a7fd7e06025ef2e5e278025bdc269dbdd1215f6eb7524f19a37d00bb2d0f323906953ac228ef32fcdef8f1ff732bb9d9122df97d25897322998752fdcbffd101aad4a01b017da1bf1c00543d9b3ff049f3fca7268ae225e529f5f799c56123d86273b0783e55075ad1f959ec05fc02c069ea69abf0d60197c0029434b126174503ad7350a656881e5b1b357e68636b0bf3e4359ff5e4646152b2c3c435e12852da6a66ebffb6beb287800000007010000210000000000000091050003146400180530d781b723e5358dc5ebfe38a3f871e145ae562d774b2d5630cbb104f95b12d4c8c2e971e4e06100006c0000010000b000e3fae0fcc3c36544b6cee9b3ee28d68393d22c8e3dadce76d0d864fabaebea2272ac1ed6e9dba2ee385b7ea7522d41b447501ff3f2bcdf5086229f7898a7a5966c79e55ffcfa22ad5c96d4103f0343960315cf04a9e87d0de9fa8dcfc8d5000b55dad20f4acb3613b179fcf807b89feddd969db85e3772b626634666ada1e4a53ec5263ac769d3fcf1805d33a800d9ea512c2d6ee6133e9a2107d6d6d1e60ac77cbbe3e2cfcbab5caa6dcb46d6cdcbae97355d9e471e4a176ec7e9cdea9f6e193bedcb352190c5000072000000000100004200000000000000d1020003146400180677e8b0a8afd403d50d68378477cab6006b0000710000010000bcc5800f6261ebf6302c13602dcc84b7731ad1a44eef5858c338609c5063b6843819fca539eed25785ba59dd2d1e47774f263d6883f35d085fe1e3f49d0c939b25373d62e21a63f3f58a7db399c90c207089c92c58aaac3379bff87ce7997d7cecc88397975902d82cbe0008f4b120df47b59aa79f595fa6780efcc28d5da6e953675c654b3f649f9926223c770185f30d7224df951cfeda68e9562789524b7b7d49e9a2d3540f94850504c2b65987af1811aa01e5410b29bfe43f8c57b2060e69e00bbf9d23a5982f689610eaf5df6818416bb5d4b17705520d00000000"

// Generated with libvpx v1.16.0 vpxenc --token-parts=3 and verified with the
// libvpx v1.16.0 checksum oracle through the VP8 test harness. This vector exercises
// eight independent token partitions across keyframe and interframes.
const libvpxEightTokenPartitionIVFHex = "444b4946000020005650383020002000e8030000010000000300000000000000520200000000000000000000100e009d012a2000200000c708858588858488cc820275ba24c1bed7c677ad1de2c907e58bf25f657f5673c0f981fd01fd80f792e88cfe03d603e803e595fb57f051fe77fdf7ea4fb3d5605642b8731069e87bb46645fee3b0d7a107ea4fb61b1e4224dc6f78f88c683f39d895f11e309ea0f5e7f3104bc52000c70000f60000010000010000010000010000010000fefd6e60365fae3bfa713577fc000dea7323075ac92eade10d0d7dc353fdbbbaf647041544cc2314bf909ae0119a768b157a57995e7bffa71760d7f3427039eae897ca2fa90d6bffea8ac39d80bcfd9d0ecb5a46dcff125fb29f37f4473f01ba4eaab4acd0fc23f8d223cc91340baf5a1fa957fa783da4209a6b3336d374c9c59992de10d0e5c862223ef33edddd85106f6c89ed8d59fab44b939d5ac2a4e329fa66b05a93499fb807a29b9858f971a673dc0011be375149d28f9747245d3aa856fc55caf79200f34b252c87ab873c609fe5da6f81d539d5fef149f40f63902f22593d16d1e69a02cbb13e35ce3bfc62b1ac9d9fc9bb9f06cb734efb53fde27f02195f90c5ca850b5fd5e66367ac058c9ebe8a7fd7e06025ef2e5e278025bdc269dbdd1215f6eb7524f19a37d00bb2d0f323906953ac228ef32fcdef8f1ff732bb9d9122df97d25897322998752fdcbffd101aad4a01b017da1bf1c00543d9b3ff049f3fca7268ae225e529f5f799c56123d86273b0783e55075ad1f959ec05fc02c069ea69abf0d60197c0029434b126174503ad7350a656881e5b1b357e68636b0bf3e4359ff5e4646152b2c3c435e12852da6a66ebffb6beb28780000000000000017010000210000000000000091050003166400180530d781b723e5358dc5ebfe38a3f871e145ae562d774b2d5630cbb104f95b12d4c8c2e971e4e06100006c0000010000010000010000010000010000b000e3fae0fcc3c36544b6cee9b3ee28d68393d22c8e3dadce76d0d864fabaebea2272ac1ed6e9dba2ee385b7ea7522d41b447501ff3f2bcdf5086229f7898a7a5966c79e55ffcfa22ad5c96d4103f0343960315cf04a9e87d0de9fa8dcfc8d5000b55dad20f4acb3613b179fcf807b89feddd969db85e3772b626634666ada1e4a53ec5263ac769d3fcf1805d33a800d9ea512c2d6ee6133e9a2107d6d6d1e60ac77cbbe3e2cfcbab5caa6dcb46d6cdcbae97355d9e471e4a176ec7e9cdea9f6e193bedcb352190c500007200000000000000100100004200000000000000d1020003166400180677e8b0a8afd403d50d68378477cab6006b0000710000010000010000010000010000010000bcc5800f6261ebf6302c13602dcc84b7731ad1a44eef5858c338609c5063b6843819fca539eed25785ba59dd2d1e47774f263d6883f35d085fe1e3f49d0c939b25373d62e21a63f3f58a7db399c90c207089c92c58aaac3379bff87ce7997d7cecc88397975902d82cbe0008f4b120df47b59aa79f595fa6780efcc28d5da6e953675c654b3f649f9926223c770185f30d7224df951cfeda68e9562789524b7b7d49e9a2d3540f94850504c2b65987af1811aa01e5410b29bfe43f8c57b2060e69e00bbf9d23a5982f689610eaf5df6818416bb5d4b17705520d0000000000000000"

var libvpxTokenPartitionChecksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     32,
		Height:    32,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: checksumMD5(
			"bb79ef3d12ed9575939fe4cc88a7d9ac",
			"94490fdfb02e3d91c37abcb5eb009891",
			"9016fe38506a5e2a8d4180f9302a3b92",
			"eea937bf8fd1c4398130e3be1b0071e6",
		),
	},
	{
		Index:     1,
		Width:     32,
		Height:    32,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: checksumMD5(
			"8a5ed431495c70dbfd7f1be30c161605",
			"bd06190306cc2c06bd453f7ee1fe0489",
			"14f22b6bee5f47fd2d3e36df281e018b",
			"cf326f763bdc6f7b18bfdf60f52d75db",
		),
	},
	{
		Index:     2,
		Width:     32,
		Height:    32,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: checksumMD5(
			"a522fcff14fe5faeb9e5b8632cc239e1",
			"64c4cbf0f983644a000f6b12f304865d",
			"f02a75aed0d945c31677c4d8c0f4990d",
			"fc4719b039d181cdab1026c6c62f0ac8",
		),
	},
}

// Generated with libvpx v1.16.0 vpxenc --profile=1 and verified with the
// libvpx v1.16.0 checksum oracle through the VP8 test harness.
const libvpxProfile1IVFHex = "444b4946000020005650383010001000e8030000010000000100000000000000ce00000000000000000000001204009d012a100010001207088585888584880c8202755400e815feb3551b7ea6d701364311b82d3560fef8705c0b77f027ad9b2f323e480c39d3e537d4dc61e777991a498ff841877fa01e966d1394ac83c5d06d2e772f084f2fed6abf3ffef699a2c633c7b4becb930acb2c46148d316f583f8567393ff142c40ae889bdb01f2d654498ab36547b115b49f5bfc5e7569a7fa5390c3155c5f1bfd87f90424fee15afc84828fbce3e639fddd1c478510023ffd833ff8b1f6eeead7fe6c95086f15eb0f974bba1a1b97ed7539400"

var libvpxProfile1Checksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     16,
		Height:    16,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: checksumMD5(
			"86f4fa6bbcb473d5b9f760f18f45fc62",
			"c2f94143306052e11f5943ce20a1cb0d",
			"ad094a02c4633e1280ca37fea5702871",
			"626219ba7cbef22e7e20090bd8781595",
		),
	},
}

// Generated with libvpx v1.16.0 vpxenc --profile=2 and verified with the
// libvpx v1.16.0 checksum oracle through the VP8 test harness.
const libvpxProfile2IVFHex = "444b4946000020005650383010001000e8030000010000000100000000000000ce00000000000000000000001404009d012a100010000007088585888584880c8202755400e815feb3551b7ea6d701364311b82d3560fef8705c0b77f027ad9b2f323e480c39d3e537d4dc61e777991a498ff841877fa01e966d1394ac83c5d06d2e772f084f2fed6abf3ffef699a2c633c7b4becb930acb2c46148d316f583f8567393ff142c40ae889bdb01f2d654498ab36547b115b49f5bfc5e7569a7fa5390c3155c5f1bfd87f90424fee15afc84828fbce3e639fddd1c478510023ffd833ff8b1f6eeead7fe6c95086f15eb0f974bba1a1b97ed7539400"

var libvpxProfile2Checksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     16,
		Height:    16,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: checksumMD5(
			"5926ffee33058455d8a0de656fc8ba77",
			"c2f94143306052e11f5943ce20a1cb0d",
			"ad094a02c4633e1280ca37fea5702871",
			"c9edf89b38c6d6213d12b33ab343ea7b",
		),
	},
}

// Generated with libvpx v1.16.0 vpxenc --profile=3 and verified with the
// libvpx v1.16.0 checksum oracle through the VP8 test harness.
const libvpxProfile3IVFHex = "444b4946000020005650383010001000e8030000010000000100000000000000ce00000000000000000000001604009d012a100010001007088585888584880c8202755400e815feb3551b7ea6d701364311b82d3560fef8705c0b77f027ad9b2f323e480c39d3e537d4dc61e777991a498ff841877fa01e966d1394ac83c5d06d2e772f084f2fed6abf3ffef699a2c633c7b4becb930acb2c46148d316f583f8567393ff142c40ae889bdb01f2d654498ab36547b115b49f5bfc5e7569a7fa5390c3155c5f1bfd87f90424fee15afc84828fbce3e639fddd1c478510023ffd833ff8b1f6eeead7fe6c95086f15eb0f974bba1a1b97ed7539400"

var libvpxProfile3Checksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     16,
		Height:    16,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: checksumMD5(
			"5926ffee33058455d8a0de656fc8ba77",
			"c2f94143306052e11f5943ce20a1cb0d",
			"ad094a02c4633e1280ca37fea5702871",
			"c9edf89b38c6d6213d12b33ab343ea7b",
		),
	},
}

// Generated with libvpx v1.16.0 vpxenc --sharpness=7 and verified with the
// libvpx v1.16.0 checksum oracle through the VP8 test harness.
const libvpxSharpness7IVFHex = "444b4946000020005650383020002000e8030000010000000300000000000000360200000000000000000000100e009d012a2000200000c7088585888584880c820275ba24c1bed7c677ad1de2c907e58bf25f657f5673c0f981fd01fd80f792e88cfe03d603e803e595fb57f051fe77fdf7ea4fb3d5605642b8731069e87bb46645fee3b0d7a107ea4fb61b1e4224dc6f78f88c683f39d895f11e309ea0f5e7f3104bc52000fefd6e60365fae3bfa713577fc000dea7323075ac92eade10d0d7dc353fdbbbaf647041544cc2314bf909ae0119a768b157a57995e7bffa71760d7f3427039eae897ca2fa90d6bffea8ac39d80bcfd9d0ecb5a46dcff125fb29f37f4473f01ba4eaab4acd0fc23f8d223cc91340baf5a1fa957fa783da4209a6b3336d374c9c59992de10d0e5c862223ef33edddd85106f6c89ed8d59fab44b939d5ac2a4e329fa66b05a93499fb807a29b9858f971a673dc0011be375149d28f9747245d3aa856fc55caf79331557ba45920c0daea7c90a9a8e0bebf33844660a9faca7b933ae126a219f6bd8fd989bd50b74bf8c563593b3f93773e0d96e69df6a7fbc4fe0432bf218b950a16bfabccc6cf580b193d7d14ffafc0c04bde5cbc4f004b7b84d3b7ba242bedd6ea49e3346fa01765a1e64720d2a758451de65f9bdf1e3fee65773b2245bf2fa4b12e645330ea5fb97ffa20355a9403602fb437e3800a87b367fe093e7f94e4d15c44bca53ebef338ac247b0c4e760f07caa0eb5a3f2b3d80bf80580d3d4d357e1ac032f80052869624c2e8a075ae6a14cad103cb6366afcd0c6d617e7c86b3febc8c8c2a56587886bc250a5b4d4cdd7ff6d7d650f000fb000000210000000000000091050003f06400180530d781b723e5358dc5ebfe38a3f871e145ae562d774b2d5630cbb104f95b12d4c8c2e971e4e0b000e3fae0fcc3c36544b6cee9b3ee28d68393d22c8e3dadce76d0d864fabaebea2272ac1ed6e9dba2ee385b7ea7522d41b447501ff3f2bcdf5086229f7898a7a5966c79e55ffcfa22ad5c96d4103f0343960315cf04a9e87d0de9fa8dcfc8d5643226296a5659b09d8bcfe7c03dc4ff6eecb4edc2f1bb95b1331a33356d0f2529f62931d63b4e9fe78c02e99d4006cf5289616b773099f4d1083eb6b68f30563be5df1f167e5d5ae5536e5a36b66e5d74b9aaecf238f250bb763f4e6f54fb70c9df6e59a90c862800039000f20000004200000000000000d1020003f06400180677e8b0a8afd403d50d68378477cab600bcc5800f6261ebf6302c13602dcc84b7731ad1a44eef5858c338609c5063b6843819fca539eed25785ba59dd2d1e47774f263d6883f35d085fe1e3f49d0c939b25373d62e21a63f3f58a7db399c90c207089c92c58aaac3379bff87ce7997d7cecc88397975902d82cc14df898e6c7e3efde5fc89fd33c076d3c9a98722dd58aa6b096e17439926223c770185f30d7224df951cfeda68e9562789524b7b7d49e9a2d3540f94850504c2b65987af1811aa01e5410b29bfe43f8c57b2060e69e00bbf9d23a5982f689610eaf5df6818416bb5d4b17705520d000"

var libvpxSharpness7Checksums = [...]testutil.FrameChecksum{
	{
		Index:     0,
		Width:     32,
		Height:    32,
		KeyFrame:  true,
		ShowFrame: true,
		MD5: checksumMD5(
			"bb79ef3d12ed9575939fe4cc88a7d9ac",
			"94490fdfb02e3d91c37abcb5eb009891",
			"9016fe38506a5e2a8d4180f9302a3b92",
			"eea937bf8fd1c4398130e3be1b0071e6",
		),
	},
	{
		Index:     1,
		Width:     32,
		Height:    32,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: checksumMD5(
			"164fae1ee199b1e445e92c0bebee3f58",
			"4c757e72901142a18056553dca2edb44",
			"14f22b6bee5f47fd2d3e36df281e018b",
			"2af1619374cf372bae0fc34d3f38ac0e",
		),
	},
	{
		Index:     2,
		Width:     32,
		Height:    32,
		KeyFrame:  false,
		ShowFrame: true,
		MD5: checksumMD5(
			"2ae01d43db44f3c8a369a1530431f0f6",
			"e666c4f52e29330b07df6ab7f9d0b807",
			"f02a75aed0d945c31677c4d8c0f4990d",
			"51761ea93c0865930cdbac4cdd8f69ed",
		),
	},
}
