//go:build govpx_oracle_trace

package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoResidualInterFrame(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

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
	ivf := testutil.BuildVP8IVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
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

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoNewMVInterFrame(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

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
	ivf := testutil.BuildVP8IVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
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

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoCQLevel(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

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
	// libvpx vp8/encoder/onyx_if.c lines 3727-3739: CQ mode does not floor
	// keyframe Q to cq_target_quality; only inter non-refresh frames take
	// the floor. Assert the floor on the inter frame below.
	key, err := e.EncodeInto(keyPacket, rateControlTestFrame(32, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.Quantizer >= 36 {
		t.Fatalf("key quantizer = %d, want below CQ level 36 (libvpx allows KF below cq_target_quality)", key.Quantizer)
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, rateControlTestFrame(32, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want CQ interframe")
	}
	if inter.Quantizer != 36 || packetBaseQIndex(t, inter.Data) != vp8common.PublicQuantizerToQIndex(36) {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 36 / qindex %d", inter.Quantizer, packetBaseQIndex(t, inter.Data), vp8common.PublicQuantizerToQIndex(36))
	}

	ivf := testutil.BuildVP8IVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "CQLevel interframe", got, oracleFrames)
}

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoSplitMVInterFrame(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

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

	ivf := testutil.BuildVP8IVF(32, 32, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
	got := decodeIVFChecksums(t, ivf)
	assertFrameChecksumsEqual(t, "SPLITMV interframe", got, oracleFrames)
}

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoSubpixelNewMVInterFrame(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

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
	ivf := testutil.BuildVP8IVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
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

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoLargeResidualInterFrame(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

	e := newEntropyRefreshTestEncoder(t, false)
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
	if e.interFrameModes[0].RefFrame != vp8common.LastFrame || e.interFrameModes[0].MBSkipCoeff || !e.interFrameModes[0].MV.IsZero() {
		t.Fatalf("mode[0] = %+v, want LAST zero-motion residual macroblock", e.interFrameModes[0])
	}
	if e.interFrameModes[0].Mode != vp8common.ZeroMV && e.interFrameModes[0].Mode != vp8common.NewMV {
		t.Fatalf("mode[0] = %+v, want LAST zero-motion residual mode", e.interFrameModes[0])
	}

	govpxFrames := decodeFrameSequence(t, key.Data, inter.Data)
	ivf := testutil.BuildVP8IVF(16, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
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

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoLoopFilteredInterFrame(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

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
	ivf := testutil.BuildVP8IVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
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

func TestVP8OracleLibvpxChecksumMatchesEncodeIntoStaticThresholdSegmentation(t *testing.T) {
	vp8test.RequireOracle(t, "libvpx oracle checksum tests")
	oracle := vp8test.NewChecksumOracle(t)

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
		for col := range 16 {
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
	ivf := testutil.BuildVP8IVF(32, 16, 30, 1, [][]byte{key.Data, inter.Data})
	oracleFrames := oracle.Frames(t, ivf)
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
