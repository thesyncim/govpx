package decoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodeframe.c macroblock inverse transform setup
// - vp8/common/invtrans.h inverse-transform dispatch
// - vp8/common/setupintrarecon.c intra edge setup
// - vp8/common/reconinter.c whole-macroblock inter predictor offsets

var (
	ErrReconstructGridBufferTooSmall      = errors.New("libgopx: VP8 reconstruction grid buffer too small")
	ErrUnsupportedIntraReconstructionMode = errors.New("libgopx: unsupported VP8 intra reconstruction mode")
	ErrUnsupportedInterReconstructionMode = errors.New("libgopx: unsupported VP8 inter reconstruction mode")
)

type MacroblockResidual struct {
	DQCoeff [25 * 16]int16
}

type IntraPredictorRefs struct {
	YAbove []byte
	YLeft  []byte
	UAbove []byte
	ULeft  []byte
	VAbove []byte
	VLeft  []byte

	YTopLeft byte
	UTopLeft byte
	VTopLeft byte

	UpAvailable   bool
	LeftAvailable bool
}

type IntraPredictorScratch struct {
	YAbove [20]byte
	YLeft  [16]byte
	UAbove [8]byte
	ULeft  [8]byte
	VAbove [8]byte
	VLeft  [8]byte
}

type IntraReconstructionScratch struct {
	Refs     IntraPredictorScratch
	Residual MacroblockResidual
}

func (r *MacroblockResidual) Block(index int) *[16]int16 {
	return (*[16]int16)(r.DQCoeff[index*16 : index*16+16])
}

func BuildIntraPredictorRefs(img *common.Image, mbRow int, mbCol int, scratch *IntraPredictorScratch) IntraPredictorRefs {
	yRow := mbRow * 16
	yCol := mbCol * 16
	uvRow := mbRow * 8
	uvCol := mbCol * 8
	upAvailable := mbRow > 0
	leftAvailable := mbCol > 0
	codedWidth := codedImageWidth(img)
	codedHeight := codedImageHeight(img)

	buildAbove(scratch.YAbove[:], img.Y, img.YStride, codedWidth, yRow, yCol, upAvailable)
	buildLeft(scratch.YLeft[:], img.Y, img.YStride, codedHeight, yRow, yCol, leftAvailable)
	uvWidth := (codedWidth + 1) >> 1
	uvHeight := (codedHeight + 1) >> 1
	buildAbove(scratch.UAbove[:], img.U, img.UStride, uvWidth, uvRow, uvCol, upAvailable)
	buildLeft(scratch.ULeft[:], img.U, img.UStride, uvHeight, uvRow, uvCol, leftAvailable)
	buildAbove(scratch.VAbove[:], img.V, img.VStride, uvWidth, uvRow, uvCol, upAvailable)
	buildLeft(scratch.VLeft[:], img.V, img.VStride, uvHeight, uvRow, uvCol, leftAvailable)

	return IntraPredictorRefs{
		YAbove:        scratch.YAbove[:],
		YLeft:         scratch.YLeft[:],
		UAbove:        scratch.UAbove[:],
		ULeft:         scratch.ULeft[:],
		VAbove:        scratch.VAbove[:],
		VLeft:         scratch.VLeft[:],
		YTopLeft:      topLeftSample(img.Y, img.YStride, yRow, yCol, upAvailable, leftAvailable),
		UTopLeft:      topLeftSample(img.U, img.UStride, uvRow, uvCol, upAvailable, leftAvailable),
		VTopLeft:      topLeftSample(img.V, img.VStride, uvRow, uvCol, upAvailable, leftAvailable),
		UpAvailable:   upAvailable,
		LeftAvailable: leftAvailable,
	}
}

