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
//   {"type":"mb",    ...}  one per macroblock
//   {"type":"inter_candidate", ...} evaluated inter-mode candidates
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
	"github.com/thesyncim/govpx/internal/vp8/dsp"
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
	Type                 string  `json:"type"`
	FrameIndex           uint64  `json:"frame_index"`
	FrameType            string  `json:"frame_type"`
	QIndex               int     `json:"q_index"`
	BaseQIndex           int     `json:"base_q_index"`
	LoopFilter           int     `json:"loop_filter_level"`
	SharpnessLevel       int     `json:"sharpness_level"`
	RefLFDeltas          [4]int8 `json:"ref_lf_deltas"`
	ModeLFDeltas         [4]int8 `json:"mode_lf_deltas"`
	ModeRefLFDeltaEnable bool    `json:"mode_ref_lf_delta_enabled"`
	ModeRefLFDeltaUpdate bool    `json:"mode_ref_lf_delta_update"`
	RefreshLast          bool    `json:"refresh_last"`
	RefreshGolden        bool    `json:"refresh_golden"`
	RefreshAltRef        bool    `json:"refresh_altref"`
	GoldenSignBias       bool    `json:"sign_bias_golden"`
	AltRefSignBias       bool    `json:"sign_bias_altref"`
	SegEnabled           bool    `json:"segmentation_enabled"`
	RefreshEntropyProbs  bool    `json:"refresh_entropy_probs"`
	DefaultCoefReset     bool    `json:"default_coef_reset"`
	YAdler32             uint32  `json:"y_adler32"`
	UAdler32             uint32  `json:"u_adler32"`
	VAdler32             uint32  `json:"v_adler32"`
	// Probability-state digests. Each Adler32 is computed over the
	// flat byte content of the corresponding probability table at the
	// moment the frame is committed (after entropy updates have been
	// applied). The libvpx-side oracle in
	// internal/coracle/build_vpxenc_oracle.sh computes the same digests
	// over `cm->fc.coef_probs`, `cm->fc.ymode_prob`,
	// `cm->fc.uv_mode_prob`, and the row+col `cm->fc.mvc[].prob` arrays
	// at the same emission point (tail of vp8_pack_bitstream). Any
	// per-byte drift in any of those tables surfaces as a single
	// field-level diff in the comparator.
	CoefProbsAdler   uint32 `json:"coef_probs_adler"`
	YModeProbsAdler  uint32 `json:"ymode_probs_adler"`
	UVModeProbsAdler uint32 `json:"uv_mode_probs_adler"`
	MVProbsAdler     uint32 `json:"mv_probs_adler"`
	// Reference-frame coding probabilities at the same emission point.
	// Mirror libvpx's `cpi->prob_intra_coded`, `cpi->prob_last_coded`,
	// `cpi->prob_gf_coded` (vp8/encoder/onyx_int.h), which the encoder
	// updates inside the recode loop via `update_rd_ref_frame_probs`.
	// govpx tracks the same values through `e.refProbIntra`,
	// `e.refProbLast`, `e.refProbGolden`.
	ProbIntraCoded int `json:"prob_intra_coded"`
	ProbLastCoded  int `json:"prob_last_coded"`
	ProbGFCoded    int `json:"prob_gf_coded"`
	SizeBytes      int `json:"size_bytes"`
}

