package govpx

import (
	"errors"
	"math"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

// newAutoAltRefTestEncoder constructs a small two-pass VBR encoder with the
// auto-ARF driver enabled and lookahead deep enough that the libvpx-aligned
// default section interval (DEFAULT_GF_INTERVAL=7) can fire at least once.
//
// libvpx vp8/encoder/ratectrl.c calc_pframe_target_size resets
// source_alt_ref_pending=0 on every one-pass frame, so hidden ARF emission is
// gated by `twoPass.enabled()`. The test feeds a synthetic FIRSTPASS_STATS
// section with high IntraError/CodedError ratio + high PcntInter so
// pass2DetectARFPending arms the schedule (the same shape used by
// TestPass2ARFPendingTriggersFromHighMotionSection).
func newAutoAltRefTestEncoder(tb testing.TB) *VP8Encoder {
	tb.Helper()
	const sectionLen = 32
	stats := make([]FirstPassFrameStats, sectionLen)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:    20000,
			CodedError:    200,
			PcntInter:     0.95,
			PcntMotion:    0.4,
			PcntSecondRef: 0.0,
			PcntNeutral:   0.0,
			MVrAbs:        5,
			MVcAbs:        5,
			Count:         1,
			Duration:      1,
		}
	}
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlVBR,
		TargetBitrateKbps:   1500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    240,
		LookaheadFrames:     8,
		AutoAltRef:          true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TwoPassStats:        FinalizeFirstPassStats(stats),
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

// autoAltRefDriverEncodedPacket records one encoded packet plus the parsed
// stream/state headers a parity test needs to inspect.
type autoAltRefDriverEncodedPacket struct {
	data    []byte
	pts     uint64
	keyFrm  bool
	show    bool
	refresh vp8dec.RefreshHeader
}

func encodeAutoAltRefSequence(t *testing.T, e *VP8Encoder, frameCount int) []autoAltRefDriverEncodedPacket {
	t.Helper()
	const width = 32
	const height = 32
	packets := make([]autoAltRefDriverEncodedPacket, 0, frameCount+e.opts.LookaheadFrames)
	dst := make([]byte, 1<<16)
	// Drive the encoder with a deterministic moving-bar pattern so motion
	// search has something non-trivial to chew on, otherwise the inter
	// frames collapse into ZEROMV-LAST and the hidden ARF reduces to a
	// degenerate mode.
	for i := range frameCount {
		img := movingBarTestImage(width, height, i)
		result, err := e.EncodeInto(dst, img, uint64(i)*1000, 1000, 0)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		packets = append(packets, decodeAutoAltRefPacket(t, append([]byte(nil), result.Data...), result))
	}
	for {
		result, err := e.FlushInto(dst)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		packets = append(packets, decodeAutoAltRefPacket(t, append([]byte(nil), result.Data...), result))
	}
	return packets
}

func decodeAutoAltRefPacket(t *testing.T, data []byte, result EncodeResult) autoAltRefDriverEncodedPacket {
	t.Helper()
	header, err := vp8dec.ParseFrameHeader(data)
	if err != nil {
		t.Fatalf("ParseFrameHeader: %v", err)
	}
	state := parseEncoderStateHeader(t, data)
	return autoAltRefDriverEncodedPacket{
		data:    data,
		pts:     result.PTS,
		keyFrm:  header.KeyFrame(),
		show:    header.ShowFrame,
		refresh: state.Refresh,
	}
}

// movingBarTestImage builds a small luma "moving bar" pattern that gives the
// motion search non-zero residual and produces meaningful auto-ARF behavior
// even at low resolution. Chroma is held flat.
func movingBarTestImage(width int, height int, frame int) Image {
	img := testImage(width, height)
	fillImage(img, 96, 128, 128)
	barCol := (frame * 2) % width
	for row := range height {
		for col := range 4 {
			x := (barCol + col) % width
			img.Y[row*img.YStride+x] = 220
		}
	}
	return img
}

// TestAutoAltRefDriverEmitsHiddenFrame asserts the auto-ARF driver inserts at
// least one hidden alt-ref packet (show_frame=0, refresh_alt_ref=1, no
// LAST/GOLDEN refresh) into the output stream when given a 16-frame sequence
// with auto-ARF enabled.
func TestAutoAltRefDriverEmitsHiddenFrame(t *testing.T) {
	e := newAutoAltRefTestEncoder(t)
	packets := encodeAutoAltRefSequence(t, e, 16)
	if len(packets) == 0 {
		t.Fatalf("auto-ARF sequence produced no packets")
	}
	hiddenCount := 0
	for i, p := range packets {
		if p.keyFrm {
			continue
		}
		if p.show {
			continue
		}
		if !p.refresh.RefreshAltRef {
			t.Fatalf("packet %d hidden but RefreshAltRef=false (refresh=%+v)", i, p.refresh)
		}
		if p.refresh.RefreshLast {
			t.Fatalf("packet %d hidden alt-ref unexpectedly refreshes LAST", i)
		}
		if p.refresh.RefreshGolden {
			t.Fatalf("packet %d hidden alt-ref unexpectedly refreshes GOLDEN", i)
		}
		hiddenCount++
	}
	if hiddenCount == 0 {
		t.Fatalf("expected at least one hidden alt-ref packet, got 0 (packet count=%d)", len(packets))
	}
}

