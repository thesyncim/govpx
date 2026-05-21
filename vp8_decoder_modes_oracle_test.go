//go:build govpx_oracle_trace

package govpx

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestVP8OracleVpxdecDecodesEncodeIntoKeyFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle smoke tests")
	}
	vpxdec := coracletest.Vpxdec(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	packet := make([]byte, 4096)
	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	ivf := makeSingleFrameIVF(16, 16, 30, 1, result.Data)
	diag, err := coracle.VpxdecVP8SummaryIVF(ivf, coracle.VpxdecVP8Config{BinaryPath: vpxdec})
	if err != nil {
		t.Fatalf("vpxdec failed: %v\n%s", err, diag)
	}
}

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoKeyFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := coracletest.ChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(32, 16)
	fillImage(src, 220, 90, 170)
	for row := 0; row < src.Height; row++ {
		for col := 16; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = 40
		}
	}
	packet := make([]byte, 8192)
	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	ivf := makeSingleFrameIVF(32, 16, 30, 1, result.Data)
	oracleFrames := coracletest.RunVP8ChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 1 {
		t.Fatalf("oracle frame count = %d, want 1", len(oracleFrames))
	}

	decoded := decodeSingleFrame(t, result.Data)
	govpxFrame := checksumFrame(0, true, true, decoded)
	if !testutil.SameFrameChecksum(oracleFrames[0], govpxFrame) {
		t.Fatalf("checksum mismatch\nlibvpx:  %s\ngovpx: %s", formatChecksum(oracleFrames[0]), formatChecksum(govpxFrame))
	}
}

func TestVP8OracleLibvpxExtendedDecodeModesAvailable(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle capability tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedBaselineIVFHex)

	normal := coracletest.RunVP8ChecksumOracle(t, oracle, ivf)
	postproc := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-postproc", ivf)
	if len(postproc) != len(normal) {
		t.Fatalf("postprocess oracle frame count = %d, want %d", len(postproc), len(normal))
	}
	for i := range normal {
		if postproc[i].Index != normal[i].Index || postproc[i].Width != normal[i].Width || postproc[i].Height != normal[i].Height || postproc[i].KeyFrame != normal[i].KeyFrame || postproc[i].ShowFrame != normal[i].ShowFrame {
			t.Fatalf("postprocess oracle metadata[%d] = %+v, want %+v", i, postproc[i], normal[i])
		}
	}

	concealment := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-error-concealment", ivf)
	assertFrameChecksumsEqual(t, "error-concealment clean decode", concealment, normal)
}

func TestVP8OracleLibvpxErrorConcealmentClampsUnusedMalformedTokenPartition(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	key := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	first := vp8InterFirstPartitionLastZeroMVWithTokenPartition(vp8common.TwoPartition, true)
	inter := vp8InterFramePacketWithTokenPartitions(first, 10, []byte{0})
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key, inter})

	want := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-error-concealment", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{ErrorConcealment: true})
	assertFrameChecksumsEqual(t, "error-concealment malformed unused token partition", got, want)
}

func TestVP8OracleLibvpxErrorConcealmentRejectsInitialTruncatedInterFrameTag(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	key := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	truncatedInter := []byte{0x11, 0}
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key, truncatedInter})

	if err := coracletest.RunVP8ChecksumOracleModeExpectError(t, oracle, "decode-error-concealment", ivf); err == nil {
		t.Fatalf("libvpx error-concealment oracle accepted initial truncated inter frame tag, want error")
	}
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.Decode(truncatedInter); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("truncated inter Decode error = %v, want ErrInvalidData", err)
	}
}

func TestVP8OracleLibvpxErrorConcealmentRejectsTruncatedKeyFrameHeader(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	key := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	truncatedKey := vp8KeyFramePacket(16, 16, 0, 0, true)[:6]
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key, truncatedKey})

	if err := coracletest.RunVP8ChecksumOracleModeExpectError(t, oracle, "decode-error-concealment", ivf); err == nil {
		t.Fatalf("libvpx error-concealment oracle accepted truncated keyframe header, want error")
	}
	if err := decodeIVFExpectError(t, ivf, DecoderOptions{ErrorConcealment: true}); err == nil {
		t.Fatalf("govpx accepted truncated keyframe header, want error")
	}
}