func TransformMacroblockTokens(tokens *MacroblockTokens, dequant *common.MacroblockDequant, is4x4 bool, out *MacroblockResidual) {
	clearMacroblockResidual(out)

	if !is4x4 && tokens.EOB[24] > 0 {
		var y2 [16]int16
		if tokens.EOB[24] > 1 {
			dsp.DequantizeBlock(&tokens.QCoeff[24], &dequant.Y2, &y2)
			dsp.InverseWalsh4x4(&y2, out.DQCoeff[:])
		} else {
			y2[0] = tokens.QCoeff[24][0] * dequant.Y2[0]
			dsp.DCOnlyInverseWalsh4x4(y2[0], out.DQCoeff[:])
		}
	}

	yDequant := &dequant.Y1
	if !is4x4 {
		yDequant = &dequant.Y1DC
	}
	for i := 0; i < 16; i++ {
		if tokens.EOB[i] == 0 {
			continue
		}
		dequantizeInto(&tokens.QCoeff[i], yDequant, out.Block(i))
	}
	for i := 16; i < 24; i++ {
		if tokens.EOB[i] == 0 {
			continue
		}
		dequantizeInto(&tokens.QCoeff[i], &dequant.UV, out.Block(i))
	}
}

func AddMacroblockResidual(tokens *MacroblockTokens, residual *MacroblockResidual, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int) {
	for i := 0; i < 16; i++ {
		if tokens.EOB[i] == 0 {
			continue
		}
		addTransformBlock(tokens.EOB[i], residual.Block(i), y[yBlockOffset(i, yStride):], yStride)
	}
	addChromaResidual(tokens, residual, u, uStride, v, vStride)
}

func addChromaResidual(tokens *MacroblockTokens, residual *MacroblockResidual, u []byte, uStride int, v []byte, vStride int) {
	for i := 0; i < 4; i++ {
		if tokens.EOB[16+i] != 0 {
			addTransformBlock(tokens.EOB[16+i], residual.Block(16+i), u[uvBlockOffset(i, uStride):], uStride)
		}
		if tokens.EOB[20+i] != 0 {
			addTransformBlock(tokens.EOB[20+i], residual.Block(20+i), v[uvBlockOffset(i, vStride):], vStride)
		}
	}
}

func PredictIntraY16x16(mode common.MBPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, upAvailable bool, leftAvailable bool) bool {
	switch mode {
	case common.DCPred:
		dsp.IntraDCPredict16x16(dst, stride, above, left, upAvailable, leftAvailable)
	case common.VPred:
		dsp.IntraVerticalPredict16x16(dst, stride, above)
	case common.HPred:
		dsp.IntraHorizontalPredict16x16(dst, stride, left)
	case common.TMPred:
		dsp.IntraTMPredict16x16(dst, stride, above, left, topLeft)
	default:
		return false
	}
	return true
}

func PredictIntraUV8x8(mode common.MBPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, upAvailable bool, leftAvailable bool) bool {
	switch mode {
	case common.DCPred:
		dsp.IntraDCPredict8x8(dst, stride, above, left, upAvailable, leftAvailable)
	case common.VPred:
		dsp.IntraVerticalPredict8x8(dst, stride, above)
	case common.HPred:
		dsp.IntraHorizontalPredict8x8(dst, stride, left)
	case common.TMPred:
		dsp.IntraTMPredict8x8(dst, stride, above, left, topLeft)
	default:
		return false
	}
	return true
}

func PredictIntraY4x4(modes *[16]common.BPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte) bool {
	for block := 0; block < 16; block++ {
		if ok := predictIntraY4x4Block((*modes)[block], dst, stride, above, left, topLeft, block); !ok {
			return false
		}
	}
	return true
}

func ReconstructWholeBlockIntraMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, refs IntraPredictorRefs, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual) bool {
	if mode.Is4x4 || mode.Mode == common.BPred {
		return false
	}
	if !PredictIntraY16x16(mode.Mode, y, yStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, u, uStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, v, vStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if mode.MBSkipCoeff {
		return true
	}
	TransformMacroblockTokens(tokens, dequant, false, scratch)
	AddMacroblockResidual(tokens, scratch, y, yStride, u, uStride, v, vStride)
	return true
}

func ReconstructIntraMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, refs IntraPredictorRefs, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual) bool {
	if mode.Is4x4 || mode.Mode == common.BPred {
		return ReconstructBPredIntraMacroblock(mode, tokens, dequant, refs, y, yStride, u, uStride, v, vStride, scratch)
	}
	return ReconstructWholeBlockIntraMacroblock(mode, tokens, dequant, refs, y, yStride, u, uStride, v, vStride, scratch)
}

