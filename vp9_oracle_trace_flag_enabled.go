//go:build govpx_oracle_trace

package govpx

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

var vp9GTTraceOnce sync.Once
var vp9GTTraceEnabled bool

// vp9TraceCommitBlock emits one committed-block line per leaf at the bitstream
// write commit point, mirroring the libvpx encode_b GTBLK probe so a govpx run
// can be diffed against the captured libvpx ground truth. Compile-elided in
// production (vp9OracleTraceBuild==false) and silent unless GOVPX_GT_TRACE is
// set in the environment.
func (e *VP9Encoder) vp9TraceCommitBlock(frameIndex, miRow, miCol int,
	mi *vp9dec.NeighborMi, uvMode common.PredictionMode,
) {
	vp9GTTraceOnce.Do(func() {
		vp9GTTraceEnabled = os.Getenv("GOVPX_GT_TRACE") != ""
	})
	if !vp9GTTraceEnabled || mi == nil {
		return
	}
	fmt.Fprintf(os.Stderr,
		"GTBLK f=%d mi=%d,%d bs=%d mode=%d uv=%d r0=%d r1=%d if=%d skip=%d tx=%d mv0=%d,%d mv1=%d,%d\n",
		frameIndex, miRow, miCol, int(mi.SbType), int(mi.Mode), int(uvMode),
		int(mi.RefFrame[0]), int(mi.RefFrame[1]), int(mi.InterpFilter),
		int(mi.Skip), int(mi.TxSize),
		mi.Mv[0].Row, mi.Mv[0].Col, mi.Mv[1].Row, mi.Mv[1].Col)
}

// vp9TraceCommitBlockPre is the count-pre-pass twin of vp9TraceCommitBlock,
// tagged GTBLKPRE so the two passes can be compared.
func (e *VP9Encoder) vp9TraceCommitBlockPre(frameIndex, miRow, miCol int,
	mi *vp9dec.NeighborMi, uvMode common.PredictionMode,
) {
	vp9GTTraceOnce.Do(func() {
		vp9GTTraceEnabled = os.Getenv("GOVPX_GT_TRACE") != ""
	})
	if !vp9GTTraceEnabled || mi == nil {
		return
	}
	fmt.Fprintf(os.Stderr,
		"GTBLKPRE f=%d mi=%d,%d bs=%d mode=%d uv=%d r0=%d r1=%d if=%d skip=%d tx=%d mv0=%d,%d mv1=%d,%d\n",
		frameIndex, miRow, miCol, int(mi.SbType), int(mi.Mode), int(uvMode),
		int(mi.RefFrame[0]), int(mi.RefFrame[1]), int(mi.InterpFilter),
		int(mi.Skip), int(mi.TxSize),
		mi.Mv[0].Row, mi.Mv[0].Col, mi.Mv[1].Row, mi.Mv[1].Col)
}

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

	// fullRDFirstInterSubpelMv* captures the SUBPEL MV (1/8-pel) the full-RD
	// single_motion_search produced for the frame-1 SB0 64x64 (0,0) NEWMV
	// (libvpx vp9_rdopt.c:2728 find_fractional_mv_step result). Pins the
	// subpel-refine step.
	fullRDFirstInterSubpelMvValid bool
	fullRDFirstInterSubpelMvRow   int
	fullRDFirstInterSubpelMvCol   int

	// fullRDInterYRD* captures the genuine inter super_block_yrd
	// (choose_tx_size_from_rd) result the vp9FullRDInterSuperBlockYRD
	// producer computes for the frame-1 SB0 64x64 root NEWMV (ref=LAST,
	// mv=(12,4), filt=EIGHTTAP_SMOOTH). Used by the inter-yrd parity test to
	// pin tx_size/best_rd + per-tx-size arrays against the libvpx capture.
	fullRDInterYRDValid bool
	fullRDInterYRD      vp9FullRDInterYRDResult

	// fullRDInterThisRD* captures the genuine per-candidate this_rd
	// (handle_inter_mode + the rd_pick_inter_mode_sb skip pick) the
	// vp9FullRDInterThisRD assembly computes for the same frame-1 SB0 64x64
	// NEWMV candidate. Used by the inter-this_rd parity test to pin this_rd +
	// the Y/UV components against the libvpx capture.
	fullRDInterThisRDValid bool
	fullRDInterThisRD      vp9FullRDInterThisRDResult

	// fullRDSub8x8* captures the genuine sub-8x8 joint RD producer's per-label
	// decomposition (encode_inter_mb_segment + set_and_cost_bmi_mvs) and the
	// per-sub-block NEWMV motion search result for the frame-1 SB0 16x16(0,0)
	// child at mi=(0,1) BLOCK_4X4 ref=LAST EIGHTTAP. Used by the sub-8x8 parity
	// test to pin the producer against the libvpx capture.
	fullRDSub8x8Valid bool
	fullRDSub8x8      vp9Sub8x8Capture

	// sub8x8Wrapper* captures the genuine sub-8x8 wrapper's committed segment for
	// the frame-1 SB0 16x16(0,0) child at mi=(0,1): the live-derived (NOT injected)
	// segment Y rate (bsi->r) + filter, used to pin the sibling entropy-context
	// propagation (mi(0,0)'s encode_b stamp seeding mi(0,1)'s rd_pick_best_sub8x8
	// t_left). First matching mi=(0,1) commit wins.
	sub8x8WrapperValid bool
	sub8x8WrapperR     int
	sub8x8WrapperFltr  vp9dec.InterpFilter

	// sub8x8InterMi72 captures the formerly drifting frame-1 SB0 mi=(7,2)
	// committed inter leaf's UV/rate tuple. It pins the chroma entropy-context
	// side effect of committed sub-8x8 inter leaves after the luma replay fix.
	sub8x8InterMi72Valid bool
	sub8x8InterMi72      vp9Sub8x8InterCapture

	// sub8x8Intra* captures the genuine sub-8x8 wrapper's committed INTRA leaf for
	// the frame-1 SB0 16x16(0,0) child at mi=(1,0) BLOCK_8X4: the chosen Y mode +
	// per-sub-block bmi modes + UV mode + the intra Y rate (rate incl. mbmode_cost),
	// the intra Y token rate, the UV rate, distortion and this_rd. Pins the
	// rd_pick_intra_sub_8x8_y_mode + choose_intra_uv_mode port against libvpx
	// ground truth. First matching mi=(1,0) intra commit wins.
	sub8x8IntraValid bool
	sub8x8IntraData  vp9Sub8x8IntraCapture
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

