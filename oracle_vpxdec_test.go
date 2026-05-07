package govpx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestOracleVpxdecDecodesEncodeIntoKeyFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle smoke tests")
	}
	vpxdec := os.Getenv("GOVPX_VPXDEC")
	if vpxdec == "" {
		path, err := exec.LookPath("vpxdec")
		if err != nil {
			t.Skip("vpxdec not found; set GOVPX_VPXDEC to a libvpx v1.16.0 vpxdec binary")
		}
		vpxdec = path
	}

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
	path := filepath.Join(t.TempDir(), "govpx-keyframe.ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cmd := exec.Command(vpxdec, "--codec=vp8", "--noblit", "--summary", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxdec failed: %v\n%s", err, out)
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoKeyFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

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
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 1 {
		t.Fatalf("oracle frame count = %d, want 1", len(oracleFrames))
	}

	decoded := decodeSingleFrame(t, result.Data)
	govpxFrame := checksumFrame(0, true, true, decoded)
	if !testutil.SameFrameChecksum(oracleFrames[0], govpxFrame) {
		t.Fatalf("checksum mismatch\nlibvpx:  %s\ngovpx: %s", formatChecksum(oracleFrames[0]), formatChecksum(govpxFrame))
	}
}

func TestOracleLibvpxExtendedDecodeModesAvailable(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle capability tests")
	}
	oracle := findChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedSmokeIVFHex)

	normal := runLibvpxChecksumOracle(t, oracle, ivf)
	postproc := runLibvpxChecksumOracleMode(t, oracle, "decode-postproc", ivf)
	if len(postproc) != len(normal) {
		t.Fatalf("postprocess oracle frame count = %d, want %d", len(postproc), len(normal))
	}
	for i := range normal {
		if postproc[i].Index != normal[i].Index || postproc[i].Width != normal[i].Width || postproc[i].Height != normal[i].Height || postproc[i].KeyFrame != normal[i].KeyFrame || postproc[i].ShowFrame != normal[i].ShowFrame {
			t.Fatalf("postprocess oracle metadata[%d] = %+v, want %+v", i, postproc[i], normal[i])
		}
	}

	concealment := runLibvpxChecksumOracleMode(t, oracle, "decode-error-concealment", ivf)
	assertFrameChecksumsEqual(t, "error-concealment clean decode", concealment, normal)
}

func TestOracleLibvpxErrorConcealmentClampsUnusedMalformedTokenPartition(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := findChecksumOracle(t)
	key := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	first := vp8InterFirstPartitionLastZeroMVWithTokenPartition(vp8common.TwoPartition, true)
	inter := vp8InterFramePacketWithTokenPartitions(first, 10, []byte{0})
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key, inter})

	want := runLibvpxChecksumOracleMode(t, oracle, "decode-error-concealment", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{ErrorConcealment: true})
	assertFrameChecksumsEqual(t, "error-concealment malformed unused token partition", got, want)
}

func TestOracleLibvpxErrorConcealmentRejectsInitialTruncatedInterFrameTag(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := findChecksumOracle(t)
	key := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	truncatedInter := []byte{0x11, 0}
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key, truncatedInter})

	if err := runLibvpxChecksumOracleModeExpectError(t, oracle, "decode-error-concealment", ivf); err == nil {
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

func TestOracleLibvpxErrorConcealmentRejectsTruncatedKeyFrameHeader(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := findChecksumOracle(t)
	key := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	truncatedKey := vp8KeyFramePacket(16, 16, 0, 0, true)[:6]
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key, truncatedKey})

	if err := runLibvpxChecksumOracleModeExpectError(t, oracle, "decode-error-concealment", ivf); err == nil {
		t.Fatalf("libvpx error-concealment oracle accepted truncated keyframe header, want error")
	}
	if err := decodeIVFExpectError(t, ivf, DecoderOptions{ErrorConcealment: true}); err == nil {
		t.Fatalf("govpx accepted truncated keyframe header, want error")
	}
}

func TestOracleLibvpxErrorConcealmentConcealsMissingTokenPartition(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle error-concealment tests")
	}
	oracle := findChecksumOracle(t)
	frames := mustDecodeSmokeIVFFrames(t, govpxNewMVIVFHex, 2)
	truncatedInter := frames[1][:17]
	ivf := makeIVF(32, 16, 30, 1, [][]byte{frames[0], frames[1], truncatedInter})

	want := runLibvpxChecksumOracleMode(t, oracle, "decode-error-concealment", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{ErrorConcealment: true})
	assertFrameChecksumsEqual(t, "active error-concealment missing token partition", got, want)
	if len(got) != 3 {
		t.Fatalf("concealed frame count = %d, want 3", len(got))
	}
	if got[2].MD5 == got[1].MD5 {
		t.Fatalf("concealed frame copied LAST exactly, want libvpx prediction reconstruction")
	}
}

func TestOracleLibvpxKeyFrameResolutionChange(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle resolution-change tests")
	}
	oracle := findChecksumOracle(t)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{
		vp8KeyFramePacketWithPayload(16, 16, 200, 0, true),
		vp8KeyFramePacketWithPayload(32, 16, 200, 0, true),
	})

	want := runLibvpxChecksumOracleMode(t, oracle, "decode", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{})
	assertFrameChecksumsEqual(t, "keyframe resolution change", got, want)
	if len(got) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(got))
	}
	if got[1].Width != 32 || got[1].Height != 16 {
		t.Fatalf("resolution-change frame = %dx%d, want 32x16", got[1].Width, got[1].Height)
	}
}

func TestOracleLibvpxPostProcessMatchesDecoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := findChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedSmokeIVFHex)

	want := runLibvpxChecksumOracleMode(t, oracle, "decode-postproc", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcess: true})
	assertFrameChecksumsEqual(t, "postprocess Decode", got, want)
	gotFlags := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	assertFrameChecksumsEqual(t, "postprocess flags Decode", gotFlags, want)
}

func TestOracleLibvpxPostProcessMatchesProfile3Decoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := findChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxProfile3IVFHex)

	want := runLibvpxChecksumOracleMode(t, oracle, "decode-postproc", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcess: true})
	assertFrameChecksumsEqual(t, "profile3 postprocess Decode", got, want)
}

func TestOracleLibvpxPostProcessNoiseMatchesDecoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := findChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedSmokeIVFHex)

	want := runLibvpxChecksumOracleMode(t, oracle, "decode-postproc-noise", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcessFlags: PostProcessAddNoise, PostProcessNoiseLevel: 4})
	assertFrameChecksumsEqual(t, "postprocess addnoise Decode", got, want)
}

func TestOracleLibvpxPostProcessLegacyNoiseMatchesDecoder(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle postprocess tests")
	}
	oracle := findChecksumOracle(t)
	ivf := mustDecodeHex(t, libvpxEncodedSmokeIVFHex)

	want := runLibvpxChecksumOracleMode(t, oracle, "decode-postproc-all-noise", ivf)
	got := decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{PostProcess: true, PostProcessNoiseLevel: 4})
	assertFrameChecksumsEqual(t, "legacy postprocess addnoise Decode", got, want)
}

