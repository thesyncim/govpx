package codec

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"math/rand"
	"testing"
)

func (e *VP8Encoder) motion01ParentImprovedInterFrameSearchStart(
	src vp8enc.SourceImage, refFrame vp8common.MVReferenceFrame,
	mbRow int, mbCol int, mbRows int, mbCols int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
	sadCache *improvedInterFrameNearSADCache,
) interFrameSearchStart {
	if !search.improvedMVPrediction || refFrame == vp8common.IntraFrame {
		return interFrameSearchStart{}
	}
	signBias := e.interFrameSignBias()
	mbCount := mbRows * mbCols
	lastRefsAvailable := e.lastFrameInterModesValid && len(e.lastFrameInterModeRefs) >= mbCount
	includeLastSlots := (lastRefsAvailable || e.lastCodedFrameType != vp8common.KeyFrame) && mbRows > 0 && mbCols > 0
	var localSADs improvedInterFrameNearSADCache
	cache := sadCache
	if cache == nil {
		cache = &localSADs
	}
	if !cache.set {
		cache.sad[0] = currentNeighborNearSAD(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol, above)
		cache.sad[1] = currentNeighborNearSAD(src, &e.analysis.Img, mbRow, mbCol, mbRow, mbCol-1, left)
		cache.sad[2] = currentNeighborNearSAD(src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol-1, aboveLeft)
		if includeLastSlots {
			cache.sad[3] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
			cache.sad[4] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
			cache.sad[5] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
			cache.sad[6] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
			cache.sad[7] = lastNeighborNearSAD(src, &e.lastRef.Img, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
		}
		cache.set = true
	}
	var slots [8]improvedInterFrameMVSlot
	slotCount := 3
	slots[0].fillCurrent(cache.sad[0], mbRow-1, mbCol, above, signBias)
	slots[1].fillCurrent(cache.sad[1], mbRow, mbCol-1, left, signBias)
	slots[2].fillCurrent(cache.sad[2], mbRow-1, mbCol-1, aboveLeft, signBias)
	if includeLastSlots {
		slotCount = 8
		if lastRefsAvailable {
			slots[3].fillLastRef(cache.sad[3], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow, mbCol)
			slots[4].fillLastRef(cache.sad[4], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow-1, mbCol)
			slots[5].fillLastRef(cache.sad[5], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow, mbCol-1)
			slots[6].fillLastRef(cache.sad[6], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow, mbCol+1)
			slots[7].fillLastRef(cache.sad[7], e.lastFrameInterModeRefs, mbRows, mbCols, mbRow+1, mbCol)
		} else {
			slots[3] = improvedInterFrameMVSlot{sad: cache.sad[3]}
			slots[4] = improvedInterFrameMVSlot{sad: cache.sad[4]}
			slots[5] = improvedInterFrameMVSlot{sad: cache.sad[5]}
			slots[6] = improvedInterFrameMVSlot{sad: cache.sad[6]}
			slots[7] = improvedInterFrameMVSlot{sad: cache.sad[7]}
		}
	}
	biasImprovedInterFrameMVSlots(&slots, slotCount, refFrame, signBias, mbRow, mbCol, mbRows, mbCols)
	order := improvedInterFrameMVSlotOrder(slots, slotCount)
	// Both slots and order are length 8 (pow2). rank is bounded by
	// slotCount ≤ 8 and each order[rank] cell is [0,8) by construction
	// (the insertion sort writes only [0, slotCount) indices). Mask
	// with 7 to elide bounds checks on both indexed loads.
	for rank := 0; rank < slotCount; rank++ {
		slot := slots[order[rank&7]&7]
		if slot.refFrame == refFrame {
			sr := 2
			if rank < 3 {
				sr = 3
			}
			return newInterFrameSearchStart(slot.mv, sr, order[rank&7])
		}
	}
	mv := improvedInterFrameMVMedian(slots, slotCount)
	return newInterFrameSearchStart(mv, 0, -1)
}
func motion01PredictorSetup(tb testing.TB) (*VP8Encoder, vp8enc.SourceImage, [3]vp8enc.InterFrameMacroblockMode) {
	e := newSizedTestEncoder(tb, 128, 128)
	e.lastFrameInterModesValid = true
	e.lastFrameInterModeRefs = make([]vp8enc.InterFrameMVRef, 64)
	for i := range e.lastFrameInterModeRefs {
		e.lastFrameInterModeRefs[i] = vp8enc.InterFrameMVRef{MV: vp8enc.MotionVector{Row: int16(i*71 - 2000), Col: int16(i*43 - 1000)}, RefFrame: vp8common.MVReferenceFrame(1 + i%3), SignBias: i&1 != 0}
	}
	src := sourceImageFromPublic(testImage(128, 128))
	var modes [3]vp8enc.InterFrameMacroblockMode
	for i := range modes {
		modes[i] = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.MVReferenceFrame(i + 1), MV: vp8enc.MotionVector{Row: int16(3000 + i*41), Col: int16(-4000 - i*59)}}
	}
	return e, src, modes
}
func TestMotion01PRPredictor(t *testing.T) {
	e, src, m := motion01PredictorSetup(t)
	r := rand.New(rand.NewSource(511))
	for i := range 500 {
		cache := improvedInterFrameNearSADCache{set: true}
		for j := range cache.sad {
			cache.sad[j] = r.Intn(7)
		}
		for j := range m {
			m[j].RefFrame = vp8common.MVReferenceFrame(r.Intn(4))
			m[j].MV = vp8enc.MotionVector{Row: int16(r.Uint32()), Col: int16(r.Uint32())}
		}
		for j := range e.lastFrameInterModeRefs {
			e.lastFrameInterModeRefs[j].RefFrame = vp8common.MVReferenceFrame(r.Intn(4))
		}
		e.lastFrameInterModesValid = i&1 != 0
		e.lastCodedFrameType = vp8common.FrameType(i % 2)
		search := defaultInterAnalysisSearchConfig()
		search.improvedMVPrediction = true
		row, col := i%8, (i/8)%8
		ref := vp8common.MVReferenceFrame(1 + i%3)
		got := e.improvedInterFrameSearchStart(src, ref, row, col, 8, 8, &m[0], &m[1], &m[2], search, &cache)
		want := e.motion01ParentImprovedInterFrameSearchStart(src, ref, row, col, 8, 8, &m[0], &m[1], &m[2], search, &cache)
		if got != want {
			t.Fatalf("case %d got %+v want %+v", i, got, want)
		}
	}
}

var motion01PRStart interFrameSearchStart

func BenchmarkMotion01PRPredictor(b *testing.B) {
	e, s, m := motion01PredictorSetup(b)
	cache := improvedInterFrameNearSADCache{set: true, sad: [8]int{31, 28, 29, 47, 32, 43, 19, 20}}
	search := defaultInterAnalysisSearchConfig()
	search.improvedMVPrediction = true
	b.ReportAllocs()
	for b.Loop() {
		motion01PRStart = e.improvedInterFrameSearchStart(s, vp8common.LastFrame, 4, 4, 8, 8, &m[0], &m[1], &m[2], search, &cache)
	}
}