// TestAutoAltRefDriverDeferredShowFrameMatchesSource encodes the same
// sequence end-to-end through a decoder and asserts that the deferred show
// frame paired with the hidden ARF decodes within >= 25 dB PSNR of the
// original moving-bar source. The test confirms the driver does not corrupt
// the visible-frame timeline.
func TestAutoAltRefDriverDeferredShowFrameMatchesSource(t *testing.T) {
	e := newAutoAltRefTestEncoder(t)
	const frameCount = 16
	const width = 32
	const height = 32
	sources := make(map[uint64]Image, frameCount)
	for i := range frameCount {
		sources[uint64(i)*1000] = movingBarTestImage(width, height, i)
	}
	packets := encodeAutoAltRefSequence(t, e, frameCount)
	if len(packets) == 0 {
		t.Fatalf("encoder produced no packets")
	}
	// Identify the PTS of the first hidden alt-ref so we can assert quality
	// of the matching deferred show frame after decoding the full bitstream.
	var hiddenPTS uint64
	hiddenFound := false
	for _, p := range packets {
		if !p.keyFrm && !p.show && p.refresh.RefreshAltRef {
			hiddenPTS = p.pts
			hiddenFound = true
			break
		}
	}
	if !hiddenFound {
		t.Fatalf("auto-ARF driver did not emit hidden alt-ref")
	}
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder: %v", err)
	}
	var deferredImg Image
	deferredFound := false
	for _, p := range packets {
		if err := dec.DecodeWithPTS(p.data, p.pts); err != nil {
			t.Fatalf("Decode pts=%d show=%v key=%v: %v", p.pts, p.show, p.keyFrm, err)
		}
		img, ok := dec.NextFrame()
		if !ok {
			// Hidden frame: decoder emits no visible image. Continue.
			continue
		}
		if p.show && p.pts == hiddenPTS {
			// Deferred show frame matching the hidden ARF source.
			deferredImg = cloneAutoAltRefImage(img)
			deferredFound = true
		}
	}
	if !deferredFound {
		t.Fatalf("deferred show frame for hidden ARF pts=%d not seen by decoder", hiddenPTS)
	}
	src, ok := sources[hiddenPTS]
	if !ok {
		t.Fatalf("source frame for hidden ARF pts=%d not in source map", hiddenPTS)
	}
	psnr := encoderValidationImagePSNR(src, deferredImg)
	if math.IsNaN(psnr) {
		t.Fatalf("PSNR is NaN")
	}
	if psnr < 25 {
		t.Fatalf("deferred show frame PSNR = %.2f dB, want >= 25 dB", psnr)
	}
}

// TestAutoAltRefDriverSignBiasUpdatesPostHidden asserts that on the first
// inter show frame after a hidden ARF, the bitstream carries
// AltRefSignBias=true. This is the libvpx parity check that
// `sourceAltRefActive` is set after the hidden frame commits and consumed by
// the next encode.
func TestAutoAltRefDriverSignBiasUpdatesPostHidden(t *testing.T) {
	e := newAutoAltRefTestEncoder(t)
	packets := encodeAutoAltRefSequence(t, e, 16)
	hiddenIndex := -1
	for i, p := range packets {
		if !p.keyFrm && !p.show && p.refresh.RefreshAltRef {
			hiddenIndex = i
			break
		}
	}
	if hiddenIndex < 0 {
		t.Fatalf("auto-ARF driver did not emit hidden alt-ref")
	}
	// Find the next inter show frame after the hidden ARF.
	for i := hiddenIndex + 1; i < len(packets); i++ {
		p := packets[i]
		if p.keyFrm || !p.show {
			continue
		}
		if !p.refresh.AltRefSignBias {
			t.Fatalf("first inter show frame after hidden ARF (idx=%d, pts=%d) has AltRefSignBias=false", i, p.pts)
		}
		return
	}
	t.Fatalf("no inter show frame found after hidden ARF (hidden idx=%d, total packets=%d)", hiddenIndex, len(packets))
}