func TestOracleLibvpxChecksumMatchesDefaultVersionKeyFrames(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)
	for _, version := range []int{4, 5, 6, 7} {
		t.Run(fmt.Sprintf("version%d", version), func(t *testing.T) {
			packet := vp8KeyFramePacketWithPayload(16, 16, 200, version, true)
			ivf := makeSingleFrameIVF(16, 16, 30, 1, packet)

			want := runLibvpxChecksumOracle(t, oracle, ivf)
			got := decodeIVFChecksums(t, ivf)
			assertFrameChecksumsEqual(t, "default version keyframe Decode", got, want)
		})
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoBPredKeyFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

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
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 1 {
		t.Fatalf("oracle frame count = %d, want 1", len(oracleFrames))
	}
	decoded := decodeSingleFrame(t, result.Data)
	want := checksumFrame(0, true, true, decoded)
	if !testutil.SameFrameChecksum(oracleFrames[0], want) {
		t.Fatalf("checksum mismatch\nlibvpx:  %s\ngovpx: %s", formatChecksum(oracleFrames[0]), formatChecksum(want))
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 2 {
		t.Fatalf("oracle frame count = %d, want 2", len(oracleFrames))
	}
	want := []testutil.FrameChecksum{
		checksumFrame(0, true, true, reconstructed),
		checksumFrame(1, false, true, reconstructed),
	}
	for i := range want {
		if !testutil.SameFrameChecksum(oracleFrames[i], want[i]) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want[i]))
		}
	}
}

func TestOracleLibvpxChecksumMatchesTemporalBaseLayer(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	for _, tc := range temporalOracleTestCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := TemporalScalabilityConfig{
				Enabled:                true,
				Mode:                   tc.mode,
				LayerTargetBitrateKbps: tc.bitrates,
			}
			e := newTemporalTestEncoder(t, cfg)
			packet := make([]byte, 8192)
			basePackets := make([][]byte, 0, tc.frameCount)
			baseKeyFrames := make([]bool, 0, tc.frameCount)
			for i := 0; i < tc.frameCount; i++ {
				src := rateControlTestFrame(16, 16, i)
				result, err := e.EncodeInto(packet, src, uint64(i), 1, 0)
				if err != nil {
					t.Fatalf("EncodeInto %d returned error: %v", i, err)
				}
				if result.Dropped {
					t.Fatalf("EncodeInto %d dropped, want full temporal oracle sequence", i)
				}
				if result.TemporalLayerID == 0 {
					basePackets = append(basePackets, append([]byte(nil), result.Data...))
					baseKeyFrames = append(baseKeyFrames, result.KeyFrame)
				}
			}
			if len(basePackets) < 2 {
				t.Fatalf("base packet count = %d, want at least 2", len(basePackets))
			}

			govpxFrames := decodeFrameSequence(t, basePackets...)
			ivf := makeIVF(16, 16, 30, 1, basePackets)
			oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
			if len(oracleFrames) != len(govpxFrames) {
				t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
			}
			for i := range govpxFrames {
				want := checksumFrame(i, baseKeyFrames[i], true, govpxFrames[i])
				if !testutil.SameFrameChecksum(oracleFrames[i], want) {
					t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
				}
			}
		})
	}
}

func TestOracleLibvpxChecksumMatchesTemporalFullSequence(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	for _, tc := range temporalOracleTestCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := TemporalScalabilityConfig{
				Enabled:                true,
				Mode:                   tc.mode,
				LayerTargetBitrateKbps: tc.bitrates,
			}
			e := newTemporalTestEncoder(t, cfg)
			packet := make([]byte, 8192)
			packets := make([][]byte, 0, tc.frameCount)
			keyFrames := make([]bool, 0, tc.frameCount)
			for i := 0; i < tc.frameCount; i++ {
				src := rateControlTestFrame(16, 16, i)
				result, err := e.EncodeInto(packet, src, uint64(i), 1, 0)
				if err != nil {
					t.Fatalf("EncodeInto %d returned error: %v", i, err)
				}
				if result.Dropped {
					t.Fatalf("EncodeInto %d dropped, want full temporal oracle sequence", i)
				}
				packets = append(packets, append([]byte(nil), result.Data...))
				keyFrames = append(keyFrames, result.KeyFrame)
			}

			govpxFrames := decodeFrameSequence(t, packets...)
			ivf := makeIVF(16, 16, 30, 1, packets)
			oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
			if len(oracleFrames) != len(govpxFrames) {
				t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
			}
			for i := range govpxFrames {
				want := checksumFrame(i, keyFrames[i], true, govpxFrames[i])
				if !testutil.SameFrameChecksum(oracleFrames[i], want) {
					t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
				}
			}
		})
	}
}

func TestOracleLibvpxTemporalSVCExampleStreams(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx temporal SVC oracle tests")
	}
	oracle := findChecksumOracle(t)
	svcEncoder := findVpxTemporalSVCEncoder(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		frameCount = 8
	)
	sources := make([]Image, frameCount)
	for i := range sources {
		sources[i] = rateControlTestFrame(width, height, i)
	}
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "temporal-svc.yuv")
	writeEncoderValidationI420(t, yuvPath, sources)
	outputBase := filepath.Join(dir, "temporal-svc")
	cmd := exec.Command(svcEncoder,
		yuvPath, outputBase, "vp8",
		strconv.Itoa(width), strconv.Itoa(height),
		"1", strconv.Itoa(fps),
		"8", "0", "1", "1", "1",
		"720", "1200",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpx_temporal_svc_encoder failed: %v\n%s", err, out)
	}
	stats := parseTemporalSVCExampleLayerStats(t, string(out), 2)
	assertGovpxTemporalAccountingMatchesLibvpxExample(t, sources, stats)

	for layer := 0; layer < 2; layer++ {
		ivfPath := fmt.Sprintf("%s_%d.ivf", outputBase, layer)
		ivf, err := os.ReadFile(ivfPath)
		if err != nil {
			t.Fatalf("ReadFile %s returned error: %v", ivfPath, err)
		}
		govpxChecksums := decodeIVFChecksums(t, ivf)
		libvpxChecksums := runLibvpxChecksumOracle(t, oracle, ivf)
		assertFrameChecksumsEqual(t, fmt.Sprintf("libvpx temporal SVC layer %d decoded by govpx", layer), govpxChecksums, libvpxChecksums)
		if layer == 0 && len(govpxChecksums) != frameCount/2 {
			t.Fatalf("base layer checksum count = %d, want %d", len(govpxChecksums), frameCount/2)
		}
		if layer == 1 && len(govpxChecksums) != frameCount {
			t.Fatalf("full layer checksum count = %d, want %d", len(govpxChecksums), frameCount)
		}
	}
}

type temporalSVCExampleLayerStats struct {
	InputFrames         int
	EncodedFrames       int
	TargetFrameSizeBits float64
	DroppedPct          float64
}

