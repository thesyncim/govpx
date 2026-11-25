package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"image"
	"testing"
)

// TestVP9EncoderInterDisplayedForceArfZeroMotion pins libvpx's
// ref_frame_sign_bias semantics for an externally-driven FORCE_ALTREF in
// display order. set_ref_sign_bias (vp9/encoder/vp9_encoder.c:4806-4821)
// computes cm->ref_frame_sign_bias[ref] = cur_frame_index <
// ref_buf->frame_index, and set_frame_index (vp9_encoder.c:5029-5038)
// stamps each buffer with current_video_frame + arf_src_offset. On the
// one-pass realtime / externally-flag-driven path arf_src_offset is 0, so a
// FORCE_ALTREF buffer refreshed at an earlier display frame is stamped with
// a *lower* frame_index than the frame referencing it: the ALTREF sign bias
// is therefore 0, not 1. With all three references sharing sign bias 0,
// vp9_setup_compound_reference_mode disallows compound, so the inter frame
// resolves to a single LAST reference.
//
// Oracle-verified: encoding this exact (key, displayed FORCE_ALTREF, inter)
// schedule through vpxenc-vp9-frameflags at --cpu-used=1 emits an inter
// uncompressed header byte 3 of 0x92 (ALTREF ref_frame_sign_bias bit = 0).
// The previous expectation (compound prediction) reflected a non-libvpx
// per-buffer "FORCE_ARF ⇒ sign bias 1" heuristic that has been removed.
func TestVP9EncoderInterDisplayedForceArfZeroMotion(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundAverageYCbCr(width, height, -32)
	mid := vp9test.NewCompoundAverageYCbCr(width, height, 0)
	high := vp9test.NewCompoundAverageYCbCr(width, height, 32)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after inter frame")
	}
	got := d.miGrid[0]
	// libvpx: no compound (ALTREF sign bias 0), single LAST reference.
	if got.RefFrame[1] > vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want single reference (no compound)", got.RefFrame)
	}
	if got.RefFrame[0] != vp9dec.LastFrame {
		t.Fatalf("top-left primary ref = %d, want LAST_FRAME", got.RefFrame[0])
	}
	if got.Mode != common.ZeroMv && got.Mode != common.NearestMv && got.Mode != common.NearMv {
		t.Fatalf("top-left mode = %d, want zero-motion inter mode", got.Mode)
	}
}

// TestVP9EncoderInterForceArfNewMvTranslated checks the single-reference NewMv
// path for an externally-driven FORCE_ALTREF in display order. As in
// TestVP9EncoderInterDisplayedForceArfZeroMotion, libvpx set_ref_sign_bias
// leaves the ALTREF sign bias at 0 (oracle byte 3 = 0x92), so compound is
// disallowed; the +8px horizontally translated source resolves to a single
// ALTREF NewMv tracking that motion.
func TestVP9EncoderInterForceArfNewMvTranslated(t *testing.T) {
	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundPairYCbCr(width, height, false)
	high := vp9test.NewCompoundPairYCbCr(width, height, true)
	mid := shiftedVP9ReferenceYCbCrForTest(
		vp9ImageFromYCbCrForTest(vp9test.AverageYCbCr(low, high)),
		8, 0)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after motion frame")
	}
	got := d.miGrid[0]
	// libvpx: no compound (ALTREF sign bias 0); single reference NewMv.
	if got.RefFrame[1] > vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want single reference (no compound)", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left mode = %d, want NewMv", got.Mode)
	}
	if got.Mv[0].Col < 56 || got.Mv[0].Col > 72 ||
		got.Mv[0].Row < -8 || got.Mv[0].Row > 8 {
		t.Fatalf("top-left MV = %+v, want ref near +8px horizontal motion", got.Mv[0])
	}
}