func (e *VP9Encoder) recordVP9FullRDFirstInterSubpelMv(frameIndex, miRow, miCol int,
	refFrame int8, mvRow, mvCol int,
) {
	state := e.vp9OracleTraceState()
	if state == nil || state.fullRDFirstInterSubpelMvValid {
		return
	}
	if frameIndex != 1 || miRow != 0 || miCol != 0 {
		return
	}
	state.fullRDFirstInterSubpelMvValid = true
	state.fullRDFirstInterSubpelMvRow = mvRow
	state.fullRDFirstInterSubpelMvCol = mvCol
}

// vp9FullRDFirstInterSubpelMv returns the captured frame-1 SB0 (0,0) subpel MV
// (row, col in 1/8-pel) from the full-RD single_motion_search, valid only in
// govpx_oracle_trace builds after a frame-1 encode.
func (e *VP9Encoder) vp9FullRDFirstInterSubpelMv() (row, col int, ok bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.fullRDFirstInterSubpelMvValid {
		return 0, 0, false
	}
	return state.fullRDFirstInterSubpelMvRow, state.fullRDFirstInterSubpelMvCol, true
}

// recordVP9FullRDInterYRD stores the genuine inter super_block_yrd producer
// result for the frame-1 SB0 (0,0) 64x64 NEWMV. Gated to that one block so the
// first matching candidate (the EIGHTTAP_SMOOTH NEWMV) is captured.
func (e *VP9Encoder) recordVP9FullRDInterYRD(frameIndex, miRow, miCol int,
	res vp9FullRDInterYRDResult,
) {
	state := e.vp9OracleTraceState()
	if state == nil || state.fullRDInterYRDValid {
		return
	}
	if frameIndex != 1 || miRow != 0 || miCol != 0 || !res.Valid {
		return
	}
	state.fullRDInterYRDValid = true
	state.fullRDInterYRD = res
}

// vp9FullRDInterYRD returns the captured frame-1 SB0 (0,0) 64x64 NEWMV
// super_block_yrd producer result, valid only in govpx_oracle_trace builds
// after a frame-1 encode.
func (e *VP9Encoder) vp9FullRDInterYRD() (vp9FullRDInterYRDResult, bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.fullRDInterYRDValid {
		return vp9FullRDInterYRDResult{}, false
	}
	return state.fullRDInterYRD, true
}

// recordVP9FullRDInterThisRD stores the genuine per-candidate this_rd assembly
// result for the frame-1 SB0 (0,0) 64x64 NEWMV. Gated to that one block so the
// first matching candidate (the EIGHTTAP_SMOOTH NEWMV) is captured.
func (e *VP9Encoder) recordVP9FullRDInterThisRD(frameIndex, miRow, miCol int,
	res vp9FullRDInterThisRDResult,
) {
	state := e.vp9OracleTraceState()
	if state == nil || state.fullRDInterThisRDValid {
		return
	}
	if frameIndex != 1 || miRow != 0 || miCol != 0 || !res.Valid {
		return
	}
	state.fullRDInterThisRDValid = true
	state.fullRDInterThisRD = res
}