func parseTemporalSVCExampleLayerStats(t *testing.T, output string, layers int) []temporalSVCExampleLayerStats {
	t.Helper()
	stats := make([]temporalSVCExampleLayerStats, layers)
	seen := make([]bool, layers)
	currentLayer := -1
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		var layer int
		if _, err := fmt.Sscanf(line, "For layer#: %d", &layer); err == nil {
			currentLayer = layer
			continue
		}
		if currentLayer < 0 || currentLayer >= layers || !strings.HasPrefix(line, "Number of input frames, encoded") {
			if currentLayer >= 0 && currentLayer < layers && strings.HasPrefix(line, "Average frame size") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					t.Fatalf("malformed temporal SVC frame-size line: %q", line)
				}
				targetFrameSize, err := strconv.ParseFloat(fields[len(fields)-2], 64)
				if err != nil {
					t.Fatalf("parse target frame size from %q returned error: %v", line, err)
				}
				stats[currentLayer].TargetFrameSizeBits = targetFrameSize
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Fatalf("malformed temporal SVC stats line: %q", line)
		}
		inputFrames, err := strconv.Atoi(fields[len(fields)-3])
		if err != nil {
			t.Fatalf("parse input frames from %q returned error: %v", line, err)
		}
		encodedFrames, err := strconv.Atoi(fields[len(fields)-2])
		if err != nil {
			t.Fatalf("parse encoded frames from %q returned error: %v", line, err)
		}
		droppedPct, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			t.Fatalf("parse dropped pct from %q returned error: %v", line, err)
		}
		stats[currentLayer].InputFrames = inputFrames
		stats[currentLayer].EncodedFrames = encodedFrames
		stats[currentLayer].DroppedPct = droppedPct
		seen[currentLayer] = true
	}
	for layer := 0; layer < layers; layer++ {
		if !seen[layer] {
			t.Fatalf("temporal SVC output did not include layer %d stats:\n%s", layer, output)
		}
	}
	return stats
}

func assertGovpxTemporalAccountingMatchesLibvpxExample(t *testing.T, sources []Image, stats []temporalSVCExampleLayerStats) {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               sources[0].Width,
		Height:              sources[0].Height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             -8,
		KeyFrameInterval:    3000,
		ErrorResilient:      true,
		TokenPartitions:     int(vp8common.TwoPartition),
		StaticThreshold:     1,
		MaxIntraBitratePct:  1000,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 600,
		BufferOptimalSizeMs: 600,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringTwoLayers,
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{720, 1200},
		},
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, sources[0].Width*sources[0].Height*3)
	sawLayerTarget := make([]bool, len(stats))
	for i, source := range sources {
		result, err := e.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto %d dropped, want full temporal SVC accounting sequence", i)
		}
		if !result.KeyFrame && result.TemporalLayerID >= 0 && result.TemporalLayerID < len(stats) {
			wantTarget := int(stats[result.TemporalLayerID].TargetFrameSizeBits + 0.5)
			if result.FrameTargetBits != wantTarget {
				t.Fatalf("frame %d layer %d target bits = %d, want libvpx example %.0f", i, result.TemporalLayerID, result.FrameTargetBits, stats[result.TemporalLayerID].TargetFrameSizeBits)
			}
			sawLayerTarget[result.TemporalLayerID] = true
		}
	}
	cumulativeTotal := 0
	for layer := range stats {
		cumulativeTotal += stats[layer].InputFrames
		accounting := e.temporal.accounting[layer]
		if accounting.InputFrames != stats[layer].InputFrames {
			t.Fatalf("layer %d input frames = %d, want libvpx example %d", layer, accounting.InputFrames, stats[layer].InputFrames)
		}
		if accounting.EncodedFrames != stats[layer].EncodedFrames {
			t.Fatalf("layer %d encoded non-key frames = %d, want libvpx example %d", layer, accounting.EncodedFrames, stats[layer].EncodedFrames)
		}
		if accounting.TotalEncodedFrames != cumulativeTotal {
			t.Fatalf("layer %d cumulative encoded frames = %d, want %d", layer, accounting.TotalEncodedFrames, cumulativeTotal)
		}
		if stats[layer].DroppedPct != 0 {
			t.Fatalf("layer %d libvpx example dropped pct = %.2f, want 0", layer, stats[layer].DroppedPct)
		}
		if stats[layer].EncodedFrames > 0 && !sawLayerTarget[layer] {
			t.Fatalf("layer %d had encoded frames but no govpx temporal target was checked", layer)
		}
	}
}

type temporalOracleCase struct {
	name       string
	mode       TemporalLayeringMode
	bitrates   [MaxTemporalLayers]int
	frameCount int
}

func temporalOracleTestCases() []temporalOracleCase {
	return []temporalOracleCase{
		{name: "one-layer", mode: TemporalLayeringOneLayer, frameCount: 6},
		{name: "two-layers", mode: TemporalLayeringTwoLayers, frameCount: 6},
		{name: "two-layers-three-frame", mode: TemporalLayeringTwoLayersThreeFrame, frameCount: 7},
		{name: "three-layers-six-frame", mode: TemporalLayeringThreeLayersSixFrame, frameCount: 13},
		{name: "three-layers-no-inter-layer-prediction", mode: TemporalLayeringThreeLayersNoInterLayerPrediction, frameCount: 9},
		{name: "three-layers-layer-one-prediction", mode: TemporalLayeringThreeLayersLayerOnePrediction, frameCount: 9},
		{name: "three-layers", mode: TemporalLayeringThreeLayers, frameCount: 9},
		{name: "five-layers", mode: TemporalLayeringFiveLayers, bitrates: [MaxTemporalLayers]int{200, 400, 700, 950, 1200}, frameCount: 18},
		{name: "two-layers-with-sync", mode: TemporalLayeringTwoLayersWithSync, frameCount: 9},
		{name: "three-layers-with-sync", mode: TemporalLayeringThreeLayersWithSync, frameCount: 9},
		{name: "three-layers-altref-with-sync", mode: TemporalLayeringThreeLayersAltRefWithSync, frameCount: 9},
		{name: "three-layers-one-reference", mode: TemporalLayeringThreeLayersOneReference, frameCount: 9},
		{name: "three-layers-no-sync", mode: TemporalLayeringThreeLayersNoSync, frameCount: 9},
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoBPredCandidateInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newSizedTestEncoder(t, 16, 32)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 32)
	fillImage(first, 0, 90, 170)
	second := rateControlTestFrame(16, 32, 0)

	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if e.interFrameModes[1].RefFrame == vp8common.IntraFrame {
		t.Fatalf("inter mode[1] = %+v, want coded inter residual after RD scoring", e.interFrameModes[1])
	}

	ivf := makeIVF(16, 32, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "B_PRED candidate interframe", got, oracleFrames)
}

func TestOracleLibvpxChecksumMatchesEncodeIntoEightTokenPartitions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              128,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		TokenPartitions:     int(vp8common.EightPartition),
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(16, 128)
	second := testImage(16, 128)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)

	keyPacket := make([]byte, 65536)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 65536)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}

	ivf := makeIVF(16, 128, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 2 {
		t.Fatalf("oracle frame count = %d, want 2", len(oracleFrames))
	}
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "eight token partitions", got, oracleFrames)
}

