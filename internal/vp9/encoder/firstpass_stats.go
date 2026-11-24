package encoder

// FirstPassFrameStats mirrors libvpx VP9 FIRSTPASS_STATS for one analyzed
// source frame or for the finalized sequence total.
//
// libvpx: vp9/encoder/vp9_firstpass_stats.h:20
type FirstPassFrameStats struct {
	// Frame is the source-frame ordinal accumulated by libvpx first pass.
	Frame uint64
	// Weight is libvpx's per-frame first-pass complexity weight.
	Weight float64
	// IntraError is the intra prediction error.
	IntraError float64
	// CodedError is the selected coded prediction error.
	CodedError float64
	// SRCodedError is the second-reference coded prediction error.
	SRCodedError float64
	// FrameNoiseEnergy is libvpx's first-pass noise-energy accumulator.
	FrameNoiseEnergy float64
	// PcntInter is the fraction of blocks coded as inter.
	PcntInter float64
	// PcntMotion is the fraction of blocks with non-zero motion.
	PcntMotion float64
	// PcntSecondRef is the fraction of blocks preferring a second reference.
	PcntSecondRef float64
	// PcntNeutral is libvpx's neutral-block fraction.
	PcntNeutral float64
	// PcntIntraLow is the fraction of intra blocks with low variance.
	PcntIntraLow float64
	// PcntIntraHigh is the fraction of intra blocks with high variance.
	PcntIntraHigh float64
	// IntraSkipPct is the fraction of intra blocks skipped by first pass.
	IntraSkipPct float64
	// IntraSmoothPct is the fraction of smooth intra blocks.
	IntraSmoothPct float64
	// InactiveZoneRows is the inactive image-mask row count.
	InactiveZoneRows float64
	// InactiveZoneCols is the inactive image-mask column count.
	InactiveZoneCols float64
	// MVr accumulates signed row motion vectors.
	MVr float64
	// MVrAbs accumulates absolute row motion vectors.
	MVrAbs float64
	// MVc accumulates signed column motion vectors.
	MVc float64
	// MVcAbs accumulates absolute column motion vectors.
	MVcAbs float64
	// MVrv accumulates row motion-vector variance terms.
	MVrv float64
	// MVcv accumulates column motion-vector variance terms.
	MVcv float64
	// MVInOutCount is libvpx's in/out motion-vector accumulator.
	MVInOutCount float64
	// Duration is the frame duration in caller timebase units.
	Duration float64
	// Count is the number of frames represented by this record.
	Count float64
	// NewMVCount counts blocks that selected a new motion vector.
	NewMVCount float64
	// SpatialLayerID is the VP9 spatial layer this stats row belongs to.
	SpatialLayerID int64
	// IsTotal marks an entry as the libvpx terminal total-stats packet.
	IsTotal bool
}

// FinalizeFirstPassStats appends the libvpx-style terminal total-stats record
// to per-frame VP9 first-pass stats. If stats is empty or already ends in a
// total row, the input slice is returned unchanged.
func FinalizeFirstPassStats(stats []FirstPassFrameStats) []FirstPassFrameStats {
	if len(stats) == 0 || stats[len(stats)-1].IsTotal {
		return stats
	}
	var total FirstPassFrameStats
	for i := range stats {
		if stats[i].IsTotal {
			continue
		}
		AccumulateFirstPassStats(&total, stats[i])
	}
	total.IsTotal = true
	out := make([]FirstPassFrameStats, len(stats)+1)
	copy(out, stats)
	out[len(stats)] = total
	return out
}

// AccumulateFirstPassStats adds row into dst using the additive fields in
// libvpx's FIRSTPASS_STATS terminal-total packet.
func AccumulateFirstPassStats(dst *FirstPassFrameStats, row FirstPassFrameStats) {
	if dst == nil {
		return
	}
	dst.Frame += row.Frame
	dst.Weight += row.Weight
	dst.IntraError += row.IntraError
	dst.CodedError += row.CodedError
	dst.SRCodedError += row.SRCodedError
	dst.FrameNoiseEnergy += row.FrameNoiseEnergy
	dst.PcntInter += row.PcntInter
	dst.PcntMotion += row.PcntMotion
	dst.PcntSecondRef += row.PcntSecondRef
	dst.PcntNeutral += row.PcntNeutral
	dst.PcntIntraLow += row.PcntIntraLow
	dst.PcntIntraHigh += row.PcntIntraHigh
	dst.IntraSkipPct += row.IntraSkipPct
	dst.IntraSmoothPct += row.IntraSmoothPct
	dst.InactiveZoneRows += row.InactiveZoneRows
	dst.InactiveZoneCols += row.InactiveZoneCols
	dst.MVr += row.MVr
	dst.MVrAbs += row.MVrAbs
	dst.MVc += row.MVc
	dst.MVcAbs += row.MVcAbs
	dst.MVrv += row.MVrv
	dst.MVcv += row.MVcv
	dst.MVInOutCount += row.MVInOutCount
	dst.Duration += row.Duration
	dst.Count += row.Count
	dst.NewMVCount += row.NewMVCount
	dst.SpatialLayerID = row.SpatialLayerID
}