func motion02ParentBiasImprovedInterFrameMVSlots(slots *[8]improvedInterFrameMVSlot, count int, refFrame vp8common.MVReferenceFrame, signBias [vp8common.MaxRefFrames]bool, mbRow int, mbCol int, mbRows int, mbCols int) {
	if slots == nil || refFrame <= vp8common.IntraFrame || refFrame >= vp8common.MaxRefFrames {
		return
	}
	targetBias := signBias[refFrame]
	limit := min(count, len(slots))
	for i := range limit {
		slot := &slots[i]
		if slot.refFrame == vp8common.IntraFrame {
			continue
		}
		if slot.signBias != targetBias {
			slot.mv.Row = -slot.mv.Row
			slot.mv.Col = -slot.mv.Col
		}
		slot.mv = clampInterFrameModeMotionVector(slot.mv, mbRow, mbCol, mbRows, mbCols)
	}
}
func TestMotion02PRBias(t *testing.T) {
	r := rand.New(rand.NewSource(522))
	for i := range 1000 {
		var a [8]improvedInterFrameMVSlot
		for j := range a {
			a[j] = improvedInterFrameMVSlot{mv: vp8enc.MotionVector{Row: int16(r.Uint32()), Col: int16(r.Uint32())}, signBias: j%2 != 0, refFrame: vp8common.MVReferenceFrame(r.Intn(4))}
		}
		b := a
		bias := [vp8common.MaxRefFrames]bool{false, false, true, true}
		n, rows, cols := i%10, (i%10)-1, (i%7)-1
		biasImprovedInterFrameMVSlots(&a, n, vp8common.LastFrame, bias, i%8, i%8, rows, cols)
		motion02ParentBiasImprovedInterFrameMVSlots(&b, n, vp8common.LastFrame, bias, i%8, i%8, rows, cols)
		if a != b {
			t.Fatalf("case %d differs", i)
		}
	}
}

var motion02PRSlots [8]improvedInterFrameMVSlot

func BenchmarkMotion02PRBias(b *testing.B) {
	var slots [8]improvedInterFrameMVSlot
	for i := range slots {
		slots[i] = improvedInterFrameMVSlot{refFrame: vp8common.MVReferenceFrame(1 + i%3), mv: vp8enc.MotionVector{Row: int16(30 + i*71), Col: int16(-50 - i*79)}, signBias: i&1 != 0}
	}
	bias := [vp8common.MaxRefFrames]bool{false, false, true, true}
	b.ReportAllocs()
	for b.Loop() {
		a := slots
		biasImprovedInterFrameMVSlots(&a, 8, vp8common.LastFrame, bias, 4, 4, 8, 8)
		motion02PRSlots = a
	}
}

func motion03ParentMacroblockLumaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	// Uint range collapses (base >= 0) and (base+16 <= dim) into one
	// compare per dimension (works when dim >= 16; smaller dims fall
	// through to the per-pixel clamped path).
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if sse, ok := macroblockSubpixelSSEBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return sse
			}
		}
	}
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refSlice, ok := refFullPelYSlice(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SSE16x16Fast(src.Y[baseY*src.YStride+baseX:], src.YStride, refSlice, ref.YStride)
		}
	}

	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}
func motion03ParentMacroblockLumaMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if variance, sse, ok := macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return variance, sse
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if variance, sse, ok := macroblockSubpixelVarianceBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return variance, sse
			}
		}
	}
	// Uint range collapses (baseY/X >= 0) and (baseY/X+16 <= dim) into
	// one compare each, matching the pattern in macroblockLumaSSE.
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refSlice, ok := refFullPelYSlice(ref, refBaseY, refBaseX, 16, 16); ok {
			sum, sse := dsp.VarianceBlock16x16Fast(src.Y[baseY*src.YStride+baseX:], src.YStride, refSlice, ref.YStride)
			return sse - ((sum * sum) >> 8), sse
		}
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}
func motion03PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
	src := testImage(w, h)
	for i := range src.Y {
		src.Y[i] = byte(i*17 + i/11)
	}
	ref := testVP8Frame(tb, w, h, 10, 90, 170)
	for i := range ref.Img.Y {
		ref.Img.Y[i] = byte(i*23 + i/7)
	}
	ref.ExtendBorders()
	return sourceImageFromPublic(src), &ref.Img
}
func TestMotion03PRMoments(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {33, 48}, {64, 64}} {
		s, r := motion03PixelSetup(t, sz[0], sz[1])
		for y := 0; y < (s.Height+15)/16; y++ {
			for x := 0; x < (s.Width+15)/16; x++ {
				for _, mv := range []vp8enc.MotionVector{{}, {Row: 8, Col: -8}, {Row: -32, Col: 16}, {Row: 2, Col: 4}} {
					got := macroblockLumaSSE(s, r, y, x, mv)
					want := motion03ParentMacroblockLumaSSE(s, r, y, x, mv)
					a, b := macroblockLumaMotionVarianceSSE(s, r, y, x, mv)
					c, d := motion03ParentMacroblockLumaMotionVarianceSSE(s, r, y, x, mv)
					if got != want || a != c || b != d {
						t.Fatalf("size %v yx %d %d mv %v got %d %d %d want %d %d %d", sz, y, x, mv, got, a, b, want, c, d)
					}
				}
			}
		}
	}
}

var motion03PRInt int

func BenchmarkMotion03PRMoments(b *testing.B) {
	for _, which := range []string{"SSE", "variance"} {
		b.Run("partial_"+which, func(b *testing.B) {
			s, r := motion03PixelSetup(b, 65, 49)
			b.ReportAllocs()
			for b.Loop() {
				if which == "SSE" {
					motion03PRInt = macroblockLumaSSE(s, r, 3, 4, vp8enc.MotionVector{})
				} else {
					a, v := macroblockLumaMotionVarianceSSE(s, r, 3, 4, vp8enc.MotionVector{})
					motion03PRInt = a + v
				}
			}
		})
	}
}

func motion04ParentSplitBlockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
			if sad, ok := splitBlockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset); ok {
				return sad
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, width, height, srcScratch[:], 16)
			if sad, ok := splitBlockSubpixelSADBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, srcScratch[:], 16); ok {
				return sad
			}
		}
	}
	if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
		srcBlock := src.Y[baseY*src.YStride+baseX:]
		refBlock, ok := refFullPelYSlice(ref, refBaseY, refBaseX, width, height)
		if ok {
			switch {
			case width == 16 && height == 8:
				return dsp.SAD16x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 16:
				return dsp.SAD8x16(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 8:
				return dsp.SAD8x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 4 && height == 4:
				return dsp.SAD4x4(srcBlock, src.YStride, refBlock, ref.YStride)
			}
		}
	}

	// libvpx allocates every reference (cm->yv12_fb[]) and lookahead source
	// buffer with 16-aligned width/height via vp8_yv12_alloc_frame_buffer
	// (vp8/common/alloccommon.c:56-65 rounds (width|height) up to a
	// multiple of 16 before calling vp8_yv12_alloc_frame_buffer, and
	// vp8/encoder/lookahead.c:68-70 does the same), so for an odd-axis
	// frame y_crop_height == y_height == coded height (e.g. 368 for a
	// visible 360 input). The post-loop-filter publication path
	// (vp8/encoder/onyx_if.c:3212 vp8_yv12_extend_frame_borders applied to
	// cm->frame_to_show) therefore extends from coded-height-1 (not from
	// visible-height-1), and the coded-but-invisible MB-padded rows/cols
	// retain the LF-modified reconstruction. The SPLITMV picker's NEW4X4
	// SAD walk on padded-edge MBs must read from that region the same way
	// libvpx does — i.e. clamp reference coords to CodedHeight/CodedWidth,
	// not to the visible extent. The source side, by contrast, comes from
	// vp8_copy_and_extend_frame (vp8/common/extend.c:14-72, called from
	// lookahead.c:139) which is a visible-edge extend, so source coords
	// remain clamped to visible Width/Height.
	refClampHeight := ref.CodedHeight
	refClampWidth := ref.CodedWidth
	sad := 0
	for row := range height {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, refClampHeight)
		for col := range width {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, refClampWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}