func TestOracleLibvpxChecksumMatchesEncodeIntoInvisibleReferenceFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	invisiblePacket := make([]byte, 4096)
	invisible, err := e.EncodeInto(invisiblePacket, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible EncodeInto returned error: %v", err)
	}
	invisibleInfo, err := PeekVP8StreamInfo(invisible.Data)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo invisible returned error: %v", err)
	}
	if !invisible.KeyFrame || !invisibleInfo.KeyFrame || invisibleInfo.ShowFrame {
		t.Fatalf("invisible result/header = %+v/%+v, want invisible keyframe", invisible, invisibleInfo)
	}

	visiblePacket := make([]byte, 4096)
	visible, err := e.EncodeInto(visiblePacket, publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("visible EncodeInto returned error: %v", err)
	}
	if visible.KeyFrame {
		t.Fatalf("visible KeyFrame = true, want interframe after invisible reference")
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(invisible.Data); err != nil {
		t.Fatalf("Decode invisible returned error: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned invisible frame")
	}
	if err := d.Decode(visible.Data); err != nil {
		t.Fatalf("Decode visible returned error: %v", err)
	}
	govpxFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no visible frame")
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{invisible.Data, visible.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != 1 {
		t.Fatalf("oracle frame count = %d, want one visible frame", len(oracleFrames))
	}
	want := checksumFrame(0, false, true, govpxFrame)
	if !testutil.SameFrameChecksum(oracleFrames[0], want) {
		t.Fatalf("checksum mismatch\nlibvpx:  %s\ngovpx: %s", formatChecksum(oracleFrames[0]), formatChecksum(want))
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoNoUpdateLastInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe without LAST refresh")
	}
	assertImagesEqual(t, "last after no-update-last", keyFrame, publicImageFromVP8(&e.lastRef.Img))

	lastPacket := make([]byte, 4096)
	lastInter, err := e.EncodeInto(lastPacket, keyFrame, 2, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("last EncodeInto returned error: %v", err)
	}
	if lastInter.KeyFrame {
		t.Fatalf("last KeyFrame = true, want interframe using preserved LAST")
	}
	if e.interFrameModes[0].RefFrame != vp8common.LastFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped LAST/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data, lastInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data, lastInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoGoldenReferenceInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	secondPacket := make([]byte, 4096)
	secondInter, err := e.EncodeInto(secondPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	goldenPacket := make([]byte, 4096)
	goldenInter, err := e.EncodeInto(goldenPacket, keyFrame, 2, 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("golden EncodeInto returned error: %v", err)
	}
	if goldenInter.KeyFrame {
		t.Fatalf("golden reference frame KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.GoldenFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped GOLDEN/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, secondInter.Data, goldenInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, secondInter.Data, goldenInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoGFCBRBoost(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	packet := make([]byte, 8192)
	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	packets := [][]byte{append([]byte(nil), key.Data...)}
	for frame := 1; frame <= 11; frame++ {
		wantRC := e.rc
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == 11 {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(packet, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if frame == 11 {
			state := packetState(t, inter.Data)
			if !state.Refresh.RefreshGolden || inter.FrameTargetBits != wantTarget {
				t.Fatalf("inter %d refresh/target = %t/%d, want golden refresh and boosted libvpx CBR target %d", frame, state.Refresh.RefreshGolden, inter.FrameTargetBits, wantTarget)
			}
		}
		packets = append(packets, append([]byte(nil), inter.Data...))
	}

	ivf := makeIVF(16, 16, 30, 1, packets)
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, packets...)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoAltReferenceInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	altInter, err := e.EncodeInto(interPacket, keyFrame, 1, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("alt EncodeInto returned error: %v", err)
	}
	if altInter.KeyFrame {
		t.Fatalf("alt reference frame KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped ALTREF/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, altInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, altInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoPreservedAltReferenceInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe preserving altref")
	}

	altPacket := make([]byte, 4096)
	altInter, err := e.EncodeInto(altPacket, publicImageFromVP8(&e.altRef.Img), 2, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("alt EncodeInto returned error: %v", err)
	}
	if altInter.KeyFrame {
		t.Fatalf("alt KeyFrame = true, want interframe using preserved ALTREF")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped preserved ALTREF/ZEROMV", e.interFrameModes[0])
	}

	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data, altInter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data, altInter.Data)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoResidualInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want residual interframe")
	}

	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoNewMVInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newSizedTestEncoder(t, 32, 16)
	first := testImage(32, 16)
	fillImage(first, 0, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 0; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = byte(32 + col*5)
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	shifted := shiftImageRightOne(reconstructed)
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, shifted, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want NEWMV interframe")
	}

	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoCQLevel(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             36,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, rateControlTestFrame(32, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.Quantizer != 36 || packetBaseQIndex(t, key.Data) != libvpxPublicQuantizerToQIndex(36) {
		t.Fatalf("key quantizer = result:%d packet:%d, want public CQ level 36 / qindex %d", key.Quantizer, packetBaseQIndex(t, key.Data), libvpxPublicQuantizerToQIndex(36))
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, rateControlTestFrame(32, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want CQ interframe")
	}
	if inter.Quantizer != 36 || packetBaseQIndex(t, inter.Data) != libvpxPublicQuantizerToQIndex(36) {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 36 / qindex %d", inter.Quantizer, packetBaseQIndex(t, inter.Data), libvpxPublicQuantizerToQIndex(36))
	}

	ivf := makeIVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "CQLevel interframe", got, oracleFrames)
}

func TestOracleLibvpxChecksumMatchesEncodeIntoSplitMVInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newSizedTestEncoder(t, 32, 32)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(32, 32)
	fillImage(first, 0, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 0; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = byte((row*37 + col*13) & 255)
		}
	}
	keyPacket := make([]byte, 32768)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	second := testImage(32, 32)
	fillImage(second, 13, 90, 170)
	copyShifted8x8FromImage(second, reconstructed, 0, 0, 0, 1)
	copyShifted8x8FromImage(second, reconstructed, 0, 8, 1, 0)
	copyShifted8x8FromImage(second, reconstructed, 8, 0, 0, 2)
	copyShifted8x8FromImage(second, reconstructed, 8, 8, 2, 0)

	interPacket := make([]byte, 32768)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want SPLITMV interframe")
	}
	mode := e.interFrameModes[0]
	if mode.Mode != vp8common.SplitMV || mode.Partition != 2 {
		t.Fatalf("mode[0] = %+v, want SPLITMV partition 2", mode)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].Mode != vp8common.SplitMV || d.modes[0].Partition != 2 {
		t.Fatalf("decoded mode[0] = %+v, want SPLITMV partition 2", d.modes[0])
	}

	ivf := makeIVF(32, 32, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "SPLITMV interframe", got, oracleFrames)
}

func TestOracleLibvpxChecksumMatchesEncodeIntoSubpixelNewMVInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	fillImage(first, 0, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 0; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = byte(32 + ((row*17 + col*13) & 127))
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	second := testImage(16, 16)
	fillImage(second, 0, 90, 170)
	ref := &e.lastRef.Img
	start := ref.YOrigin - 2*ref.YStride - 2
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, 2, 2, second.Y, second.YStride)
	reconstructed := publicImageFromVP8(ref)
	copy(second.U, reconstructed.U)
	copy(second.V, reconstructed.V)

	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want subpixel NEWMV interframe")
	}
	if e.interFrameModes[0].Mode != vp8common.NewMV || e.interFrameModes[0].MV != (vp8enc.MotionVector{Row: 2, Col: 2}) {
		t.Fatalf("mode[0] = %+v, want subpixel NEWMV +2,+2", e.interFrameModes[0])
	}

	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoLargeResidualInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 0, 90, 170)
	fillImage(second, 128, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want residual interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.LastFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV {
		t.Fatalf("mode[0] = %+v, want LAST/ZEROMV residual macroblock", e.interFrameModes[0])
	}

	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoLoopFilteredInterFrame(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		Sharpness:           3,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(32, 16)
	fillImage(first, 220, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 16; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = 40
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	second := testImage(32, 16)
	fillImage(second, 40, 90, 170)
	for row := 0; row < second.Height; row++ {
		for col := 16; col < second.Width; col++ {
			second.Y[row*second.YStride+col] = 220
		}
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want loop-filtered interframe")
	}

	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleLibvpxChecksumMatchesEncodeIntoStaticThresholdSegmentation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run libvpx oracle checksum tests")
	}
	oracle := findChecksumOracle(t)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := segmentedQuantizationTestImage()
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	second := segmentedQuantizationTestImage()
	for row := 0; row < second.Height; row++ {
		for col := 0; col < 16; col++ {
			second.Y[row*second.YStride+col] = 96
		}
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want segmented interframe")
	}

	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := makeIVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := runLibvpxChecksumOracle(t, oracle, ivf)
	if len(oracleFrames) != len(govpxFrames) {
		t.Fatalf("oracle frame count = %d, want %d", len(oracleFrames), len(govpxFrames))
	}
	for i, frame := range govpxFrames {
		want := checksumFrame(i, i == 0, true, frame)
		if !testutil.SameFrameChecksum(oracleFrames[i], want) {
			t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(oracleFrames[i]), formatChecksum(want))
		}
	}
}

func TestOracleExternalIVFTestDataMatchesLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run external libvpx conformance tests")
	}
	root, ok := externalIVFTestDataRoot(t, "set GOVPX_TEST_DATA_PATH to a VP8 IVF file or directory")
	if !ok {
		return
	}
	oracle := findChecksumOracle(t)
	paths := findVP8IVFTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no VP8 IVF files found under %s", root)
	}
	assertExternalIVFTestDataMinimum(t, paths)

	for _, path := range paths {
		path := path
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := runLibvpxChecksumOracleFile(t, oracle, path)
			got := decodeIVFChecksums(t, ivf)
			if len(got) != len(want) {
				t.Fatalf("frame count = %d, want %d from libvpx", len(got), len(want))
			}
			for i := range want {
				if !testutil.SameFrameChecksum(got[i], want[i]) {
					t.Fatalf("frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(want[i]), formatChecksum(got[i]))
				}
			}
		})
	}
}

func TestOracleExternalIVFTestDataDecodeIntoMatchesLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run external libvpx DecodeInto conformance tests")
	}
	root, ok := externalIVFTestDataRoot(t, "set GOVPX_TEST_DATA_PATH to a VP8 IVF file or directory")
	if !ok {
		return
	}
	oracle := findChecksumOracle(t)
	paths := findVP8IVFTestData(t, root)
	if len(paths) == 0 {
		t.Fatalf("no VP8 IVF files found under %s", root)
	}
	assertExternalIVFTestDataMinimum(t, paths)

	for _, path := range paths {
		path := path
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want := runLibvpxChecksumOracleFile(t, oracle, path)
			got := decodeIVFIntoChecksums(t, ivf)
			if len(got) != len(want) {
				t.Fatalf("DecodeInto frame count = %d, want %d from libvpx", len(got), len(want))
			}
			for i := range want {
				if !testutil.SameFrameChecksum(got[i], want[i]) {
					t.Fatalf("DecodeInto frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", i, formatChecksum(want[i]), formatChecksum(got[i]))
				}
			}
		})
	}
}

func TestOracleExternalInvalidIVFTestDataRejectedLikeLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run external invalid libvpx conformance tests")
	}
	root, ok := externalInvalidIVFTestDataRoot(t)
	if !ok {
		return
	}
	oracle := findChecksumOracle(t)
	paths := findInvalidVP8IVFTestData(t, root)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_INVALID_TEST_DATA_REQUIRED") == "1" || externalInvalidIVFTestMinimum(t) > 0 {
			t.Fatalf("no invalid VP8 IVF files found under %s", root)
		}
		t.Skipf("no invalid VP8 IVF files found under %s", root)
	}
	assertExternalInvalidIVFTestDataMinimum(t, paths)

	for _, path := range paths {
		path := path
		t.Run(safeIVFTestName(root, path), func(t *testing.T) {
			if err := runLibvpxChecksumOracleFileExpectError(t, oracle, path); err == nil {
				t.Fatalf("libvpx oracle decoded invalid VP8 IVF without error")
			}
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			if err := decodeIVFExpectError(t, ivf, DecoderOptions{}); err == nil {
				t.Fatalf("Decode accepted invalid VP8 IVF that libvpx rejected")
			}
			if err := decodeIVFIntoExpectError(t, ivf); err == nil {
				t.Fatalf("DecodeInto accepted invalid VP8 IVF that libvpx rejected")
			}
		})
	}
}

func TestOracleGeneratedLibvpxCorpusMatchesLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run generated libvpx conformance tests")
	}
	oracle := findChecksumOracle(t)
	vpxenc := findVpxenc(t)
	dir := t.TempDir()

	cases := []generatedLibvpxCorpusCase{
		{name: "baseline", width: 32, height: 32, frames: 6, checkProfile: true, wantProfile: 0, checkTokenPartition: true, wantTokenPartition: vp8common.OnePartition},
		{name: "narrow", width: 48, height: 24, frames: 6},
		{name: "profile1", width: 32, height: 32, frames: 6, args: []string{"--profile=1"}, checkProfile: true, wantProfile: 1},
		{name: "narrow-profile2", width: 48, height: 24, frames: 6, args: []string{"--profile=2"}, checkProfile: true, wantProfile: 2},
		{name: "profile3", width: 32, height: 32, frames: 3, args: []string{"--profile=3"}, checkProfile: true, wantProfile: 3},
		{name: "token-two", width: 32, height: 32, frames: 6, args: []string{"--token-parts=1"}, checkTokenPartition: true, wantTokenPartition: vp8common.TwoPartition},
		{name: "token-four", width: 32, height: 32, frames: 6, args: []string{"--token-parts=2"}, checkTokenPartition: true, wantTokenPartition: vp8common.FourPartition},
		{name: "token-eight", width: 32, height: 32, frames: 6, args: []string{"--token-parts=3"}, checkTokenPartition: true, wantTokenPartition: vp8common.EightPartition},
		{name: "token-eight-tall", width: 32, height: 128, frames: 6, args: []string{"--token-parts=3"}, checkTokenPartition: true, wantTokenPartition: vp8common.EightPartition, checkAllTokenPartitionsActive: true},
		{name: "error-resilient", width: 32, height: 32, frames: 6, args: []string{"--error-resilient=1"}},
		{name: "cyclic-refresh-error-resilient", width: 80, height: 80, frames: 8, args: []string{"--error-resilient=1"}, checkSegmentationMap: true},
		{name: "sharpness7", width: 32, height: 32, frames: 6, args: []string{"--sharpness=7"}},
		{name: "static-threshold", width: 64, height: 64, frames: 8, args: []string{"--static-thresh=1000"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ivfPath := generateLibvpxCorpusIVF(t, vpxenc, dir, tc)
			ivf, err := os.ReadFile(ivfPath)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			assertGeneratedLibvpxCorpusFeatures(t, ivf, tc)
			want := runLibvpxChecksumOracleFile(t, oracle, ivfPath)
			got := decodeIVFChecksums(t, ivf)
			gotInto := decodeIVFIntoChecksums(t, ivf)
			assertFrameChecksumsEqual(t, "Decode", got, want)
			assertFrameChecksumsEqual(t, "DecodeInto", gotInto, want)
		})
	}
}

