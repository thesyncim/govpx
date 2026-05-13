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

func newVP9YCbCrForTest(width, height int, y, u, v byte) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	fillVP9YCbCrForTest(img, y, u, v)
	return img
}

func fillVP9YCbCrForTest(img *image.YCbCr, y, u, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.Cb {
		img.Cb[i] = u
	}
	for i := range img.Cr {
		img.Cr[i] = v
	}
}

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

func TestVP9EncoderRejectsInvalidSourceShape(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	dst := make([]byte, 1024)

	if _, err := e.EncodeInto(nil, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil source err = %v, want ErrInvalidConfig", err)
	}

	wrongSize := image.NewYCbCr(image.Rect(0, 0, 32, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(wrongSize, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-size source err = %v, want ErrInvalidConfig", err)
	}

	wrongChroma := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio444)
	if _, err := e.EncodeInto(wrongChroma, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-chroma source err = %v, want ErrInvalidConfig", err)
	}

	valid := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(valid, nil); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("empty dst err = %v, want ErrBufferTooSmall", err)
	}
}

// TestVP9EncoderKeyframeStubProducesParseableBitstream: the
// zero-residue keyframe path emits a Block64x64 PartitionNone + DC-pred +
// skip=1 frame whose every layer parses cleanly through the
// existing decoder primitives.
func TestVP9EncoderKeyframeStubProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9YCbCrForTest(64, 64, 128, 128, 128)
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

func TestVP9EncoderKeyframeConstantSourceRoundTrip(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 80})
	img := newVP9YCbCrForTest(96, 80, 91, 143, 37)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after source-backed keyframe")
	}
	assertVP9FilledFrame(t, frame, 96, 80, 91, 143, 37)
}

func TestVP9EncoderInterSkipRepeatsReconstructedReference(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 80})
	keySrc := newVP9YCbCrForTest(96, 80, 82, 123, 211)
	interSrc := newVP9YCbCrForTest(96, 80, 201, 44, 19)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	assertVP9FilledFrame(t, frame, 96, 80, 82, 123, 211)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter frame")
	}
	assertVP9FilledFrame(t, frame, 96, 80, 82, 123, 211)
}

// TestVP9EncoderInterSkipProducesParseableBitstream covers the public
// second-frame path: a visible LAST/ZeroMv skipped inter frame whose
// reference dimensions come from the preceding keyframe.
func TestVP9EncoderInterSkipProducesParseableBitstream(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if len(inter) == 0 {
		t.Fatal("Encode returned empty inter frame")
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, perr := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", perr)
	}

	var interBR vp9dec.BitReader
	interBR.Init(inter)
	refDims := func(slot uint8) (uint32, uint32) {
		if slot != 0 {
			t.Fatalf("inter header requested ref slot %d, want 0", slot)
		}
		return 64, 64
	}
	interHeader, perr := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader, refDims)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", perr)
	}
	if interHeader.FrameType != common.InterFrame {
		t.Errorf("FrameType = %d, want InterFrame", interHeader.FrameType)
	}
	if !interHeader.ShowFrame {
		t.Error("ShowFrame = false, want true")
	}
	if interHeader.IntraOnly {
		t.Error("IntraOnly = true, want false")
	}
	if interHeader.RefreshFrameFlags != 1 {
		t.Errorf("RefreshFrameFlags = %#x, want 0x1", interHeader.RefreshFrameFlags)
	}
	if interHeader.Width != 64 || interHeader.Height != 64 {
		t.Errorf("size = (%d, %d), want (64, 64)", interHeader.Width, interHeader.Height)
	}
	if interHeader.InterRef.RefIndex != [3]uint8{0, 0, 0} {
		t.Errorf("RefIndex = %v, want [0 0 0]", interHeader.InterRef.RefIndex)
	}
	if interHeader.InterRef.SignBias != [3]uint8{0, 0, 0} {
		t.Errorf("SignBias = %v, want [0 0 0]", interHeader.InterRef.SignBias)
	}
	if interHeader.AllowHighPrecisionMv {
		t.Error("AllowHighPrecisionMv = true, want false")
	}
	if interHeader.InterpFilter != vp9dec.InterpEighttap {
		t.Errorf("InterpFilter = %d, want Eighttap", interHeader.InterpFilter)
	}
	if interHeader.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0 (compressed header empty)")
	}

	uncSize := interBR.BytesRead()
	compEnd := uncSize + int(interHeader.FirstPartitionSize)
	if compEnd > len(inter) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(inter))
	}
	var cr bitstream.Reader
	if err := cr.Init(inter[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            false,
		KeyFrame:             false,
		InterpFilter:         vp9dec.InterpEighttap,
		AllowHighPrecisionMv: false,
		CompoundRefAllowed:   false,
	})
	if cr.HasError() {
		t.Fatal("compressed header reader reported over-read")
	}
	if out.TxMode != common.Only4x4 {
		t.Errorf("TxMode = %d, want Only4x4", out.TxMode)
	}
	if out.ReferenceMode != vp9dec.SingleReference {
		t.Errorf("ReferenceMode = %d, want SingleReference", out.ReferenceMode)
	}
	if compEnd >= len(inter) {
		t.Fatal("inter frame has no tile payload")
	}
}

// TestVP9EncoderKeyframeMultiSb: 128x64 frame → 2 SBs side-by-side.
// Confirms the SB walker emits 2 PartitionNone leaves in row-major
// order and both decode through the per-block keyframe driver.
func TestVP9EncoderKeyframeMultiSb(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := newVP9YCbCrForTest(128, 64, 128, 128, 128)
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
	img := newVP9YCbCrForTest(64, 64, 128, 128, 128)
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
	img := newVP9YCbCrForTest(256, 192, 128, 128, 128)
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

func TestVP9EncoderEncodeIntoSourceKeyframeSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := newVP9YCbCrForTest(256, 192, 87, 144, 39)
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
	img := newVP9YCbCrForTest(256, 192, 128, 128, 128)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm keyframe EncodeInto: %v", err)
	}
	if _, err := e.EncodeInto(img, dst); err != nil {
		t.Fatalf("warm inter EncodeInto: %v", err)
	}

	var n int
	var err error
	allocs := testing.AllocsPerRun(100, func() {
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
