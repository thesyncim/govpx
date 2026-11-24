// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
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
	for i := 0; i < 500; i++ {
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