func motion04PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
	src := testImage(w, h)
	for i := range src.Y {
		src.Y[i] = byte(i*17 + i/11)
	}
	ref := testVP8Frame(tb, w, h, 10, 90, 170)
	for i := range ref.Img.Y {
		ref.Img.Y[i] = byte(i*23 + i/7)
	}
	ref.ExtendBorders()
	return sourceImageFromPublic(src), &ref.Img
}
func TestMotion04PRSplitSAD(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {33, 48}, {64, 64}} {
		s, r := motion04PixelSetup(t, sz[0], sz[1])
		for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
			for _, block := range []int{0, 5, 15} {
				for _, mv := range []vp8enc.MotionVector{{}, {Row: 8, Col: -8}, {Row: -32, Col: 16}, {Row: 2, Col: 4}} {
					y, x := (s.Height-1)/16, (s.Width-1)/16
					got := splitBlockSAD(s, r, y, x, block, shape[0], shape[1], mv)
					want := motion04ParentSplitBlockSAD(s, r, y, x, block, shape[0], shape[1], mv)
					if got != want {
						t.Fatalf("size %v shape %v block %d mv %v got %d want %d", sz, shape, block, mv, got, want)
					}
				}
			}
		}
	}
}

var motion04PRInt int

func BenchmarkMotion04PRSplitSAD(b *testing.B) {
	for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
		b.Run(fmt.Sprintf("partial_%dx%d", shape[0], shape[1]), func(b *testing.B) {
			s, r := motion04PixelSetup(b, 65, 49)
			b.ReportAllocs()
			for b.Loop() {
				motion04PRInt = splitBlockSAD(s, r, 3, 4, 0, shape[0], shape[1], vp8enc.MotionVector{})
			}
		})
	}
}

func motion06ParentFillSplitMotionResidual4x4(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred *[16]byte, out *[16]int16) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	for row := range 4 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			out[row*4+col] = int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*4+col]))
		}
	}
}
func motion06PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
	src := testImage(w, h)
	for i := range src.Y {
		src.Y[i] = byte(i*17 + i/11)
	}
	ref := testVP8Frame(tb, w, h, 10, 90, 170)
	for i := range ref.Img.Y {
		ref.Img.Y[i] = byte(i*23 + i/7)
	}
	ref.ExtendBorders()
	return sourceImageFromPublic(src), &ref.Img
}
func TestMotion06PRResidual(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {64, 64}} {
		s, _ := motion06PixelSetup(t, sz[0], sz[1])
		var p [16]byte
		for i := range p {
			p[i] = byte(i * 13)
		}
		for y := 0; y < (s.Height+15)/16; y++ {
			for x := 0; x < (s.Width+15)/16; x++ {
				for block := range 16 {
					var a, b [16]int16
					fillSplitMotionResidual4x4(s, y, x, block, &p, &a)
					motion06ParentFillSplitMotionResidual4x4(s, y, x, block, &p, &b)
					if a != b {
						t.Fatal(sz, y, x, block)
					}
				}
			}
		}
	}
}

var motion06PRResidual [16]int16

func BenchmarkMotion06PRResidual(b *testing.B) {
	s, _ := motion06PixelSetup(b, 64, 64)
	var p [16]byte
	for i := range p {
		p[i] = byte(i * 13)
	}
	b.ReportAllocs()
	for b.Loop() {
		fillSplitMotionResidual4x4(s, 1, 1, 5, &p, &motion06PRResidual)
	}
}

func motion07ParentIterativeInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, errorPerBit int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	br := (int(best.Row) >> 3) * 4
	bc := (int(best.Col) >> 3) * 4
	tr := br
	tc := bc
	bestMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	bestDist, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, br, bc)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost := bestDist + interMotionSearchErrorVectorCostWithErrorPerBitAndCostTables(bestMV, bestRefMV, errorPerBit, mvProbs, mvCosts)
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := vp8enc.InterFrameSubpelSearchBoundsFor(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	_ = qIndex
	var cache subpelEvalCache
	cand := func(r, c int) int {
		return cachedBoundedSplitBlockSubpixelMotionSearchCandidateCost(&cache, bounds, src, ref, mbRow, mbCol, block, width, height, r, c, bestRefMV, errorPerBit, mvProbs, mvCosts)
	}

	for range 3 {
		leftCost := cand(tr, tc-2)
		rightCost := cand(tr, tc+2)
		upCost := cand(tr-2, tc)
		downCost := cand(tr+2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+2, tc)

		diagRow := tr - 2
		if upCost >= downCost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftCost >= rightCost {
			diagCol = tc + 2
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftCost := cand(tr, tc-1)
		rightCost := cand(tr, tc+1)
		upCost := cand(tr-1, tc)
		downCost := cand(tr+1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+1, tc)

		diagRow := tr - 1
		if upCost >= downCost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftCost >= rightCost {
			diagCol = tc + 1
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !vp8enc.InterFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}
func motion07PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
	src := testImage(w, h)
	for i := range src.Y {
		src.Y[i] = byte(i*17 + i/11)
	}
	ref := testVP8Frame(tb, w, h, 10, 90, 170)
	for i := range ref.Img.Y {
		ref.Img.Y[i] = byte(i*23 + i/7)
	}
	ref.ExtendBorders()
	return sourceImageFromPublic(src), &ref.Img
}
func TestMotion07PRSubpelSource(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {64, 64}} {
		s, r := motion07PixelSetup(t, sz[0], sz[1])
		var costs vp8enc.MotionVectorCostTables
		costs.Build(&vp8tables.DefaultMVContext)
		for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
			for _, block := range []int{0, 5, 15} {
				for _, center := range []vp8enc.MotionVector{{}, {Row: 8, Col: -8}, {Row: -32, Col: 16}} {
					y, x := (s.Height-1)/16, (s.Width-1)/16
					a, b, c := iterativeInterFrameSplitBlockSubpixelMotionVector(s, r, y, x, block, shape[0], shape[1], center, vp8enc.MotionVector{}, 44, 34, &vp8tables.DefaultMVContext, &costs)
					d, e, f := motion07ParentIterativeInterFrameSplitBlockSubpixelMotionVector(s, r, y, x, block, shape[0], shape[1], center, vp8enc.MotionVector{}, 44, 34, &vp8tables.DefaultMVContext, &costs)
					if a != d || b != e || c != f {
						t.Fatal(sz, shape, block, center, a, b, c, d, e, f)
					}
				}
			}
		}
	}
}