func TestSourceAltRefShowFrameForcesZeroMVAltRefWhenARNROff(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
	}{
		{name: "rd", deadline: DeadlineBestQuality, cpuUsed: 0},
		{name: "fast", deadline: DeadlineRealtime, cpuUsed: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := NewVP8Encoder(EncoderOptions{
				Width:               32,
				Height:              32,
				FPS:                 30,
				RateControlMode:     RateControlCBR,
				TargetBitrateKbps:   1500,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				Deadline:            tt.deadline,
				CpuUsed:             tt.cpuUsed,
				KeyFrameInterval:    240,
				BufferSizeMs:        600,
				BufferInitialSizeMs: 400,
				BufferOptimalSizeMs: 500,
				ARNRMaxFrames:       0,
			})
			if err != nil {
				t.Fatalf("NewVP8Encoder returned error: %v", err)
			}
			dst := make([]byte, 1<<16)
			keySrc := sourceImageFromImage(movingBarTestImage(32, 32, 0))
			altSrc := sourceImageFromImage(movingBarTestImage(32, 32, 1))

			key, err := e.encodeSourceInto(dst, keySrc, 0, 1, 0, encodeSourceMetadata{})
			if err != nil {
				t.Fatalf("key encodeSourceInto returned error: %v", err)
			}
			if !key.KeyFrame {
				t.Fatalf("key result = inter, want key")
			}
			hidden, err := e.encodeSourceInto(dst, altSrc, 1, 1, autoAltRefHiddenFlags, encodeSourceMetadata{internalInvisible: true})
			if err != nil {
				t.Fatalf("hidden ARF encodeSourceInto returned error: %v", err)
			}
			if hidden.KeyFrame {
				t.Fatalf("hidden ARF result = key, want inter")
			}

			e.altRefSourceValid = true
			e.altRefSourcePTS = 1
			e.sourceAltRefActive = true
			show, err := e.encodeSourceInto(dst, altSrc, 1, 1, 0, encodeSourceMetadata{})
			if err != nil {
				t.Fatalf("source-alt-ref show encodeSourceInto returned error: %v", err)
			}
			if show.KeyFrame {
				t.Fatalf("source-alt-ref show result = key, want inter")
			}
			for i, mode := range e.interFrameModes[:4] {
				if mode.RefFrame != vp8common.AltRefFrame || mode.Mode != vp8common.ZeroMV {
					t.Fatalf("mode[%d] = ref:%v mode:%v, want ALTREF/ZEROMV", i, mode.RefFrame, mode.Mode)
				}
			}
		})
	}
}

func TestTwoPassAutoAltRefDoesNotScheduleWhenStatsRejectARF(t *testing.T) {
	stats := make([]FirstPassFrameStats, 12)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:          1000,
			CodedError:          900,
			SSIMWeightedPredErr: 900,
			PcntInter:           0.50,
			Count:               1,
			Duration:            1,
		}
	}
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 500,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		LookaheadFrames:   8,
		AutoAltRef:        true,
		TwoPassStats:      FinalizeFirstPassStats(stats),
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := sourceImageFromImage(movingBarTestImage(32, 32, 0))
	for i := 0; i < e.opts.LookaheadFrames; i++ {
		if err := e.pushLookahead(src, uint64(i), 1, 0); err != nil {
			t.Fatalf("pushLookahead[%d]: %v", i, err)
		}
	}

	e.autoAltRefMaybeSchedule()

	if e.sourceAltRefPending || e.framesTillAltRefFrame != 0 || e.altRefSourceValid {
		t.Fatalf("two-pass fallback ARF schedule = pending:%t frames:%d valid:%t, want no eager ARF when stats reject it",
			e.sourceAltRefPending, e.framesTillAltRefFrame, e.altRefSourceValid)
	}
}

