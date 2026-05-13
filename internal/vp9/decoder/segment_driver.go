package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 per-block segment-id drivers. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodemv.c — read_intra_segment_id and
// read_inter_segment_id. The key/intra path writes the freshly-read
// id into the current-frame seg map; the inter path adds the
// temporal-update prediction layer on top.
//
// Together with [[ReadRefFrames]] and [[ReadSkipWithSeg]] these form
// the segment-id half of the read_intra_frame_mode_info /
// read_inter_frame_mode_info composition.

// CopySegmentId mirrors libvpx's copy_segment_id. Copies a
// [yMis × xMis] window from `lastSeg` (nullable; nil means zero) into
// `currentSeg`, indexing both with miCols stride. Both arguments
// alias the per-frame segment-id raster maps.
func CopySegmentId(currentSeg, lastSeg []uint8, miCols, miOffset, xMis, yMis int) {
	for y := 0; y < yMis; y++ {
		for x := 0; x < xMis; x++ {
			off := miOffset + y*miCols + x
			if lastSeg != nil {
				currentSeg[off] = lastSeg[off]
			} else {
				currentSeg[off] = 0
			}
		}
	}
}

// SetSegmentId mirrors libvpx's set_segment_id. Writes `segID` into
// every entry of the [yMis × xMis] window.
func SetSegmentId(currentSeg []uint8, miCols, miOffset, xMis, yMis, segID int) {
	v := uint8(segID)
	for y := 0; y < yMis; y++ {
		for x := 0; x < xMis; x++ {
			currentSeg[miOffset+y*miCols+x] = v
		}
	}
}

// IntraSegmentMaps groups the two segment-id raster maps libvpx's
// VP9_COMMON carries for the intra path. LastFrameSegMap may be nil
// when the prior frame had segmentation disabled or didn't update
// the map.
type IntraSegmentMaps struct {
	CurrentFrameSegMap []uint8
	LastFrameSegMap    []uint8 // may be nil
	MiCols             int
}

// ReadIntraSegmentId mirrors libvpx's read_intra_segment_id. Mirrors
// the gate logic: disabled segmentation always returns 0; UpdateMap
// off triggers a copy of the last-frame map into the current-frame
// map (and still returns 0 — libvpx counts the keyframe seg map as
// a fresh write); otherwise the segment id is decoded from the
// reader and written into the current-frame map.
func ReadIntraSegmentId(r *bitstream.Reader, seg *SegmentationParams,
	maps *IntraSegmentMaps, miOffset, xMis, yMis int,
) int {
	if !seg.Enabled {
		return 0
	}
	if !seg.UpdateMap {
		CopySegmentId(maps.CurrentFrameSegMap, maps.LastFrameSegMap,
			maps.MiCols, miOffset, xMis, yMis)
		return 0
	}
	segID := ReadSegmentIDProb(r, seg)
	SetSegmentId(maps.CurrentFrameSegMap, maps.MiCols, miOffset, xMis, yMis, segID)
	return segID
}

// InterSegmentMaps extends IntraSegmentMaps for the temporal-update
// path. SegIDPredictedOut is written back to the per-block MI's
// seg_id_predicted slot when libvpx would set it.
type InterSegmentMaps struct {
	IntraSegmentMaps
	SegIDPredictedOut *uint8 // optional sink for predicted-flag write-back
}

// ReadInterSegmentId mirrors libvpx's read_inter_segment_id. When
// temporal update is enabled the decoder reads a single predicted-id
// bit against the seg-id-pred prob row (indexed by GetPredContextSegId
// over the above/left blocks). When the bit is 1, libvpx skips the
// segment-tree read and reuses the predicted id from the prior
// frame's seg map.
func ReadInterSegmentId(r *bitstream.Reader, seg *SegmentationParams,
	maps *InterSegmentMaps, miOffset, xMis, yMis int,
	above, left *NeighborMi,
) int {
	if !seg.Enabled {
		return 0
	}
	predictedID := 0
	if maps.LastFrameSegMap != nil {
		predictedID = DecGetSegmentId(maps.LastFrameSegMap,
			maps.MiCols, miOffset, xMis, yMis)
	}
	if !seg.UpdateMap {
		CopySegmentId(maps.CurrentFrameSegMap, maps.LastFrameSegMap,
			maps.MiCols, miOffset, xMis, yMis)
		return predictedID
	}
	var segID int
	if seg.TemporalUpdate {
		ctx := GetPredContextSegId(above, left)
		predProb := uint32(seg.PredProbs[ctx])
		bit := uint8(r.Read(predProb))
		if maps.SegIDPredictedOut != nil {
			*maps.SegIDPredictedOut = bit
		}
		if bit != 0 {
			segID = predictedID
		} else {
			segID = ReadSegmentIDProb(r, seg)
		}
	} else {
		segID = ReadSegmentIDProb(r, seg)
	}
	SetSegmentId(maps.CurrentFrameSegMap, maps.MiCols, miOffset, xMis, yMis, segID)
	return segID
}

// ReadSwitchableInterpFilter mirrors libvpx's
// read_switchable_interp_filter. Picks a per-block interpolation
// filter via vp9_switchable_interp_tree using the SwitchableInterpProb
// slot keyed by GetPredContextSwitchableInterp.
func ReadSwitchableInterpFilter(r *bitstream.Reader, fc *FrameContext,
	above, left *NeighborMi,
) InterpFilter {
	ctx := GetPredContextSwitchableInterp(above, left)
	leaf := r.ReadTree(common.SwitchableInterpTree[:], fc.SwitchableInterpProb[ctx][:])
	return InterpFilter(leaf)
}