var motion07PRInt int
var motion07PRMV vp8enc.MotionVector

func BenchmarkMotion07PRSubpelSource(b *testing.B) {
	for _, shape := range [][2]int{{8, 8}, {4, 4}} {
		b.Run(fmt.Sprintf("partial_%dx%d", shape[0], shape[1]), func(b *testing.B) {
			s, r := motion07PixelSetup(b, 65, 49)
			var costs vp8enc.MotionVectorCostTables
			costs.Build(&vp8tables.DefaultMVContext)
			b.ReportAllocs()
			for b.Loop() {
				mv, c, ok := iterativeInterFrameSplitBlockSubpixelMotionVector(s, r, 3, 4, 0, shape[0], shape[1], vp8enc.MotionVector{}, vp8enc.MotionVector{}, 44, 34, &vp8tables.DefaultMVContext, &costs)
				motion07PRInt = c
				if !ok {
					b.Fatal("invalid")
				}
				motion07PRMV = mv
			}
		})
	}
}

func motion08ParentImprovedInterFrameMVMedian(slots [8]improvedInterFrameMVSlot, count int) vp8enc.MotionVector {
	if count <= 0 {
		return vp8enc.MotionVector{}
	}
	var rows [8]int
	var cols [8]int
	// limit ∈ [0, 8] by construction (min with array length); using
	// limit instead of count for both the sort range and the median
	// index avoids a per-call IsSliceInBounds on rows[:count]/
	// cols[:count] (count is unbounded from the compiler's view) and
	// also closes a latent panic when count > 8.
	limit := min(count, len(slots))
	for i := range limit {
		rows[i] = int(slots[i].mv.Row)
		cols[i] = int(slots[i].mv.Col)
	}
	insertionSortInts(rows[:limit])
	insertionSortInts(cols[:limit])
	return vp8enc.MotionVector{Row: int16(rows[limit/2]), Col: int16(cols[limit/2])}
}
func TestMotion08PRMedian(t *testing.T) {
	r := rand.New(rand.NewSource(588))
	for range 1000 {
		var a [8]improvedInterFrameMVSlot
		for j := range a {
			a[j].mv = vp8enc.MotionVector{Row: int16(r.Uint32()), Col: int16(r.Uint32())}
		}
		for n := 0; n <= 9; n++ {
			if got, want := improvedInterFrameMVMedian(a, n), motion08ParentImprovedInterFrameMVMedian(a, n); got != want {
				t.Fatal(n, got, want)
			}
		}
	}
}

var motion08PRMV vp8enc.MotionVector

func BenchmarkMotion08PRMedian(b *testing.B) {
	var a [8]improvedInterFrameMVSlot
	for i := range a {
		a[i].mv = vp8enc.MotionVector{Row: int16(i*47 - 70), Col: int16(20 - i*72)}
	}
	b.ReportAllocs()
	for b.Loop() {
		motion08PRMV = improvedInterFrameMVMedian(a, 3)
	}
}

func motion09ParentDiamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(search *splitFullPelSearchCtx, center vp8enc.MotionVector, centerWalkCost int, centerReturnCost int, bestRefMV vp8enc.MotionVector, refRowFull int, refColFull int, qIndex int, errorPerBit int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, bounds vp8enc.InterFrameFullPixelBounds, searchParam int) splitFullPixelSearchResult {
	sites := vp8enc.InterFrameNstepSearchSites[:]
	if searchParam < 0 {
		searchParam = 0
	} else if searchParam >= interFrameMaxMVSearchSteps {
		searchParam = interFrameMaxMVSearchSteps - 1
	}
	best := center
	bestWalkCost := centerWalkCost
	start := center
	startIndex := searchParam * 8
	totalSteps := (len(sites) / 8) - searchParam
	i := 1
	bestSite := 0
	lastSite := 0
	num00 := 0
	for range totalSteps {
		for range 8 {
			site := sites[startIndex+i]
			row := (int(best.Row) >> 3) + int(site.Row)
			col := (int(best.Col) >> 3) + int(site.Col)
			if bounds.ContainsFullPelStrict(row, col) {
				cost := search.fullPelCostFull(row, col, refRowFull, refColFull, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = i
				}
			}
			i++
		}
		if bestSite != lastSite {
			site := sites[startIndex+bestSite]
			best = vp8enc.MotionVector{
				Row: int16(int(best.Row) + int(site.Row)*interFrameMVFullPixelStep),
				Col: int16(int(best.Col) + int(site.Col)*interFrameMVFullPixelStep),
			}
			lastSite = bestSite
		} else if best == start {
			num00++
		}
	}
	returnCost := centerReturnCost
	if best != center {
		var ok bool
		returnCost, ok = interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(search.src, search.ref, search.mbRow, search.mbCol, search.block, search.width, search.height, best, bestRefMV, errorPerBit, mvProbs, mvCosts)
		if !ok {
			returnCost = maxInt()
		}
	}
	return splitFullPixelSearchResult{mv: best, walkCost: bestWalkCost, returnCost: returnCost, num00: uint8(num00)}
}
func motion09ParentFullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(search *splitFullPelSearchCtx, center vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, refRowFull int, refColFull int, qIndex int, errorPerBit int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, bounds vp8enc.InterFrameFullPixelBounds, distance int) splitFullPixelSearchResult {
	refRow := int(center.Row) >> 3
	refCol := int(center.Col) >> 3
	rowMin := refRow - distance
	rowMax := refRow + distance
	colMin := refCol - distance
	colMax := refCol + distance
	if rowMin < bounds.RowMin {
		rowMin = bounds.RowMin
	}
	if rowMax > bounds.RowMax {
		rowMax = bounds.RowMax
	}
	if colMin < bounds.ColMin {
		colMin = bounds.ColMin
	}
	if colMax > bounds.ColMax {
		colMax = bounds.ColMax
	}
	best := center
	bestWalkCost := search.fullPelCostFull(int(best.Row)>>3, int(best.Col)>>3, refRowFull, refColFull, qIndex)
	for row := rowMin; row < rowMax; row++ {
		for col := colMin; col < colMax; col++ {
			mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
			cost := search.fullPelCostFull(row, col, refRowFull, refColFull, qIndex)
			if cost < bestWalkCost {
				best = mv
				bestWalkCost = cost
			}
		}
	}
	returnCost, ok := interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(search.src, search.ref, search.mbRow, search.mbCol, search.block, search.width, search.height, best, bestRefMV, errorPerBit, mvProbs, mvCosts)
	if !ok {
		returnCost = maxInt()
	}
	return splitFullPixelSearchResult{mv: best, walkCost: bestWalkCost, returnCost: returnCost}
}
func motion09PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
	src := testImage(w, h)
	for i := range src.Y {
		src.Y[i] = byte(i*17 + i/11)
	}
	ref := testVP8Frame(tb, w, h, 10, 90, 170)
	for i := range ref.Img.Y {
		ref.Img.Y[i] = byte(i*23 + i/7)
	}
	ref.ExtendBorders()
	return sourceImageFromPublic(src), &ref.Img
}
func TestMotion09PRSplitRateBound(t *testing.T) {
	for _, sz := range [][2]int{{64, 64}, {65, 49}} {
		src, ref := motion09PixelSetup(t, sz[0], sz[1])
		var costs vp8enc.MotionVectorCostTables
		costs.Build(&vp8tables.DefaultMVContext)
		for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
			for _, q := range []int{0, 44, 127} {
				y, x := (src.Height-1)/16, (src.Width-1)/16
				c1 := newSplitFullPelSearchCtx(src, ref, y, x, 0, shape[0], shape[1])
				c2 := c1
				bounds := vp8enc.InterFrameFullPixelSearchBounds(vp8enc.MotionVector{}, y, x, (src.Height+15)/16, (src.Width+15)/16)
				center := vp8enc.MotionVector{}
				a := fullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c1, center, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, 4)
				b := motion09ParentFullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c2, center, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, 4)
				if a != b {
					t.Fatal("full", sz, shape, q, a, b)
				}
				wc := c1.fullPelCostFull(0, 0, 0, 0, q)
				rc, ok := interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(src, ref, y, x, 0, shape[0], shape[1], center, center, 34, &vp8tables.DefaultMVContext, &costs)
				if !ok {
					t.Fatal("returncost")
				}
				for param := range 8 {
					a := diamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c1, center, wc, rc, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, param)
					b := motion09ParentDiamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c2, center, wc, rc, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, param)
					if a != b {
						t.Fatal("diamond", sz, shape, q, param, a, b)
					}
				}
			}
		}
	}
}