func ReconstructKeyFrameIntraGrid(img *common.Image, rows int, cols int, modes []MacroblockMode, tokens []MacroblockTokens, dequants *[common.MaxMBSegments]common.MacroblockDequant, scratch *IntraReconstructionScratch) error {
	if rows < 0 || cols < 0 {
		return ErrReconstructGridBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrReconstructGridBufferTooSmall
	}
	required := rows * cols
	if img == nil || dequants == nil || scratch == nil || len(modes) < required || len(tokens) < required {
		return ErrReconstructGridBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) {
		return ErrReconstructGridBufferTooSmall
	}

	for row := 0; row < rows; row++ {
		yRow := row * 16 * img.YStride
		uRow := row * 8 * img.UStride
		vRow := row * 8 * img.VStride
		for col := 0; col < cols; col++ {
			index := row*cols + col
			mode := &modes[index]
			if mode.SegmentID >= common.MaxMBSegments {
				return ErrUnsupportedIntraReconstructionMode
			}
			refs := BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
			yOff := yRow + col*16
			uOff := uRow + col*8
			vOff := vRow + col*8
			if !ReconstructIntraMacroblock(mode, &tokens[index], &(*dequants)[mode.SegmentID], refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual) {
				return ErrUnsupportedIntraReconstructionMode
			}
		}
	}
	return nil
}

func ReconstructInterFrameGrid(img *common.Image, last *common.Image, golden *common.Image, alt *common.Image, rows int, cols int, modes []MacroblockMode, tokens []MacroblockTokens, dequants *[common.MaxMBSegments]common.MacroblockDequant, scratch *IntraReconstructionScratch) error {
	if rows < 0 || cols < 0 {
		return ErrReconstructGridBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrReconstructGridBufferTooSmall
	}
	required := rows * cols
	if img == nil || last == nil || golden == nil || alt == nil || dequants == nil || scratch == nil || len(modes) < required || len(tokens) < required {
		return ErrReconstructGridBufferTooSmall
	}
	if !imageHasMacroblockGrid(img, rows, cols) || !imageHasMacroblockGrid(last, rows, cols) || !imageHasMacroblockGrid(golden, rows, cols) || !imageHasMacroblockGrid(alt, rows, cols) {
		return ErrReconstructGridBufferTooSmall
	}

	for row := 0; row < rows; row++ {
		yRow := row * 16 * img.YStride
		uRow := row * 8 * img.UStride
		vRow := row * 8 * img.VStride
		for col := 0; col < cols; col++ {
			index := row*cols + col
			mode := &modes[index]
			if mode.SegmentID >= common.MaxMBSegments {
				return ErrUnsupportedInterReconstructionMode
			}
			yOff := yRow + col*16
			uOff := uRow + col*8
			vOff := vRow + col*8
			if mode.RefFrame == common.IntraFrame {
				refs := BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
				if !ReconstructIntraMacroblock(mode, &tokens[index], &(*dequants)[mode.SegmentID], refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual) {
					return ErrUnsupportedInterReconstructionMode
				}
				continue
			}

			ref := referenceImageForMode(mode.RefFrame, last, golden, alt)
			if ref == nil {
				return ErrUnsupportedInterReconstructionMode
			}
			if !ReconstructWholeMVInterMacroblock(mode, &tokens[index], &(*dequants)[mode.SegmentID], ref, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col) {
				return ErrUnsupportedInterReconstructionMode
			}
		}
	}
	return nil
}

func ReconstructWholeMVInterMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, ref *common.Image, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual, mbRow int, mbCol int) bool {
	if mode.RefFrame == common.IntraFrame || mode.Is4x4 || !isWholeMacroblockInterMode(mode.Mode) {
		return false
	}
	if mode.Mode == common.ZeroMV && !mode.MV.IsZero() {
		return false
	}
	yRef, uRef, vRef, ok := wholeMVReferenceOffsets(ref, mbRow, mbCol, mode.MV)
	if !ok {
		return false
	}
	dsp.Copy16x16(ref.Y[yRef:], ref.YStride, y, yStride)
	dsp.Copy8x8(ref.U[uRef:], ref.UStride, u, uStride)
	dsp.Copy8x8(ref.V[vRef:], ref.VStride, v, vStride)
	if mode.MBSkipCoeff {
		return true
	}
	TransformMacroblockTokens(tokens, dequant, false, scratch)
	AddMacroblockResidual(tokens, scratch, y, yStride, u, uStride, v, vStride)
	return true
}

