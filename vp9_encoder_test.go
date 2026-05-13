package govpx

import (
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestNewVP9EncoderRequiresDimensions: Width and Height must both be
// positive; zero or negative values get rejected with
// ErrInvalidConfig.
func TestNewVP9EncoderRequiresDimensions(t *testing.T) {
	cases := []VP9EncoderOptions{
		{Width: 0, Height: 480},
		{Width: 640, Height: 0},
		{Width: -1, Height: 480},
	}
	for i, opts := range cases {
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: err = %v, want ErrInvalidConfig", i, err)
		}
	}
}

// TestNewVP9EncoderRejectsBadOptions covers the per-field bounds
// checks beyond the dimension gate.
func TestNewVP9EncoderRejectsBadOptions(t *testing.T) {
	base := VP9EncoderOptions{Width: 320, Height: 240}
	type bad struct {
		mutate func(*VP9EncoderOptions)
		want   error
	}
	cases := []bad{
		{func(o *VP9EncoderOptions) { o.Threads = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TargetBitrateKbps = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = 256 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.MaxKeyframeInterval = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.FPS = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TimebaseNum = 1 }, ErrInvalidConfig}, // missing Den
		{func(o *VP9EncoderOptions) { o.TimebaseDen = 1 }, ErrInvalidConfig}, // missing Num
	}
	for i, c := range cases {
		opts := base
		c.mutate(&opts)
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, c.want) {
			t.Errorf("case %d: err = %v, want %v", i, err, c.want)
		}
	}
}

// TestNewVP9EncoderAcceptsMinimalOptions: a bare {Width,Height}
// works.
func TestNewVP9EncoderAcceptsMinimalOptions(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := e.Codec(); got != CodecVP9 {
		t.Errorf("Codec() = %v, want CodecVP9", got)
	}
}

// TestVP9EncoderKeyframeStubProducesParseableBitstream: the
// stub-keyframe path emits a Block64x64 PartitionNone + DC-pred +
// skip=1 frame whose every layer parses cleanly through the
// existing decoder primitives.
func TestVP9EncoderKeyframeStubProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Encode returned empty bytes")
	}

	// Layer 1: uncompressed header.
	var br vp9dec.BitReader
	br.Init(got)
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader: %v", perr)
	}
	if h.Width != 64 || h.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", h.Width, h.Height)
	}
	if h.FrameType != common.KeyFrame {
		t.Errorf("FrameType = %d, want KeyFrame", h.FrameType)
	}
	if h.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0 (compressed header empty)")
	}
	uncSize := br.BytesRead()

	// Layer 2: compressed header (no-update body — every prob slot
	// stays at the libvpx default).
	compEnd := uncSize + int(h.FirstPartitionSize)
	if compEnd > len(got) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(got))
	}
	var cr bitstream.Reader
	if err := cr.Init(got[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     false,
		IntraOnly:    true,
		KeyFrame:     true,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Only4x4 {
		t.Errorf("TxMode = %d, want Only4x4", out.TxMode)
	}

	// Layer 3: tile body. The 1-tile case has no size prefix; the
	// SB walk starts immediately after the compressed header.
	var tr bitstream.Reader
	if err := tr.Init(got[compEnd:]); err != nil {
		t.Fatalf("tile reader Init: %v", err)
	}
	aboveCtx := make([]int8, 16)
	leftCtx := make([]int8, common.MiBlockSize)
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, 64),
		MiCols:             8,
	}
	var seg vp9dec.SegmentationParams

	// Single SB at Block64x64 with PartitionNone — walk the
	// partition + read the per-block intra mode.
	bsl := int(common.BWidthLog2Lookup[common.Block64x64])
	bs := (1 << uint(bsl)) / 4
	ctx := vp9dec.PartitionPlaneContext(aboveCtx, leftCtx, 0, 0, common.Block64x64)
	// Keyframes use vp9_kf_partition_probs, not fc.PartitionProb —
	// see set_partition_probs in libvpx's vp9_onyxc_int.h.
	probs := tables.KfPartitionProbs[ctx][:]
	miRows := int((h.Height + 7) >> 3)
	miCols := int((h.Width + 7) >> 3)
	hasRows := bs < miRows
	hasCols := bs < miCols
	partition := vp9dec.ReadPartition(&tr, probs, hasRows, hasCols)
	if partition != common.PartitionNone {
		t.Errorf("root partition = %d, want PartitionNone", partition)
	}

	leafMi := &vp9dec.NeighborMi{SbType: common.Block64x64}
	mode := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
		Reader:   &tr,
		Fc:       &fc,
		Seg:      &seg,
		Maps:     &maps,
		TxMode:   common.Only4x4,
		MiOffset: 0,
		XMis:     8, YMis: 8,
	}, leafMi)
	if leafMi.Mode != common.DcPred {
		t.Errorf("Y mode = %d, want DcPred", leafMi.Mode)
	}
	if leafMi.Skip != 1 {
		t.Errorf("Skip = %d, want 1", leafMi.Skip)
	}
	if mode.UvMode != common.DcPred {
		t.Errorf("UV mode = %d, want DcPred", mode.UvMode)
	}
	if leafMi.RefFrame[0] != vp9dec.IntraFrame {
		t.Errorf("RefFrame[0] = %d, want IntraFrame", leafMi.RefFrame[0])
	}
}