var motion09PRInt int
var motion09PRMV vp8enc.MotionVector

func BenchmarkMotion09PRSplitRateBound(b *testing.B) {
	for _, shape := range [][2]int{{8, 8}, {4, 4}} {
		for _, which := range []string{"full", "nstep"} {
			b.Run(fmt.Sprintf("%s_%dx%d", which, shape[0], shape[1]), func(b *testing.B) {
				src, ref := motion09PixelSetup(b, 128, 96)
				for y := 0; y < src.Height; y++ {
					for x := 0; x < src.Width; x++ {
						src.Y[y*src.YStride+x] = ref.Y[y*ref.YStride+min(x+1, ref.CodedWidth-1)]
					}
				}
				ctx := newSplitFullPelSearchCtx(src, ref, 2, 2, 0, shape[0], shape[1])
				var costs vp8enc.MotionVectorCostTables
				costs.Build(&vp8tables.DefaultMVContext)
				bounds := vp8enc.InterFrameFullPixelSearchBounds(vp8enc.MotionVector{}, 2, 2, 6, 8)
				center := vp8enc.MotionVector{}
				wc := ctx.fullPelCostFull(0, 0, 0, 0, 44)
				rc, _ := interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(src, ref, 2, 2, 0, shape[0], shape[1], center, center, 34, &vp8tables.DefaultMVContext, &costs)
				b.ReportAllocs()
				for b.Loop() {
					var r splitFullPixelSearchResult
					if which == "full" {
						r = fullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&ctx, center, center, 0, 0, 44, 34, &vp8tables.DefaultMVContext, &costs, bounds, 6)
					} else {
						r = diamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&ctx, center, wc, rc, center, 0, 0, 44, 34, &vp8tables.DefaultMVContext, &costs, bounds, 4)
					}
					motion09PRMV = r.mv
					motion09PRInt = r.returnCost + r.walkCost
				}
			})
		}
	}
}

