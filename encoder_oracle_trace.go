package govpx

// Encoder oracle trace mode (off-by-default validation harness).
//
// When EncoderOptions.OracleTraceWriter is non-nil the encoder emits a
// deterministic JSON Lines stream describing per-frame state and per-MB
// decisions. The format is intended to be diffed against equivalent output
// instrumented from libvpx (vp8/encoder/encodeframe.c, pickinter.c, rdopt.c,
// onyx_if.c, bitstream.c) for parity validation. Each line is a JSON object
// with a "type" field that selects the row schema:
//
//   {"type":"frame", ...}  one per encoded (non-dropped) frame
//   {"type":"mb",    ...}  one per macroblock for inter frames
//
// Output is emitted in deterministic order (frame trace after the frame is
// committed; per-MB rows in raster scan order). When the writer is nil there
// is no allocation and no per-MB cost.

import (
	"encoding/json"
	"fmt"
	"hash/adler32"
	"io"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// oracleTraceFrameRow is the per-frame oracle trace row.
//
// RefreshEntropyProbs mirrors libvpx's `cm->refresh_entropy_probs` after the
// `vp8_pack_bitstream` error-resilient override (bitstream.c around line
// 1226): when error_resilient_mode includes VPX_ERROR_RESILIENT_PARTITIONS,
// libvpx forces refresh_entropy_probs=1 on key frames and refresh_entropy_probs=0
// on inter frames regardless of the encoder's earlier choice. govpx tracks
// the same per-attempt flag through `keyFrameEncodeAttempt.RefreshEntropyProbs`
// / `interFrameEncodeAttempt.Config.RefreshEntropyProbs`.
//
// DefaultCoefReset mirrors libvpx's "force-default coef probs/counts" gate:
// when error_resilient_mode is VPX_ERROR_RESILIENT_PARTITIONS AND the frame
// is a key frame, libvpx resets `cm->fc.coef_probs` and `mb->coef_counts` to
// their defaults via vp8_setup_key_frame -> vp8_default_coef_probs and the
// on-the-fly bitpacking branch in `vp8_update_coef_context`. The flag is the
// gate, not the side-effect, so parity tests can confirm both encoders took
// the same branch even when the underlying tables already matched.
type oracleTraceFrameRow struct {
	Type                string `json:"type"`
	FrameIndex          uint64 `json:"frame_index"`
	FrameType           string `json:"frame_type"`
	QIndex              int    `json:"q_index"`
	BaseQIndex          int    `json:"base_q_index"`
	LoopFilter          int    `json:"loop_filter_level"`
	RefreshLast         bool   `json:"refresh_last"`
	RefreshGolden       bool   `json:"refresh_golden"`
	RefreshAltRef       bool   `json:"refresh_altref"`
	GoldenSignBias      bool   `json:"sign_bias_golden"`
	AltRefSignBias      bool   `json:"sign_bias_altref"`
	SegEnabled          bool   `json:"segmentation_enabled"`
	RefreshEntropyProbs bool   `json:"refresh_entropy_probs"`
	DefaultCoefReset    bool   `json:"default_coef_reset"`
	YAdler32            uint32 `json:"y_adler32"`
	UAdler32            uint32 `json:"u_adler32"`
	VAdler32            uint32 `json:"v_adler32"`
	SizeBytes           int    `json:"size_bytes"`
}

// oracleTraceRateRow mirrors the libvpx-side "rate" row emitted from
// internal/coracle/build_vpxenc_oracle.sh just before vp8_pack_bitstream.
// Field semantics match the libvpx VP8_COMP fields documented in
// vp8/encoder/onyx_int.h:
//
//	q_index               -> the final accepted Q for this frame
//	active_worst_quality  -> cpi->active_worst_quality
//	active_best_quality   -> cpi->active_best_quality
//	buffer_level          -> cpi->buffer_level (bits)
//	total_byte_count      -> cpi->total_byte_count (cumulative bytes emitted)
//	projected_frame_size  -> cpi->projected_frame_size (bits, post-entropy-savings)
//	this_frame_target     -> cpi->this_frame_target (bits)
//	kf_overspend_bits     -> cpi->kf_overspend_bits
//	gf_overspend_bits     -> cpi->gf_overspend_bits
//
// Fields without a govpx equivalent yet are emitted as zero/sentinel; the
// parity doc tracks the residuals.
type oracleTraceRateRow struct {
	Type               string `json:"type"`
	FrameIndex         uint64 `json:"frame_index"`
	FrameType          string `json:"frame_type"`
	QIndex             int    `json:"q_index"`
	ActiveWorstQ       int    `json:"active_worst_quality"`
	ActiveBestQ        int    `json:"active_best_quality"`
	BufferLevel        int64  `json:"buffer_level"`
	TotalByteCount     int64  `json:"total_byte_count"`
	ProjectedFrameSize int    `json:"projected_frame_size"`
	ThisFrameTarget    int    `json:"this_frame_target"`
	KFOverspendBits    int    `json:"kf_overspend_bits"`
	GFOverspendBits    int    `json:"gf_overspend_bits"`
	// ZbinOverQuant mirrors libvpx's `cpi->mb.zbin_over_quant` at the
	// emission point (just before vp8_pack_bitstream), i.e. the active
	// zbin-overshoot value that drove quantize for the accepted recode
	// attempt. govpx feeds the same value from
	// `e.rc.currentZbinOverQuant`, which is committed by the recode loop
	// for both the GF/ARF boost branch and the regular size-recode branch.
	ZbinOverQuant int `json:"zbin_over_quant"`
}

// oracleTraceRecodeRow mirrors the libvpx-side "recode" row, emitted only
// when the frame's recode loop ran more than one iteration. LoopCount counts
// every encode pass (including the first); FinalQ is the accepted Q the
// loop converged to. Reason is an inferred classification:
//
//	"altref_src"        -> cpi->is_src_frame_alt_ref forced Loop=0
//	"kf_forced_quality" -> KEY_FRAME with this_key_frame_forced
//	"size_recode"       -> recode_loop_test driven termination (default)
type oracleTraceRecodeRow struct {
	Type       string `json:"type"`
	FrameIndex uint64 `json:"frame_index"`
	LoopCount  int    `json:"loop_count"`
	FinalQ     int    `json:"final_q"`
	Reason     string `json:"reason"`
}

// oracleTraceMBRow is the per-macroblock oracle trace row (inter frames only).
type oracleTraceMBRow struct {
	Type       string    `json:"type"`
	FrameIndex uint64    `json:"frame_index"`
	MBRow      int       `json:"mb_row"`
	MBCol      int       `json:"mb_col"`
	SegmentID  int       `json:"segment_id"`
	Mode       string    `json:"mode"`
	RefFrame   string    `json:"ref_frame"`
	MVRow      int16     `json:"mv_row"`
	MVCol      int16     `json:"mv_col"`
	Skip       bool      `json:"skip"`
	EOB        [25]uint8 `json:"eob"`
	EOBSum     int       `json:"eob_sum"`

	ImprovedMVStart        bool  `json:"improved_mv_start"`
	ImprovedMVNearSADIndex int   `json:"improved_mv_near_sadidx"`
	ImprovedMVRow          int16 `json:"improved_mv_row"`
	ImprovedMVCol          int16 `json:"improved_mv_col"`
	ImprovedMVSR           int   `json:"improved_mv_sr"`
}

// oracleTraceEnabled reports whether the encoder is configured to emit the
// oracle trace. Callers should guard tracing logic with this so the per-MB
// fast path performs no extra work when the harness is off.
func (e *VP8Encoder) oracleTraceEnabled() bool {
	return e != nil && e.opts.OracleTraceWriter != nil
}

// oracleTraceFrameSummary is the minimal slice of frame state that callers
// pass to emitOracleFrameTrace. It exists so the call site does not depend on
// the exact attempt struct shape. The frame row's `refresh_entropy_probs`
// and `default_coef_reset` fields are derived inside emitOracleFrameTrace
// from `summary.FrameType` and `e.opts.ErrorResilient`, mirroring libvpx's
// `vp8_pack_bitstream` error-resilient override and the
// `error_resilient && key-frame` default-coef gate respectively, so callers
// do not need to thread those values through the summary struct.
type oracleTraceFrameSummary struct {
	FrameType      vp8common.FrameType
	BaseQIndex     int
	LoopFilter     int
	RefreshLast    bool
	RefreshGolden  bool
	RefreshAltRef  bool
	GoldenSignBias bool
	AltRefSignBias bool
	SegEnabled     bool
	SizeBytes      int
}

// emitOracleFrameTrace writes a single per-frame trace row to the configured
// oracle writer. The encoder's reference reconstruction state has already been
// committed; planeAdler32 reads the visible region of the LAST reference,
// which is the most recently refreshed buffer for typical inter frames and
// the just-encoded frame's reconstruction for keyframes.
func (e *VP8Encoder) emitOracleFrameTrace(summary oracleTraceFrameSummary) {
	if !e.oracleTraceEnabled() {
		return
	}
	keyFrame := summary.FrameType == vp8common.KeyFrame
	// Mirror libvpx vp8/encoder/bitstream.c around line 1226: when
	// error_resilient_mode == VPX_ERROR_RESILIENT_PARTITIONS the encoder
	// forces refresh_entropy_probs to 1 on key frames and to 0 on inter
	// frames, regardless of the per-attempt choice. Outside the
	// error-resilient mode govpx's keyframe path always sets
	// RefreshEntropyProbs=true (see encoder.go: WriteCoefficientKeyFrame
	// configuration), and the inter path sets true unless the caller
	// passed EncodeNoUpdateEntropy. The trace approximates the typical
	// case (no NoUpdateEntropy flag) so libvpx and govpx match in the
	// common configuration; a future hook can override this when the
	// per-attempt flag becomes part of the summary.
	refreshEntropyProbs := true
	if e.opts.ErrorResilient && !keyFrame {
		refreshEntropyProbs = false
	}
	// Mirror libvpx vp8/encoder/bitstream.c default-coef gate exposed by
	// the oracle TU: error_resilient_mode is set AND frame is a key
	// frame. govpx uses `e.opts.ErrorResilient && keyframe` to match.
	defaultCoefReset := e.opts.ErrorResilient && keyFrame
	row := oracleTraceFrameRow{
		Type:                "frame",
		FrameIndex:          e.frameCount,
		QIndex:              e.rc.currentQuantizer,
		BaseQIndex:          summary.BaseQIndex,
		LoopFilter:          summary.LoopFilter,
		RefreshLast:         summary.RefreshLast,
		RefreshGolden:       summary.RefreshGolden,
		RefreshAltRef:       summary.RefreshAltRef,
		GoldenSignBias:      summary.GoldenSignBias,
		AltRefSignBias:      summary.AltRefSignBias,
		SegEnabled:          summary.SegEnabled,
		RefreshEntropyProbs: refreshEntropyProbs,
		DefaultCoefReset:    defaultCoefReset,
		SizeBytes:           summary.SizeBytes,
	}
	if keyFrame {
		row.FrameType = "key"
	} else {
		row.FrameType = "inter"
	}
	row.YAdler32, row.UAdler32, row.VAdler32 = oracleTraceReferenceChecksums(&e.lastRef.Img)
	emitOracleTraceRow(e.opts.OracleTraceWriter, &row)
}

// oracleTraceRateSummary is the slice of rate-control state callers pass to
// emitOracleRateTrace. ActiveWorstQ / ActiveBestQ mirror libvpx's
// active_worst_quality / active_best_quality at the point the recode loop
// has accepted an attempt; ProjectedFrameSizeBits and ThisFrameTargetBits
// are in bits to align with libvpx's int field semantics.
type oracleTraceRateSummary struct {
	FrameType              vp8common.FrameType
	QIndex                 int
	ActiveWorstQ           int
	ActiveBestQ            int
	BufferLevelBits        int64
	TotalByteCount         int64
	ProjectedFrameSizeBits int
	ThisFrameTargetBits    int
	KFOverspendBits        int
	GFOverspendBits        int
	// ZbinOverQuant mirrors libvpx's `cpi->mb.zbin_over_quant` at the
	// emission point. Fed from `e.rc.currentZbinOverQuant` which the
	// recode loop commits for the GF/ARF boost branch and the regular
	// size-recode branch alike.
	ZbinOverQuant int
}

// emitOracleRateTrace writes a single per-frame "rate" row capturing the
// rate-control state at the point the encoder has accepted the final
// recoded attempt. The row schema matches the libvpx-side oracle-trace
// patch in internal/coracle/build_vpxenc_oracle.sh.
func (e *VP8Encoder) emitOracleRateTrace(summary oracleTraceRateSummary) {
	if !e.oracleTraceEnabled() {
		return
	}
	row := oracleTraceRateRow{
		Type:               "rate",
		FrameIndex:         e.frameCount,
		QIndex:             summary.QIndex,
		ActiveWorstQ:       summary.ActiveWorstQ,
		ActiveBestQ:        summary.ActiveBestQ,
		BufferLevel:        summary.BufferLevelBits,
		TotalByteCount:     summary.TotalByteCount,
		ProjectedFrameSize: summary.ProjectedFrameSizeBits,
		ThisFrameTarget:    summary.ThisFrameTargetBits,
		KFOverspendBits:    summary.KFOverspendBits,
		GFOverspendBits:    summary.GFOverspendBits,
		ZbinOverQuant:      summary.ZbinOverQuant,
	}
	switch summary.FrameType {
	case vp8common.KeyFrame:
		row.FrameType = "key"
	default:
		row.FrameType = "inter"
	}
	emitOracleTraceRow(e.opts.OracleTraceWriter, &row)
}

// oracleTraceRecodeSummary describes the recode-loop outcome for the just
// finished frame. LoopCount counts every encode pass, including the first;
// rows are only emitted when LoopCount > 1.
type oracleTraceRecodeSummary struct {
	LoopCount int
	FinalQ    int
	Reason    string
}

// emitOracleRecodeTrace writes a single per-frame "recode" row when the
// frame's recode loop ran more than once. Reason is one of "altref_src",
// "kf_forced_quality", or "size_recode" to align with the libvpx-side
// classification.
func (e *VP8Encoder) emitOracleRecodeTrace(summary oracleTraceRecodeSummary) {
	if !e.oracleTraceEnabled() || summary.LoopCount <= 1 {
		return
	}
	row := oracleTraceRecodeRow{
		Type:       "recode",
		FrameIndex: e.frameCount,
		LoopCount:  summary.LoopCount,
		FinalQ:     summary.FinalQ,
		Reason:     summary.Reason,
	}
	if row.Reason == "" {
		row.Reason = "size_recode"
	}
	emitOracleTraceRow(e.opts.OracleTraceWriter, &row)
}

// emitOracleRateAndRecodeTrace emits the per-frame "rate" row plus, when
// the recode loop ran more than once, a "recode" row. The pair is written
// before the corresponding "frame" row so the JSONL ordering matches the
// libvpx-side patch in internal/coracle/build_vpxenc_oracle.sh, which emits
// rate/recode rows immediately before vp8_pack_bitstream and the per-frame
// row inside vp8_pack_bitstream itself.
//
// frameType drives the goldenFrame heuristic for the active-Q bound
// computation: KeyFrame queries the key-frame branch, otherwise the
// inter/golden branch. sizeBytes accumulates into oracleTraceTotalByteCount
// AFTER the rate row is emitted so the field reflects libvpx's
// cpi->total_byte_count which is updated post-pack (i.e. before this
// frame's contribution).
func (e *VP8Encoder) emitOracleRateAndRecodeTrace(frameType vp8common.FrameType, finalQuantizer int, sizeBytes int) {
	if !e.oracleTraceEnabled() {
		return
	}
	keyFrame := frameType == vp8common.KeyFrame
	activeBest, activeWorst := e.rc.libvpxActiveQuantizerBounds(keyFrame, false)
	projectedBits := sizeBytes * 8
	e.emitOracleRateTrace(oracleTraceRateSummary{
		FrameType:              frameType,
		QIndex:                 finalQuantizer,
		ActiveWorstQ:           activeWorst,
		ActiveBestQ:            activeBest,
		BufferLevelBits:        int64(e.rc.bufferLevelBits),
		TotalByteCount:         e.oracleTraceTotalByteCount,
		ProjectedFrameSizeBits: projectedBits,
		ThisFrameTargetBits:    e.rc.frameTargetBits,
		KFOverspendBits:        e.rc.kfOverspendBits,
		GFOverspendBits:        e.rc.gfOverspendBits,
		ZbinOverQuant:          e.rc.currentZbinOverQuant,
	})
	if e.oracleTraceRecodeLoopCount > 1 {
		reason := "size_recode"
		// govpx does not currently model libvpx's is_src_frame_alt_ref or
		// this_key_frame_forced gating on its recode loop; fall back to
		// "size_recode" which is libvpx's default reason once the alt-ref
		// override and forced-keyframe quality branches are excluded.
		e.emitOracleRecodeTrace(oracleTraceRecodeSummary{
			LoopCount: e.oracleTraceRecodeLoopCount,
			FinalQ:    finalQuantizer,
			Reason:    reason,
		})
	}
	// Mirror libvpx's "cpi->total_byte_count += projected_bytes" which runs
	// after pack_bitstream. The trace row already reflects the pre-frame
	// total so the next frame's rate row sees the same cumulative value
	// libvpx would.
	e.oracleTraceTotalByteCount += int64(sizeBytes)
}

// resetOracleMBTraceBuffer clears any accumulated per-MB trace rows. It is
// called at the start of each inter-frame coefficient build pass so retried
// (recoded) attempts overwrite earlier rows; the final attempt's rows are
// flushed by flushOracleMBTraceBuffer at frame commit time.
func (e *VP8Encoder) resetOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
}

