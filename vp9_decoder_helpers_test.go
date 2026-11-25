package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
	"math"
	"strconv"
	"testing"
)

func itoa(v int) string { return strconv.Itoa(v) }

func clampEncodeCoord(v int, limit int) int {
	return min(max(v, 0), limit-1)
}

func encoderValidationImagePSNR(src Image, dst Image) float64 {
	sse, count := encoderValidationPlaneSSE(src.Y, src.YStride, dst.Y, dst.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	uSSE, uCount := encoderValidationPlaneSSE(src.U, src.UStride, dst.U, dst.UStride, uvWidth, uvHeight)
	vSSE, vCount := encoderValidationPlaneSSE(src.V, src.VStride, dst.V, dst.VStride, uvWidth, uvHeight)
	sse += uSSE + vSSE
	count += uCount + vCount
	if sse == 0 {
		return 100
	}
	mse := float64(sse) / float64(count)
	return 10 * math.Log10((255*255)/mse)
}

func encoderValidationPlaneSSE(a []byte, aStride int, b []byte, bStride int, width int, height int) (int64, int) {
	var sse int64
	count := width * height
	for row := range height {
		ar := a[row*aStride : row*aStride+width]
		br := b[row*bStride : row*bStride+width]
		for col := range width {
			d := int64(ar[col]) - int64(br[col])
			sse += d * d
		}
	}
	return sse, count
}

func newTestImage(width int, height int) Image {
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func assertVP9NeutralFrame(t *testing.T, got Image, width, height int) {
	t.Helper()
	assertVP9FilledFrame(t, got, width, height, 128, 128, 128)
}

func vp9DecodeLastVisibleFrameForTest(t *testing.T, packets ...[]byte) Image {
	t.Helper()
	return vp9DecodeLastVisibleFrameWithOptionsForTest(t, VP9DecoderOptions{},
		packets...)
}

func vp9DecodeLastVisibleFrameWithOptionsForTest(t *testing.T,
	opts VP9DecoderOptions, packets ...[]byte,
) Image {
	t.Helper()
	d, err := NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	var last Image
	ok := false
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, frameOK := d.NextFrame(); frameOK {
			last = frame
			ok = true
		}
	}
	if !ok {
		t.Fatal("packet sequence did not publish a visible frame")
	}
	return last
}

func assertVP9ImagesEqual(t *testing.T, want, got Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, want.Width, want.Height)
	}
	if !vp9VisiblePlanesEqual(want.Y, want.YStride, got.Y, got.YStride,
		want.Width, want.Height) {
		t.Fatal("Y plane differs")
	}
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	if !vp9VisiblePlanesEqual(want.U, want.UStride, got.U, got.UStride,
		uvWidth, uvHeight) {
		t.Fatal("U plane differs")
	}
	if !vp9VisiblePlanesEqual(want.V, want.VStride, got.V, got.VStride,
		uvWidth, uvHeight) {
		t.Fatal("V plane differs")
	}
}

func vp9VisiblePlanesEqual(a []byte, aStride int, b []byte, bStride int,
	width, height int,
) bool {
	for row := range height {
		aStart := row * aStride
		bStart := row * bStride
		if !bytes.Equal(a[aStart:aStart+width], b[bStart:bStart+width]) {
			return false
		}
	}
	return true
}

func appendVP9YForTest(out []byte, img Image) []byte {
	return testutil.AppendPlane(out, img.Y, img.YStride, img.Width, img.Height)
}

func vp9YRectDiffers(a, b Image, x, y, width, height int) bool {
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			if a.Y[row*a.YStride+col] != b.Y[row*b.YStride+col] {
				return true
			}
		}
	}
	return false
}

func fillVP9PublicImage(img *Image, value byte) {
	for i := range img.Y {
		img.Y[i] = value
	}
	for i := range img.U {
		img.U[i] = value
	}
	for i := range img.V {
		img.V[i] = value
	}
}