func (ctx *loopFilterPickContext) motion10ParentPickFastNoStats(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	level := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	bestLevel := level
	bestErr := ctx.cachedPartialLumaSSE(level, &ssErr, &ssSet)
	if traceEnabled {
		e.emitOracleLFTrial("seed", level, bestErr)
	}

	filtLevel := level - vp8enc.LoopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := ctx.cachedPartialLumaSSE(filtLevel, &ssErr, &ssSet)
		if traceEnabled {
			e.emitOracleLFTrial("down", filtLevel, filtErr)
		}
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= vp8enc.LoopFilterSearchStep(filtLevel)
	}

	filtLevel = level + vp8enc.LoopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := ctx.cachedPartialLumaSSE(filtLevel, &ssErr, &ssSet)
			if traceEnabled {
				e.emitOracleLFTrial("up", filtLevel, filtErr)
			}
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += vp8enc.LoopFilterSearchStep(filtLevel)
		}
	}
	return uint8(vp8enc.ClampLoopFilterPickLevel(bestLevel, minLevel, maxLevel)), nil
}
func (ctx *loopFilterPickContext) motion10ParentPickFullNoStats(seedLevel uint8, minLevel int) (uint8, error) {
	e := ctx.encoder
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if ctx.fullFrameConfig.SegmentationEnabled {
		e.loopFilterSegmentLF = ctx.fullFrameConfig.SegmentLF
	}
	maxLevel := e.libvpxMaxLoopFilterLevelForFrame()
	filtMid := vp8enc.ClampLoopFilterPickLevel(int(seedLevel), minLevel, maxLevel)
	filterStep := 4
	if filtMid >= 16 {
		filterStep = filtMid / 4
	}
	ssErr := [vp8common.MaxLoopFilter + 1]int{}
	ssSet := [vp8common.MaxLoopFilter + 1]bool{}
	residentLevel := -1
	bestLumaLevel := -1

	bestErr := ctx.cachedFullLumaSSE(filtMid, &ssErr, &ssSet, &residentLevel)
	if traceEnabled {
		e.emitOracleLFTrial("full", filtMid, bestErr)
	}
	filtBest := filtMid
	filtDirection := 0
	for filterStep > 0 {
		// Mirror libvpx vp8/encoder/picklpf.c vp8cx_pick_filter_level
		// (Bias = (best_err >> (15 - (filt_mid / 8))) * filter_step), then
		// scale that bias by section_intra_rating / 20 when the intra
		// rating is below 20. One-pass and realtime paths can therefore
		// use zero bias because libvpx's calloc'd two-pass state leaves
		// section_intra_rating at zero.
		bias := vp8enc.LoopFilterFullPickerBias(bestErr, filtMid, filterStep, e.twoPass.sectionIntraRating)
		filtHigh := min(filtMid+filterStep, maxLevel)
		filtLow := max(filtMid-filterStep, minLevel)

		// Parallel filt_low / filt_high trials at Threads >= 2 when this
		// iteration would otherwise run both serially (filtDirection == 0,
		// both bracket levels distinct from filtMid, neither cached). The
		// filt_low trial runs on the row-worker pool against the alt
		// scratch (loopFilterPickAlt + loopInfoAlt); the filt_high trial
		// runs inline on the calling goroutine against the resident
		// scratch (loopFilterPick + loopInfo). After both finish the
		// score-update conditionals are applied in the same serial order
		// as the non-parallel branch, so bestErr / filtBest converge
		// byte-identically to the serial run. residentLevel is set to
		// filtHigh to match the post-iteration state of the serial code.
		// loopFilterPick (resident scratch) holds the filtHigh-filtered
		// luma; loopFilterPickAlt holds the filtLow-filtered luma. If
		// filtLow becomes the new filtBest we patch loopFilterBest with
		// the alt-scratch luma so the chroma post-pass reuse decision in
		// applyReconstructionLoopFilter sees the correct preserved image
		// (mirroring the serial preserveBestBeforeTrial-before-filtHigh
		// copy that would have run had the trials been serial).
		// ssSet is [MaxLoopFilter+1=64]bool (pow2); LF trial levels are
		// clamped to [0, MaxLoopFilter]. AND-mask with 63 elides the
		// per-iter bounds check on both lookups.
		if filtDirection == 0 && filtLow != filtMid && filtHigh != filtMid &&
			!ssSet[filtLow&63] && !ssSet[filtHigh&63] && e.canParallelLFTrials() {
			ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
			filtErrLow, filtErrHigh := ctx.dispatchLFTrialPair(filtLow, filtHigh)
			// ssErr/ssSet are [MaxLoopFilter+1=64]; mask with 63 to elide
			// the bounds check on the indexed writes (matches the read
			// path above).
			ssErr[filtLow&63] = filtErrLow
			ssSet[filtLow&63] = true
			ssErr[filtHigh&63] = filtErrHigh
			ssSet[filtHigh&63] = true
			if filtHigh != 0 {
				residentLevel = filtHigh
			}
			if traceEnabled {
				e.emitOracleLFTrial("full", filtLow, filtErrLow)
				e.emitOracleLFTrial("full", filtHigh, filtErrHigh)
			}
			if filtErrLow-bias < bestErr {
				if filtErrLow < bestErr {
					bestErr = filtErrLow
				}
				filtBest = filtLow
				// Replicate the serial "preserveBestBeforeTrial(filtHigh,
				// filtBest=filtLow)" copy that would have run between
				// the two trials: residentLevel == filtBest == filtLow,
				// so loopFilterPick (which would hold the filtLow-filtered
				// luma in serial) gets copied to loopFilterBest. In
				// parallel that filtLow-filtered luma is in
				// loopFilterPickAlt instead, so we copy from there.
				if filtBest > 0 && bestLumaLevel != filtBest {
					vp8common.CopyImageLuma(&e.loopFilterBest.Img, &e.loopFilterPickAlt.Img)
					bestLumaLevel = filtBest
				}
			}
			if filtErrHigh < bestErr-bias {
				bestErr = filtErrHigh
				filtBest = filtHigh
			}
		} else {
			if filtDirection <= 0 && filtLow != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtLow, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtLow]
				filtErr := ctx.cachedFullLumaSSE(filtLow, &ssErr, &ssSet, &residentLevel)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtLow, filtErr)
				}
				if filtErr-bias < bestErr {
					if filtErr < bestErr {
						bestErr = filtErr
					}
					filtBest = filtLow
				}
			}
			if filtDirection >= 0 && filtHigh != filtMid {
				ctx.preserveBestLoopFilterLumaBeforeTrial(filtHigh, filtBest, &ssSet, residentLevel, &bestLumaLevel)
				freshTrial := !ssSet[filtHigh]
				filtErr := ctx.cachedFullLumaSSE(filtHigh, &ssErr, &ssSet, &residentLevel)
				if traceEnabled && freshTrial {
					e.emitOracleLFTrial("full", filtHigh, filtErr)
				}
				if filtErr < bestErr-bias {
					bestErr = filtErr
					filtBest = filtHigh
				}
			}
		}
		if filtBest == filtMid {
			filterStep /= 2
			filtDirection = 0
		} else {
			if filtBest < filtMid {
				filtDirection = -1
			} else {
				filtDirection = 1
			}
			filtMid = filtBest
		}
	}
	if filtBest > 0 {
		switch {
		case residentLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = false
		case bestLumaLevel == filtBest:
			e.loopFilterPickReady = true
			e.loopFilterPickLevel = uint8(filtBest)
			e.loopFilterPickBest = true
		}
	}
	return uint8(filtBest), nil
}
func motion10PRContext(tb testing.TB, varied bool) loopFilterPickContext {
	const w, h = 128, 96
	src := testImage(w, h)
	e := newSizedTestEncoder(tb, w, h)
	for y := range h {
		for x := range w {
			v := byte(128)
			if varied {
				v = byte(40 + (y*17+x*13)%180)
			}
			src.Y[y*src.YStride+x] = v
			e.analysis.Img.Y[y*e.analysis.Img.YStride+x] = v
		}
	}
	e.analysis.ExtendBorders()
	const rows, cols = h / 16, w / 16
	if len(e.reconstructModes) < rows*cols {
		e.reconstructModes = make([]vp8dec.MacroblockMode, rows*cols)
	}
	for i := range e.reconstructModes {
		e.reconstructModes[i] = vp8dec.MacroblockMode{Mode: vp8common.NearestMV, RefFrame: vp8common.LastFrame}
	}
	return e.newLoopFilterPickContext(sourceImageFromPublic(src), vp8common.InterFrame, 0, rows, cols, rows*cols, vp8enc.SegmentationConfig{})
}
func TestMotion10PRZeroLFError(t *testing.T) {
	for _, varied := range []bool{false, true} {
		for _, level := range []uint8{0, 1, 8, 16, 32, 63} {
			for _, full := range []bool{false, true} {
				a, b := motion10PRContext(t, varied), motion10PRContext(t, varied)
				var got, want uint8
				var ge, we error
				if full {
					got, ge = a.pickFullNoStats(level, 0)
					want, we = b.motion10ParentPickFullNoStats(level, 0)
				} else {
					got, ge = a.pickFastNoStats(level, 0)
					want, we = b.motion10ParentPickFastNoStats(level, 0)
				}
				if got != want || ge != we {
					t.Fatal(varied, level, full, got, want, ge, we)
				}
				if a.encoder.loopFilterPickReady != b.encoder.loopFilterPickReady || a.encoder.loopFilterPickLevel != b.encoder.loopFilterPickLevel {
					t.Fatal("resident flag", varied, level, full)
				}
				if a.encoder.loopFilterPickReady {
					p, q := &a.encoder.loopFilterPick.Img, &b.encoder.loopFilterPick.Img
					if a.encoder.loopFilterPickBest {
						p = &a.encoder.loopFilterBest.Img
					}
					if b.encoder.loopFilterPickBest {
						q = &b.encoder.loopFilterBest.Img
					}
					for y := range 96 {
						for x := range 128 {
							if p.Y[y*p.YStride+x] != q.Y[y*q.YStride+x] {
								t.Fatal("resident pixels", varied, level, full, y, x)
							}
						}
					}
				}
			}
		}
	}
}

var motion10PRInt int