func isWholeMacroblockInterMode(mode common.MBPredictionMode) bool {
	switch mode {
	case common.ZeroMV, common.NearestMV, common.NearMV, common.NewMV:
		return true
	default:
		return false
	}
}

func wholeMVReferenceOffsets(ref *common.Image, mbRow int, mbCol int, mv MotionVector) (int, int, int, bool) {
	if ref == nil {
		return 0, 0, 0, false
	}
	if (int(mv.Row)|int(mv.Col))&7 != 0 {
		return 0, 0, 0, false
	}

	yRow := mbRow*16 + int(mv.Row>>3)
	yCol := mbCol*16 + int(mv.Col>>3)
	if !imageHasReferenceBlock(ref.Y, ref.YStride, codedImageWidth(ref), codedImageHeight(ref), yRow, yCol, 16, 16) {
		return 0, 0, 0, false
	}

	uvMVRow := chromaMotionVectorComponent(mv.Row)
	uvMVCol := chromaMotionVectorComponent(mv.Col)
	if (uvMVRow|uvMVCol)&7 != 0 {
		return 0, 0, 0, false
	}

	uvRow := mbRow*8 + (uvMVRow >> 3)
	uvCol := mbCol*8 + (uvMVCol >> 3)
	uvWidth := (codedImageWidth(ref) + 1) >> 1
	uvHeight := (codedImageHeight(ref) + 1) >> 1
	if !imageHasReferenceBlock(ref.U, ref.UStride, uvWidth, uvHeight, uvRow, uvCol, 8, 8) ||
		!imageHasReferenceBlock(ref.V, ref.VStride, uvWidth, uvHeight, uvRow, uvCol, 8, 8) {
		return 0, 0, 0, false
	}

	return yRow*ref.YStride + yCol, uvRow*ref.UStride + uvCol, uvRow*ref.VStride + uvCol, true
}

func chromaMotionVectorComponent(v int16) int {
	mv := int(v)
	if mv < 0 {
		mv--
	} else {
		mv++
	}
	return mv / 2
}

func referenceImageForMode(refFrame common.MVReferenceFrame, last *common.Image, golden *common.Image, alt *common.Image) *common.Image {
	switch refFrame {
	case common.LastFrame:
		return last
	case common.GoldenFrame:
		return golden
	case common.AltRefFrame:
		return alt
	default:
		return nil
	}
}