// flushOracleMBTraceBuffer writes the buffered per-MB rows to the configured
// writer in scan order and clears the buffer.
func (e *VP8Encoder) flushOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	w := e.opts.OracleTraceWriter
	for i := range e.oracleTraceMBBuffer {
		emitOracleTraceRow(w, &e.oracleTraceMBBuffer[i])
	}
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
}

// emitOracleMBTrace appends a per-macroblock trace row to the encoder's
// internal buffer. The row is flushed to the writer when the surrounding
// frame is committed; rows from intermediate (recoded) attempts are
// discarded by resetOracleMBTraceBuffer. mode and coeffs must reference the
// freshly written entries for (mbRow, mbCol). The caller already holds these
// values in govpx's per-MB inter loop, so this function performs no
// additional VP8 computation.
func (e *VP8Encoder) emitOracleMBTrace(
	mbRow int, mbCol int,
	mode *vp8enc.InterFrameMacroblockMode,
	coeffs *vp8enc.MacroblockCoefficients,
) {
	if !e.oracleTraceEnabled() || mode == nil || coeffs == nil {
		return
	}
	row := oracleTraceMBRow{
		Type:       "mb",
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		SegmentID:  int(mode.SegmentID),
		Mode:       oracleTraceModeName(mode.Mode),
		RefFrame:   oracleTraceRefName(mode.RefFrame),
		MVRow:      mode.MV.Row,
		MVCol:      mode.MV.Col,
		Skip:       mode.MBSkipCoeff,

		ImprovedMVNearSADIndex: -1,
		ImprovedMVSR:           -1,
	}
	if mode.ImprovedMVStart {
		row.ImprovedMVStart = true
		row.ImprovedMVNearSADIndex = int(mode.ImprovedMVNearSADIndex)
		row.ImprovedMVRow = mode.ImprovedMVPredictor.Row
		row.ImprovedMVCol = mode.ImprovedMVPredictor.Col
		row.ImprovedMVSR = int(mode.ImprovedMVSR)
	}
	sum := 0
	for i := 0; i < 25; i++ {
		row.EOB[i] = coeffs.EOB[i]
		sum += int(coeffs.EOB[i])
	}
	row.EOBSum = sum
	e.oracleTraceMBBuffer = append(e.oracleTraceMBBuffer, row)
}

