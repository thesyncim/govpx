//go:build !govpx_oracle_trace

package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const oracleTraceBuild = false

const oracleTraceInterCandidateUnknown = -1

type staleY2Snapshot struct{}

type oracleTraceFrameSummary struct {
	FrameType            vp8common.FrameType
	BaseQIndex           int
	LoopFilter           int
	SharpnessLevel       int
	RefLFDeltas          [vp8common.MaxRefLFDeltas]int8
	ModeLFDeltas         [vp8common.MaxModeLFDeltas]int8
	ModeRefLFDeltaEnable bool
	ModeRefLFDeltaUpdate bool
	RefreshLast          bool
	RefreshGolden        bool
	RefreshAltRef        bool
	GoldenSignBias       bool
	AltRefSignBias       bool
	SegEnabled           bool
	SizeBytes            int
}

type oracleTraceInterCandidateSummary struct {
	Picker          string
	MBRow           int
	MBCol           int
	ModeIndex       int
	Mode            vp8common.MBPredictionMode
	RefSlot         int
	RefFrame        vp8common.MVReferenceFrame
	Threshold       int
	BestScoreBefore int
	BestYRDBefore   int
	BestSSEBefore   int
	Outcome         string
	BecameBest      bool
	LoopBreak       bool
	Score           int
	YRD             int
	Rate            int
	RateY           int
	RateUV          int
	Distortion      int
	DistortionUV    int
	SSE             int
	Skip            bool
	MV              vp8enc.MotionVector
	ModeTrace       vp8enc.InterFrameMacroblockMode
	HasModeTrace    bool

	ImprovedMVStart        bool
	ImprovedMVNearSADIndex int
	ImprovedMVRow          int16
	ImprovedMVCol          int16
	ImprovedMVSR           int
}

func (e *VP8Encoder) oracleTraceEnabled() bool { return false }

func (e *VP8Encoder) resetOracleTraceState() {}

func (e *VP8Encoder) resetOracleTraceRecode() {}

func (e *VP8Encoder) incrementOracleTraceRecodeLoop() {}

func (e *VP8Encoder) setOracleTraceRecodeReason(string) {}

func (e *VP8Encoder) oracleTraceRecodeLoopCountForTest() int { return 0 }

func (e *VP8Encoder) oracleTraceMBBufferLenForTest() int { return 0 }

func (e *VP8Encoder) resetOracleMBTraceBuffer() {}

func (e *VP8Encoder) flushOracleMBTraceBuffer() {}

func (e *VP8Encoder) emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary) {}

func (e *VP8Encoder) emitFastPickerIntraCandidateTrace(int, int, int, int, int, int, bool, int, int, int, int, *vp8enc.InterFrameMacroblockMode) {
}

func (e *VP8Encoder) emitFastPickerInterCandidateTrace(int, int, int, int, vp8common.MVReferenceFrame, int, int, int, bool, bool, int, int, int, int, *vp8enc.InterFrameMacroblockMode, interFrameSearchStart) {
}

func (e *VP8Encoder) emitOracleMBTrace(int, int, *vp8enc.InterFrameMacroblockMode, *vp8enc.MacroblockCoefficients, interFrameSearchStart, int, int) {
}

func (e *VP8Encoder) emitOracleKeyFrameMBTrace(int, int, *vp8enc.KeyFrameMacroblockMode, *vp8enc.MacroblockCoefficients, int, int) {
}

func (e *VP8Encoder) emitOracleLFTrial(string, int, int) {}

func (e *VP8Encoder) emitOracleInterPredictorTrace(int, int, *vp8common.Image) {}

func (e *VP8Encoder) emitOracleInterReconstructedTrace(int, int, *vp8common.Image) {}

func (e *VP8Encoder) emitOracleLastRefWindow(*vp8common.Image) {}

func (e *VP8Encoder) emitOracleFrameTrace(oracleTraceFrameSummary) {}

func (e *VP8Encoder) emitOracleDroppedFrameTrace(string) {}

func (e *VP8Encoder) emitOracleRateAndRecodeTrace(vp8common.FrameType, int, int, int, int, int) {}

func makeOracleStaleY2Snapshot(uint8, [16]int16) staleY2Snapshot { return staleY2Snapshot{} }

func oracleStaleY2SnapshotSet(staleY2Snapshot) bool { return false }

func applyOracleStaleY2Snapshot(*vp8enc.MacroblockCoefficients, staleY2Snapshot) {}

func recordOracleY1DCEOB1(*vp8enc.MacroblockCoefficients, int, uint8) {}

func recordOracleStaleY2(*vp8enc.MacroblockCoefficients, uint8, [16]int16) {}

func libvpxY1DCWouldQuantizeNonzero(int16, *vp8enc.BlockQuant, int, int, bool) uint8 {
	return 0
}