func ReconstructBPredIntraMacroblock(mode *MacroblockMode, tokens *MacroblockTokens, dequant *common.MacroblockDequant, refs IntraPredictorRefs, y []byte, yStride int, u []byte, uStride int, v []byte, vStride int, scratch *MacroblockResidual) bool {
	if !mode.Is4x4 || mode.Mode != common.BPred {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, u, uStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !PredictIntraUV8x8(mode.UVMode, v, vStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if mode.MBSkipCoeff {
		return PredictIntraY4x4(&mode.BModes, y, yStride, refs.YAbove, refs.YLeft, refs.YTopLeft)
	}

	TransformMacroblockTokens(tokens, dequant, true, scratch)
	for block := 0; block < 16; block++ {
		if ok := predictIntraY4x4Block(mode.BModes[block], y, yStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block); !ok {
			return false
		}
		if tokens.EOB[block] != 0 {
			addTransformBlock(tokens.EOB[block], scratch.Block(block), y[yBlockOffset(block, yStride):], yStride)
		}
	}
	addChromaResidual(tokens, scratch, u, uStride, v, vStride)
	return true
}

func clearMacroblockResidual(out *MacroblockResidual) {
	for i := range out.DQCoeff {
		out.DQCoeff[i] = 0
	}
}

func dequantizeInto(qcoeff *[16]int16, dequant *[16]int16, out *[16]int16) {
	for i := 0; i < 16; i++ {
		out[i] += qcoeff[i] * dequant[i]
	}
}

func buildAbove(out []byte, plane []byte, stride int, width int, row int, col int, available bool) {
	if !available {
		for i := range out {
			out[i] = 127
		}
		return
	}
	src := (row-1)*stride + col
	for i := range out {
		if col+i < width {
			out[i] = plane[src+i]
		} else {
			out[i] = 127
		}
	}
}

func buildLeft(out []byte, plane []byte, stride int, height int, row int, col int, available bool) {
	if !available {
		for i := range out {
			out[i] = 129
		}
		return
	}
	src := row*stride + col - 1
	for i := range out {
		if row+i < height {
			out[i] = plane[src+i*stride]
		} else {
			out[i] = 129
		}
	}
}

func topLeftSample(plane []byte, stride int, row int, col int, upAvailable bool, leftAvailable bool) byte {
	if !upAvailable {
		return 127
	}
	if !leftAvailable {
		return 129
	}
	return plane[(row-1)*stride+col-1]
}

func imageHasMacroblockGrid(img *common.Image, rows int, cols int) bool {
	if img.YStride <= 0 || img.UStride <= 0 || img.VStride <= 0 {
		return false
	}
	yWidth := cols * 16
	yHeight := rows * 16
	uvWidth := cols * 8
	uvHeight := rows * 8
	if codedImageWidth(img) < yWidth || codedImageHeight(img) < yHeight {
		return false
	}
	return planeHasBlock(img.Y, img.YStride, yWidth, yHeight) &&
		planeHasBlock(img.U, img.UStride, uvWidth, uvHeight) &&
		planeHasBlock(img.V, img.VStride, uvWidth, uvHeight)
}

func planeHasBlock(plane []byte, stride int, width int, height int) bool {
	if width < 0 || height < 0 || stride < width {
		return false
	}
	if height == 0 {
		return true
	}
	need := (height-1)*stride + width
	return need <= len(plane)
}

func imageHasReferenceBlock(plane []byte, stride int, codedWidth int, codedHeight int, row int, col int, width int, height int) bool {
	if row < 0 || col < 0 || width < 0 || height < 0 || stride < codedWidth {
		return false
	}
	if col > codedWidth-width || row > codedHeight-height {
		return false
	}
	if height == 0 {
		return true
	}
	need := (row+height-1)*stride + col + width
	return need <= len(plane)
}

func codedImageWidth(img *common.Image) int {
	if img.CodedWidth > 0 {
		return img.CodedWidth
	}
	return img.Width
}

func codedImageHeight(img *common.Image) int {
	if img.CodedHeight > 0 {
		return img.CodedHeight
	}
	return img.Height
}

func predictIntraY4x4Block(mode common.BPredictionMode, dst []byte, stride int, above []byte, left []byte, topLeft byte, block int) bool {
	blockRow := block >> 2
	blockCol := block & 3
	y := blockRow * 4
	x := blockCol * 4
	var blockAbove [8]byte
	var blockLeft [4]byte

	if blockRow == 0 {
		copy(blockAbove[:], above[x:x+8])
	} else {
		aboveOff := (y-1)*stride + x
		copy(blockAbove[:4], dst[aboveOff:aboveOff+4])
		if blockCol < 3 {
			copy(blockAbove[4:], dst[aboveOff+4:aboveOff+8])
		} else {
			copy(blockAbove[4:], above[16:20])
		}
	}

	if blockCol == 0 {
		copy(blockLeft[:], left[y:y+4])
	} else {
		for i := 0; i < 4; i++ {
			blockLeft[i] = dst[(y+i)*stride+x-1]
		}
	}

	blockTopLeft := topLeft
	switch {
	case blockRow == 0 && blockCol == 0:
	case blockRow == 0:
		blockTopLeft = above[x-1]
	case blockCol == 0:
		blockTopLeft = left[y-1]
	default:
		blockTopLeft = dst[(y-1)*stride+x-1]
	}

	return dsp.Intra4x4Predict(dst[y*stride+x:], stride, mode, blockAbove[:], blockLeft[:], blockTopLeft)
}

func addTransformBlock(eob uint8, coeff *[16]int16, dst []byte, stride int) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dsp.DCOnlyIDCT4x4Add(coeff[0], dst, stride, dst, stride)
		return
	}
	dsp.IDCT4x4Add(coeff, dst, stride, dst, stride)
}

func yBlockOffset(block int, stride int) int {
	return (block>>2)*4*stride + (block&3)*4
}

func uvBlockOffset(block int, stride int) int {
	return (block>>1)*4*stride + (block&1)*4
}
