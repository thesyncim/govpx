//go:build govpx_oracle_trace

package govpx

import (
	"encoding/json"
	"io"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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
	EffectiveTargetKbps  int     `json:"effective_target_bitrate_kbps"`
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

	// fullRDFirstInterMv* captures the full-pel MV the verbatim full-RD
	// full_pixel_diamond selected for the FIRST full-RD single-ref NEWMV
	// search at frame 1, SB0, block (0,0) (the documented first inter
	// divergence). Used by TestVP9EncoderFullRDFrame1SB0FullPelMvParity to
	// pin the wiring against libvpx's single_motion_search tmp_mv.
	fullRDFirstInterMvValid bool
	fullRDFirstInterMvRow   int
	fullRDFirstInterMvCol   int
}

type vp9OracleTraceHolder struct {
	oracleTrace *vp9OracleTraceState
}

// SetOracleTraceWriter enables VP9 encoder oracle trace emission. It is
// available only in govpx_oracle_trace builds.
func (e *VP9Encoder) SetOracleTraceWriter(w io.Writer) {
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

func (e *VP9Encoder) vp9OracleTraceEnabled() bool {
	state := e.vp9OracleTraceState()
	return state != nil && state.writer != nil
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

func (e *VP9Encoder) recordVP9FullRDFirstInterMv(frameIndex, miRow, miCol int,
	refFrame int8, mvRow, mvCol int,
) {
	state := e.vp9OracleTraceState()
	if state == nil || state.fullRDFirstInterMvValid {
		return
	}
	if frameIndex != 1 || miRow != 0 || miCol != 0 {
		return
	}
	state.fullRDFirstInterMvValid = true
	state.fullRDFirstInterMvRow = mvRow
	state.fullRDFirstInterMvCol = mvCol
}

// vp9FullRDFirstInterMv returns the captured frame-1 SB0 (0,0) full-pel MV
// (row, col) selected by the full-RD full_pixel_diamond, valid only in
// govpx_oracle_trace builds after a frame-1 encode.
func (e *VP9Encoder) vp9FullRDFirstInterMv() (row, col int, ok bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.fullRDFirstInterMvValid {
		return 0, 0, false
	}
	return state.fullRDFirstInterMvRow, state.fullRDFirstInterMvCol, true
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

func (e *VP9Encoder) emitVP9OracleDroppedFrameTrace(flags EncodeFlags, width, height uint32, temporalFrame temporalFrame, dropReason vp9DropReason) {
	if !e.vp9OracleTraceEnabled() {
		return
	}
	e.emitVP9OracleFrameTrace(vp9OracleFrameSummary{
		Row:                 "vp9_frame",
		FrameIndex:          e.frameIndex,
		Flags:               uint32(flags),
		Dropped:             true,
		DropReason:          vp9DropReasonString(dropReason),
		ShowFrame:           true,
		CodedWidth:          int(width),
		CodedHeight:         int(height),
		TemporalLayerID:     temporalFrame.LayerID,
		TemporalLayerCount:  temporalFrame.LayerCount,
		TemporalLayerSync:   temporalFrame.LayerSync,
		TL0PICIDX:           temporalFrame.TL0PICIDX,
		TargetBitrateKbps:   e.rc.targetBitrateKbps,
		EffectiveTargetKbps: e.rc.effectiveBitrateKbps,
		FrameTargetBits:     e.rc.frameTargetBits,
		BufferLevelBits:     e.rc.bufferLevelBits,
		BufferOptimalBits:   e.rc.bufferOptimalBits,
	})
}

func (e *VP9Encoder) emitVP9OracleEncodedFrameTrace(encodedFrameIndex int, flags EncodeFlags, header *vp9dec.UncompressedHeader, txMode int, referenceMode int, compoundAllowed bool, result VP9EncodeResult, encodedBytes int) {
	if !e.vp9OracleTraceEnabled() {
		return
	}
	activeBestQ, activeWorstQ, rateCorrectionFactor, recodeAllowed,
		recodeLoopCount := e.vp9OracleRateSelectionTrace()
	e.emitVP9OracleFrameTrace(vp9OracleFrameSummary{
		Row:                  "vp9_frame",
		FrameIndex:           encodedFrameIndex,
		Flags:                uint32(flags),
		KeyFrame:             result.KeyFrame,
		IntraOnly:            result.IntraOnly,
		ShowFrame:            header.ShowFrame,
		Droppable:            result.Droppable,
		CodedWidth:           int(header.Width),
		CodedHeight:          int(header.Height),
		BaseQIndex:           int(header.Quant.BaseQindex),
		PublicQuantizer:      result.Quantizer,
		SizeBytes:            encodedBytes,
		FirstPartitionSize:   int(header.FirstPartitionSize),
		RefreshFrameFlags:    header.RefreshFrameFlags,
		RefreshFrameContext:  header.RefreshFrameContext,
		ErrorResilient:       header.ErrorResilientMode,
		FrameParallel:        header.FrameParallelDecoding,
		FrameContextIdx:      header.FrameContextIdx,
		TxMode:               txMode,
		InterpFilter:         int(header.InterpFilter),
		ReferenceMode:        referenceMode,
		CompoundAllowed:      compoundAllowed,
		ReferenceMask:        vp9InterReferenceMask(flags),
		LoopFilterLevel:      int(header.Loopfilter.FilterLevel),
		TemporalLayerID:      result.TemporalLayerID,
		TemporalLayerCount:   result.TemporalLayerCount,
		TemporalLayerSync:    result.TemporalLayerSync,
		TL0PICIDX:            result.TL0PICIDX,
		TargetBitrateKbps:    result.TargetBitrateKbps,
		EffectiveTargetKbps:  e.rc.effectiveBitrateKbps,
		FrameTargetBits:      result.FrameTargetBits,
		BufferLevelBits:      result.BufferLevelBits,
		BufferOptimalBits:    e.rc.bufferOptimalBits,
		ActiveBestQ:          activeBestQ,
		ActiveWorstQ:         activeWorstQ,
		RateCorrectionFactor: rateCorrectionFactor,
		RecodeAllowed:        recodeAllowed,
		RecodeLoopCount:      recodeLoopCount,
		TileLog2Cols:         int(header.Tile.Log2TileCols),
		TileLog2Rows:         int(header.Tile.Log2TileRows),
	})
}
