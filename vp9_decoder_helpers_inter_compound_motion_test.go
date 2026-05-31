package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func seedVP9CompoundMotionRefsForTest(t *testing.T, d *VP9Decoder, width, height int) {
	t.Helper()
	key := vp9test.ColumnResidueKeyframe(t, width, height, 0, 32)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, width, height,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode compound LAST seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("compound LAST seed keyframe did not publish output")
	}
	if err := d.Decode(hidden); err != nil {
		t.Fatalf("Decode compound ALTREF seed intra-only frame: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("compound ALTREF seed intra-only frame published output")
	}
	if !d.refFrames[0].valid || !d.refFrames[vp9CompoundAltrefSlotForTest].valid {
		t.Fatal("compound motion reference setup did not populate LAST and ALTREF slots")
	}
}

func seedVP9CompoundTripleRefsForTest(t *testing.T, d *VP9Decoder, width, height int) {
	t.Helper()
	key := vp9test.ColumnResidueKeyframe(t, width, height, 0, 32)
	golden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, width, height,
		1<<uint(vp9CompoundGoldenSlotForTest), 32)
	altref := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, width, height,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode compound LAST seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("compound LAST seed keyframe did not publish output")
	}
	if err := d.Decode(golden); err != nil {
		t.Fatalf("Decode compound GOLDEN seed intra-only frame: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("compound GOLDEN seed intra-only frame published output")
	}
	if err := d.Decode(altref); err != nil {
		t.Fatalf("Decode compound ALTREF seed intra-only frame: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("compound ALTREF seed intra-only frame published output")
	}
	if !d.refFrames[0].valid ||
		!d.refFrames[vp9CompoundGoldenSlotForTest].valid ||
		!d.refFrames[vp9CompoundAltrefSlotForTest].valid {
		t.Fatal("compound reference setup did not populate LAST/GOLDEN/ALTREF slots")
	}
}

func vp9CompoundInterNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 64, 64, 0, 0,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9CompoundInterGoldenAltrefNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeRefDimsForTest(t, 64, 64, 0, 0,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, vp9CompoundGoldenSlotForTest, vp9CompoundAltrefSlotForTest},
		vp9dec.CompoundReference,
		[2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame}, 64, 64)
}

func vp9CompoundFixedGoldenSignBiasNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t,
		64, 64, 0, 0, vp9dec.MV{Col: 256},
		vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, vp9CompoundGoldenSlotForTest, vp9CompoundAltrefSlotForTest},
		vp9dec.CompoundReference, [2]int8{vp9dec.AltrefFrame, vp9dec.GoldenFrame},
		[3]uint8{0, 1, 0}, 64, 64)
}

func vp9CompoundFixedLastSignBiasNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t,
		64, 64, 0, 0, vp9dec.MV{Col: 256},
		vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, vp9CompoundGoldenSlotForTest, vp9CompoundAltrefSlotForTest},
		vp9dec.CompoundReference, [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
		[3]uint8{0, 1, 1}, 64, 64)
}

func vp9CompoundInterReferenceModeSelectNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeForTest(t, 64, 64, 0, 0,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest}, vp9dec.ReferenceModeSelect)
}

func vp9CompoundInterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameForTest(t, common.NearestMv)
}

func vp9CompoundInterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameForTest(t, common.NearMv)
}

func vp9ScaledCompoundInterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameRefDimsForTest(t, common.NearestMv, 128, 128)
}

func vp9ScaledCompoundInterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameRefDimsForTest(t, common.NearMv, 128, 128)
}

func vp9CompoundInterSubpelNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 96, 96, 4, 0,
		vp9dec.MV{Row: 4, Col: 260}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9CompoundInterSubpelBilinearNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 96, 96, 4, 0,
		vp9dec.MV{Row: 4, Col: 260}, vp9dec.InterpBilinear, vp9dec.InterpBilinear,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9CompoundInterSubpelSwitchableSmoothNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 96, 96, 4, 0,
		vp9dec.MV{Row: 4, Col: 260}, vp9dec.InterpSwitchable, vp9dec.InterpEighttapSmooth,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9ScaledCompoundInterZeroMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeRefDimsForTest(t, 32, 32, -1, -1,
		vp9dec.MV{}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest}, vp9dec.CompoundReference, 64, 64)
}

func vp9ScaledCompoundInterNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeRefDimsForTest(t, 32, 32, 0, 0,
		vp9dec.MV{Col: 32}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest}, vp9dec.CompoundReference, 64, 64)
}

func vp9SetupCompoundHeaderRefsForTest(header *vp9dec.UncompressedHeader,
	refIndex [3]uint8,
) ([vp9dec.MaxRefFrames]uint8, vp9dec.CompoundFrameRefs) {
	return vp9SetupCompoundHeaderRefsSignBiasForTest(header, refIndex, [3]uint8{0, 0, 1})
}