func TestVP8OracleLibvpxErrorConcealmentConcealsMissingTokenPartition(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	frames := mustDecodeIVFFrames(t, govpxNewMVIVFHex, 2)
	truncatedInter := frames[1][:17]
	ivf := makeIVF(32, 16, 30, 1, [][]byte{frames[0], frames[1], truncatedInter})

	want := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-error-concealment", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{ErrorConcealment: true})
	assertFrameChecksumsEqual(t, "active error-concealment missing token partition", got, want)
	if len(got) != 3 {
		t.Fatalf("concealed frame count = %d, want 3", len(got))
	}
	if got[2].MD5 == got[1].MD5 {
		t.Fatalf("concealed frame copied LAST exactly, want libvpx prediction reconstruction")
	}
}

func TestVP8OracleLibvpxKeyFrameResolutionChange(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle resolution-change tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{
		vp8KeyFramePacketWithPayload(16, 16, 200, 0, true),
		vp8KeyFramePacketWithPayload(32, 16, 200, 0, true),
	})

	want := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{})
	assertFrameChecksumsEqual(t, "keyframe resolution change", got, want)
	if len(got) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(got))
	}
	if got[1].Width != 32 || got[1].Height != 16 {
		t.Fatalf("resolution-change frame = %dx%d, want 32x16", got[1].Width, got[1].Height)
	}
}

func TestVP8OracleLibvpxPostProcessMatchesDecoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedBaselineIVFHex)

	want := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-postproc", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	assertFrameChecksumsEqual(t, "postprocess Decode", got, want)
	gotFlags := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	assertFrameChecksumsEqual(t, "postprocess flags Decode", gotFlags, want)
}

func TestVP8OracleLibvpxPostProcessMatchesProfile3Decoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxProfile3IVFHex)

	want := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-postproc", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	assertFrameChecksumsEqual(t, "profile3 postprocess Decode", got, want)
}

func TestVP8OracleLibvpxPostProcessNoiseMatchesDecoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedBaselineIVFHex)

	want := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-postproc-noise", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcessFlags: PostProcessAddNoise, PostProcessNoiseLevel: 4})
	assertFrameChecksumsEqual(t, "postprocess addnoise Decode", got, want)
}

func TestVP8OracleLibvpxPostProcessAllNoiseMatchesDecoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedBaselineIVFHex)

	want := coracletest.RunVP8ChecksumOracleMode(t, oracle, "decode-postproc-all-noise", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise | PostProcessMFQE, PostProcessNoiseLevel: 4})
	assertFrameChecksumsEqual(t, "legacy postprocess addnoise Decode", got, want)
}

func TestVP8OracleLibvpxChecksumMatchesDefaultVersionKeyFrames(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := coracletest.ChecksumOracle(t)
	for _, version := range []int{4, 5, 6, 7} {
		t.Run(fmt.Sprintf("version%d", version), func(t *testing.T) {
			packet := vp8KeyFramePacketWithPayload(16, 16, 200, version, true)
			ivf := makeSingleFrameIVF(16, 16, 30, 1, packet)

			want := coracletest.RunVP8ChecksumOracle(t, oracle, ivf)
			got := decodeIVFChecksums(t, ivf)
			assertFrameChecksumsEqual(t, "default version keyframe Decode", got, want)
		})
	}
}

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoBPredKeyFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := coracletest.ChecksumOracle(t)

	e := newSizedTestEncoder(t, 16, 32)
	src := rateControlTestFrame(16, 32, 0)

	packet := make([]byte, 8192)
	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if e.keyFrameModes[1].YMode != vp8common.BPred || e.keyFrameModes[1].UVMode != vp8common.VPred {
		t.Fatalf("key mode[1] = %+v, want B_PRED/V_PRED", e.keyFrameModes[1])
	}

	ivf := makeSingleFrameIVF(16, 32, 30, 1, result.Data)
	oracleFrames := coracletest.RunVP8ChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 1 {
		t.Fatalf("oracle frame count = %d, want 1", len(oracleFrames))
	}
	decoded := decodeSingleFrame(t, result.Data)
	want := checksumFrame(0, true, true, decoded)
	if !testutil.SameFrameChecksum(oracleFrames[0], want) {
		t.Fatalf("checksum mismatch\nlibvpx:  %s\ngovpx: %s", formatChecksum(oracleFrames[0]), formatChecksum(want))
	}
}
