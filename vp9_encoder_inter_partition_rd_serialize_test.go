package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