// TestVP9EncoderInterForceArfNewMvStationary checks the single-reference NewMv
// path for an externally-driven FORCE_ALTREF in display order with a source
// that averages a stationary half with an +8px translated half. libvpx
// set_ref_sign_bias leaves the ALTREF sign bias at 0 (oracle byte 3 = 0x92),
// so compound is disallowed and the block resolves to a single LAST reference
// NewMv near zero (tracking the stationary, lower-cost half).
func TestVP9EncoderInterForceArfNewMvStationary(t *testing.T) {
	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height,
		Deadline: DeadlineBestQuality, CpuUsed: 1,
	})
	low := vp9test.NewCompoundPairYCbCr(width, height, false)
	high := vp9test.NewCompoundPairYCbCr(width, height, true)
	shiftedHigh := shiftedVP9ReferenceYCbCrForTest(vp9ImageFromYCbCrForTest(high), 8, 0)
	mid := vp9test.AverageYCbCr(low, shiftedHigh)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode asymmetric motion inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, packet := range [][]byte{key, alt, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame returned !ok after packet %d", i)
		}
	}
	got := d.miGrid[0]
	// libvpx: no compound (ALTREF sign bias 0); single reference NewMv.
	if got.RefFrame[1] > vp9dec.IntraFrame {
		t.Fatalf("top-left ref pair = %v, want single reference (no compound)", got.RefFrame)
	}
	if got.Mode != common.NewMv {
		t.Fatalf("top-left mode = %d, want NewMv", got.Mode)
	}
	if got.Mv[0].Col < -4 || got.Mv[0].Col > 4 ||
		got.Mv[0].Row < -4 || got.Mv[0].Row > 4 {
		t.Fatalf("single-ref MV = %+v, want near zero (stationary half)", got.Mv[0])
	}
}

// withDeepVP9InterRDPartition routes pickVP9InterPartitionBlockSize through the
// genuine depth-first pickVP9InterPartitionRD recursion (instead of the proven
// no-op rdPickVP9InterPartition skeleton) for the duration of a test. The flag
// defaults to false so production encodes stay on the skeleton; these tests flip
// it to exercise the deep recursion's serialization through the real
// writeVP9ModesSb bitstream walker.
func withDeepVP9InterRDPartition(t *testing.T) {
	t.Helper()
	prev := vp9InterUseDeepRDPartition
	vp9InterUseDeepRDPartition = true
	t.Cleanup(func() { vp9InterUseDeepRDPartition = prev })
}

// assertVP9InterPartitionTreeRoundTrips re-derives the partition tree from a
// freshly decoded mi grid exactly as the encoder's writeVP9ModesSb walker did
// (PartitionLookup[bsl][mi.SbType] at each node, descending on PARTITION_SPLIT)
// and verifies that every emitted partition geometry stays inside the frame and
// the per-cell SbType the writer would re-derive matches the decoder's grid.
// A serialization desync (the bug this test pins) corrupts the decoder's
// partition tree and is caught either by Decode failing outright or by this
// re-derivation walking off a non-leaf cell.
func assertVP9InterPartitionTreeRoundTrips(t *testing.T, d *VP9Decoder,
	miRows, miCols int,
) {
	t.Helper()
	var walk func(miRow, miCol int, bsize common.BlockSize)
	walk = func(miRow, miCol int, bsize common.BlockSize) {
		if miRow >= miRows || miCol >= miCols {
			return
		}
		bsl := int(common.BWidthLog2Lookup[bsize])
		bs := (1 << uint(bsl)) / 4
		mi := d.miGrid[miRow*miCols+miCol]
		partition := common.PartitionLookup[bsl][mi.SbType]
		subsize := common.SubsizeLookup[partition][bsize]
		if subsize >= common.BlockSizes {
			t.Fatalf("node (%d,%d,%d): SbType %d -> partition %d yields invalid subsize",
				miRow, miCol, bsize, mi.SbType, partition)
		}
		if subsize < common.Block8x8 {
			return
		}
		switch partition {
		case common.PartitionNone, common.PartitionHorz, common.PartitionVert:
			return
		default: // PARTITION_SPLIT
			walk(miRow, miCol, subsize)
			walk(miRow, miCol+bs, subsize)
			walk(miRow+bs, miCol, subsize)
			walk(miRow+bs, miCol+bs, subsize)
		}
	}
	for miRow := 0; miRow < miRows; miRow += int(common.MiBlockSize) {
		for miCol := 0; miCol < miCols; miCol += int(common.MiBlockSize) {
			walk(miRow, miCol, common.Block64x64)
		}
	}
}

