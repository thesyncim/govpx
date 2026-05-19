//go:build govpx_oracle_trace

package govpx

import (
	"encoding/json"
	"io"
)

const vp9OracleTraceBuild = true

type vp9OracleFrameSummary struct {
	Row                  string  `json:"row"`
	FrameIndex           int     `json:"frame_index"`
	Flags                uint32  `json:"flags"`
	Dropped              bool    `json:"dropped,omitempty"`
	DropReason           string  `json:"drop_reason,omitempty"`
	KeyFrame             bool    `json:"key_frame"`
	IntraOnly            bool    `json:"intra_only"`
	ShowFrame            bool    `json:"show_frame"`
	Droppable            bool    `json:"droppable"`
	CodedWidth           int     `json:"coded_width"`
	CodedHeight          int     `json:"coded_height"`
	BaseQIndex           int     `json:"base_qindex"`
	PublicQuantizer      int     `json:"public_quantizer"`
	SizeBytes            int     `json:"size_bytes"`
	FirstPartitionSize   int     `json:"first_partition_size"`
	RefreshFrameFlags    uint8   `json:"refresh_frame_flags"`
	RefreshFrameContext  bool    `json:"refresh_frame_context"`
	ErrorResilient       bool    `json:"error_resilient"`
	FrameParallel        bool    `json:"frame_parallel"`
	FrameContextIdx      uint8   `json:"frame_context_idx"`
	TxMode               int     `json:"tx_mode"`
	InterpFilter         int     `json:"interp_filter"`
	ReferenceMode        int     `json:"reference_mode"`
	CompoundAllowed      bool    `json:"compound_allowed"`
	ReferenceMask        uint8   `json:"reference_mask"`
	LoopFilterLevel      int     `json:"loop_filter_level"`
	TemporalLayerID      int     `json:"temporal_layer_id"`
	TemporalLayerCount   int     `json:"temporal_layer_count"`
	TemporalLayerSync    bool    `json:"temporal_layer_sync"`
	TL0PICIDX            uint8   `json:"tl0_pic_idx"`
	TargetBitrateKbps    int     `json:"target_bitrate_kbps"`
	FrameTargetBits      int     `json:"frame_target_bits"`
	BufferLevelBits      int     `json:"buffer_level_bits"`
	BufferOptimalBits    int     `json:"buffer_optimal_bits"`
	ActiveBestQ          int     `json:"active_best_q"`
	ActiveWorstQ         int     `json:"active_worst_q"`
	RateCorrectionFactor float64 `json:"rate_correction_factor"`
	RecodeAllowed        bool    `json:"recode_allowed"`
	RecodeLoopCount      int     `json:"recode_loop_count"`
	TileLog2Cols         int     `json:"tile_log2_cols"`
	TileLog2Rows         int     `json:"tile_log2_rows"`
}

type vp9OracleTraceState struct {
	writer               io.Writer
	activeBestQ          int
	activeWorstQ         int
	rateCorrectionFactor float64
	recodeAllowed        bool
	recodeLoopCount      int
}

// SetVP9OracleTraceWriter enables VP9 encoder oracle trace emission. It is
// available only in govpx_oracle_trace builds.
func (e *VP9Encoder) SetVP9OracleTraceWriter(w io.Writer) {
	if e == nil {
		return
	}
	if w == nil {
		e.oracleTrace = nil
		return
	}
	state := e.vp9OracleTraceStateCreate()
	state.writer = w
}

func (e *VP9Encoder) vp9OracleTraceState() *vp9OracleTraceState {
	if e == nil {
		return nil
	}
	return e.oracleTrace
}

func (e *VP9Encoder) vp9OracleTraceStateCreate() *vp9OracleTraceState {
	if e.oracleTrace != nil {
		return e.oracleTrace
	}
	state := &vp9OracleTraceState{}
	e.oracleTrace = state
	return state
}

func (e *VP9Encoder) resetVP9OracleTraceState() {
	e.oracleTrace = nil
}

func (e *VP9Encoder) resetVP9OracleRateSelectionTrace() {
	state := e.vp9OracleTraceState()
	if state == nil {
		return
	}
	state.activeBestQ = 0
	state.activeWorstQ = 0
	state.rateCorrectionFactor = 0
	state.recodeAllowed = false
	state.recodeLoopCount = 0
}

func (e *VP9Encoder) recordVP9OracleRateSelectionTrace(activeBestQ int, activeWorstQ int, rateCorrectionFactor float64, recodeAllowed bool, recodeLoopCount int) {
	state := e.vp9OracleTraceState()
	if state == nil {
		return
	}
	state.activeBestQ = activeBestQ
	state.activeWorstQ = activeWorstQ
	state.rateCorrectionFactor = rateCorrectionFactor
	state.recodeAllowed = recodeAllowed
	state.recodeLoopCount = recodeLoopCount
}

func (e *VP9Encoder) vp9OracleRateSelectionTrace() (activeBestQ int, activeWorstQ int, rateCorrectionFactor float64, recodeAllowed bool, recodeLoopCount int) {
	state := e.vp9OracleTraceState()
	if state == nil {
		return 0, 0, 0, false, 0
	}
	return state.activeBestQ, state.activeWorstQ, state.rateCorrectionFactor,
		state.recodeAllowed, state.recodeLoopCount
}

func (e *VP9Encoder) emitVP9OracleFrameTrace(row vp9OracleFrameSummary) {
	state := e.vp9OracleTraceState()
	if state == nil || state.writer == nil {
		return
	}
	if row.Row == "" {
		row.Row = "vp9_frame"
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		return
	}
	_, _ = state.writer.Write(encoded)
	_, _ = state.writer.Write([]byte{'\n'})
}