// oracleTraceDroppedFrameRow mirrors the libvpx-side "dropped_frame" row
// emitted from internal/coracle/build_vpxenc_oracle.sh at each of the three
// drop-decision return paths in vp8/encoder/onyx_if.c
// (vp8_check_drop_buffer, vp8_pick_frame_size buffer-underrun, and
// vp8_drop_encodedframe_overshoot). The schema captures the rate-control
// state that the libvpx parity oracle exposes for drop-frame parity:
//
//	frame_index   - source-frame ordinal (matches a non-dropped frame row's
//	                FrameIndex slot in the encoded output)
//	dropped       - always true on this row; set so a downstream consumer
//	                that filters on type=="frame" still distinguishes
//	                emitted frames from dropped ones
//	force_maxqp   - libvpx cpi->force_maxqp AFTER the drop decision committed
//	                its lifecycle update (set on overshoot drops, cleared on
//	                the next non-dropped frame)
//	buffer_level  - cpi->buffer_level (bits) AFTER the drop accounting
//	                refunded av_per_frame_bandwidth and clamped to
//	                maximum_buffer_size (mirrors govpx's rc.bufferLevelBits
//	                after rc.postDropFrame)
//	this_frame_target - the per-frame bandwidth target that was active when
//	                the drop fired (libvpx cpi->this_frame_target / govpx
//	                rc.frameTargetBits or rc.bitsPerFrame)
//	reason        - inferred classification: "buffer_underrun" when the
//	                buffer-underrun branch fired; "overshoot" when the
//	                post-encode overshoot drop fired; "decimation" when the
//	                drop-frames-water-mark decimation branch fired. govpx
//	                only implements the buffer-underrun branch today, so it
//	                always emits "buffer_underrun".
type oracleTraceDroppedFrameRow struct {
	Type            string `json:"type"`
	FrameIndex      uint64 `json:"frame_index"`
	FrameType       string `json:"frame_type"`
	Dropped         bool   `json:"dropped"`
	ForceMaxQP      bool   `json:"force_maxqp"`
	BufferLevel     int64  `json:"buffer_level"`
	ThisFrameTarget int    `json:"this_frame_target"`
	Reason          string `json:"reason"`
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

// oracleTraceMBRow is the per-macroblock oracle trace row.
type oracleTraceMBRow struct {
	Type       string        `json:"type"`
	FrameIndex uint64        `json:"frame_index"`
	MBRow      int           `json:"mb_row"`
	MBCol      int           `json:"mb_col"`
	SegmentID  int           `json:"segment_id"`
	Mode       string        `json:"mode"`
	RefFrame   string        `json:"ref_frame"`
	MVRow      int16         `json:"mv_row"`
	MVCol      int16         `json:"mv_col"`
	Skip       bool          `json:"skip"`
	Partition  *int          `json:"partition,omitempty"`
	BlockMVRow []int16       `json:"block_mv_rows,omitempty"`
	BlockMVCol []int16       `json:"block_mv_cols,omitempty"`
	UVMode     string        `json:"uv_mode,omitempty"`
	BModes     []string      `json:"b_modes,omitempty"`
	EOB        [25]uint8     `json:"eob"`
	EOBSum     int           `json:"eob_sum"`
	QCoeff     [25][16]int16 `json:"qcoeff"`

	ImprovedMVStart        bool  `json:"improved_mv_start"`
	ImprovedMVNearSADIndex int   `json:"improved_mv_near_sadidx"`
	ImprovedMVRow          int16 `json:"improved_mv_row"`
	ImprovedMVCol          int16 `json:"improved_mv_col"`
	ImprovedMVSR           int   `json:"improved_mv_sr"`
}

// oracleTracePredictorRow captures the inter-prediction output for a single
// macroblock plane before residual is added. Mirrors the libvpx-side
// "predictor" row emitted from internal/coracle/build_vpxenc_oracle.sh
// (oracle_trace.c: govpx_oracle_emit_predictor) which captures
// xd->dst.{y,u,v}_buffer between vp8_encode_inter16x16 and
// vp8_inverse_transform_mby. govpx captures the same point via
// reconstructInterAnalysisMacroblock(MBSkipCoeff=1) which writes only the
// predictor into the analysis image. Used to localize chroma sub-pel
// rounding gaps at sizes >64x64 (see plan.md "Encoder Quality" first
// bullet). Emission is gated by GOVPX_ORACLE_PREDICTOR_DUMP=1 on both
// sides so the row is captured only when explicitly requested.
type oracleTracePredictorRow struct {
	Type       string `json:"type"`
	FrameIndex uint64 `json:"frame_index"`
	MBRow      int    `json:"mb_row"`
	MBCol      int    `json:"mb_col"`
	Plane      string `json:"plane"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Hex        string `json:"hex"`
}

// oracleTraceLastRefWindowRow captures the LAST reference's Y/U/V planes
// including border content at the start of an inter frame's encode pass.
// Used to verify that border-extension state matches between govpx and
// libvpx; the chroma sub-pel filter taps reach into the top border for
// MB row 0, so a border drift surfaces as a predictor diff at frame N+1
// even when the encode-time visible region matches at frame N.
type oracleTraceLastRefWindowRow struct {
	Type       string `json:"type"`
	FrameIndex uint64 `json:"frame_index"`
	Plane      string `json:"plane"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	BorderTop  int    `json:"border_top"`
	BorderLeft int    `json:"border_left"`
	Hex        string `json:"hex"`
}

// oracleTraceLFTrialRow records a single per-trial-level evaluation inside
// the fast loop-filter picker (pickLoopFilterLevelFast). Each row carries
// the trial filter level and the resulting partial-frame Y SSE as scored
// by loopFilterTrialLumaSSE. The libvpx-side oracle patch in
// internal/coracle/build_vpxenc_oracle.sh emits the matching
// {"type":"lf_trial",...} rows from vp8cx_pick_filter_level_fast after
// each calc_partial_ssl_err call. Phase mirrors the libvpx call site and
// is one of "seed", "down", "up". FrameIndex uses the upcoming frame's
// index so the picker call for frame N reports N.
type oracleTraceLFTrialRow struct {
	Type       string `json:"type"`
	FrameIndex uint64 `json:"frame_index"`
	Phase      string `json:"phase"`
	TrialLevel int    `json:"trial_level"`
	TrialYSSE  int    `json:"trial_y_sse"`
}

type oracleTraceInterCandidateRow struct {
	Type       string `json:"type"`
	FrameIndex uint64 `json:"frame_index"`
	MBRow      int    `json:"mb_row"`
	MBCol      int    `json:"mb_col"`

	Picker    string `json:"picker"`
	ModeIndex int    `json:"mode_index"`
	Mode      string `json:"mode"`
	RefSlot   int    `json:"ref_slot"`
	RefFrame  string `json:"ref_frame"`

	Threshold       int    `json:"threshold"`
	BestScoreBefore int    `json:"best_score_before"`
	BestYRDBefore   int    `json:"best_yrd_before"`
	BestSSEBefore   int    `json:"best_sse_before"`
	Outcome         string `json:"outcome"`
	BecameBest      bool   `json:"became_best"`
	LoopBreak       bool   `json:"loop_break"`

	Score        int  `json:"score"`
	YRD          int  `json:"yrd"`
	Rate         int  `json:"rate"`
	RateY        int  `json:"rate_y"`
	RateUV       int  `json:"rate_uv"`
	Distortion   int  `json:"distortion"`
	DistortionUV int  `json:"distortion_uv"`
	SSE          int  `json:"sse"`
	Skip         bool `json:"skip"`

	MVRow int16 `json:"mv_row"`
	MVCol int16 `json:"mv_col"`

	ImprovedMVStart        bool  `json:"improved_mv_start"`
	ImprovedMVNearSADIndex int   `json:"improved_mv_near_sadidx"`
	ImprovedMVRow          int16 `json:"improved_mv_row"`
	ImprovedMVCol          int16 `json:"improved_mv_col"`
	ImprovedMVSR           int   `json:"improved_mv_sr"`
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
}

const oracleTraceInterCandidateUnknown = -1

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
//
// SharpnessLevel mirrors libvpx's `cm->sharpness_level`. RefLFDeltas /
// ModeLFDeltas / ModeRefLFDeltaEnable / ModeRefLFDeltaUpdate mirror
// `cm->ref_lf_deltas[]`, `cm->mode_lf_deltas[]`,
// `cm->mode_ref_lf_delta_enabled`, and `cm->mode_ref_lf_delta_update`
// respectively. The govpx side reads these straight from the accepted
// attempt's loop-filter header; the libvpx-side oracle patch in
// internal/coracle/build_vpxenc_oracle.sh reads them from the VP8_COMMON
// state at the same emission point.
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
		Type:                 "frame",
		FrameIndex:           e.frameCount,
		QIndex:               e.rc.currentQuantizer,
		BaseQIndex:           summary.BaseQIndex,
		LoopFilter:           summary.LoopFilter,
		SharpnessLevel:       summary.SharpnessLevel,
		RefLFDeltas:          summary.RefLFDeltas,
		ModeLFDeltas:         summary.ModeLFDeltas,
		ModeRefLFDeltaEnable: summary.ModeRefLFDeltaEnable,
		ModeRefLFDeltaUpdate: summary.ModeRefLFDeltaUpdate,
		RefreshLast:          summary.RefreshLast,
		RefreshGolden:        summary.RefreshGolden,
		RefreshAltRef:        summary.RefreshAltRef,
		GoldenSignBias:       summary.GoldenSignBias,
		AltRefSignBias:       summary.AltRefSignBias,
		SegEnabled:           summary.SegEnabled,
		RefreshEntropyProbs:  refreshEntropyProbs,
		DefaultCoefReset:     defaultCoefReset,
		SizeBytes:            summary.SizeBytes,
	}
	if keyFrame {
		row.FrameType = "key"
	} else {
		row.FrameType = "inter"
	}
	row.YAdler32, row.UAdler32, row.VAdler32 = oracleTraceReferenceChecksums(&e.lastRef.Img)
	row.CoefProbsAdler, row.YModeProbsAdler, row.UVModeProbsAdler, row.MVProbsAdler = e.oracleTraceProbabilityDigests()
	row.ProbIntraCoded = int(e.refProbIntra)
	row.ProbLastCoded = int(e.refProbLast)
	row.ProbGFCoded = int(e.refProbGolden)
	emitOracleTraceRow(e.opts.OracleTraceWriter, &row)
}