func BenchmarkMotion10PRZeroLFError(b *testing.B) {
	for _, full := range []bool{false, true} {
		b.Run(fmt.Sprint("full", full), func(b *testing.B) {
			ctx := motion10PRContext(b, false)
			b.ReportAllocs()
			for b.Loop() {
				var l uint8
				var e error
				if full {
					l, e = ctx.pickFullNoStats(32, 0)
				} else {
					l, e = ctx.pickFastNoStats(32, 0)
				}
				motion10PRInt = int(l)
				if e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}

func (ctx *splitMotionSubsetContext) motion11ParentSelectMotion() (vp8enc.MotionVector, vp8common.BPredictionMode, int, int, int, int, int) {
	// MBSplitOffset is [4][16]uint8: ctx.mode.Partition ∈ [0,4) and
	// ctx.subset ∈ [0,16) by upstream validation. Pow2 AND-masks
	// elide both bounds checks on this hot per-subset load.
	block := int(vp8tables.MBSplitOffset[ctx.mode.Partition&3][ctx.subset&15])
	leftMV := vp8enc.SplitLeftMV(ctx.mode, ctx.left, block)
	aboveMV := vp8enc.SplitAboveMV(ctx.mode, ctx.above, block)
	mbRows := (ctx.src.Height + 15) >> 4
	mbCols := (ctx.src.Width + 15) >> 4
	bestMV := vp8enc.MotionVector{}
	bestMode := vp8common.Zero4x4
	bestRD := maxInt()
	bestRate := 0
	bestYRate := 0
	bestDistortion := 0
	// lastTTEOB tracks the per-block-tteob from the LAST inner-loop iteration
	// that actually called vp8_encode_inter_mb_segment (i.e. wasn't UMV-
	// trapped and wasn't gated out before the call). libvpx returns this
	// side-effect via `xd->eobs[i]` because rd_check_segment never restores
	// the eobs after the winning mode is re-selected by `labels2mode`
	// (rdopt.c:1152-1158): only the entropy contexts (t_above_b/t_left_b)
	// are restored, the per-block eob registers retain the last call's
	// values, and bsi->eobs[i] = xd->eobs[i] at rdopt.c:1180 then captures
	// that stale snapshot for the segment_rd-wins path. calculate_final_rd_
	// costs reads tteob back through those stale eobs (rdopt.c:1689-1697),
	// so the SPLITMV skip-backout gate depends on the LAST-tested mode's
	// tteob, not the RD-winning mode's. Track it independently here.
	lastTTEOB := 0
	var bestAbove [4]uint8
	var bestLeft [4]uint8
	bestHasContexts := false

	tryCandidate := func(candidateMode vp8common.BPredictionMode, mv vp8enc.MotionVector) {
		// rd_check_segment applies the same UMV trap to inherited
		// LEFT/ABOVE/ZERO labels as it does to searched NEW labels.
		if !vp8enc.InterFrameUMVFullPixelInRange(mv, ctx.mbRow, ctx.mbCol, mbRows, mbCols) {
			return
		}
		rate := vp8enc.SplitSubMotionLabelRate(candidateMode, ctx.subMVRefProbs)
		rd, labelRate, labelYRate, distortion, tteob, nextAbove, nextLeft, hasContexts := ctx.candidateRD(block, mv, rate)
		lastTTEOB = tteob
		if rd < bestRD {
			bestRD = rd
			bestRate = labelRate
			bestYRate = labelYRate
			bestDistortion = distortion
			bestMV = mv
			bestMode = candidateMode
			bestAbove = nextAbove
			bestLeft = nextLeft
			bestHasContexts = hasContexts
		}
	}

	tryCandidate(vp8common.Left4x4, leftMV)
	if aboveMV != leftMV {
		tryCandidate(vp8common.Above4x4, aboveMV)
	}
	tryCandidate(vp8common.Zero4x4, vp8enc.MotionVector{})

	// libvpx: `if (best_label_rd < label_mv_thresh) break;` — the running
	// best label score is already below the per-label MV threshold, skip
	// the NEW4X4 motion search (the most expensive trial). When the gate
	// is disabled (labelMVThresh == 0) we keep the legacy behavior of
	// always running the NEW4X4 search. We compare in RDCOST space so
	// the threshold matches the libvpx rd_threshes scale.
	//
	// libvpx breaks out of the inner mode loop here BEFORE calling
	// vp8_encode_inter_mb_segment for NEW4X4 (rdopt.c:1027 is inside the
	// `if (this_mode == NEW4X4)` block and precedes the segment-encode
	// call at 1135), so the lastTTEOB carry from ZERO4X4 (or the prior
	// non-trapped mode) is what bsi->eobs sees. lastTTEOB stays untouched
	// in this branch — _not_ assigned bestTTEOB — exactly matching that
	// side-effect.
	if ctx.labelMVThresh > 0 && bestRD < ctx.labelMVThresh {
		if ctx.labelRD != nil && bestHasContexts {
			ctx.labelRD.yAbove = bestAbove
			ctx.labelRD.yLeft = bestLeft
		}
		return bestMV, bestMode, bestRD, bestRate, bestYRate, bestDistortion, lastTTEOB
	}

	errorPerBit := ctx.errorPerBit
	if errorPerBit <= 0 {
		errorPerBit = vp8enc.ErrorPerBit(ctx.qIndex)
	}
	// libvpx vp8_rd_pick_best_mbsegmentation (vp8/encoder/rdopt.c:1199-
	// 1303) runs each rd_check_segment call inside one of two branches:
	//
	//   * Best mode (cpi->compressor_speed == 0, rdopt.c:1220-1226):
	//     all four segmentations (BLOCK_16X8/8X16/8X8/4X4) execute back-
	//     to-back with x->mv_col_min/x->mv_col_max untouched at their
	//     wide MB-scope UMV window. The [best_ref_mv ± MAX_FULL_PEL_VAL]
	//     intersection block (rdopt.c:1233-1248) lives in the `else`
	//     speed-mode branch and is never reached here.
	//
	//   * Speed mode (compressor_speed != 0, rdopt.c:1227-1302): the
	//     first BLOCK_8X8 call runs against the wide UMV window, then
	//     mv_col_min/max are tightened to the intersection with
	//     [best_ref_mv ± MAX_FULL_PEL_VAL] before BLOCK_8X16/16X8/4X4
	//     and restored afterwards (rdopt.c:1297-1301).
	//
	// govpx mirrors both shapes: in best mode every partition uses the
	// wide UMV window; in speed mode only partition 2 (BLOCK_8X8) does,
	// and the secondary partitions intersect with bestRefMV±MAX_FULL_PEL_
	// VAL. Earlier code intersected the window in best mode too,
	// truncating the SPLITMV per-label diamond search at MB(0,0) frame 1
	// for partitions 0/1/3 on the 1280x720 SSIM-best cohort and biasing
	// the picker away from SPLITMV.
	var bounds vp8enc.InterFrameFullPixelBounds
	if ctx.compressorSpeed == 0 || (ctx.mode != nil && ctx.mode.Partition == 2) {
		bounds = vp8enc.InterFrameUMVOnlyFullPixelSearchBounds(ctx.mbRow, ctx.mbCol, mbRows, mbCols)
	} else {
		bounds = vp8enc.InterFrameFullPixelSearchBounds(ctx.bestRefMV, ctx.mbRow, ctx.mbCol, mbRows, mbCols)
	}
	newMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorWithBounds(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, block, ctx.width, ctx.height, ctx.searchCenter, ctx.bestRefMV, ctx.qIndex, errorPerBit, ctx.stepParam, ctx.fullSearchFallback, ctx.mvProbs, ctx.mvCosts, bounds)
	if refinedMV, _, ok := refineInterFrameSplitBlockSubpixelMotionVectorWithErrorPerBitAndCostTables(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, block, ctx.width, ctx.height, newMV, ctx.bestRefMV, ctx.qIndex, errorPerBit, ctx.search, ctx.mvProbs, ctx.mvCosts); ok {
		newMV = refinedMV
	}
	newRate := vp8enc.SplitSubMotionLabelRate(vp8common.New4x4, ctx.subMVRefProbs)
	delta := vp8enc.MotionVector{Row: int16(int(newMV.Row) - int(ctx.bestRefMV.Row)), Col: int16(int(newMV.Col) - int(ctx.bestRefMV.Col))}
	if ctx.mvCosts != nil {
		newRate += vp8enc.SplitMotionVectorCost(delta, nil, ctx.mvCosts)
	} else {
		newRate += vp8enc.SplitMotionVectorCost(delta, ctx.mvProbs, nil)
	}
	newRD, labelRate, labelYRate, distortion, tteob, nextAbove, nextLeft, hasContexts := ctx.candidateRD(block, newMV, newRate)
	lastTTEOB = tteob
	if newRD < bestRD {
		bestRD = newRD
		bestRate = labelRate
		bestYRate = labelYRate
		bestDistortion = distortion
		bestMV = newMV
		bestMode = vp8common.New4x4
		bestAbove = nextAbove
		bestLeft = nextLeft
		bestHasContexts = hasContexts
	}
	if ctx.labelRD != nil && bestHasContexts {
		ctx.labelRD.yAbove = bestAbove
		ctx.labelRD.yLeft = bestLeft
	}

	return bestMV, bestMode, bestRD, bestRate, bestYRate, bestDistortion, lastTTEOB
}
func motion11PRContext(tb testing.TB, partition int, fast bool) splitMotionSubsetContext {
	src, ref, _, quant, q := splitMVDecisionRDFixture(tb)
	ev := new(splitMotionLabelRDEvaluator)
	ev.init(0, 0, nil, nil, fast, false)
	mode := &vp8enc.InterFrameMacroblockMode{Mode: vp8common.SplitMV, Partition: uint8(partition)}
	w, h := splitMotionPartitionBlockSize(partition)
	return splitMotionSubsetContext{src: src, ref: ref, mode: mode, subset: 0, width: w, height: h, qIndex: q, labelMVThresh: maxInt(), labelRD: ev, quant: &quant, coefProbs: &vp8tables.DefaultCoefProbs, mvProbs: &vp8tables.DefaultMVContext, search: defaultInterAnalysisSearchConfig(), stepParam: 7}
}
func TestMotion11PRDuplicateSplitLabels(t *testing.T) {
	for partition := range 4 {
		for _, fast := range []bool{false, true} {
			for _, fallback := range []bool{false, true} {
				for _, gate := range []int{0, maxInt()} {
					ctx := motion11PRContext(t, partition, fast)
					ctx.labelMVThresh = gate
					if fallback {
						ctx.labelRD = nil
					}
					a, b := ctx, ctx
					var ea, eb splitMotionLabelRDEvaluator
					if ctx.labelRD != nil {
						ea, eb = *ctx.labelRD, *ctx.labelRD
						a.labelRD = &ea
						b.labelRD = &eb
					}
					mv, mode, rd, rate, yr, dist, te := a.selectMotion()
					wm, wb, wr, wt, wy, wd, we := b.motion11ParentSelectMotion()
					if mv != wm || mode != wb || rd != wr || rate != wt || yr != wy || dist != wd || te != we || ea != eb {
						t.Fatal(partition, fast, fallback, gate, mv, mode, rd, rate, yr, dist, te, wm, wb, wr, wt, wy, wd, we, ea, eb)
					}
				}
			}
		}
	}
}

var motion11PRInt int
var motion11PRMV vp8enc.MotionVector

func BenchmarkMotion11PRDuplicateSplitLabels(b *testing.B) {
	for _, part := range []int{0, 2, 3} {
		b.Run(fmt.Sprint("partition", part), func(b *testing.B) {
			ctx := motion11PRContext(b, part, true)
			initial := *ctx.labelRD
			b.ReportAllocs()
			for b.Loop() {
				*ctx.labelRD = initial
				mv, _, rd, rate, yr, dist, te := ctx.selectMotion()
				motion11PRMV = mv
				motion11PRInt = rd + rate + yr + dist + te
			}
		})
	}
}

func motion12ParentFillInterFrameSplitSubsetWithMode(mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector, firstMode vp8common.BPredictionMode) {
	if mode == nil || mode.Partition >= vp8tables.NumMBSplits {
		return
	}
	for block := range 16 {
		// MBSplits is [4][16]uint8 indexed by validated Partition ∈ [0,4)
		// and block ∈ [0,16). Pow2 masks elide both bounds checks.
		if int(vp8tables.MBSplits[mode.Partition&3][block&15]) != subset {
			continue
		}
		bMode := firstMode
		// block-1 ∈ [0,15) and block-4 ∈ [-4,12); the guards above
		// (block&3 != 0, block>>2 != 0) ensure block-1 ≥ 0 and
		// block-4 ≥ 0 respectively. AND-mask with 15 elides bounds
		// checks on the [16]uint8 inner array.
		if block&3 != 0 && int(vp8tables.MBSplits[mode.Partition&3][(block-1)&15]) == subset {
			bMode = vp8common.Left4x4
		} else if block>>2 != 0 && int(vp8tables.MBSplits[mode.Partition&3][(block-4)&15]) == subset {
			bMode = vp8common.Above4x4
		}
		mode.BlockMV[block&15] = mv
		mode.BModes[block&15] = bMode
	}
}

var motion12PRMode vp8enc.InterFrameMacroblockMode

func TestMotion12PRSubsetTraversal(t *testing.T) {
	for partition := range 4 {
		for subset := -1; subset <= 17; subset++ {
			a := vp8enc.InterFrameMacroblockMode{Partition: uint8(partition)}
			for i := range a.BlockMV {
				a.BlockMV[i] = vp8enc.MotionVector{Row: int16(i * 19), Col: int16(i * 31)}
				a.BModes[i] = vp8common.Above4x4
			}
			b := a
			fillInterFrameSplitSubsetWithMode(&a, subset, vp8enc.MotionVector{Row: -32, Col: 24}, vp8common.New4x4)
			motion12ParentFillInterFrameSplitSubsetWithMode(&b, subset, vp8enc.MotionVector{Row: -32, Col: 24}, vp8common.New4x4)
			if a != b {
				t.Fatal(partition, subset, a, b)
			}
		}
	}
}
func BenchmarkMotion12PRSubsetTraversal(b *testing.B) {
	for _, p := range []int{0, 1, 2, 3} {
		b.Run(fmt.Sprint("fill", p), func(b *testing.B) {
			mode := vp8enc.InterFrameMacroblockMode{Partition: uint8(p)}
			b.ReportAllocs()
			for b.Loop() {
				fillInterFrameSplitSubsetWithMode(&mode, 0, vp8enc.MotionVector{Row: -32, Col: 24}, vp8common.New4x4)
				motion12PRMode = mode
			}
		})
	}
}
