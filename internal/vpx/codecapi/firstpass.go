package codecapi

// FirstPassFrameStats mirrors libvpx FIRSTPASS_STATS for one analyzed frame or
// for the finalized sequence total.
type FirstPassFrameStats struct {
	Frame               uint64
	IntraError          float64
	CodedError          float64
	SSIMWeightedPredErr float64
	PcntInter           float64
	PcntMotion          float64
	PcntSecondRef       float64
	PcntNeutral         float64
	MVr                 float64
	MVrAbs              float64
	MVc                 float64
	MVcAbs              float64
	MVrv                float64
	MVcv                float64
	MVInOutCount        float64
	NewMVCount          float64
	Duration            float64
	Count               float64
	IsTotal             bool
}

func AccumulateFirstPassStats(section *FirstPassFrameStats, frame FirstPassFrameStats) {
	if section == nil {
		return
	}
	section.Frame += frame.Frame
	section.IntraError += frame.IntraError
	section.CodedError += frame.CodedError
	section.SSIMWeightedPredErr += frame.SSIMWeightedPredErr
	section.PcntInter += frame.PcntInter
	section.PcntMotion += frame.PcntMotion
	section.PcntSecondRef += frame.PcntSecondRef
	section.PcntNeutral += frame.PcntNeutral
	section.MVr += frame.MVr
	section.MVrAbs += frame.MVrAbs
	section.MVc += frame.MVc
	section.MVcAbs += frame.MVcAbs
	section.MVrv += frame.MVrv
	section.MVcv += frame.MVcv
	section.MVInOutCount += frame.MVInOutCount
	section.NewMVCount += frame.NewMVCount
	section.Count += frame.Count
	section.Duration += frame.Duration
}

func SubtractFirstPassStats(section *FirstPassFrameStats, frame FirstPassFrameStats) {
	if section == nil {
		return
	}
	section.Frame -= frame.Frame
	section.IntraError -= frame.IntraError
	section.CodedError -= frame.CodedError
	section.SSIMWeightedPredErr -= frame.SSIMWeightedPredErr
	section.PcntInter -= frame.PcntInter
	section.PcntMotion -= frame.PcntMotion
	section.PcntSecondRef -= frame.PcntSecondRef
	section.PcntNeutral -= frame.PcntNeutral
	section.MVr -= frame.MVr
	section.MVrAbs -= frame.MVrAbs
	section.MVc -= frame.MVc
	section.MVcAbs -= frame.MVcAbs
	section.MVrv -= frame.MVrv
	section.MVcv -= frame.MVcv
	section.MVInOutCount -= frame.MVInOutCount
	section.NewMVCount -= frame.NewMVCount
	section.Count -= frame.Count
	section.Duration -= frame.Duration
}

func FinalizeFirstPassStats(stats []FirstPassFrameStats) []FirstPassFrameStats {
	if len(stats) == 0 {
		return stats
	}
	if stats[len(stats)-1].IsTotal {
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