// TestVP9InterPartitionRDSerializesDecodable is the pin for the
// pickVP9InterPartitionRD serialization fix. With the genuine depth-first RD
// recursion active, the planted-motion inter frames it produces must encode to
// VALID, self-consistent VP9 partition trees: they decode cleanly and the
// partition tree the decoder reconstructs round-trips through the same
// PartitionLookup re-derivation the bitstream writer used. Before the
// partition-context-neutrality fix in pickVP9InterPartitionRD these frames
// decoded as "govpx: invalid VP9 data" because the RD picker left the shared
// above/left segmentation context stamped, so the writer's WritePartitionForBlock
// read an already-updated context and UpdatePartitionContext double-stamped it.
func TestVP9InterPartitionRDSerializesDecodable(t *testing.T) {
	withDeepVP9InterRDPartition(t)
	tests := []struct {
		name          string
		width, height int
		inter         func(t *testing.T, ref Image) *image.YCbCr
	}{
		{
			name: "quadrant-motion-64x64", width: 64, height: 64,
			inter: func(t *testing.T, ref Image) *image.YCbCr {
				return quadrantShiftedVP9ReferenceYCbCrForTest(ref,
					image.Point{X: 8}, image.Point{X: -8},
					image.Point{Y: 8}, image.Point{Y: -8})
			},
		},
		{
			name: "horizontal-mixed-64x64", width: 64, height: 64,
			inter: func(t *testing.T, ref Image) *image.YCbCr {
				return splitShiftedVP9ReferenceYCbCrForTest(ref, 8, -8)
			},
		},
		{
			name: "eighth-pel-128x64", width: 128, height: 64,
			inter: func(t *testing.T, ref Image) *image.YCbCr {
				return predictedVP9ReferenceYCbCrForTest(t, ref, vp9dec.MV{Col: 57})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := NewVP9Encoder(VP9EncoderOptions{
				Width: tc.width, Height: tc.height, CpuUsed: -3,
			})
			keySrc := vp9test.NewMotionYCbCr(tc.width, tc.height)
			key, err := e.Encode(keySrc)
			if err != nil {
				t.Fatalf("Encode keyframe: %v", err)
			}
			interSrc := tc.inter(t, e.refFrames[0].img)
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
			if _, ok := d.NextFrame(); !ok {
				t.Fatal("NextFrame !ok after keyframe")
			}
			if err := d.Decode(inter); err != nil {
				t.Fatalf("Decode inter (deep RD partition) failed: %v", err)
			}
			miCols := (tc.width + 7) >> 3
			miRows := (tc.height + 7) >> 3
			assertVP9InterPartitionTreeRoundTrips(t, d, miRows, miCols)
			if _, ok := d.NextFrame(); !ok {
				t.Fatal("NextFrame !ok after deep-RD inter frame")
			}
		})
	}
}

// TestVP9InterPartitionRDPreservesPartitionContext pins the precise serialization
// fix at the function boundary: a single pickVP9InterPartitionRD call must be net
// partition-context-neutral. The committed scorer it runs fills the mi grid (the
// bitstream writer's source of truth) but must restore the shared above/left
// segmentation context to its entry state so writeVP9ModesSb's own
// UpdatePartitionContext performs the single canonical stamp. Any leaked context
// update is exactly what desynced the decoded partition tree.
func TestVP9InterPartitionRDPreservesPartitionContext(t *testing.T) {
	withDeepVP9InterRDPartition(t)
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := quadrantShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img,
		image.Point{X: 8}, image.Point{X: -8},
		image.Point{Y: 8}, image.Point{Y: -8})

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	e.resetVP9EncoderCodingState(width, height)
	inter := &vp9InterEncodeState{
		img:           interSrc,
		refMask:       1 << uint(vp9dec.LastFrame),
		allowHP:       true,
		selectFc:      fc,
		referenceMode: vp9dec.SingleReference,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: 8, MiColStart: 0, MiColEnd: 8}

	aboveBefore := append([]int8(nil), e.aboveSegCtx...)
	leftBefore := append([]int8(nil), e.leftSegCtx...)

	rd, ok := e.pickVP9InterPartitionRD(inter, tile, &fc.PartitionProb,
		8, 8, 0, 0, common.Block64x64)
	if !ok {
		t.Fatal("pickVP9InterPartitionRD returned !ok")
	}
	if rd.target >= common.BlockSizes {
		t.Fatalf("pickVP9InterPartitionRD target = %d, want a valid block size", rd.target)
	}

	for i := range aboveBefore {
		if e.aboveSegCtx[i] != aboveBefore[i] {
			t.Fatalf("aboveSegCtx[%d] = %d after pickVP9InterPartitionRD, want %d (leaked partition-context update)",
				i, e.aboveSegCtx[i], aboveBefore[i])
		}
	}
	for i := range leftBefore {
		if e.leftSegCtx[i] != leftBefore[i] {
			t.Fatalf("leftSegCtx[%d] = %d after pickVP9InterPartitionRD, want %d (leaked partition-context update)",
				i, e.leftSegCtx[i], leftBefore[i])
		}
	}
}