func TestFindVP8IVFTestData(t *testing.T) {
	dir := t.TempDir()
	vp8Path := filepath.Join(dir, "vp8.ivf")
	if err := os.WriteFile(vp8Path, makeIVF(16, 16, 30, 1, [][]byte{{1}}), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	vp9Path := filepath.Join(dir, "vp9.ivf")
	vp9 := makeIVF(16, 16, 30, 1, [][]byte{{1}})
	copy(vp9[8:12], []byte("VP90"))
	if err := os.WriteFile(vp9Path, vp9, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("not ivf"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	paths := findVP8IVFTestData(t, dir)
	if len(paths) != 1 || paths[0] != vp8Path {
		t.Fatalf("paths = %v, want [%s]", paths, vp8Path)
	}
}

func TestExternalIVFTestMinimum(t *testing.T) {
	t.Setenv("GOVPX_TEST_DATA_MIN", "3")

	if got := externalIVFTestMinimum(t); got != 3 {
		t.Fatalf("minimum = %d, want 3", got)
	}
}

func makeSingleFrameIVF(width int, height int, den uint32, num uint32, frame []byte) []byte {
	return makeIVF(width, height, den, num, [][]byte{frame})
}

func makeIVF(width int, height int, den uint32, num uint32, frames [][]byte) []byte {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	size := fileHeaderSize
	for _, frame := range frames {
		size += frameHeaderSize + len(frame)
	}
	out := make([]byte, size)
	copy(out[0:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(out[4:6], 0)
	binary.LittleEndian.PutUint16(out[6:8], fileHeaderSize)
	copy(out[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint16(out[12:14], uint16(width))
	binary.LittleEndian.PutUint16(out[14:16], uint16(height))
	binary.LittleEndian.PutUint32(out[16:20], den)
	binary.LittleEndian.PutUint32(out[20:24], num)
	binary.LittleEndian.PutUint32(out[24:28], uint32(len(frames)))
	offset := fileHeaderSize
	for i, frame := range frames {
		binary.LittleEndian.PutUint32(out[offset:offset+4], uint32(len(frame)))
		binary.LittleEndian.PutUint64(out[offset+4:offset+12], uint64(i))
		copy(out[offset+frameHeaderSize:], frame)
		offset += frameHeaderSize + len(frame)
	}
	return out
}

func findChecksumOracle(t *testing.T) string {
	t.Helper()
	oracle := os.Getenv("GOVPX_ORACLE")
	if oracle != "" {
		return oracle
	}
	path, err := exec.LookPath("govpx-vpx-oracle")
	if err != nil {
		t.Skip("set GOVPX_ORACLE to the libvpx v1.16.0 checksum oracle binary")
	}
	return path
}

func findVpxenc(t *testing.T) string {
	t.Helper()
	if vpxenc := os.Getenv("GOVPX_VPXENC"); vpxenc != "" {
		return vpxenc
	}
	if path, err := exec.LookPath("vpxenc"); err == nil {
		return path
	}
	local := filepath.Join("internal", "coracle", "build", "vpxenc")
	info, err := os.Stat(local)
	if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return local
	}
	t.Skip("set GOVPX_VPXENC to a libvpx v1.16.0 vpxenc binary")
	return ""
}

func findVpxTemporalSVCEncoder(t *testing.T) string {
	t.Helper()
	if svcEncoder := os.Getenv("GOVPX_VPX_TEMPORAL_SVC_ENCODER"); svcEncoder != "" {
		return svcEncoder
	}
	if path, err := exec.LookPath("vpx_temporal_svc_encoder"); err == nil {
		return path
	}
	local := filepath.Join("internal", "coracle", "build", "vpx_temporal_svc_encoder")
	info, err := os.Stat(local)
	if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return local
	}
	t.Skip("set GOVPX_VPX_TEMPORAL_SVC_ENCODER to a libvpx v1.16.0 vpx_temporal_svc_encoder binary")
	return ""
}

func runLibvpxChecksumOracle(t *testing.T, oracle string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-keyframe.ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return runLibvpxChecksumOracleFile(t, oracle, path)
}

func runLibvpxChecksumOracleMode(t *testing.T, oracle string, mode string, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-"+mode+".ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return runLibvpxChecksumOracleFileMode(t, oracle, mode, path)
}

func runLibvpxChecksumOracleModeExpectError(t *testing.T, oracle string, mode string, ivf []byte) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "govpx-"+mode+".ivf")
	if err := os.WriteFile(path, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return runLibvpxChecksumOracleFileModeExpectError(t, oracle, mode, path)
}

func runLibvpxChecksumOracleFile(t *testing.T, oracle string, path string) []testutil.FrameChecksum {
	t.Helper()
	return runLibvpxChecksumOracleFileMode(t, oracle, "decode", path)
}

func runLibvpxChecksumOracleFileMode(t *testing.T, oracle string, mode string, path string) []testutil.FrameChecksum {
	t.Helper()
	cmd := exec.Command(oracle, mode, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("libvpx oracle failed: %v\n%s", err, out)
	}
	frames, err := testutil.ParseFrameChecksumJSONLines(out)
	if err != nil {
		if errors.Is(err, testutil.ErrInvalidOracleOutput) {
			t.Fatalf("libvpx oracle produced invalid output:\n%s", out)
		}
		t.Fatalf("ParseFrameChecksumJSONLines returned error: %v", err)
	}
	return frames
}

func runLibvpxChecksumOracleFileExpectError(t *testing.T, oracle string, path string) error {
	t.Helper()
	return runLibvpxChecksumOracleFileModeExpectError(t, oracle, "decode", path)
}

func runLibvpxChecksumOracleFileModeExpectError(t *testing.T, oracle string, mode string, path string) error {
	t.Helper()
	cmd := exec.Command(oracle, mode, path)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("libvpx oracle failed to start: %v\n%s", err, out)
	}
	return err
}

func assertFrameChecksumsEqual(t *testing.T, label string, got []testutil.FrameChecksum, want []testutil.FrameChecksum) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s frame count = %d, want %d from libvpx", label, len(got), len(want))
	}
	for i := range want {
		if !testutil.SameFrameChecksum(got[i], want[i]) {
			t.Fatalf("%s frame %d checksum mismatch\nlibvpx:  %s\ngovpx: %s", label, i, formatChecksum(want[i]), formatChecksum(got[i]))
		}
	}
}

func decodeIVFChecksums(t *testing.T, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	return decodeIVFChecksumsWithOptions(t, ivf, DecoderOptions{})
}

func decodeIVFChecksumsWithOptions(t *testing.T, ivf []byte, opts DecoderOptions) []testutil.FrameChecksum {
	t.Helper()
	if _, err := testutil.ParseIVFHeader(ivf); err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	var frames []testutil.FrameChecksum
	outputIndex := 0
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", inputIndex, err)
		}
		if err := d.Decode(frame.Data); err != nil {
			t.Fatalf("Decode frame %d returned error: %v", inputIndex, err)
		}
		info := d.lastInfo
		img, ok := d.NextFrame()
		if info.ShowFrame {
			if !ok {
				t.Fatalf("NextFrame frame %d returned no frame", inputIndex)
			}
			frames = append(frames, checksumFrame(outputIndex, info.KeyFrame, info.ShowFrame, img))
			outputIndex++
		} else if ok {
			t.Fatalf("NextFrame frame %d returned an invisible frame", inputIndex)
		}
		offset = next
	}
	return frames
}

