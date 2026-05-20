package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestEncodeIntoUsesSourcePixels(t *testing.T) {
	darkEncoder := newTestEncoder(t)
	brightEncoder := newTestEncoder(t)
	dark := testImage(16, 16)
	bright := testImage(16, 16)
	fillImage(bright, 220, 128, 128)
	dstDark := make([]byte, 4096)
	dstBright := make([]byte, 4096)

	darkResult, err := darkEncoder.EncodeInto(dstDark, dark, 0, 1, 0)
	if err != nil {
		t.Fatalf("dark EncodeInto returned error: %v", err)
	}
	brightResult, err := brightEncoder.EncodeInto(dstBright, bright, 0, 1, 0)
	if err != nil {
		t.Fatalf("bright EncodeInto returned error: %v", err)
	}

	darkFrame := decodeSingleFrame(t, darkResult.Data)
	brightFrame := decodeSingleFrame(t, brightResult.Data)
	if brightFrame.Y[0] <= darkFrame.Y[0] {
		t.Fatalf("decoded Y0 dark/bright = %d/%d, want bright greater", darkFrame.Y[0], brightFrame.Y[0])
	}
}

func TestEncodeIntoReconstructsReferencesLikeDecoder(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	src := testImage(32, 16)
	fillImage(src, 220, 90, 170)
	for row := 0; row < src.Height; row++ {
		for col := 16; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = 40
		}
	}
	dst := make([]byte, 8192)

	result, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	decoded := decodeSingleFrame(t, result.Data)

	assertImagesEqual(t, "current", decoded, publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded, publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", decoded, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", decoded, publicImageFromVP8(&e.altRef.Img))
}

func TestEncodeIntoWritesInterFrameForMatchingReference(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	dstKey := make([]byte, 4096)
	key, err := e.EncodeInto(dstKey, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	dstInter := make([]byte, 4096)

	inter, err := e.EncodeInto(dstInter, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("second frame KeyFrame = true, want interframe")
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
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "inter", reconstructed, frame)
	assertImagesEqual(t, "encoder current", frame, publicImageFromVP8(&e.current.Img))
}

func BenchmarkLoopFilterTrialLumaSSEPartialLargeFrame(b *testing.B) {
	const width, height = 1024, 1024
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
		}
	}
	e := newSizedTestEncoder(b, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	if len(e.reconstructModes) < required {
		e.reconstructModes = make([]vp8dec.MacroblockMode, required)
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}
	srcImg := sourceImageFromPublic(src)
	ctx := e.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})

	b.Run("partial_no_stats", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEPartial(24)
		}
	})
	b.Run("partial_stats", func(b *testing.B) {
		var stats EncoderPhaseStats
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEPartialStats(24, &stats)
		}
	})
	b.Run("full_no_stats", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEFull(24)
		}
	})
	b.Run("full_stats", func(b *testing.B) {
		var stats EncoderPhaseStats
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx.trialLumaSSEFullStats(24, &stats)
		}
	})
}