func assertVP9FilledFrame(t *testing.T, got Image, width, height int,
	yValue, uValue, vValue byte,
) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	assertVP9PlaneFilled(t, "Y", got.Y, got.YStride, width, height, yValue)
	assertVP9PlaneFilled(t, "U", got.U, got.UStride, uvWidth, uvHeight, uValue)
	assertVP9PlaneFilled(t, "V", got.V, got.VStride, uvWidth, uvHeight, vValue)
}

func assertVP9FilledFrameWithin(t *testing.T, got Image, width, height int,
	yValue, uValue, vValue, tolerance byte,
) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	assertVP9PlaneFilledWithin(t, "Y", got.Y, got.YStride, width, height,
		yValue, tolerance)
	assertVP9PlaneFilledWithin(t, "U", got.U, got.UStride, uvWidth, uvHeight,
		uValue, tolerance)
	assertVP9PlaneFilledWithin(t, "V", got.V, got.VStride, uvWidth, uvHeight,
		vValue, tolerance)
}

func assertVP9PlaneFilled(t *testing.T, name string, plane []byte,
	stride, width, height int, want byte,
) {
	t.Helper()
	if stride < width {
		t.Fatalf("%s stride = %d, want at least %d", name, stride, width)
	}
	wantLen := buffers.PlaneLen(stride, height, width)
	if len(plane) < wantLen {
		t.Fatalf("%s plane len = %d, want at least %d",
			name, len(plane), wantLen)
	}
	for row := range height {
		for col := range width {
			if got := plane[row*stride+col]; got != want {
				t.Fatalf("%s[%d,%d] = %d, want %d",
					name, row, col, got, want)
			}
		}
	}
}

func assertVP9PlaneFilledWithin(t *testing.T, name string, plane []byte,
	stride, width, height int, want, tolerance byte,
) {
	t.Helper()
	if stride < width {
		t.Fatalf("%s stride = %d, want at least %d", name, stride, width)
	}
	wantLen := buffers.PlaneLen(stride, height, width)
	if len(plane) < wantLen {
		t.Fatalf("%s plane len = %d, want at least %d",
			name, len(plane), wantLen)
	}
	for row := range height {
		for col := range width {
			got := plane[row*stride+col]
			if vp9AbsInt(int(got)-int(want)) > int(tolerance) {
				t.Fatalf("%s[%d,%d] = %d, want %d +/- %d",
					name, row, col, got, want, tolerance)
			}
		}
	}
}

func writeVP9InterResidueTileForTest(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range leftSegCtx {
			leftSegCtx[i] = 0
		}
		for plane := range vp9dec.MaxMbPlane {
			left := planes[plane].LeftContext
			for i := range left {
				left[i] = 0
			}
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeVP9InterResidueSbForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	if miRow >= miRows || miCol >= miCols {
		return nil
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, bsize)
	partition := common.PartitionLookup[bsl][target]
	vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
		AboveSegCtx:    aboveSegCtx,
		LeftSegCtx:     leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
			subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
			dcCoeff, coeffs, zeroCoeffs); err != nil {
			return err
		}
	} else {
		switch partition {
		case common.PartitionNone:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		case common.PartitionHorz:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if miRow+bs < miRows {
				if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow+bs, miCol,
					subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
					dcCoeff, coeffs, zeroCoeffs); err != nil {
					return err
				}
			}
		case common.PartitionVert:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if miCol+bs < miCols {
				if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol+bs,
					subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
					dcCoeff, coeffs, zeroCoeffs); err != nil {
					return err
				}
			}
		default:
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(aboveSegCtx, leftSegCtx,
			miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
	}
	return nil
}