func decodeIVFExpectError(t *testing.T, ivf []byte, opts DecoderOptions) error {
	t.Helper()
	if _, err := testutil.ParseIVFHeader(ivf); err != nil {
		return err
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return err
	}
	d, err := NewVP8Decoder(opts)
	if err != nil {
		return err
	}
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		if _, err := PeekVP8StreamInfo(frame.Data); err != nil {
			return err
		}
		if err := d.Decode(frame.Data); err != nil {
			return err
		}
		_, _ = d.NextFrame()
		offset = next
	}
	return nil
}

type generatedLibvpxCorpusCase struct {
	name                          string
	width                         int
	height                        int
	frames                        int
	args                          []string
	checkProfile                  bool
	wantProfile                   int
	checkTokenPartition           bool
	wantTokenPartition            vp8common.TokenPartition
	checkSegmentationMap          bool
	checkAllTokenPartitionsActive bool
}

func assertGeneratedLibvpxCorpusFeatures(t *testing.T, ivf []byte, tc generatedLibvpxCorpusCase) {
	t.Helper()
	if !tc.checkProfile && !tc.checkTokenPartition && !tc.checkSegmentationMap && !tc.checkAllTokenPartitionsActive {
		return
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	previousQuant := vp8dec.QuantHeader{}
	sawProfile := !tc.checkProfile
	sawTokenPartition := !tc.checkTokenPartition
	sawSegmentationMap := !tc.checkSegmentationMap
	sawAllTokenPartitionsActive := !tc.checkAllTokenPartitionsActive
	decoder, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	for frameIndex := 0; offset < len(ivf); frameIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, frameIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame returned error: %v", err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
		}
		if tc.checkProfile && info.Profile == tc.wantProfile {
			sawProfile = true
		}
		_, state, err := vp8dec.ParseStateHeader(frame.Data, previousQuant)
		if err != nil {
			t.Fatalf("ParseStateHeader returned error: %v", err)
		}
		if tc.checkTokenPartition && state.TokenPartition == tc.wantTokenPartition {
			sawTokenPartition = true
		}
		if tc.checkAllTokenPartitionsActive {
			header, err := vp8dec.ParseFrameHeader(frame.Data)
			if err != nil {
				t.Fatalf("ParseFrameHeader returned error: %v", err)
			}
			var layout vp8dec.PartitionLayout
			if err := vp8dec.ParsePartitionLayout(frame.Data, header, state.TokenPartition, &layout); err != nil {
				t.Fatalf("ParsePartitionLayout returned error: %v", err)
			}
			allActive := layout.TokenCount == int(1<<uint(tc.wantTokenPartition))
			for i := 0; i < layout.TokenCount; i++ {
				if len(layout.Tokens[i]) <= 1 {
					allActive = false
					break
				}
			}
			if allActive {
				sawAllTokenPartitionsActive = true
			}
		}
		if tc.checkSegmentationMap {
			if err := decoder.Decode(frame.Data); err != nil {
				t.Fatalf("Decode frame %d returned error while checking generated features: %v", frameIndex, err)
			}
			for _, segmentID := range decoder.segmentMap {
				if segmentID != 0 {
					sawSegmentationMap = true
					break
				}
			}
		}
		previousQuant = state.Quant
		offset = next
	}
	if !sawProfile {
		t.Fatalf("generated corpus profile = no frame with profile %d", tc.wantProfile)
	}
	if !sawTokenPartition {
		t.Fatalf("generated corpus token partition = no frame with partition %d", tc.wantTokenPartition)
	}
	if !sawSegmentationMap {
		t.Fatalf("generated corpus did not contain a nonzero segmentation map")
	}
	if !sawAllTokenPartitionsActive {
		t.Fatalf("generated corpus did not exercise all token partitions with active payload")
	}
}

func generateLibvpxCorpusIVF(t *testing.T, vpxenc string, dir string, tc generatedLibvpxCorpusCase) string {
	t.Helper()
	yuvPath := filepath.Join(dir, tc.name+".yuv")
	ivfPath := filepath.Join(dir, tc.name+".ivf")
	writeDeterministicI420(t, yuvPath, tc.width, tc.height, tc.frames)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--good",
		"--cpu-used=0",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--end-usage=vbr",
		"--target-bitrate=200",
		"--i420",
		"--width=" + strconv.Itoa(tc.width),
		"--height=" + strconv.Itoa(tc.height),
		"--fps=30/1",
		"--limit=" + strconv.Itoa(tc.frames),
		"--output=" + ivfPath,
	}
	args = append(args, tc.args...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxenc, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, out)
	}
	return ivfPath
}

func writeDeterministicI420(t *testing.T, path string, width int, height int, frames int) {
	t.Helper()
	if width <= 0 || height <= 0 || frames <= 0 || width%2 != 0 || height%2 != 0 {
		t.Fatalf("invalid I420 corpus dimensions %dx%d frames=%d", width, height, frames)
	}
	uvWidth := width / 2
	uvHeight := height / 2
	buf := make([]byte, 0, frames*(width*height+2*uvWidth*uvHeight))
	for frame := 0; frame < frames; frame++ {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				buf = append(buf, byte((x*5+y*3+frame*17)&0xff))
			}
		}
		for y := 0; y < uvHeight; y++ {
			for x := 0; x < uvWidth; x++ {
				buf = append(buf, byte((96+x*3+y+frame*7)&0xff))
			}
		}
		for y := 0; y < uvHeight; y++ {
			for x := 0; x < uvWidth; x++ {
				buf = append(buf, byte((160+x+y*5+frame*11)&0xff))
			}
		}
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func decodeIVFIntoChecksums(t *testing.T, ivf []byte) []testutil.FrameChecksum {
	t.Helper()
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader returned error: %v", err)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := testImage(header.Width, header.Height)

	var frames []testutil.FrameChecksum
	outputIndex := 0
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", inputIndex, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo[%d] returned error: %v", inputIndex, err)
		}
		if info.KeyFrame && (dst.Width != info.Width || dst.Height != info.Height) {
			dst = testImage(info.Width, info.Height)
		}
		frameInfo, err := d.DecodeInto(frame.Data, &dst)
		if err != nil {
			t.Fatalf("DecodeInto frame %d returned error: %v", inputIndex, err)
		}
		if _, ok := d.NextFrame(); ok {
			t.Fatalf("DecodeInto frame %d queued a NextFrame output", inputIndex)
		}
		if frameInfo.ShowFrame {
			frames = append(frames, checksumFrame(outputIndex, frameInfo.KeyFrame, frameInfo.ShowFrame, dst))
			outputIndex++
		}
		offset = next
	}
	return frames
}