func BenchmarkEncodeIntoMatchingReferenceInterFrame(b *testing.B) {
	e := newTestEncoder(b)
	if err := e.SetKeyFrameInterval(0); err != nil {
		b.Fatalf("SetKeyFrameInterval returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		b.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(b, key.Data)
	interPacket := make([]byte, 4096)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.EncodeInto(interPacket, reconstructed, uint64(i+1), 1, 0); err != nil {
			b.Fatalf("inter EncodeInto returned error: %v", err)
		}
	}
}

func BenchmarkEncodeIntoGoldenReferenceInterFrame(b *testing.B) {
	e := newTestEncoder(b)
	if err := e.SetKeyFrameInterval(0); err != nil {
		b.Fatalf("SetKeyFrameInterval returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		b.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(b, key.Data)
	interPacket := make([]byte, 4096)
	if _, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef); err != nil {
		b.Fatalf("second EncodeInto returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.EncodeInto(interPacket, keyFrame, uint64(i+2), 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef); err != nil {
			b.Fatalf("golden EncodeInto returned error: %v", err)
		}
	}
}

func TestEncodeIntoWritesResidualInterFrameWhenSourceDiffersFromReference(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}
	dst := make([]byte, 4096)

	result, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("second frame KeyFrame = true, want residual interframe")
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
	if err := d.Decode(result.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	if frame.Y[0] >= 220 {
		t.Fatalf("inter decoded Y0 = %d, want residual to move toward darker source", frame.Y[0])
	}
	assertImagesEqual(t, "encoder current", frame, publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoUsesNewMVForShiftedReference(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	// Negative cpu_used pins explicit Speed=-cpu_used (libvpx encodeframe.c:686),
	// bypassing vp8_auto_select_speed; positive cpu_used is now an auto-budget
	// target rather than a fixed Speed.
	if err := e.SetCPUUsed(-3); err != nil {
		t.Fatalf("SetCPUUsed returned error: %v", err)
	}
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
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].Mode != vp8common.NewMV || e.interFrameModes[0].MV != (vp8enc.MotionVector{Col: -8}) {
		t.Fatalf("mode[0] = %+v, want NEWMV col -8", e.interFrameModes[0])
	}
	if e.interFrameModes[1].Mode != vp8common.NearestMV || e.interFrameModes[1].MV != (vp8enc.MotionVector{Col: -8}) {
		t.Fatalf("mode[1] = %+v, want NEARESTMV col -8", e.interFrameModes[1])
	}
}

func TestEncodeIntoCanEmitSplitMVForQuadrantMotion(t *testing.T) {
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
		t.Fatalf("inter KeyFrame = true, want interframe")
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
	decoded, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "splitmv encoder current", decoded, publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoKeyFrameSelectsBPredLumaAndVerticalChroma(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 32)
	src := rateControlTestFrame(16, 32, 0)

	if _, err := e.EncodeInto(make([]byte, 8192), src, 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	if e.keyFrameModes[1].YMode != vp8common.BPred {
		t.Fatalf("key mode[1] = %+v, want B_PRED luma for repeated rows", e.keyFrameModes[1])
	}
	if e.keyFrameModes[1].UVMode != vp8common.VPred {
		t.Fatalf("key UV mode[1] = %+v, want vertical prediction for repeated chroma rows", e.keyFrameModes[1])
	}
}

func TestEncodeIntoBPredKeyFrameUsesInterleavedReconstruction(t *testing.T) {
	opts := encoderValidationOptions(64, 128, 30, 700, nil)
	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := rateControlTestFrame(64, 128, 0)
	packet := make([]byte, 64*128*3)

	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	bpredCount := 0
	for _, mode := range e.keyFrameModes {
		if mode.YMode == vp8common.BPred {
			bpredCount++
		}
	}
	if bpredCount == 0 {
		t.Fatalf("B_PRED macroblocks = 0, want regression frame to exercise 4x4 intra reconstruction")
	}
	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "B_PRED keyframe current", decoded, publicImageFromVP8(&e.current.Img))
	if psnr := encoderValidationImagePSNR(src, decoded); psnr < 45 {
		t.Fatalf("B_PRED keyframe PSNR = %.2f dB, want >= 45 dB", psnr)
	}
}

func TestEncodeIntoInterFrameCanChooseBPredIntraAfterRDScoring(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 32)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 32)
	fillImage(first, 0, 90, 170)
	second := rateControlTestFrame(16, 32, 0)
	keyPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(keyPacket, first, 0, 1, 0); err != nil {
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
	if e.interFrameModes[1].RefFrame != vp8common.IntraFrame || e.interFrameModes[1].Mode != vp8common.BPred {
		t.Fatalf("inter mode[1] = %+v, want libvpx-style B_PRED intra candidate after RD scoring", e.interFrameModes[1])
	}
}

func TestEncodeIntoInterFrameCodesLargeUniformResidual(t *testing.T) {
	// This test pins residual inter coding for the normal entropy path.
	// Error-resilient key frames intentionally refresh independent coefficient
	// contexts like libvpx, which can make this synthetic single-MB fixture pick
	// an intra inter-frame mode instead.
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
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.LastFrame || e.interFrameModes[0].MBSkipCoeff || !e.interFrameModes[0].MV.IsZero() {
		t.Fatalf("mode[0] = %+v, want LAST zero-motion residual macroblock", e.interFrameModes[0])
	}
	if e.interFrameModes[0].Mode != vp8common.ZeroMV && e.interFrameModes[0].Mode != vp8common.NewMV {
		t.Fatalf("mode[0] = %+v, want LAST zero-motion residual mode", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "intra interframe current", decoded[1], publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoInterFrameCanSkipLastRefresh(t *testing.T) {
	e := newTestEncoder(t)
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
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	assertImagesEqual(t, "last", keyFrame, publicImageFromVP8(&e.lastRef.Img))
	if publicImageFromVP8(&e.current.Img).Y[0] == keyFrame.Y[0] {
		t.Fatalf("current Y0 = last Y0 = %d, want current reconstructed without last refresh", keyFrame.Y[0])
	}
}

func TestEncodeIntoInterFramePreservesGoldenAndAltRefByDefault(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", keyFrame, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", keyFrame, publicImageFromVP8(&e.altRef.Img))
}

func TestEncodeIntoCanForceGoldenAndAltRefRefresh(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeForceGoldenFrame|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	state := packetState(t, inter.Data)
	if !state.Refresh.RefreshLast || !state.Refresh.RefreshGolden || !state.Refresh.RefreshAltRef {
		t.Fatalf("refresh flags = %+v, want last/golden/altref refresh", state.Refresh)
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", decoded[1], publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", decoded[1], publicImageFromVP8(&e.altRef.Img))
	if planeEqual(keyFrame.Y, keyFrame.YStride, e.goldenRef.Img.Y, e.goldenRef.Img.YStride, keyFrame.Width, keyFrame.Height) {
		t.Fatalf("golden reference still matches keyframe after forced refresh")
	}
}

func TestEncodeIntoRejectsConflictingForceReferenceFlags(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	dst := make([]byte, 4096)

	tests := []struct {
		name  string
		flags EncodeFlags
	}{
		{name: "golden", flags: EncodeForceGoldenFrame | EncodeNoUpdateGolden},
		{name: "altref", flags: EncodeForceAltRefFrame | EncodeNoUpdateAltRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.EncodeInto(dst, src, 0, 1, tt.flags); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("EncodeInto error = %v, want ErrInvalidConfig", err)
			}
			if e.frameCount != 0 {
				t.Fatalf("frameCount = %d, want no mutation after invalid flags", e.frameCount)
			}
		})
	}
}

func TestBoostedReferenceRateControlFrameMirrorsLibvpxRefreshFlags(t *testing.T) {
	if !boostedReferenceRateControlFrame(true, 0) {
		t.Fatalf("golden CBR refresh = false, want boosted reference rate-control frame")
	}
	if !boostedReferenceRateControlFrame(false, EncodeForceGoldenFrame) {
		t.Fatalf("force golden refresh = false, want boosted reference rate-control frame")
	}
	if !boostedReferenceRateControlFrame(false, EncodeForceAltRefFrame) {
		t.Fatalf("force altref refresh = false, want boosted reference rate-control frame")
	}
	if boostedReferenceRateControlFrame(false, EncodeNoUpdateGolden|EncodeNoUpdateAltRef) {
		t.Fatalf("no-update flags = true, want normal inter rate-control frame")
	}
}

func TestShouldCopyOldGoldenToAltRefOnGoldenRefreshMirrorsLibvpxPolicy(t *testing.T) {
	if !shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, true, 0) {
		t.Fatalf("internal GF refresh copy = false, want libvpx copy old GF to ARF")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(true, true, 0) {
		t.Fatalf("error-resilient GF refresh copy = true, want disabled")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, true, EncodeForceGoldenFrame) {
		t.Fatalf("user-forced GF refresh copy = true, want disabled for external refresh flags")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, true, EncodeNoUpdateLast) {
		t.Fatalf("user reference-update flags copy = true, want disabled for external refresh flags")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, false, 0) {
		t.Fatalf("non-GF-refresh copy = true, want disabled")
	}
}

func TestRefreshInterFrameReferencesCopiesOldGoldenToAltBeforeGoldenRefresh(t *testing.T) {
	e := newTestEncoder(t)
	fillVP8Image(&e.lastRef.Img, 10)
	fillVP8Image(&e.goldenRef.Img, 20)
	fillVP8Image(&e.altRef.Img, 30)
	fillVP8Image(&e.analysis.Img, 40)

	e.refreshInterFrameReferencesFromAnalysis(vp8enc.InterFrameStateConfig{
		RefreshLast:        true,
		RefreshGolden:      true,
		CopyBufferToAltRef: 2,
	})

	if e.altRef.Img.Y[0] != 20 {
		t.Fatalf("alt Y[0] = %d, want old golden 20", e.altRef.Img.Y[0])
	}
	if e.goldenRef.Img.Y[0] != 40 {
		t.Fatalf("golden Y[0] = %d, want current 40", e.goldenRef.Img.Y[0])
	}
	if e.lastRef.Img.Y[0] != 40 {
		t.Fatalf("last Y[0] = %d, want current 40", e.lastRef.Img.Y[0])
	}
}