// vp9CapturedFullRDInterThisRD returns the captured frame-1 SB0 (0,0) 64x64
// NEWMV genuine per-candidate this_rd assembly result, valid only in
// govpx_oracle_trace builds after a frame-1 encode.
func (e *VP9Encoder) vp9CapturedFullRDInterThisRD() (vp9FullRDInterThisRDResult, bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.fullRDInterThisRDValid {
		return vp9FullRDInterThisRDResult{}, false
	}
	return state.fullRDInterThisRD, true
}

// recordVP9FullRDSub8x8 stores the genuine sub-8x8 producer capture (first
// matching call wins).
func (e *VP9Encoder) recordVP9FullRDSub8x8(cap vp9Sub8x8Capture) {
	state := e.vp9OracleTraceState()
	if state == nil || state.fullRDSub8x8Valid {
		return
	}
	state.fullRDSub8x8Valid = true
	state.fullRDSub8x8 = cap
}

// vp9CapturedFullRDSub8x8 returns the captured frame-1 SB0 16x16(0,0) child
// sub-8x8 producer decomposition, valid only in govpx_oracle_trace builds.
func (e *VP9Encoder) vp9CapturedFullRDSub8x8() (vp9Sub8x8Capture, bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.fullRDSub8x8Valid {
		return vp9Sub8x8Capture{}, false
	}
	return state.fullRDSub8x8, true
}

// recordVP9Sub8x8WrapperCommit stores the genuine sub-8x8 wrapper's live-derived
// committed segment Y rate + filter for mi=(0,1) (first matching commit wins).
func (e *VP9Encoder) recordVP9Sub8x8WrapperCommit(miRow, miCol, segR int,
	filter vp9dec.InterpFilter,
) {
	state := e.vp9OracleTraceState()
	if state == nil || state.sub8x8WrapperValid {
		return
	}
	if miRow != 0 || miCol != 1 {
		return
	}
	state.sub8x8WrapperValid = true
	state.sub8x8WrapperR = segR
	state.sub8x8WrapperFltr = filter
}

// vp9CapturedSub8x8WrapperCommit returns the captured mi=(0,1) committed segment
// Y rate + filter, valid only in govpx_oracle_trace builds.
func (e *VP9Encoder) vp9CapturedSub8x8WrapperCommit() (int, vp9dec.InterpFilter, bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.sub8x8WrapperValid {
		return 0, 0, false
	}
	return state.sub8x8WrapperR, state.sub8x8WrapperFltr, true
}

func (e *VP9Encoder) recordVP9Sub8x8InterCommit(cap vp9Sub8x8InterCapture) {
	state := e.vp9OracleTraceState()
	if state == nil || state.sub8x8InterMi72Valid {
		return
	}
	if cap.MiRow != 7 || cap.MiCol != 2 {
		return
	}
	state.sub8x8InterMi72Valid = true
	state.sub8x8InterMi72 = cap
}

func (e *VP9Encoder) vp9CapturedSub8x8InterMi72Commit() (vp9Sub8x8InterCapture, bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.sub8x8InterMi72Valid {
		return vp9Sub8x8InterCapture{}, false
	}
	return state.sub8x8InterMi72, true
}

// recordVP9Sub8x8IntraCommit stores the genuine sub-8x8 wrapper's committed INTRA
// leaf decomposition for the frame-1 SB0 16x16(0,0) child at mi=(1,0). LAST
// matching mi=(1,0) intra commit wins: the deep recursion evaluates the three
// sub-8x8 shapes (SPLIT/HORZ/VERT) then re-runs the WINNING shape last, so the
// final overwrite is the committed (BLOCK_8X4 HORZ) intra leaf rather than an
// earlier losing trial shape.
func (e *VP9Encoder) recordVP9Sub8x8IntraCommit(cap vp9Sub8x8IntraCapture) {
	state := e.vp9OracleTraceState()
	if state == nil {
		return
	}
	if cap.MiRow != 1 || cap.MiCol != 0 {
		return
	}
	state.sub8x8IntraValid = true
	state.sub8x8IntraData = cap
}

// vp9CapturedSub8x8IntraCommit returns the captured mi=(1,0) committed intra leaf
// decomposition, valid only in govpx_oracle_trace builds.
func (e *VP9Encoder) vp9CapturedSub8x8IntraCommit() (vp9Sub8x8IntraCapture, bool) {
	state := e.vp9OracleTraceState()
	if state == nil || !state.sub8x8IntraValid {
		return vp9Sub8x8IntraCapture{}, false
	}
	return state.sub8x8IntraData, true
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
