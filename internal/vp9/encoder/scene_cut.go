package encoder

import "github.com/thesyncim/govpx/internal/vpx/arith"

const (
	sceneCutMinimumReferenceSSEPerMB = 16 * 16 * 64 * 64
	sceneCutHighReferenceSSEPerMB    = 16 * 16 * 48 * 48
	sceneCutIntraWinPct              = 75
	sceneCutHighErrorPct             = 75
	sceneCutIntraErrorRatio          = 4
)

// LumaPlane describes a visible 8-bit luma plane.
type LumaPlane struct {
	Pixels []byte
	Stride int
	Width  int
	Height int
}

// SceneCutFrameStats accumulates the per-macroblock error totals used by VP9
// one-pass adaptive keyframe promotion.
type SceneCutFrameStats struct {
	Macroblocks       int
	ReferenceError    int64
	IntraError        int64
	IntraBetterBlocks int
	HighErrorBlocks   int
}

// AddMacroblock records one macroblock's best reference SSE and intra
// mean-luma SSE.
func (s *SceneCutFrameStats) AddMacroblock(referenceSSE, intraSSE int) {
	if s == nil {
		return
	}
	referenceError := int64(referenceSSE)
	intraError := int64(intraSSE)
	s.ReferenceError += referenceError
	s.IntraError += intraError
	if referenceError > intraError*sceneCutIntraErrorRatio {
		s.IntraBetterBlocks++
	}
	if referenceSSE >= sceneCutHighReferenceSSEPerMB {
		s.HighErrorBlocks++
	}
}

// PromotesKeyFrame applies libvpx's one-pass scene-cut thresholds to the
// accumulated frame stats.
func (s SceneCutFrameStats) PromotesKeyFrame() bool {
	if s.Macroblocks <= 0 {
		return false
	}
	if s.ReferenceError < int64(sceneCutMinimumReferenceSSEPerMB)*int64(s.Macroblocks) {
		return false
	}
	if s.IntraBetterBlocks*100 < s.Macroblocks*sceneCutIntraWinPct {
		return false
	}
	if s.HighErrorBlocks*100 < s.Macroblocks*sceneCutHighErrorPct {
		return false
	}
	return s.ReferenceError > s.IntraError*sceneCutIntraErrorRatio
}

// MacroblockLumaSSE returns the 16x16 source/reference luma SSE with visible
// edge replication.
func MacroblockLumaSSE(src, ref LumaPlane, mbRow, mbCol int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	sse := 0
	for row := range 16 {
		srcY := arith.ClampCoord(baseY+row, src.Height)
		refY := arith.ClampCoord(baseY+row, ref.Height)
		for col := range 16 {
			srcX := arith.ClampCoord(baseX+col, src.Width)
			refX := arith.ClampCoord(baseX+col, ref.Width)
			diff := int(src.Pixels[srcY*src.Stride+srcX]) -
				int(ref.Pixels[refY*ref.Stride+refX])
			sse += diff * diff
		}
	}
	return sse
}

// MacroblockMeanLumaSSE returns the 16x16 luma variance around the block mean
// with visible edge replication.
func MacroblockMeanLumaSSE(src LumaPlane, mbRow, mbCol int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := arith.ClampCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := arith.ClampCoord(baseX+col, src.Width)
			v := int(src.Pixels[srcY*src.Stride+srcX])
			sum += v
			sse += v * v
		}
	}
	variance := sse - int((int64(sum)*int64(sum)+128)>>8)
	if variance < 0 {
		return 0
	}
	return variance
}
