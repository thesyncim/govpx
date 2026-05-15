package govpx

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

const (
	vp9CyclicRefreshSegmentBoost1    = 1
	vp9CyclicRefreshPercentRefresh   = 10
	vp9CyclicRefreshMaxQDeltaPercent = 60
)

type vp9CyclicRefreshState struct {
	enabled bool
	apply   bool
	miRows  int
	miCols  int
	cursor  uint32
	segMap  []uint8
}

func (cr *vp9CyclicRefreshState) configure(enabled bool, width int, height int) {
	cr.enabled = enabled
	cr.apply = false
	if !enabled {
		cr.miRows = 0
		cr.miCols = 0
		cr.cursor = 0
		return
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	cr.miRows = miRows
	cr.miCols = miCols
	n := miRows * miCols
	if cap(cr.segMap) < n {
		cr.segMap = make([]uint8, n)
	} else {
		cr.segMap = cr.segMap[:n]
		for i := range cr.segMap {
			cr.segMap[i] = 0
		}
	}
	if n > 0 && cr.cursor >= uint32(n) {
		cr.cursor = 0
	}
}

func (cr *vp9CyclicRefreshState) prepareFrame(apply bool, miRows int, miCols int) {
	cr.apply = false
	if !cr.enabled || !apply || miRows <= 0 || miCols <= 0 {
		return
	}
	n := miRows * miCols
	if n <= 0 || len(cr.segMap) < n {
		return
	}
	cr.miRows = miRows
	cr.miCols = miCols
	active := cr.segMap[:n]
	for i := range active {
		active[i] = 0
	}
	target := n * vp9CyclicRefreshPercentRefresh / 100
	if target <= 0 {
		target = 1
	}
	start := int(cr.cursor % uint32(n))
	for i := 0; i < target; i++ {
		active[(start+i)%n] = vp9CyclicRefreshSegmentBoost1
	}
	cr.cursor = uint32((start + target) % n)
	cr.apply = true
}

func (cr *vp9CyclicRefreshState) segmentID(miRow int, miCol int) uint8 {
	if !cr.enabled || !cr.apply || miRow < 0 || miCol < 0 ||
		miRow >= cr.miRows || miCol >= cr.miCols {
		return 0
	}
	idx := miRow*cr.miCols + miCol
	if idx < 0 || idx >= len(cr.segMap) {
		return 0
	}
	if cr.segMap[idx] >= vp9dec.MaxSegments {
		return 0
	}
	return cr.segMap[idx]
}

func (cr *vp9CyclicRefreshState) segmentationParams(baseQIndex int) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = vp9dec.MaxProb
	}
	for i := range vp9dec.PredictionProbs {
		seg.PredProbs[i] = vp9dec.MaxProb
	}
	delta := vp9CyclicRefreshQDelta(baseQIndex)
	if delta != 0 {
		seg.FeatureMask[vp9CyclicRefreshSegmentBoost1] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[vp9CyclicRefreshSegmentBoost1][vp9dec.SegLvlAltQ] = delta
	}
	return seg
}

func vp9CyclicRefreshQDelta(baseQIndex int) int16 {
	if baseQIndex <= 0 {
		return 0
	}
	delta := -(baseQIndex * vp9CyclicRefreshMaxQDeltaPercent / 200)
	if delta < -255 {
		return -255
	}
	return int16(delta)
}