func decodeIVFIntoExpectError(t *testing.T, ivf []byte) error {
	t.Helper()
	header, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		return err
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return err
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		return err
	}
	dst := testImage(header.Width, header.Height)
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			return err
		}
		if info.KeyFrame && (dst.Width != info.Width || dst.Height != info.Height) {
			dst = testImage(info.Width, info.Height)
		}
		if _, err := d.DecodeInto(frame.Data, &dst); err != nil {
			return err
		}
		offset = next
	}
	return nil
}

func findVP8IVFTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalIVFTestLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		if !isInvalidVP8IVFTestDataName(root) && isVP8IVFTestData(t, root) {
			paths = append(paths, root)
		}
		return paths
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ivf") || isInvalidVP8IVFTestDataName(path) {
			return nil
		}
		if isVP8IVFTestData(t, path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func findInvalidVP8IVFTestData(t *testing.T, root string) []string {
	t.Helper()
	limit := externalInvalidIVFTestLimit(t)
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		if isInvalidVP8IVFTestDataName(root) && isVP8IVFTestData(t, root) {
			paths = append(paths, root)
		}
		return paths
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ivf") || !isInvalidVP8IVFTestDataName(path) {
			return nil
		}
		if isVP8IVFTestData(t, path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func isInvalidVP8IVFTestDataName(path string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(path)), "invalid-")
}

func externalIVFTestDataRoot(t *testing.T, skipMessage string) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOVPX_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_TEST_DATA_REQUIRED=1 but GOVPX_TEST_DATA_PATH is not set")
	}
	t.Skip(skipMessage)
	return "", false
}

func externalInvalidIVFTestDataRoot(t *testing.T) (string, bool) {
	t.Helper()
	root := os.Getenv("GOVPX_INVALID_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	root = os.Getenv("GOVPX_TEST_DATA_PATH")
	if root != "" {
		return root, true
	}
	if os.Getenv("GOVPX_INVALID_TEST_DATA_REQUIRED") == "1" {
		t.Fatalf("GOVPX_INVALID_TEST_DATA_REQUIRED=1 but neither GOVPX_INVALID_TEST_DATA_PATH nor GOVPX_TEST_DATA_PATH is set")
	}
	t.Skip("set GOVPX_INVALID_TEST_DATA_PATH to invalid VP8 IVF data or point GOVPX_TEST_DATA_PATH at a full libvpx test-data directory")
	return "", false
}

func externalIVFTestLimit(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_TEST_DATA_LIMIT")
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("GOVPX_TEST_DATA_LIMIT = %q, want a non-negative integer", raw)
	}
	return limit
}

func externalInvalidIVFTestLimit(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_INVALID_TEST_DATA_LIMIT")
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		t.Fatalf("GOVPX_INVALID_TEST_DATA_LIMIT = %q, want a non-negative integer", raw)
	}
	return limit
}

func externalIVFTestMinimum(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_TEST_DATA_MIN")
	if raw == "" {
		return 0
	}
	minimum, err := strconv.Atoi(raw)
	if err != nil || minimum < 0 {
		t.Fatalf("GOVPX_TEST_DATA_MIN = %q, want a non-negative integer", raw)
	}
	return minimum
}

func externalInvalidIVFTestMinimum(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("GOVPX_INVALID_TEST_DATA_MIN")
	if raw == "" {
		return 0
	}
	minimum, err := strconv.Atoi(raw)
	if err != nil || minimum < 0 {
		t.Fatalf("GOVPX_INVALID_TEST_DATA_MIN = %q, want a non-negative integer", raw)
	}
	return minimum
}

func assertExternalIVFTestDataMinimum(t *testing.T, paths []string) {
	t.Helper()
	minimum := externalIVFTestMinimum(t)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("VP8 IVF test data count = %d, want at least %d from GOVPX_TEST_DATA_MIN", len(paths), minimum)
	}
}

func assertExternalInvalidIVFTestDataMinimum(t *testing.T, paths []string) {
	t.Helper()
	minimum := externalInvalidIVFTestMinimum(t)
	if minimum > 0 && len(paths) < minimum {
		t.Fatalf("invalid VP8 IVF test data count = %d, want at least %d from GOVPX_INVALID_TEST_DATA_MIN", len(paths), minimum)
	}
}

func isVP8IVFTestData(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s returned error: %v", path, err)
	}
	defer file.Close()
	header := make([]byte, testutil.IVFFileHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			t.Fatalf("%s is not valid IVF data: %v", path, testutil.ErrInvalidIVF)
		}
		t.Fatalf("ReadFull %s returned error: %v", path, err)
	}
	_, err = testutil.ParseIVFHeader(header)
	if err == nil {
		return true
	}
	if errors.Is(err, testutil.ErrUnsupportedFourCC) {
		return false
	}
	t.Fatalf("%s is not valid VP8 IVF data: %v", path, err)
	return false
}

func safeIVFTestName(root string, path string) string {
	name, err := filepath.Rel(root, path)
	if err != nil || name == "." {
		name = filepath.Base(path)
	}
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	if name == "" {
		return "ivf"
	}
	return name
}

func decodeFrameSequence(t *testing.T, packets ...[]byte) []Image {
	t.Helper()
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	frames := make([]Image, 0, len(packets))
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d returned error: %v", i, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("NextFrame packet %d returned no frame", i)
		}
		frames = append(frames, cloneImage(frame))
	}
	return frames
}

func cloneImage(src Image) Image {
	dst := testImage(src.Width, src.Height)
	copyPlane(dst.Y, dst.YStride, src.Y, src.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
	return dst
}

func checksumFrame(index int, keyFrame bool, showFrame bool, img Image) testutil.FrameChecksum {
	return testutil.FrameChecksum{
		Index:     index,
		Width:     img.Width,
		Height:    img.Height,
		KeyFrame:  keyFrame,
		ShowFrame: showFrame,
		MD5:       testutil.MD5Planes(img.Y, img.YStride, img.U, img.UStride, img.V, img.VStride, img.Width, img.Height),
	}
}

func formatChecksum(frame testutil.FrameChecksum) string {
	return fmt.Sprintf("frame=%d %dx%d key=%t show=%t y=%s u=%s v=%s full=%s",
		frame.Index,
		frame.Width,
		frame.Height,
		frame.KeyFrame,
		frame.ShowFrame,
		testutil.MD5Hex(frame.MD5.Y),
		testutil.MD5Hex(frame.MD5.U),
		testutil.MD5Hex(frame.MD5.V),
		testutil.MD5Hex(frame.MD5.Full),
	)
}