// vp9FullRDInterNoOpStreamSHA256 pins the SHA-256 of the full-RD inter
// bitstream produced by the SearchPartition path (CpuUsed:-3) over a fixed
// key + 5 inter frame sequence that stresses every partition arm
// (HORZ/VERT/SPLIT/NONE). It was captured at base 6c3f4815 BEFORE the
// depth-first rd_pick_partition skeleton (rdPickVP9InterPartition,
// vp9_encoder_inter_partition_rd.go) was wired into
// pickVP9InterPartitionBlockSize.
//
// Step (a) of the full-RD inter partition port
// (docs/vp9_fullrd_partition_port_plan.md) is a PROVEN NO-OP: standing up the
// libvpx-shaped recursion skeleton must not move a single bit of the full-RD
// inter output. This hash is the byte-parity anchor for that proof; it MUST
// stay constant until step (c) (candidate[2] propagation) intentionally moves
// bytes, at which point it is re-derived from a libvpx trace, never hand-tuned.
// Updated after fixing pickVP9InterTxSize to clamp to TxModeToBiggestTxSize
// when tx_mode != TX_MODE_SELECT (e.g. ALLOW_32X32 at CpuUsed:-3).
const vp9FullRDInterNoOpStreamSHA256 = "829bc2ba48d4d1858b7e202b57799f1e27c4f7f8878eb90260f2389ea71cf8fa"

// vp9EncodeFullRDInterNoOpStream encodes the fixed step-(a) parity sequence and
// returns the concatenated bitstream.
func vp9EncodeFullRDInterNoOpStream(t *testing.T) []byte {
	t.Helper()
	const width, height = 64, 64
	// CpuUsed:-3 pins PartitionSearchType=SearchPartition, the full-RD inter
	// path that reaches rdPickVP9InterPartition. The default speed-8 path is
	// VAR_BASED and never enters the skeleton.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})

	var stream []byte
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	stream = append(stream, key...)

	srcs := []*image.YCbCr{
		splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8),
		splitYShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8),
		quadrantShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img,
			image.Point{X: 8}, image.Point{X: -8},
			image.Point{Y: 8}, image.Point{Y: -8}),
		shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0),
		splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 4, -4),
	}
	for i, s := range srcs {
		buf, err := e.Encode(s)
		if err != nil {
			t.Fatalf("Encode inter %d: %v", i, err)
		}
		stream = append(stream, buf...)
	}
	return stream
}

// TestVP9FullRDInterPartitionRDSkeletonIsNoOp asserts that the depth-first
// rd_pick_partition inter skeleton produces the byte-identical full-RD inter
// bitstream pinned at base. This is the step-(a) no-op gate.
func TestVP9FullRDInterPartitionRDSkeletonIsNoOp(t *testing.T) {
	stream := vp9EncodeFullRDInterNoOpStream(t)
	sum := sha256.Sum256(stream)
	got := hex.EncodeToString(sum[:])
	if got != vp9FullRDInterNoOpStreamSHA256 {
		t.Fatalf("full-RD inter bitstream SHA-256 = %s (%d bytes), want %s\n"+
			"the rd_pick_partition skeleton changed full-RD inter decisions; "+
			"step (a) must be a byte-exact no-op",
			got, len(stream), vp9FullRDInterNoOpStreamSHA256)
	}
}

// TestVP9FullRDInterPartitionRDNodeSentinel pins the per-node predMv reset to
// the libvpx INT16_MAX sentinel (vp9/encoder/vp9_encodeframe.c:4215-4218), the
// home of x->pred_mv[] the step-(b)/(c) thread will populate. Guards against a
// future edit silently swapping in the INT16_MIN intra sentinel.
func TestVP9FullRDInterPartitionRDNodeSentinel(t *testing.T) {
	node := newVP9InterPartitionRDNode(common.Block64x64)
	want := vp9dec.MV{Row: int16(0x7fff), Col: int16(0x7fff)}
	for ref, mv := range node.predMv {
		if mv != want {
			t.Fatalf("predMv[%d] = %+v, want INT16_MAX sentinel %+v", ref, mv, want)
		}
	}
	if node.partitioning != common.Block64x64 {
		t.Fatalf("node.partitioning = %d, want Block64x64", node.partitioning)
	}
	// store_pred_mv / load_pred_mv round-trip is inert plumbing in step (a):
	// loadPredMv must return exactly what storePredMv wrote.
	seeded := [vp9dec.MaxRefFrames]vp9dec.MV{
		{Row: 1, Col: 2}, {Row: 3, Col: 4}, {Row: 5, Col: 6}, {Row: 7, Col: 8},
	}
	node.storePredMv(seeded)
	if got := node.loadPredMv(); got != seeded {
		t.Fatalf("loadPredMv after storePredMv = %+v, want %+v", got, seeded)
	}
}