func TestTwoPassHiddenAltRefChargesBitsWithoutConsumingVisibleStats(t *testing.T) {
	stats := make([]FirstPassFrameStats, 4)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:          1200 + float64(i*100),
			CodedError:          350 + float64(i*50),
			SSIMWeightedPredErr: 350 + float64(i*50),
			PcntInter:           0.75,
			Count:               1,
			Duration:            1,
		}
	}
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 500,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		TwoPassStats:      FinalizeFirstPassStats(stats),
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 1<<16)
	keySrc := sourceImageFromImage(movingBarTestImage(32, 32, 0))
	altSrc := sourceImageFromImage(movingBarTestImage(32, 32, 1))
	showSrc := sourceImageFromImage(movingBarTestImage(32, 32, 2))

	key, err := e.encodeSourceInto(dst, keySrc, 0, 1, 0, encodeSourceMetadata{})
	if err != nil {
		t.Fatalf("key encodeSourceInto returned error: %v", err)
	}
	if !key.KeyFrame || e.frameCount != 1 || e.twoPass.frameIndex != 1 {
		t.Fatalf("after key = key:%t frameCount:%d twoPass:%d, want true/1/1", key.KeyFrame, e.frameCount, e.twoPass.frameIndex)
	}
	afterKeyBitsLeft := e.twoPass.bitsLeft
	afterKeyFramesSinceKey := e.rc.framesSinceKeyframe

	hidden, err := e.encodeSourceInto(dst, altSrc, 1, 1, autoAltRefHiddenFlags, encodeSourceMetadata{internalInvisible: true})
	if err != nil {
		t.Fatalf("hidden ARF encodeSourceInto returned error: %v", err)
	}
	if hidden.KeyFrame || hidden.SizeBytes == 0 {
		t.Fatalf("hidden ARF result = key:%t size:%d, want non-key packet", hidden.KeyFrame, hidden.SizeBytes)
	}
	wantBitsLeft := max(afterKeyBitsLeft-int64(encodedSizeBits(hidden.SizeBytes)), 0)
	if e.frameCount != 1 || e.twoPass.frameIndex != 1 || e.twoPass.bitsLeft != wantBitsLeft {
		t.Fatalf("after hidden ARF = frameCount:%d twoPass:%d bitsLeft:%d, want 1/1/%d",
			e.frameCount, e.twoPass.frameIndex, e.twoPass.bitsLeft, wantBitsLeft)
	}
	if e.rc.framesSinceKeyframe != afterKeyFramesSinceKey {
		t.Fatalf("after hidden ARF framesSinceKeyframe = %d, want unchanged %d",
			e.rc.framesSinceKeyframe, afterKeyFramesSinceKey)
	}

	visible, err := e.encodeSourceInto(dst, showSrc, 2, 1, 0, encodeSourceMetadata{})
	if err != nil {
		t.Fatalf("visible encodeSourceInto returned error: %v", err)
	}
	if visible.KeyFrame || e.frameCount != 2 || e.twoPass.frameIndex != 2 {
		t.Fatalf("after visible = key:%t frameCount:%d twoPass:%d, want false/2/2", visible.KeyFrame, e.frameCount, e.twoPass.frameIndex)
	}
	if e.rc.framesSinceKeyframe != afterKeyFramesSinceKey+1 {
		t.Fatalf("after visible framesSinceKeyframe = %d, want %d",
			e.rc.framesSinceKeyframe, afterKeyFramesSinceKey+1)
	}
}

func TestOnePassHiddenAltRefAccumulatesFullPostPackOverspend(t *testing.T) {
	// One-pass-specific accounting probe. The auto-ARF driver itself is
	// gated on two-pass (see autoAltRefDriverEnabled), but the
	// `encodeSourceInto` post-pack accounting that this test pins runs on
	// any caller-driven hidden ARF emission regardless of pass count.
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    240,
		LookaheadFrames:     8,
		AutoAltRef:          true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 1<<16)
	keySrc := sourceImageFromImage(movingBarTestImage(32, 32, 0))
	altSrc := sourceImageFromImage(movingBarTestImage(32, 32, 1))

	key, err := e.encodeSourceInto(dst, keySrc, 0, 1, 0, encodeSourceMetadata{})
	if err != nil {
		t.Fatalf("key encodeSourceInto returned error: %v", err)
	}
	if !key.KeyFrame {
		t.Fatalf("key result = inter, want key")
	}

	e.rc.gfOverspendBits = 0
	e.rc.nonGFBitrateAdjustment = 0
	e.rc.framesTillGFUpdateDue = 10
	e.rc.interFrameTarget = 1 << 30

	hidden, err := e.encodeSourceInto(dst, altSrc, 1, 1, autoAltRefHiddenFlags, encodeSourceMetadata{internalInvisible: true})
	if err != nil {
		t.Fatalf("hidden ARF encodeSourceInto returned error: %v", err)
	}
	if hidden.KeyFrame || hidden.SizeBytes == 0 {
		t.Fatalf("hidden ARF result = key:%t size:%d, want non-key packet", hidden.KeyFrame, hidden.SizeBytes)
	}

	wantBits := encodedSizeBits(hidden.SizeBytes)
	if e.rc.gfOverspendBits != wantBits {
		t.Fatalf("one-pass hidden ARF gfOverspendBits = %d, want full packet bits %d",
			e.rc.gfOverspendBits, wantBits)
	}
	if e.rc.nonGFBitrateAdjustment != wantBits/10 {
		t.Fatalf("one-pass hidden ARF nonGFBitrateAdjustment = %d, want %d",
			e.rc.nonGFBitrateAdjustment, wantBits/10)
	}
}

// cloneAutoAltRefImage deep-copies a returned decoder Image so subsequent
// NextFrame calls cannot overwrite the buffers.
func cloneAutoAltRefImage(src Image) Image {
	return Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       append([]byte(nil), src.Y...),
		U:       append([]byte(nil), src.U...),
		V:       append([]byte(nil), src.V...),
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
}