// TestVP9EncoderKeyframeMultiSb: 128x64 frame → 2 SBs side-by-side.
// Confirms the SB walker emits 2 PartitionNone leaves in row-major
// order and both decode through the per-block keyframe driver.
func TestVP9EncoderKeyframeMultiSb(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 128, 64), image.YCbCrSubsampleRatio420)
	got, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(got)
	h, _ := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	uncSize := br.BytesRead()
	compEnd := uncSize + int(h.FirstPartitionSize)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var cr bitstream.Reader
	cr.Init(got[uncSize:compEnd])
	vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless: false, IntraOnly: true, KeyFrame: true,
		InterpFilter: vp9dec.InterpEighttap,
	})

	var tr bitstream.Reader
	tr.Init(got[compEnd:])
	miRows := int((h.Height + 7) >> 3)
	miCols := int((h.Width + 7) >> 3)
	aboveCtx := make([]int8, miCols)
	leftCtx := make([]int8, common.MiBlockSize)
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: make([]uint8, miRows*miCols),
		MiCols:             miCols,
	}
	var seg vp9dec.SegmentationParams
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	miAt := func(r, c int) *vp9dec.NeighborMi {
		if r < 0 || c < 0 || r >= miRows || c >= miCols {
			return nil
		}
		return &miGrid[r*miCols+c]
	}
	fillMi := func(r, c int, bsize common.BlockSize, mi vp9dec.NeighborMi) {
		rows := int(common.Num8x8BlocksHighLookup[bsize])
		cols := int(common.Num8x8BlocksWideLookup[bsize])
		for rr := 0; rr < rows && r+rr < miRows; rr++ {
			row := miGrid[(r+rr)*miCols:]
			for cc := 0; cc < cols && c+cc < miCols; cc++ {
				row[c+cc] = mi
			}
		}
	}

	// Half-step (hbs) for Block64x64 in mi units: (1 << bsl) / 4 = 4.
	const hbs = 4
	walked := 0
	for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
		ctx := vp9dec.PartitionPlaneContext(aboveCtx, leftCtx, 0, miCol, common.Block64x64)
		probs := tables.KfPartitionProbs[ctx][:]
		hasRows := (0 + hbs) < miRows
		hasCols := (miCol + hbs) < miCols
		p := vp9dec.ReadPartition(&tr, probs, hasRows, hasCols)
		if p != common.PartitionNone {
			t.Errorf("SB at miCol=%d: partition = %d, want PartitionNone", miCol, p)
		}
		leafMi := &vp9dec.NeighborMi{SbType: common.Block64x64}
		mode := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
			Reader: &tr, Fc: &fc, Seg: &seg, Maps: &maps,
			TxMode:   common.Only4x4,
			MiOffset: miCol, XMis: common.MiBlockSize, YMis: common.MiBlockSize,
			Above: miAt(-1, miCol),
			Left:  miAt(0, miCol-1),
		}, leafMi)
		if leafMi.Mode != common.DcPred || mode.UvMode != common.DcPred {
			t.Errorf("SB at miCol=%d: Y=%d UV=%d, want DcPred/DcPred",
				miCol, leafMi.Mode, mode.UvMode)
		}
		fillMi(0, miCol, common.Block64x64, *leafMi)
		// Update partition context (decoder side mirror of encoder stamp).
		vp9dec.UpdatePartitionContext(aboveCtx, leftCtx, 0, miCol,
			common.Block64x64, common.MiBlockSize)
		walked++
	}
	if walked != 2 {
		t.Errorf("walked %d SBs, want 2", walked)
	}
}

// TestVP9EncoderIVFRoundTrip wraps the encoded keyframe in an IVF
// container and round-trips it through the existing IVF parser.
// Confirms the encoder's output is a valid VP9-IVF stream — the
// shape vpxdec --codec=vp9 expects on disk.
func TestVP9EncoderIVFRoundTrip(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
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
	img := image.NewYCbCr(image.Rect(0, 0, 256, 192), image.YCbCrSubsampleRatio420)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(100, func() {
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

// TestVP9EncoderClose: after Close, Encode/EncodeInto return
// ErrClosed.
func TestVP9EncoderClose(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	img := image.NewYCbCr(image.Rect(0, 0, 320, 240), image.YCbCrSubsampleRatio420)
	if _, err := e.Encode(img); !errors.Is(err, ErrClosed) {
		t.Errorf("Encode after Close err = %v, want ErrClosed", err)
	}
}

// TestVP9EncoderIsKeyFrameNextCadence: first frame is always a key;
// later frames key on MaxKeyframeInterval boundaries (default 128).
func TestVP9EncoderIsKeyFrameNextCadence(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: 320, Height: 240, MaxKeyframeInterval: 4,
	})
	if !e.IsKeyFrameNext() {
		t.Error("first frame should be key")
	}
	// Pretend we encoded one frame.
	e.frameIndex = 1
	if e.IsKeyFrameNext() {
		t.Error("frame 1 should NOT be key when cadence=4")
	}
	e.frameIndex = 4
	if !e.IsKeyFrameNext() {
		t.Error("frame 4 should be key (cadence boundary)")
	}
	// After Close → never key.
	e.Close()
	if e.IsKeyFrameNext() {
		t.Error("closed encoder should never report key")
	}
}