func writeVP9InterResidueBlockForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	miGrid []vp9dec.NeighborMi,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	cur := baseMi
	cur.SbType = bsize
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = vp9MiGridAtForTest(miGrid, miRows, miCols, miRow, miCol-1)
	}
	vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
		Seg:          seg,
		Mi:           &cur,
		AboveMi:      vp9MiGridAtForTest(miGrid, miRows, miCols, miRow-1, miCol),
		LeftMi:       left,
		Fc:           fc,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap,
		InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
			miRows, miRow, miCol, bsize),
	})
	aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(planes, miRow, miCol)
	if err := vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
		BSize:        bsize,
		MiTxSize:     common.Tx4x4,
		IsInter:      1,
		Lossless:     false,
		Mi:           &cur,
		Planes:       planes,
		AboveOffsets: aboveOffsets,
		LeftOffsets:  leftOffsets,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			dq.Y[0],
			dq.Uv[0],
			dq.Uv[0],
		},
		Fc: &fc.CoefProbs,
		GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
			if dcCoeff != 0 && plane == 0 && r == 0 && c == 0 {
				return coeffs[:vp9dec.MaxEobForTxSize(tx)]
			}
			if dcCoeff == 0 {
				return coeffs[:vp9dec.MaxEobForTxSize(tx)]
			}
			return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
		},
	}); err != nil {
		return err
	}
	fillVP9MiGridForTest(miGrid, miRows, miCols, miRow, miCol, bsize, cur)
	return nil
}

func vp9PlaneContextOffsetsForTest(planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	miRow, miCol int,
) (above [vp9dec.MaxMbPlane]int, left [vp9dec.MaxMbPlane]int) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &planes[plane]
		above[plane] = (miCol * 2) >> pd.SubsamplingX
		left[plane] = ((miRow * 2) >> pd.SubsamplingY) % len(pd.LeftContext)
	}
	return above, left
}

func vp9InterFrameContextUpdatePacketForTest(t *testing.T, width, height int,
	contextIdx uint8, refreshFrameContext bool,
) ([]byte, uint8) {
	t.Helper()
	w := uint32(width)
	h := uint32(height)

	var probs vp9dec.FrameContext
	vp9dec.ResetFrameContext(&probs)
	var counts vp9enc.FrameCounts
	counts.Skip[0] = [2]uint32{1, 4096}
	var seg vp9dec.SegmentationParams
	aboveSegCtx := make([]int8, common.AlignToSB(miColsForSize(w)))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miColsForSize(w)*miColsForSize(h))
	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   refreshFrameContext,
		FrameParallelDecoding: true,
		FrameContextIdx:       contextIdx,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1

	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		CountsArgs: &vp9enc.WriteCompressedHeaderFromCountsArgs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
			CoefStepsize:         1,
			Probs:                &probs,
			Counts:               &counts,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			miCols := miColsForSize(w)
			miRows := miColsForSize(h)
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &probs.PartitionProb,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:          &seg,
				Mi:           &mi,
				Fc:           &probs,
				TxMode:       common.Only4x4,
				FrameRefMode: vp9dec.SingleReference,
				InterpFilter: vp9dec.InterpEighttap,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
			})
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	if probs.SkipProbs[0] == tables.DefaultSkipProbs[0] {
		t.Fatalf("compressed-header counts left skip prob at default %d", probs.SkipProbs[0])
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet, probs.SkipProbs[0]
}

func vp9ColumnResidueHiddenIntraOnlyFrameForTest(t *testing.T,
	width, height int, refreshFrameFlags uint8, dcCoeff int16,
) []byte {
	t.Helper()
	return vp9ColumnResidueIntraFrameForMotionTest(t, vp9ColumnResidueIntraFrameArgs{
		Width:             width,
		Height:            height,
		KeyFrame:          false,
		ShowFrame:         false,
		RefreshFrameFlags: refreshFrameFlags,
		FilterLevel:       0,
		DCCoeff:           dcCoeff,
	})
}