// emitOracleTraceRow marshals a row to JSON, appends a newline, and writes a
// single payload to the configured writer. Marshal errors are silently
// ignored to avoid disturbing the encode path; the trace is a debugging aid.
func emitOracleTraceRow(w io.Writer, row interface{}) {
	if w == nil {
		return
	}
	buf, err := json.Marshal(row)
	if err != nil {
		return
	}
	buf = append(buf, '\n')
	_, _ = w.Write(buf)
}

// oracleTraceReferenceChecksums computes Adler32 checksums over the visible
// region of the supplied reconstruction image (Y/U/V planes). Adler32 is
// chosen because it is cheap, deterministic, available in the standard
// library, and aligns with libvpx's existing checksum tooling.
func oracleTraceReferenceChecksums(img *vp8common.Image) (uint32, uint32, uint32) {
	if img == nil {
		return 0, 0, 0
	}
	yChecksum := planeAdler32(img.Y, img.Width, img.Height, img.YStride)
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	uChecksum := planeAdler32(img.U, uvWidth, uvHeight, img.UStride)
	vChecksum := planeAdler32(img.V, uvWidth, uvHeight, img.VStride)
	return yChecksum, uChecksum, vChecksum
}

func planeAdler32(plane []byte, width int, height int, stride int) uint32 {
	if width <= 0 || height <= 0 || stride <= 0 {
		return 0
	}
	h := adler32.New()
	for row := 0; row < height; row++ {
		start := row * stride
		end := start + width
		if end > len(plane) {
			break
		}
		_, _ = h.Write(plane[start:end])
	}
	return h.Sum32()
}

func oracleTraceModeName(mode vp8common.MBPredictionMode) string {
	switch mode {
	case vp8common.DCPred:
		return "DC_PRED"
	case vp8common.VPred:
		return "V_PRED"
	case vp8common.HPred:
		return "H_PRED"
	case vp8common.TMPred:
		return "TM_PRED"
	case vp8common.BPred:
		return "B_PRED"
	case vp8common.NearestMV:
		return "NEARESTMV"
	case vp8common.NearMV:
		return "NEARMV"
	case vp8common.ZeroMV:
		return "ZEROMV"
	case vp8common.NewMV:
		return "NEWMV"
	case vp8common.SplitMV:
		return "SPLITMV"
	default:
		return fmt.Sprintf("MODE_%d", int(mode))
	}
}

func oracleTraceRefName(ref vp8common.MVReferenceFrame) string {
	switch ref {
	case vp8common.IntraFrame:
		return "INTRA_FRAME"
	case vp8common.LastFrame:
		return "LAST_FRAME"
	case vp8common.GoldenFrame:
		return "GOLDEN_FRAME"
	case vp8common.AltRefFrame:
		return "ALTREF_FRAME"
	default:
		return fmt.Sprintf("REF_%d", int(ref))
	}
}