// oracleTraceProbabilityDigests returns Adler32 digests over the encoder's
// frame-level probability tables at the moment of emission. The four digests
// align byte-for-byte with the libvpx-side hashes computed by the patched
// vpxenc oracle (see internal/coracle/build_vpxenc_oracle.sh): the coef table
// covers BLOCK_TYPES * COEF_BANDS * PREV_COEF_CONTEXTS * ENTROPY_NODES = 1056
// bytes; the YMode / UVMode digests cover the inter-frame intra-mode probs
// (4 / 3 bytes); the MV digest concatenates row+col probability components
// (2 * 19 = 38 bytes) so a one-byte drift in either component is detected.
func (e *VP8Encoder) oracleTraceProbabilityDigests() (uint32, uint32, uint32, uint32) {
	var coefBuf [4 * 8 * 3 * 11]byte
	off := 0
	for block := 0; block < 4; block++ {
		for band := 0; band < 8; band++ {
			for ctx := 0; ctx < 3; ctx++ {
				for node := 0; node < 11; node++ {
					coefBuf[off] = e.coefProbs[block][band][ctx][node]
					off++
				}
			}
		}
	}
	coefHash := adler32.Checksum(coefBuf[:])
	yModeHash := adler32.Checksum(e.modeProbs.YMode[:])
	uvModeHash := adler32.Checksum(e.modeProbs.UVMode[:])
	var mvBuf [2 * 19]byte
	for i := 0; i < 19; i++ {
		mvBuf[i] = e.modeProbs.MV[0][i]
		mvBuf[19+i] = e.modeProbs.MV[1][i]
	}
	mvHash := adler32.Checksum(mvBuf[:])
	return coefHash, yModeHash, uvModeHash, mvHash
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

// emitOracleDroppedFrameTrace writes a single per-frame trace row capturing
// a drop decision. Called from the encoder's CBR drop branch in encoder.go
// after rc.postDropFrame() has committed the buffer accounting and
// e.forceMaxQuantizer has been set, so BufferLevel and ForceMaxQP reflect
// the post-drop state the next frame will see (matching libvpx's emission
// point right after vp8_check_drop_buffer / vp8_pick_frame_size returns 1
// or vp8_drop_encodedframe_overshoot returns 1, with cpi->buffer_level and
// cpi->force_maxqp already updated).
//
// reason is one of:
//
//	"buffer_underrun" - libvpx calc_pframe_target_size buffer<0 branch
//	"overshoot"       - libvpx vp8_drop_encodedframe_overshoot
//	"decimation"      - libvpx vp8_check_drop_buffer decimation_factor
//
// govpx currently only implements the buffer_underrun branch (see
// rateControlState.shouldDropInterFrame which gates on bufferLevelBits<0);
// the helper takes reason as a parameter so future govpx drops can reuse it.
func (e *VP8Encoder) emitOracleDroppedFrameTrace(reason string) {
	if !e.oracleTraceEnabled() {
		return
	}
	row := oracleTraceDroppedFrameRow{
		Type:            "frame",
		FrameIndex:      e.frameCount,
		FrameType:       "inter",
		Dropped:         true,
		ForceMaxQP:      e.forceMaxQuantizer,
		BufferLevel:     int64(e.rc.bufferLevelBits),
		ThisFrameTarget: e.rc.frameTargetBits,
		Reason:          reason,
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
// frame's contribution). projectedBits is the accepted attempt's pre-pack
// RD projection after libvpx-style entropy-savings subtraction.
func (e *VP8Encoder) emitOracleRateAndRecodeTrace(frameType vp8common.FrameType, finalQuantizer int, sizeBytes int, projectedBits int) {
	if !e.oracleTraceEnabled() {
		return
	}
	keyFrame := frameType == vp8common.KeyFrame
	activeBest, activeWorst := e.rc.libvpxActiveQuantizerBounds(keyFrame, false)
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
		reason := e.oracleTraceRecodeReason
		if reason == "" {
			reason = "size_recode"
		}
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
// called at the start of each coefficient build pass so retried
// (recoded) attempts overwrite earlier rows; the final attempt's rows are
// flushed by flushOracleMBTraceBuffer at frame commit time.
func (e *VP8Encoder) resetOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
	e.oracleTraceInterCandidateBuffer = e.oracleTraceInterCandidateBuffer[:0]
}

// flushOracleMBTraceBuffer writes the buffered per-MB rows to the configured
// writer in scan order and clears the buffer.
func (e *VP8Encoder) flushOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	w := e.opts.OracleTraceWriter
	for i := range e.oracleTraceInterCandidateBuffer {
		emitOracleTraceRow(w, &e.oracleTraceInterCandidateBuffer[i])
	}
	for i := range e.oracleTraceMBBuffer {
		emitOracleTraceRow(w, &e.oracleTraceMBBuffer[i])
	}
	e.oracleTraceInterCandidateBuffer = e.oracleTraceInterCandidateBuffer[:0]
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
}

func (e *VP8Encoder) emitOracleInterCandidateTrace(summary oracleTraceInterCandidateSummary) {
	if !e.oracleTraceEnabled() {
		return
	}
	mv := summary.MV
	improvedMVNearSADIndex := oracleTraceInterCandidateUnknown
	improvedMVSR := oracleTraceInterCandidateUnknown
	var improvedMVPredictor vp8enc.MotionVector
	if summary.HasModeTrace {
		mv = summary.ModeTrace.MV
		if summary.ModeTrace.ImprovedMVStart {
			improvedMVNearSADIndex = int(summary.ModeTrace.ImprovedMVNearSADIndex)
			improvedMVSR = int(summary.ModeTrace.ImprovedMVSR)
			improvedMVPredictor = summary.ModeTrace.ImprovedMVPredictor
		}
	}
	if summary.RefFrame == vp8common.IntraFrame || summary.Mode == vp8common.SplitMV {
		mv = vp8enc.MotionVector{}
	}
	outcome := summary.Outcome
	if outcome == "" {
		outcome = "tested"
	}
	row := oracleTraceInterCandidateRow{
		Type:       "inter_candidate",
		FrameIndex: e.frameCount,
		MBRow:      summary.MBRow,
		MBCol:      summary.MBCol,

		Picker:    summary.Picker,
		ModeIndex: summary.ModeIndex,
		Mode:      oracleTraceModeName(summary.Mode),
		RefSlot:   summary.RefSlot,
		RefFrame:  oracleTraceRefName(summary.RefFrame),

		Threshold:       summary.Threshold,
		BestScoreBefore: summary.BestScoreBefore,
		BestYRDBefore:   summary.BestYRDBefore,
		BestSSEBefore:   summary.BestSSEBefore,
		Outcome:         outcome,
		BecameBest:      summary.BecameBest,
		LoopBreak:       summary.LoopBreak,

		Score:        summary.Score,
		YRD:          summary.YRD,
		Rate:         summary.Rate,
		RateY:        summary.RateY,
		RateUV:       summary.RateUV,
		Distortion:   summary.Distortion,
		DistortionUV: summary.DistortionUV,
		SSE:          summary.SSE,
		Skip:         summary.Skip,

		MVRow: mv.Row,
		MVCol: mv.Col,

		ImprovedMVStart:        summary.HasModeTrace && summary.ModeTrace.ImprovedMVStart,
		ImprovedMVNearSADIndex: improvedMVNearSADIndex,
		ImprovedMVRow:          improvedMVPredictor.Row,
		ImprovedMVCol:          improvedMVPredictor.Col,
		ImprovedMVSR:           improvedMVSR,
	}
	e.oracleTraceInterCandidateBuffer = append(e.oracleTraceInterCandidateBuffer, row)
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
	if mode.Mode == vp8common.SplitMV {
		partition := int(mode.Partition)
		row.Partition = &partition
		row.BlockMVRow = make([]int16, len(mode.BlockMV))
		row.BlockMVCol = make([]int16, len(mode.BlockMV))
		for i := range mode.BlockMV {
			row.BlockMVRow[i] = mode.BlockMV[i].Row
			row.BlockMVCol[i] = mode.BlockMV[i].Col
		}
	}
	sum := 0
	for i := 0; i < 25; i++ {
		row.EOB[i] = coeffs.EOB[i]
		row.QCoeff[i] = coeffs.QCoeff[i]
	}
	is4x4 := false
	if mode.RefFrame != vp8common.IntraFrame {
		is4x4 = mode.Mode == vp8common.SplitMV
	} else {
		is4x4 = mode.Mode == vp8common.BPred
	}
	segID := int(mode.SegmentID)
	if segID >= 0 && segID < len(e.dequants) {
		applyOracleEOBAdjust(coeffs, &e.dequants[segID].Y2, is4x4, &row.EOB)
	}
	if is4x4 && coeffs.OracleStaleY2Set {
		// libvpx's vp8_quantize_mb skips block 24 for SPLITMV/B_PRED,
		// so xd->block[24].qcoeff/eobs[24] retain stale data from the
		// last RD-pick mode that quantized Y2. Mirror that contribution
		// using the chosen mode's Y2-equivalent computation captured by
		// buildPredictedMacroblockCoefficientsRD; this keeps the
		// per-MB eob_sum scoreboard aligned with libvpx without
		// modifying the actual encoder block-24 state.
		row.EOB[24] = coeffs.OracleStaleY2EOB
		row.QCoeff[24] = coeffs.OracleStaleY2QCoeff
	}
	for i := 0; i < 25; i++ {
		sum += int(row.EOB[i])
	}
	row.EOBSum = sum
	e.oracleTraceMBBuffer = append(e.oracleTraceMBBuffer, row)
}

func (e *VP8Encoder) emitOracleKeyFrameMBTrace(
	mbRow int, mbCol int,
	mode *vp8enc.KeyFrameMacroblockMode,
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
		Mode:       oracleTraceModeName(mode.YMode),
		RefFrame:   oracleTraceRefName(vp8common.IntraFrame),
		UVMode:     oracleTraceModeName(mode.UVMode),

		ImprovedMVNearSADIndex: -1,
		ImprovedMVSR:           -1,
	}
	if mode.YMode == vp8common.BPred {
		row.BModes = make([]string, len(mode.BModes))
		for i, bMode := range mode.BModes {
			row.BModes[i] = oracleTraceBModeName(bMode)
		}
	}
	sum := 0
	for i := 0; i < 25; i++ {
		row.EOB[i] = coeffs.EOB[i]
		row.QCoeff[i] = coeffs.QCoeff[i]
	}
	is4x4 := mode.YMode == vp8common.BPred
	segID := int(mode.SegmentID)
	if segID >= 0 && segID < len(e.dequants) {
		applyOracleEOBAdjust(coeffs, &e.dequants[segID].Y2, is4x4, &row.EOB)
	}
	if is4x4 && coeffs.OracleStaleY2Set {
		row.EOB[24] = coeffs.OracleStaleY2EOB
		row.QCoeff[24] = coeffs.OracleStaleY2QCoeff
	}
	for i := 0; i < 25; i++ {
		sum += int(row.EOB[i])
	}
	row.EOBSum = sum
	e.oracleTraceMBBuffer = append(e.oracleTraceMBBuffer, row)
}

// applyOracleEOBAdjust mirrors libvpx's per-Y-block eob bump for the per-MB
// oracle trace. There are two libvpx code paths that can leave eob=1 with
// an all-zero qcoeff[0] in xd->eobs / xd->block[i].qcoeff at oracle-capture
// time:
//
//  1. vp8_quantize_mb runs vp8_fast_quantize_b_c (or
//     vp8_regular_quantize_b_c) on the Y block with the original (un-zeroed)
//     dct[0] against Y1DC's zbin/round/quant. If that DC quantizes to
//     non-zero, *d->eob is set to 1 even when every other position is zero.
//     vp8_dequant_idct_add_y_block later memsets qcoeff[0..1] back to zero,
//     but eob=1 survives. govpx tracks the would-have-been bit per Y block
//     in coeffs.OracleY1DCEOB1[block].
//
//  2. vp8_inverse_transform_mby runs the inverse Walsh on the Y2 block,
//     writing a per-Y-block DC value into xd->qcoeff[i*16]. eob_adjust then
//     bumps eobs[i] from 0 to 1 if that DC is non-zero, so the IDCT path
//     doesn't skip the block. The same memset clears qcoeff[0..1] later.
//
// The adjustment is purely cosmetic for the trace (bitstream tokenize,
// reconstruction, and the parity decoder all already handle the eob=0 vs
// eob=1 distinction correctly because the qcoeff payload is identical). It
// only happens when the macroblock has a Y2 second-order block (i.e. the
// non-4x4 / non-SPLITMV / non-B_PRED case).
//
// y2Dequant is the segment-specific Y2 dequant table (cpi->common.Y2dequant
// in libvpx). is4x4 mirrors libvpx's `mode != SPLITMV` (or `mode != B_PRED`
// for keyframes) gate that skips the eob_adjust.
func applyOracleEOBAdjust(coeffs *vp8enc.MacroblockCoefficients, y2Dequant *[16]int16, is4x4 bool, eob *[25]uint8) {
	if coeffs == nil || y2Dequant == nil || eob == nil || is4x4 {
		return
	}
	// Path 1: bump from libvpx Y1DC quantize on the original dct[0] of each
	// Y block. coeffs.OracleY1DCEOB1[block] was populated at quantize time
	// from the same dct[0] that fed the Y2 forward Walsh.
	for js := 0; js < 16; js++ {
		if eob[js] == 0 && coeffs.OracleY1DCEOB1[js] != 0 {
			eob[js] = 1
		}
	}
	// Path 2: bump from libvpx eob_adjust against the inverse-Walsh DC of
	// the Y2 block. This is the residual case where the post-Walsh DC is
	// non-zero even though Y1DC quantize produced zero.
	var y2DQ [16]int16
	for i := 0; i < 16; i++ {
		y2DQ[i] = int16(int(coeffs.QCoeff[24][i]) * int(y2Dequant[i]))
	}
	var dcSlots [16 * 16]int16
	if eob[24] > 1 {
		dsp.InverseWalsh4x4(&y2DQ, dcSlots[:])
	} else {
		dsp.DCOnlyInverseWalsh4x4(y2DQ[0], dcSlots[:])
	}
	for js := 0; js < 16; js++ {
		if eob[js] == 0 && dcSlots[js*16] != 0 {
			eob[js] = 1
		}
	}
}

// emitOracleLFTrial writes a single per-trial-level row for the fast
// loop-filter picker. Each call corresponds to one libvpx-side
// calc_partial_ssl_err invocation inside vp8cx_pick_filter_level_fast,
// at one of three phases: "seed" (initial cm->filter_level scoring),
// "down" (decreasing-level loop body), "up" (increasing-level loop
// body). The libvpx-side oracle patch in
// internal/coracle/build_vpxenc_oracle.sh emits the matching row from
// govpx_oracle_emit_lf_trial after each calc_partial_ssl_err call.
func (e *VP8Encoder) emitOracleLFTrial(phase string, trialLevel int, trialYSSE int) {
	if !e.oracleTraceEnabled() {
		return
	}
	emitOracleTraceRow(e.opts.OracleTraceWriter, &oracleTraceLFTrialRow{
		Type:       "lf_trial",
		FrameIndex: e.frameCount,
		Phase:      phase,
		TrialLevel: trialLevel,
		TrialYSSE:  trialYSSE,
	})
}

// emitOracleInterPredictorTrace writes "predictor" rows for the supplied
// macroblock's Y/U/V predictor planes, encoded as ASCII hex. Emission is
// gated by EncoderOptions.OracleTracePredictorDump so the regular oracle
// trace stream stays compact; when enabled the writer receives one row per
// plane keyed by (frame_index, mb_row, mb_col, plane). Mirrors the
// libvpx-side `govpx_oracle_emit_predictor` C helper in
// internal/coracle/build_vpxenc_oracle.sh which captures
// `xd->dst.{y,u,v}_buffer` between `vp8_encode_inter16x16` and
// `vp8_inverse_transform_mby`. govpx captures the same value via
// `reconstructInterAnalysisMacroblock(MBSkipCoeff=1)` which writes only the
// predictor into the analysis image.
func (e *VP8Encoder) emitOracleInterPredictorTrace(mbRow int, mbCol int, img *vp8common.Image) {
	e.emitOracleInterMBPlanesTrace("predictor", mbRow, mbCol, img)
}

// emitOracleInterReconstructedTrace mirrors emitOracleInterPredictorTrace
// but captures the post-residual-add buffer (i.e. the final reconstructed
// MB output that becomes part of the LAST reference for the next frame).
// The libvpx-side counterpart lives at the tail of
// vp8cx_encode_inter_macroblock, after vp8_dequant_idct_add_uv_block / the
// invtrans_mby step.
func (e *VP8Encoder) emitOracleInterReconstructedTrace(mbRow int, mbCol int, img *vp8common.Image) {
	e.emitOracleInterMBPlanesTrace("reconstructed", mbRow, mbCol, img)
}

// emitOracleLastRefWindow writes "last_ref_window" rows capturing the
// LAST reference's Y/U/V planes including the border bytes the chroma
// sub-pel filter taps reach for MB row 0. Mirrors the libvpx-side
// `govpx_oracle_emit_last_ref_window` helper. Called once per inter
// frame, before the first MB is encoded, to localize whether border
// content matches between encoders.
func (e *VP8Encoder) emitOracleLastRefWindow(ref *vp8common.Image) {
	if !e.oracleTraceEnabled() || !e.opts.OracleTracePredictorDump || ref == nil {
		return
	}
	w := e.opts.OracleTraceWriter
	border := ref.YBorder
	uvBorder := ref.UVBorder
	yWindowH := border + 16
	uvWindowH := uvBorder + 8
	yWindowW := border + ref.CodedWidth
	uvWindowW := uvBorder + (ref.CodedWidth+1)>>1
	// Step back by border rows and border columns to reach top-left of
	// captured window.
	yStart := ref.YOrigin - border*ref.YStride - border
	uStart := ref.UOrigin - uvBorder*ref.UStride - uvBorder
	vStart := ref.VOrigin - uvBorder*ref.VStride - uvBorder
	if yStart < 0 || uStart < 0 || vStart < 0 {
		return
	}
	emitOracleTraceRow(w, &oracleTraceLastRefWindowRow{
		Type:       "last_ref_window",
		FrameIndex: e.frameCount,
		Plane:      "y",
		Width:      yWindowW,
		Height:     yWindowH,
		BorderTop:  border,
		BorderLeft: border,
		Hex:        oracleTraceHexEncodePlane(ref.YFull[yStart:], yWindowW, yWindowH, ref.YStride),
	})
	emitOracleTraceRow(w, &oracleTraceLastRefWindowRow{
		Type:       "last_ref_window",
		FrameIndex: e.frameCount,
		Plane:      "u",
		Width:      uvWindowW,
		Height:     uvWindowH,
		BorderTop:  uvBorder,
		BorderLeft: uvBorder,
		Hex:        oracleTraceHexEncodePlane(ref.UFull[uStart:], uvWindowW, uvWindowH, ref.UStride),
	})
	emitOracleTraceRow(w, &oracleTraceLastRefWindowRow{
		Type:       "last_ref_window",
		FrameIndex: e.frameCount,
		Plane:      "v",
		Width:      uvWindowW,
		Height:     uvWindowH,
		BorderTop:  uvBorder,
		BorderLeft: uvBorder,
		Hex:        oracleTraceHexEncodePlane(ref.VFull[vStart:], uvWindowW, uvWindowH, ref.VStride),
	})
}

func (e *VP8Encoder) emitOracleInterMBPlanesTrace(rowType string, mbRow int, mbCol int, img *vp8common.Image) {
	if !e.oracleTraceEnabled() || !e.opts.OracleTracePredictorDump || img == nil {
		return
	}
	if !e.opts.OracleTracePredictorDumpAllRows && mbRow != 0 {
		// Default scope: row 0 only (8 MBs at 128 px wide). Set
		// EncoderOptions.OracleTracePredictorDumpAllRows to capture
		// every row when tracking down a divergence beyond row 0
		// (e.g., the partial-frame loop-filter trial reads MB row
		// rows/2). The libvpx-side helper applies the same gate via
		// GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS so the captured key
		// sets line up.
		return
	}
	w := e.opts.OracleTraceWriter
	yOff := mbRow*16*img.YStride + mbCol*16
	uOff := mbRow*8*img.UStride + mbCol*8
	vOff := mbRow*8*img.VStride + mbCol*8
	emitOracleTraceRow(w, &oracleTracePredictorRow{
		Type:       rowType,
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Plane:      "y",
		Width:      16,
		Height:     16,
		Hex:        oracleTraceHexEncodePlane(img.Y[yOff:], 16, 16, img.YStride),
	})
	emitOracleTraceRow(w, &oracleTracePredictorRow{
		Type:       rowType,
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Plane:      "u",
		Width:      8,
		Height:     8,
		Hex:        oracleTraceHexEncodePlane(img.U[uOff:], 8, 8, img.UStride),
	})
	emitOracleTraceRow(w, &oracleTracePredictorRow{
		Type:       rowType,
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Plane:      "v",
		Width:      8,
		Height:     8,
		Hex:        oracleTraceHexEncodePlane(img.V[vOff:], 8, 8, img.VStride),
	})
}

// oracleTraceHexEncodePlane returns a width*height-byte ASCII-hex
// (lowercase) encoding of a plane region. Matches the C-side
// govpx_oracle_emit_plane_hex helper exactly so the resulting JSON rows
// are byte-comparable across encoders.
func oracleTraceHexEncodePlane(plane []byte, width int, height int, stride int) string {
	const hex = "0123456789abcdef"
	if width <= 0 || height <= 0 || stride <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*width*height)
	for row := 0; row < height; row++ {
		start := row * stride
		end := start + width
		if end > len(plane) {
			break
		}
		for _, b := range plane[start:end] {
			out = append(out, hex[(b>>4)&0xf], hex[b&0xf])
		}
	}
	return string(out)
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

func oracleTraceBModeName(mode vp8common.BPredictionMode) string {
	switch mode {
	case vp8common.BDCPred:
		return "B_DC_PRED"
	case vp8common.BTMPred:
		return "B_TM_PRED"
	case vp8common.BVEPred:
		return "B_VE_PRED"
	case vp8common.BHEPred:
		return "B_HE_PRED"
	case vp8common.BLDPred:
		return "B_LD_PRED"
	case vp8common.BRDPred:
		return "B_RD_PRED"
	case vp8common.BVRPred:
		return "B_VR_PRED"
	case vp8common.BVLPred:
		return "B_VL_PRED"
	case vp8common.BHDPred:
		return "B_HD_PRED"
	case vp8common.BHUPred:
		return "B_HU_PRED"
	case vp8common.Left4x4:
		return "LEFT4X4"
	case vp8common.Above4x4:
		return "ABOVE4X4"
	case vp8common.Zero4x4:
		return "ZERO4X4"
	case vp8common.New4x4:
		return "NEW4X4"
	default:
		return fmt.Sprintf("B_MODE_%d", int(mode))
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
