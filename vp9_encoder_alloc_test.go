package govpx

import (
	"bytes"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9EncoderTileRowsSteadyStateAlloc(t *testing.T) {
	const width, height = 1024, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		Threads:      2,
		Log2TileRows: 1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frames := [4]*image.YCbCr{}
	for i := range frames {
		frames[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	dst := make([]byte, 1<<20)
	for i := range frames {
		if _, err := e.EncodeInto(frames[i], dst); err != nil {
			t.Fatalf("warm EncodeInto[%d]: %v", i, err)
		}
	}
	idx := 0
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		frame := frames[idx&3]
		idx++
		if _, err := e.EncodeInto(frame, dst); err != nil {
			t.Fatalf("EncodeInto tile-row alloc run: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("tile-row EncodeInto steady-state allocs = %f, want 0", allocs)
	}
}

// TestVP9EncoderIVFRoundTrip wraps the encoded keyframe in an IVF
// container and round-trips it through the existing IVF parser.
// Confirms the encoder's output is a valid VP9-IVF stream — the
// shape vpxdec --codec=vp9 expects on disk.
func TestVP9EncoderIVFRoundTrip(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               64,
		Height:              64,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          1,
	}
	stream := append(testutil.WriteIVFHeader(header), testutil.WriteIVFFrame(payload, 0)...)

	gotHdr, err := testutil.ParseIVFHeader(stream)
	if err != nil {
		t.Fatalf("ParseIVFHeader: %v", err)
	}
	if gotHdr.FourCC != header.FourCC {
		t.Errorf("FourCC = %v, want VP90", gotHdr.FourCC)
	}
	if gotHdr.Width != 64 || gotHdr.Height != 64 {
		t.Errorf("ivf size = (%d, %d), want (64, 64)", gotHdr.Width, gotHdr.Height)
	}

	offset, err := testutil.FirstIVFFrameOffset(stream)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	frame, _, err := testutil.NextIVFFrame(stream, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}
	if len(frame.Data) != len(payload) {
		t.Errorf("frame size = %d, want %d", len(frame.Data), len(payload))
	}
	for i := range payload {
		if frame.Data[i] != payload[i] {
			t.Errorf("byte %d differs: %#x != %#x", i, frame.Data[i], payload[i])
			break
		}
	}

	// And the recovered payload still parses as a VP9 keyframe.
	var br vp9dec.BitReader
	br.Init(frame.Data)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader on IVF payload: %v", perr)
	}
	if h.FrameType != common.KeyFrame {
		t.Errorf("recovered FrameType = %d, want KeyFrame", h.FrameType)
	}
}

// TestVP9EncoderEncodeIntoSteadyStateAlloc verifies that the
// caller-owned output path allocates only during setup / growth. The
// hot path reuses the compressed-header scratch, partition contexts,
// and MI grid across frames.
func TestVP9EncoderEncodeIntoSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := vp9test.NewYCbCr(256, 192, 128, 128, 128)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(vp9EncoderKeyframeAllocRuns, func() {
		e.frameIndex = 0
		n, err = e.EncodeInto(img, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderEncodeIntoSourceKeyframeSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := vp9test.NewYCbCr(256, 192, 87, 144, 39)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(vp9EncoderKeyframeAllocRuns, func() {
		e.frameIndex = 0
		n, err = e.EncodeInto(img, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto source keyframe: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto source keyframe wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto source keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9EncoderEncodeIntoInterSteadyStateAlloc verifies that visible
// inter-frame header/mode emission reuses the keyframe-allocated scratch,
// partition contexts, and MI grid.
func TestVP9EncoderEncodeIntoInterSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := vp9test.NewYCbCr(256, 192, 128, 128, 128)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm inter EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		n, err = e.EncodeInto(img, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderEncodeIntoInterResidueSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	keySrc := vp9test.NewYCbCr(256, 192, 81, 123, 210)
	interSrc := vp9test.NewYCbCr(256, 192, 113, 123, 210)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm inter EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto inter residue: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto inter residue wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("EncodeInto inter residue steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderCyclicRefreshAQInterSteadyStateAlloc(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              128,
		Height:             128,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		AQMode:             VP9AQCyclicRefresh,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := vp9test.NewYCbCr(128, 128, 81, 123, 210)
	interSrc := vp9test.NewYCbCr(128, 128, 113, 123, 210)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm cyclic inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto cyclic inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto cyclic inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 cyclic AQ inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderActiveMapInterSteadyStateAlloc(t *testing.T) {
	const width, height = 128, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	activeMap := make([]uint8, rows*cols)
	for row := range rows {
		for col := range cols {
			activeMap[row*cols+col] = 1
			if (row+col)&1 == 0 {
				activeMap[row*cols+col] = 0
			}
		}
	}
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	keySrc := vp9test.NewYCbCr(width, height, 81, 123, 210)
	interSrc := vp9test.NewYCbCr(width, height, 113, 123, 210)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm active-map inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto active-map inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto active-map inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 active-map inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderROIMapInterSteadyStateAlloc(t *testing.T) {
	const width, height = 128, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	roi := ROIMap{
		Enabled:   true,
		Rows:      miRows,
		Cols:      miCols,
		SegmentID: make([]uint8, miRows*miCols),
	}
	for row := range miRows {
		for col := range miCols {
			if row == col || row+col == miCols-1 {
				roi.SegmentID[row*miCols+col] = 1
			}
		}
	}
	roi.DeltaQuantizer[1] = -4
	roi.DeltaLoopFilter[1] = 3
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	keySrc := vp9test.NewYCbCr(width, height, 81, 123, 210)
	interSrc := vp9test.NewYCbCr(width, height, 113, 123, 210)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm ROI inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto ROI inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto ROI inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 ROI inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderDenoiserInterSteadyStateAlloc(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := vp9test.NewYCbCr(width, height, 100, 96, 160)
	interSrc := vp9test.NewYCbCr(width, height, 102, 98, 158)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	var keyRef vp9ReferenceFrame
	keyRef.store(e.reconFrame)
	keyAvg := *image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
	copyVP9LookaheadImage(&keyAvg, &e.denoiser.runningAvg[vp9DenoiserAvgLast],
		width, height)
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("warm denoiser inter EncodeInto: %v", err)
	}

	var n int
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.refFrames[0].store(keyRef.img)
		copyVP9LookaheadImage(&e.denoiser.runningAvg[vp9DenoiserAvgLast],
			&keyAvg, width, height)
		e.denoiser.reset = false
		n, err = e.EncodeInto(interSrc, dst)
	})
	if err != nil {
		t.Fatalf("EncodeInto denoiser inter: %v", err)
	}
	if n == 0 {
		t.Fatal("EncodeInto denoiser inter wrote no bytes")
	}
	if allocs != 0 {
		t.Fatalf("VP9 denoiser inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderAutoAltRefLookaheadSteadyStateAlloc(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		CpuUsed:            4,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  300,
		LookaheadFrames:    4,
		AutoAltRef:         true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	sources := [8]*image.YCbCr{
		vp9test.NewYCbCr(width, height, 80, 128, 128),
		vp9test.NewYCbCr(width, height, 96, 128, 128),
		vp9test.NewYCbCr(width, height, 112, 128, 128),
		vp9test.NewYCbCr(width, height, 128, 128, 128),
		vp9test.NewYCbCr(width, height, 144, 128, 128),
		vp9test.NewYCbCr(width, height, 160, 128, 128),
		vp9test.NewYCbCr(width, height, 176, 128, 128),
		vp9test.NewYCbCr(width, height, 192, 128, 128),
	}
	dst := make([]byte, 65536)
	for i := range 5 {
		_, err := e.EncodeIntoWithResult(sources[i], dst)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("warm EncodeIntoWithResult frame %d: %v", i, err)
		}
	}
	if !e.autoAltRefPendingSet || !e.autoAltRefEmitted {
		t.Fatalf("auto-alt-ref warm state = pending:%t emitted:%t, want true/true",
			e.autoAltRefPendingSet, e.autoAltRefEmitted)
	}

	idx := 5
	var result VP9EncodeResult
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		result, err = e.EncodeIntoWithResult(sources[idx&7], dst)
		idx++
	})
	if err != nil {
		t.Fatalf("EncodeIntoWithResult auto-alt-ref steady state: %v", err)
	}
	if result.Dropped || !result.ShowFrame || len(result.Data) == 0 {
		t.Fatalf("auto-alt-ref steady packet = dropped:%t show:%t bytes:%d, want visible packet",
			result.Dropped, result.ShowFrame, len(result.Data))
	}
	if allocs != 0 {
		t.Fatalf("VP9 auto-alt-ref lookahead steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderCBRDropSteadyStateAlloc(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              256,
		Height:             192,
		FPS:                30,
		TargetBitrateKbps:  1,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := vp9test.NewYCbCr(256, 192, 128, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithResult(img, dst); err != nil {
		t.Fatalf("warm keyframe EncodeIntoWithResult: %v", err)
	}

	var result VP9EncodeResult
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		e.frameIndex = 1
		e.rc.bufferLevelBits = -1
		result, err = e.EncodeIntoWithResult(img, dst)
	})
	if err != nil {
		t.Fatalf("drop EncodeIntoWithResult: %v", err)
	}
	if !result.Dropped || len(result.Data) != 0 {
		t.Fatalf("drop result = dropped:%t data:%d, want dropped empty output",
			result.Dropped, len(result.Data))
	}
	if allocs != 0 {
		t.Fatalf("VP9 CBR drop steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9EncoderAllocatingWrapperGrowsForLargePacket(t *testing.T) {
	const width, height = 512, 512
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 1,
	})
	img := vp9test.NewCheckerYCbCr(width, height, 16, 240, 96, 224)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode large keyframe: %v", err)
	}
	if len(packet) <= 65536 {
		t.Fatalf("large keyframe packet size = %d, want > 65536 to cover allocating growth", len(packet))
	}
	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo large keyframe: %v", err)
	}
	if !info.KeyFrame || info.Width != width || info.Height != height {
		t.Fatalf("large keyframe info = %+v, want %dx%d keyframe", info, width, height)
	}
}

func TestVP9EncoderBufferFullInterRetryPreservesFrameContext(t *testing.T) {
	const width, height = 64, 64
	keySrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	interSrc := vp9test.NewMotionYCbCr(width, height)

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, err := e.EncodeInto(interSrc, make([]byte, 512)); !errors.Is(err, vp9enc.ErrPackBufferFull) &&
		!errors.Is(err, vp9enc.ErrTileBufferFull) {
		t.Fatalf("short inter EncodeInto error = %v, want VP9 buffer-full error", err)
	}
	got, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("retry Encode inter: %v", err)
	}

	fresh, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if _, err := fresh.Encode(keySrc); err != nil {
		t.Fatalf("fresh Encode keyframe: %v", err)
	}
	want, err := fresh.Encode(interSrc)
	if err != nil {
		t.Fatalf("fresh Encode inter: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("inter retry changed packet after buffer-full failure: got %x want %x", got, want)
	}
}

func TestVP9EncoderEncodeIntoRejectsTinyBuffer(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	if _, err := e.EncodeInto(img, make([]byte, vp9MinEncodeIntoBuffer-1)); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("tiny EncodeInto error = %v, want ErrBufferTooSmall", err)
	}
}