func vp9SegmentedAltQKeyframeForTest(t *testing.T) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	seg := vp9SegmentationAltQForTest()
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: 1,
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, vp9dec.PlaneEntropyLen(common.AlignToSB(miCols), 0))
	planes[0].LeftContext = make([]uint8, vp9dec.PlaneEntropyLen(common.MiBlockSize, 0))
	planes[1].AboveContext = make([]uint8, vp9dec.PlaneEntropyLen(common.AlignToSB(miCols), 1))
	planes[1].LeftContext = make([]uint8, vp9dec.PlaneEntropyLen(common.MiBlockSize, 1))
	planes[2].AboveContext = make([]uint8, vp9dec.PlaneEntropyLen(common.AlignToSB(miCols), 1))
	planes[2].LeftContext = make([]uint8, vp9dec.PlaneEntropyLen(common.MiBlockSize, 1))

	partitionProbs := tables.KfPartitionProbs
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
	coeffsBySeg := [2][]int16{
		make([]int16, 1024),
		make([]int16, 1024),
	}
	for i := range coeffsBySeg {
		coeffsBySeg[i][0] = dq.Y[i][0]
	}
	zeroCoeffs := make([]int16, 1024)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		Seg:                   seg,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	baseMi := vp9dec.NeighborMi{
		SbType: common.Block32x32,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   0,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			var writeErr error
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
							if writeErr != nil {
								return
							}
							segID := 0
							if miCol >= 4 {
								segID = 1
							}
							cur := baseMi
							cur.SbType = bsize
							cur.SegmentID = uint8(segID)
							cur.SegIDPredicted = uint8(segID)
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
								Seg:       &seg,
								Mi:        &cur,
								AboveMi:   vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol),
								LeftMi:    left,
								TxMode:    common.Only4x4,
								SkipProbs: fc.SkipProbs,
							})
							vp9enc.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
							aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(&planes, miRow, miCol)
							writeErr = vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
								BSize:        bsize,
								MiTxSize:     common.Tx4x4,
								IsInter:      0,
								Lossless:     false,
								Mi:           &cur,
								Planes:       &planes,
								AboveOffsets: aboveOffsets,
								LeftOffsets:  leftOffsets,
								PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
									dq.Y[segID],
									dq.Uv[segID],
									dq.Uv[segID],
								},
								Fc: &fc.CoefProbs,
								GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
									if plane == 0 && r == 0 && c == 0 {
										return coeffsBySeg[segID][:vp9dec.MaxEobForTxSize(tx)]
									}
									return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
								},
							})
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return writeErr
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentationAltQForTest() vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   true,
	}
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.FeatureMask[1] = 1 << uint(vp9dec.SegLvlAltQ)
	seg.FeatureData[1][vp9dec.SegLvlAltQ] = 96
	return seg
}

func miColsForSize(v uint32) int {
	miCols := (v + 7) >> 3
	return int(miCols)
}

const (
	vp9CompoundGoldenSlotForTest = 4
	vp9CompoundAltrefSlotForTest = 5
)

func vp9MiGridAtForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &miGrid[r*miCols+c]
}

func fillVP9MiGridForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int,
	bsize common.BlockSize, mi vp9dec.NeighborMi,
) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

// TestVP9DecoderMaxWidthRejectsLargerKeyframe: a header whose width
// exceeds the configured MaxWidth gets rejected before tile parsing or

func vp9SVCStyleSuperframeForTest(t *testing.T) []byte {
	t.Helper()
	return vp9test.SuperframePacket(t,
		vp9EncodedKeyframeForTest(t, 32, 32, 80),
		vp9EncodedKeyframeForTest(t, 64, 64, 160),
	)
}

func vp9EncodedKeyframeForTest(t *testing.T, width, height int, y byte) []byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 37,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder %dx%d: %v", width, height, err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize %dx%d: %v", width, height, err)
	}
	dst := make([]byte, dstSize)
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height, y, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult %dx%d: %v", width, height, err)
	}
	if !result.KeyFrame || !result.ShowFrame || len(result.Data) == 0 {
		t.Fatalf("encoded test frame result = %+v, want visible keyframe", result)
	}
	return append([]byte(nil), result.Data...)
}

// TestVP9DecoderClose marks the decoder as closed; subsequent Decode
// returns ErrClosed.

func TestVP9DecoderInterNearestMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter nearestmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter nearestmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter nearestmv steady state: got %v allocs/op, want 0", allocs)
	}
}