func vp9SetupCompoundHeaderRefsSignBiasForTest(header *vp9dec.UncompressedHeader,
	refIndex [3]uint8, headerSignBias [3]uint8,
) ([vp9dec.MaxRefFrames]uint8, vp9dec.CompoundFrameRefs) {
	header.InterRef.RefIndex = refIndex
	header.InterRef.SignBias = headerSignBias
	signBias := vp9dec.FrameRefSignBias(header)
	return signBias, vp9dec.SetupCompoundReferenceMode(signBias)
}

func vp9CompoundInterMotionFrameForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		refIndex, vp9dec.CompoundReference)
}

func vp9CompoundInterMotionFrameModeForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeRefDimsForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		refIndex, referenceMode, uint32(width), uint32(height))
}

func vp9CompoundInterMotionFrameModeRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeRefDimsForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		refIndex, referenceMode,
		[2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame}, refWidth, refHeight)
}

func vp9CompoundInterMotionRefsFrameModeRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
	refFrames [2]int8,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t,
		width, height, targetMiRow, targetMiCol, targetMV,
		frameFilter, blockFilter, refIndex, referenceMode, refFrames,
		[3]uint8{0, 0, 1}, refWidth, refHeight)
}

func vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
	refFrames [2]int8,
	headerSignBias [3]uint8,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          frameFilter,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	signBias, refs := vp9SetupCompoundHeaderRefsSignBiasForTest(&header,
		refIndex, headerSignBias)

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(blockFilter),
		Skip:         1,
		RefFrame:     refFrames,
	}
	dest := make([]byte, 131072)
	scratch := make([]byte, 131072)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         frameFilter,
			ReferenceMode:        referenceMode,
			CompoundRefAllowed:   true,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
					tile := vp9dec.TileBounds{
						MiRowStart: 0,
						MiRowEnd:   miRows,
						MiColStart: 0,
						MiColEnd:   miCols,
					}
					vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
						AboveSegCtx:    aboveSegCtx,
						LeftSegCtx:     leftSegCtx,
						MiRows:         miRows,
						MiCols:         miCols,
						PartitionProbs: &partitionProbs,
						GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
							return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
						},
						WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
							cur := baseMi
							cur.SbType = bsize
							var mv [2]vp9dec.MV
							if miRow == targetMiRow && miCol == targetMiCol {
								cur.Mode = common.NewMv
								mv[0] = targetMV
								mv[1] = targetMV
							}
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
							vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
								Seg:              &seg,
								Mi:               &cur,
								AboveMi:          above,
								LeftMi:           left,
								Fc:               &fc,
								TxMode:           common.Only4x4,
								FrameRefMode:     referenceMode,
								InterpFilter:     frameFilter,
								CompFixedRef:     refs.CompFixedRef,
								CompVarRef:       refs.CompVarRef,
								RefFrameSignBias: signBias,
								InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
									miRows, miRow, miCol, bsize),
								SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(above, left),
								AllowHP:             false,
								IsCompound:          true,
								Mv:                  mv,
							})
							cur.Mv = mv
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9CompoundInterMvReuseFrameForTest(t *testing.T,
	reuseMode common.PredictionMode,
) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameRefDimsForTest(t, reuseMode, 64, 64)
}

func vp9CompoundInterMvReuseFrameRefDimsForTest(t *testing.T,
	reuseMode common.PredictionMode,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, common.AlignToSB(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	signBias, refs := vp9SetupCompoundHeaderRefsForTest(&header,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})

	firstMV := vp9dec.MV{}
	secondMV := vp9dec.MV{Row: -128}
	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.CompoundReference,
			CompoundRefAllowed:   true,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv, bestRefMv [2]vp9dec.MV
					switch {
					case miRow == 0 && miCol == 0:
						cur.Mode = common.NewMv
						mv = [2]vp9dec.MV{firstMV, firstMV}
					case miRow == 0 && miCol == 4:
						cur.Mode = common.NewMv
						mv = [2]vp9dec.MV{secondMV, secondMV}
						bestRefMv = [2]vp9dec.MV{firstMV, firstMV}
					case miRow == 4 && miCol == 4:
						cur.Mode = reuseMode
						if reuseMode == common.NearMv {
							mv = [2]vp9dec.MV{firstMV, firstMV}
						} else {
							mv = [2]vp9dec.MV{secondMV, secondMV}
						}
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:              &seg,
						Mi:               &cur,
						AboveMi:          above,
						LeftMi:           left,
						Fc:               &fc,
						TxMode:           common.Only4x4,
						FrameRefMode:     vp9dec.CompoundReference,
						InterpFilter:     vp9dec.InterpEighttap,
						CompFixedRef:     refs.CompFixedRef,
						CompVarRef:       refs.CompVarRef,
						RefFrameSignBias: signBias,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						AllowHP:    false,
						IsCompound: true,
						Mv:         mv,
						BestRefMv:  bestRefMv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}
