package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

const (
	vp9CyclicRefreshSegmentBoost1    = 1
	vp9CyclicRefreshSegmentBoost2    = 2
	vp9CyclicRefreshSuperblockMi     = 8
	vp9CyclicRefreshPercentRefresh   = 10
	vp9CyclicRefreshMaxQDeltaPercent = 60
)

type vp9CyclicRefreshState struct {
	enabled bool
	apply   bool
	miRows  int
	miCols  int
	cursor  uint32
	sbIndex uint32
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
	sbCount := vp9CyclicRefreshSuperblockCount(miRows, miCols)
	if sbCount > 0 && cr.sbIndex >= uint32(sbCount) {
		cr.sbIndex = 0
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
	if cr.prepareSuperblockFrame(active, miRows, miCols, target) {
		return
	}
	start := int(cr.cursor % uint32(n))
	for i := 0; i < target; i++ {
		active[(start+i)%n] = vp9CyclicRefreshSegmentBoost1
	}
	cr.cursor = uint32((start + target) % n)
	cr.apply = true
}

func (cr *vp9CyclicRefreshState) prepareSuperblockFrame(active []uint8,
	miRows int, miCols int, target int,
) bool {
	sbCols := (miCols + vp9CyclicRefreshSuperblockMi - 1) /
		vp9CyclicRefreshSuperblockMi
	sbRows := (miRows + vp9CyclicRefreshSuperblockMi - 1) /
		vp9CyclicRefreshSuperblockMi
	sbCount := sbRows * sbCols
	if sbCount <= 0 || len(active) < miRows*miCols {
		return false
	}
	start := int(cr.sbIndex % uint32(sbCount))
	marked := 0
	idx := start
	for visited := 0; marked < target && visited < sbCount; visited++ {
		sbRow := idx / sbCols
		sbCol := idx - sbRow*sbCols
		miRow := sbRow * vp9CyclicRefreshSuperblockMi
		miCol := sbCol * vp9CyclicRefreshSuperblockMi
		yEnd := min(miRow+vp9CyclicRefreshSuperblockMi, miRows)
		xEnd := min(miCol+vp9CyclicRefreshSuperblockMi, miCols)
		for y := miRow; y < yEnd; y++ {
			row := active[y*miCols:]
			for x := miCol; x < xEnd; x++ {
				row[x] = vp9CyclicRefreshSegmentBoost1
			}
		}
		marked += (yEnd - miRow) * (xEnd - miCol)
		idx++
		if idx == sbCount {
			idx = 0
		}
	}
	cr.sbIndex = uint32(idx)
	cr.apply = marked > 0
	return cr.apply
}

func vp9CyclicRefreshSuperblockCount(miRows int, miCols int) int {
	if miRows <= 0 || miCols <= 0 {
		return 0
	}
	sbCols := (miCols + vp9CyclicRefreshSuperblockMi - 1) /
		vp9CyclicRefreshSuperblockMi
	sbRows := (miRows + vp9CyclicRefreshSuperblockMi - 1) /
		vp9CyclicRefreshSuperblockMi
	return sbRows * sbCols
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
	cr.setSegmentTreeProbs(&seg)
	delta1 := vp9CyclicRefreshQDeltaByRate(baseQIndex, 3, 1)
	if delta1 != 0 {
		seg.FeatureMask[vp9CyclicRefreshSegmentBoost1] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[vp9CyclicRefreshSegmentBoost1][vp9dec.SegLvlAltQ] = delta1
	}
	delta2 := vp9CyclicRefreshQDeltaByRate(baseQIndex, 9, 2)
	if delta2 != 0 {
		seg.FeatureMask[vp9CyclicRefreshSegmentBoost2] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[vp9CyclicRefreshSegmentBoost2][vp9dec.SegLvlAltQ] = delta2
	}
	return seg
}

func (cr *vp9CyclicRefreshState) setSegmentTreeProbs(seg *vp9dec.SegmentationParams) {
	if cr == nil || seg == nil || !cr.apply || cr.miRows <= 0 || cr.miCols <= 0 {
		return
	}
	n := cr.miRows * cr.miCols
	if n <= 0 || len(cr.segMap) < n {
		return
	}
	var counts [vp9dec.MaxSegments]uint32
	for _, segmentID := range cr.segMap[:n] {
		if segmentID < vp9dec.MaxSegments {
			counts[segmentID]++
		}
	}
	var branchCounts [vp9dec.SegTreeProbs][2]uint32
	vp9enc.TreeProbsFromDistribution(common.SegmentTree[:],
		branchCounts[:], counts[:])
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = vp9enc.GetBinaryProb(branchCounts[i][0],
			branchCounts[i][1])
	}
}

func vp9CyclicRefreshQDeltaByRate(baseQIndex int, ratioNum int, ratioDen int) int16 {
	if baseQIndex <= 0 {
		return 0
	}
	delta := vp9ComputeQDeltaByRate(0, 255, false, baseQIndex,
		ratioNum, ratioDen)
	maxDrop := baseQIndex * vp9CyclicRefreshMaxQDeltaPercent / 100
	if -delta > maxDrop {
		delta = -maxDrop
	}
	if delta < -255 {
		delta = -255
	} else if delta > 255 {
		delta = 255
	}
	return int16(delta)
}
