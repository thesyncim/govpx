//go:build govpx_oracle_trace

package govpx

// Encoder oracle trace mode (off-by-default validation harness).
//
// When oracle tracing is enabled for an encoder, the encoder emits a
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
	"hash/adler32"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// oracleTraceFrameRow is the per-frame oracle trace row.
//
// RefreshEntropyProbs mirrors libvpx's `cm->refresh_entropy_probs` after the
// `vp8_pack_bitstream` / encode loop error-resilient overrides: plain
// error-resilient mode forces refresh_entropy_probs=0, while
// VPX_ERROR_RESILIENT_PARTITIONS forces refresh_entropy_probs=1 on key frames
// and refresh_entropy_probs=0 on inter frames. govpx tracks the same
// per-attempt flag through `keyFrameEncodeAttempt.RefreshEntropyProbs` /
// `interFrameEncodeAttempt.Config.RefreshEntropyProbs`.
//
// DefaultCoefReset mirrors libvpx's "force-default coef probs/counts" gate:
// when error_resilient_mode includes VPX_ERROR_RESILIENT_PARTITIONS AND the
// frame is a key frame, libvpx resets `cm->fc.coef_probs` and `mb->coef_counts`
// to their defaults via vp8_setup_key_frame -> vp8_default_coef_probs and the
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
//	cpi_speed             -> cpi->Speed after per-frame realtime auto-select
//	avg_encode_time       -> cpi->avg_encode_time before this frame's timer commit
//	avg_pick_mode_time    -> cpi->avg_pick_mode_time before this frame's timer commit
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
	CPISpeed           int    `json:"cpi_speed"`
	AvgEncodeTime      int    `json:"avg_encode_time"`
	AvgPickModeTime    int    `json:"avg_pick_mode_time"`
	// Entropy-savings breakdown matching libvpx vp8_estimate_entropy_savings:
	// coef_savings_bits is the coefficient-prob update savings
	// (default_coef_context_savings or independent_coef_context_savings),
	// ref_frame_savings_bits is the inter-frame ref-frame probability
	// re-coding savings (zero on key frames). The pre-entropy-savings
	// projection equals projected_frame_size + coef_savings_bits +
	// ref_frame_savings_bits. Used to localize entropy-savings parity gaps
	// behind projected_frame_size (see docs/vp8_encoder_parity.md "Encode
	// Driver, Recode, And Q Bounds").
	CoefSavingsBits     int `json:"coef_savings_bits"`
	RefFrameSavingsBits int `json:"ref_frame_savings_bits"`
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

// oracleTraceRecodeIterRow is the per-recode-iteration trace row. Emitted
// once per encode pass inside the size_recode loop, AFTER the entropy-savings
// subtraction so ProjectedFrameSize matches libvpx's `cpi->projected_frame_size`
// at line 3983 of onyx_if.c. Fields capture the recode-loop state that drives
// `recode_loop_test` and the post-test q_low / q_high / Q update path:
//
//   - Iter             : 1-indexed counter (mirrors libvpx loop_count semantics)
//   - Q                : the Q just used by vp8_set_quantizer / encode pass
//   - ProjectedFrameSize : post-savings bytes (matches libvpx's
//     `cpi->projected_frame_size` after `vp8_estimate_entropy_savings`)
//   - ThisFrameTarget  : `cpi->this_frame_target` (bits)
//   - QLow / QHigh     : recode bounds at the END of this iteration (after
//     the q_low / q_high tightening for this iter)
//   - ActiveBest / ActiveWorst : the Q-range used by `vp8_regulate_q`
//     (mirrors `cpi->active_best_quality` / `cpi->active_worst_quality`)
//   - ActiveWorstQChanged : 1 if the relax-active-worst block ran this iter
//   - OvershootSeen / UndershootSeen : recode-loop state flags (see libvpx
//     `overshoot_seen` / `undershoot_seen` locals in `encode_frame_to_data_rate`)
//   - ZbinOverQuant    : `cpi->mb.zbin_over_quant` after this iter's update
//   - RateCorrectionFactor : the active rcf chosen by frame type/refresh
//     class (matches libvpx's `cpi->rate_correction_factor` /
//     `cpi->gf_rate_correction_factor` / `cpi->key_frame_rate_correction_factor`)
//   - NextQ            : Q chosen for the NEXT encode pass (after recode);
//     equals Q when the loop is about to exit (Loop=0 in libvpx terms)
//   - Recoded          : true when the loop will iterate again (libvpx Loop=1)
//   - OvershootLimit / UndershootLimit : `frame_over_shoot_limit` /
//     `frame_under_shoot_limit` (bits). Captured to localize the gate edge
//     condition when `recode_loop_test` flips state mid-loop.
type oracleTraceRecodeIterRow struct {
	Type                 string  `json:"type"`
	FrameIndex           uint64  `json:"frame_index"`
	Iter                 int     `json:"iter"`
	Q                    int     `json:"q"`
	ProjectedFrameSize   int     `json:"projected_frame_size"`
	ThisFrameTarget      int     `json:"this_frame_target"`
	QLow                 int     `json:"q_low"`
	QHigh                int     `json:"q_high"`
	ActiveBest           int     `json:"active_best"`
	ActiveWorst          int     `json:"active_worst"`
	ActiveWorstQChanged  int     `json:"active_worst_qchanged"`
	OvershootSeen        int     `json:"overshoot_seen"`
	UndershootSeen       int     `json:"undershoot_seen"`
	ZbinOverQuant        int     `json:"zbin_over_quant"`
	RateCorrectionFactor float64 `json:"rate_correction_factor"`
	NextQ                int     `json:"next_q"`
	Recoded              bool    `json:"recoded"`
	OvershootLimit       int     `json:"overshoot_limit"`
	UndershootLimit      int     `json:"undershoot_limit"`
	// Pre-savings raw frame rate (libvpx `totalrate >> 8` at the end of
	// vp8_encode_frame, encodeframe.c:941) and the per-component entropy
	// savings subtracted from it (vp8_estimate_entropy_savings). Exposed
	// at per-iter granularity so a Q-level picker divergence (the task
	// #212 frame 4 iter 6 Q=9 finding) can be split into a picker
	// raw-rate component vs a coef-prob / ref-frame entropy-savings
	// component, both at the same input Q on both sides.
	RawRate             int `json:"raw_rate"`
	CoefSavingsBits     int `json:"coef_savings_bits"`
	RefFrameSavingsBits int `json:"ref_frame_savings_bits"`
}

// oracleTraceMBIterRateRow is a per-(iter, mb_row, mb_col) trace row capturing
// the picker's chosen-mode rate at every recode iteration, not just the final
// accepted iter. Mirrors task #218: at iter 6 Q=9 on the
// regression_general_64x64_300kbps_spm8_f9_src0_0bb41d74 seed, govpx's
// per-MB picker reports raw_rate=3175 vs libvpx's raw_rate=3116 — a 59-bit
// drift accumulated across the 16 MBs of the 64x64 frame. The standard
// per-MB `mb` rows only fire for the accepted iter (govpx Q=9, libvpx Q=7
// for this seed), so the same Q=9 picker output cannot be compared
// directly. mb_iter_rate adds the missing iter dimension by emitting one
// row per MB inside the recode-loop emit hook, just before the next
// iteration's encode pass overwrites the per-MB rate slots.
//
// Mode / RefFrame / MV are captured for the picker's chosen candidate so
// the diff can localize a "different-mode-picked" gap separately from a
// "same-mode-different-rate" gap. The govpx-side emit fires from
// emitOracleRecodeIterTrace; the libvpx-side mirror loops over
// govpx_oracle_state.mb_rows[] inside govpx_oracle_recode_iter_emit.
type oracleTraceMBIterRateRow struct {
	Type           string `json:"type"`
	FrameIndex     uint64 `json:"frame_index"`
	Iter           int    `json:"iter"`
	Q              int    `json:"q"`
	MBRow          int    `json:"mb_row"`
	MBCol          int    `json:"mb_col"`
	Mode           string `json:"mode"`
	RefFrame       string `json:"ref_frame"`
	MVRow          int16  `json:"mv_row"`
	MVCol          int16  `json:"mv_col"`
	Skip           bool   `json:"skip"`
	MBRate         int    `json:"mb_rate"`
	AggregatedRate int    `json:"aggregated_rate"`
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

	// MBRate is the chosen-mode rate accumulated by the per-MB picker for
	// this MB (libvpx `rate` returned from vp8cx_encode_inter_macroblock /
	// vp8cx_encode_intra_macroblock). AggregatedRate is the running
	// `totalrate` after this MB's contribution has been added (libvpx's
	// `*totalrate` in encode_mb_row, before the final `>>8` to bits and
	// the entropy-savings subtraction). Both are emitted to localize
	// per-MB rate-aggregator drift behind the projected_frame_size scalar
	// (see docs/vp8_encoder_parity.md "Encode Driver, Recode, And Q
	// Bounds").
	MBRate         int `json:"mb_rate"`
	AggregatedRate int `json:"aggregated_rate"`

	// TuneSSIM activity-masking diagnostic. Mirrors libvpx v1.16.0
	// vp8/encoder/encodeframe.c:
	//   - MBActivity  : cpi->mb_activity_map[idx], populated by
	//     build_activity_map (line 225-289). Zero outside TuneSSIM.
	//   - ActZbinAdj  : x->act_zbin_adj from adjust_act_zbin
	//     (line 1074-1092). Zero outside TuneSSIM (line 588 base init).
	//   - RDMult      : x->rdmult after vp8_activity_masking (line 307).
	//     Equals cpi->RDMULT outside TuneSSIM (line 406 base assignment).
	//   - ActivityAvg : cpi->activity_avg from calc_av_activity (line 156
	//     / 164) at TuneSSIM. Defaults to 90<<12 from
	//     vp8_create_compressor (onyx_if.c:1906) for PSNR runs.
	// These were added in task #210 to localize the first diverging MB on
	// the residual 1280x720 SSIM seeds (94eb71d5, 19981bff) where the
	// per-MB tracer was missing the activity-masking quartet.
	MBActivity  uint32 `json:"mb_activity"`
	ActZbinAdj  int    `json:"act_zbin_adj"`
	RDMult      uint32 `json:"rdmult"`
	ActivityAvg uint32 `json:"activity_avg"`
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

// oracleTraceChromaOptimizeBRow records the POST-trellis qcoeff /
// dqcoeff / dequant / coeff snapshot of a single UV block (16..23) on
// the accepted-path encode, i.e. the state observed immediately after
// optimize_b finishes for that block. Mirrors the libvpx-side
// {"type":"chroma_optimize_b",...} row emitted by
// govpx_oracle_emit_chroma_optimize_b (oracle_trace.c, splice in
// vp8_encode_inter16x16 right after optimize_mb). Paired with the
// pre-trellis snapshot (oracleTracePretrellisUVRow) so the bisect can
// identify which scan_pos optimize_b flipped one way on libvpx and a
// different way on govpx — the cohort of ±1 DC keep/drop divergences
// pinpointed by task #314 (encoder qcoeff diff: 2241/3600 MBs on frame
// 1, 2115 chroma-only, 85% DC-only). Gated by the govpx_oracle_trace
// build tag plus GOVPX_ORACLE_CHROMA_OPTIMIZE_B=1 on both sides.
type oracleTraceChromaOptimizeBRow struct {
	Type       string    `json:"type"`
	FrameIndex uint64    `json:"frame_index"`
	MBRow      int       `json:"mb_row"`
	MBCol      int       `json:"mb_col"`
	Block      int       `json:"block"`
	EOB        int       `json:"eob"`
	RDMult     int       `json:"rdmult"`
	RDDiv      int       `json:"rddiv"`
	Intra      int       `json:"intra"`
	QCoeff     [16]int16 `json:"qcoeff"`
	DQCoeff    [16]int16 `json:"dqcoeff"`
	Dequant    [16]int16 `json:"dequant"`
	Coeff      [16]int16 `json:"coeff"`
}

// oracleTracePretrellisUVRow records the pre-trellis qcoeff / dqcoeff /
// coeff snapshot of a single UV block (16..23) on the accepted-path encode.
// Mirrors the libvpx-side {"type":"pretrellis_uv_qcoeff",...} row emitted
// by govpx_oracle_emit_pretrellis_uv (oracle_trace.c). Used to localize
// the ARNR pin-hold (task #207 / #227) after the #282-#294 static-
// inspection campaign exhausted candidate predictor / residual / quantize
// / RC drift sources; the static audits confirmed the govpx ports of
// vp8_subtract_mbuv / vp8_short_fdct8x4 / vp8_regular_quantize_b are
// byte-faithful, so the surviving -5/-6-byte gap must surface as a per-
// position qcoeff divergence at the trellis input. Gated by the
// govpx_oracle_trace build tag on both sides; emission additionally
// requires the encoder pretrellis-UV state to be enabled (mirrored as
// GOVPX_ORACLE_PRETRELLIS_UV=1 on the libvpx side).
type oracleTracePretrellisUVRow struct {
	Type       string    `json:"type"`
	FrameIndex uint64    `json:"frame_index"`
	MBRow      int       `json:"mb_row"`
	MBCol      int       `json:"mb_col"`
	Block      int       `json:"block"`
	EOB        int       `json:"eob"`
	Coeff      [16]int16 `json:"coeff"`
	QCoeff     [16]int16 `json:"qcoeff"`
	DQCoeff    [16]int16 `json:"dqcoeff"`
	ZbinExtra  int       `json:"zbin_extra"`
	ZbinOQ     int       `json:"zbin_oq"`
}

// oracleTraceLFTrialRow records a single per-trial-level evaluation inside
// the fast loop-filter picker (loopFilterPickContext.pickFast). Each row carries
// the trial filter level and the resulting partial-frame Y SSE as scored
// by loopFilterPickContext.trialLumaSSE. The libvpx-side oracle patch in
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

	ImprovedMVStart        bool
	ImprovedMVNearSADIndex int
	ImprovedMVRow          int16
	ImprovedMVCol          int16
	ImprovedMVSR           int
}

const oracleTraceInterCandidateUnknown = -1

// oracleTraceEnabled reports whether the encoder is configured to emit the
// oracle trace. Callers should guard tracing logic with this so the per-MB
// fast path performs no extra work when the harness is off.
func (e *VP8Encoder) oracleTraceEnabled() bool {
	state := e.oracleTraceState()
	return state != nil && state.writer != nil
}

// oracleTraceFrameSummary is the minimal slice of frame state that callers
// pass to emitOracleFrameTrace. It exists so the call site does not depend on
// the exact attempt struct shape. The frame row's `refresh_entropy_probs`
// and `default_coef_reset` fields are derived inside emitOracleFrameTrace
// from `summary.FrameType` and the error-resilient options, mirroring libvpx's
// `vp8_pack_bitstream` error-resilient overrides and the
// `VPX_ERROR_RESILIENT_PARTITIONS && key-frame` default-coef gate respectively,
// so callers do not need to thread those values through the summary struct.
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
	// Mirror libvpx's error-resilient refresh-entropy overrides: plain
	// error-resilient mode forces false for key and inter frames; partitions
	// mode forces true only for key frames. Outside error-resilient modes,
	// this trace covers the normal no-NoUpdateEntropy path.
	refreshEntropyProbs := true
	if e.opts.ErrorResilient || e.opts.ErrorResilientPartitions {
		refreshEntropyProbs = keyFrame && e.opts.ErrorResilientPartitions
	}
	// Mirror libvpx's default-coef gate: only the independent-partitions bit
	// takes the keyframe reset branch.
	defaultCoefReset := e.opts.ErrorResilientPartitions && keyFrame
	// Align oracle "frame" row q_index with libvpx's
	// build_vpxenc_oracle.sh emission semantics: the libvpx-side helper
	// writes `cm->base_qindex` for q_index (the per-frame Q that lands
	// in the bitstream), NOT the post-adjust
	// `cpi->active_worst_quality` that the next frame's regulator picks
	// up. govpx's e.rc.currentQuantizer reflects libvpx's post-adjust
	// value at the trace emission point, because postEncodeFrameWith-
	// PacketContext has already run adjustQuantizerWithContext between
	// the bitstream commit and this row. Emitting that instead of
	// summary.BaseQIndex masquerades as a +/-1 trace q_index divergence
	// on every frame where adjustQuantizerWithContext fires
	// (over/undershoot of the frame-size bounds), even when the
	// encoded bitstream byte-matches libvpx exactly. Diagnosed on
	// screen-content2-panning-256x144-realtime-cpu-3 where bitstream
	// frame 1 byte-matched libvpx (len=410, first_part=112) but the
	// trace row showed q_index=105 (govpx post-adjust) vs 106 (libvpx
	// base_qindex), spuriously surfacing as a Q gap in the parity
	// comparator. The actual residual divergence at frames 2+ is in
	// the per-MB picker rate sum (totalrate >> 8 diverges 2x despite
	// identical Q / reference / probability state) and is tracked
	// separately.
	row := oracleTraceFrameRow{
		Type:                 "frame",
		FrameIndex:           e.frameCount,
		QIndex:               summary.BaseQIndex,
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
	emitOracleTraceRow(e.oracleTraceState().writer, &row)
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
	for block := range 4 {
		for band := range 8 {
			for ctx := range 3 {
				for node := range 11 {
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
	for i := range 19 {
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
	CPISpeed               int
	AvgEncodeTime          int
	AvgPickModeTime        int
	// ZbinOverQuant mirrors libvpx's `cpi->mb.zbin_over_quant` at the
	// emission point. Fed from `e.rc.currentZbinOverQuant` which the
	// recode loop commits for the GF/ARF boost branch and the regular
	// size-recode branch alike.
	ZbinOverQuant int
	// Entropy-savings breakdown captured at the same point the rate row
	// is emitted (after the accepted attempt's entropy-savings subtraction
	// has been applied to ProjectedFrameSizeBits). See oracleTraceRateRow
	// for field semantics.
	CoefSavingsBits     int
	RefFrameSavingsBits int
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
		Type:                "rate",
		FrameIndex:          e.frameCount,
		QIndex:              summary.QIndex,
		ActiveWorstQ:        summary.ActiveWorstQ,
		ActiveBestQ:         summary.ActiveBestQ,
		BufferLevel:         summary.BufferLevelBits,
		TotalByteCount:      summary.TotalByteCount,
		ProjectedFrameSize:  summary.ProjectedFrameSizeBits,
		ThisFrameTarget:     summary.ThisFrameTargetBits,
		KFOverspendBits:     summary.KFOverspendBits,
		GFOverspendBits:     summary.GFOverspendBits,
		CPISpeed:            summary.CPISpeed,
		AvgEncodeTime:       summary.AvgEncodeTime,
		AvgPickModeTime:     summary.AvgPickModeTime,
		ZbinOverQuant:       summary.ZbinOverQuant,
		CoefSavingsBits:     summary.CoefSavingsBits,
		RefFrameSavingsBits: summary.RefFrameSavingsBits,
	}
	switch summary.FrameType {
	case vp8common.KeyFrame:
		row.FrameType = "key"
	default:
		row.FrameType = "inter"
	}
	emitOracleTraceRow(e.oracleTraceState().writer, &row)
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
	emitOracleTraceRow(e.oracleTraceState().writer, &row)
}

// oracleTraceRecodeIterSummary captures per-recode-iteration recode-loop
// state for parity diff against the libvpx-side per-iter emit hook. See
// oracleTraceRecodeIterRow for field semantics.
type oracleTraceRecodeIterSummary struct {
	Iter                 int
	Q                    int
	ProjectedFrameSize   int
	ThisFrameTarget      int
	QLow                 int
	QHigh                int
	ActiveBest           int
	ActiveWorst          int
	ActiveWorstQChanged  bool
	OvershootSeen        bool
	UndershootSeen       bool
	ZbinOverQuant        int
	RateCorrectionFactor float64
	NextQ                int
	Recoded              bool
	OvershootLimit       int
	UndershootLimit      int
	RawRate              int
	CoefSavingsBits      int
	RefFrameSavingsBits  int
}

// emitOracleRecodeIterTrace writes a single "recode_iter" row capturing the
// recode-loop state at the end of one encode pass. Mirrors the libvpx-side
// per-iter emit hook patched into encode_frame_to_data_rate by
// internal/coracle/build_vpxenc_oracle.sh after the recode_loop_test branch
// decision.
func (e *VP8Encoder) emitOracleRecodeIterTrace(summary oracleTraceRecodeIterSummary) {
	if !e.oracleTraceEnabled() {
		return
	}
	row := oracleTraceRecodeIterRow{
		Type:                 "recode_iter",
		FrameIndex:           e.frameCount,
		Iter:                 summary.Iter,
		Q:                    summary.Q,
		ProjectedFrameSize:   summary.ProjectedFrameSize,
		ThisFrameTarget:      summary.ThisFrameTarget,
		QLow:                 summary.QLow,
		QHigh:                summary.QHigh,
		ActiveBest:           summary.ActiveBest,
		ActiveWorst:          summary.ActiveWorst,
		ZbinOverQuant:        summary.ZbinOverQuant,
		RateCorrectionFactor: summary.RateCorrectionFactor,
		NextQ:                summary.NextQ,
		Recoded:              summary.Recoded,
		OvershootLimit:       summary.OvershootLimit,
		UndershootLimit:      summary.UndershootLimit,
		RawRate:              summary.RawRate,
		CoefSavingsBits:      summary.CoefSavingsBits,
		RefFrameSavingsBits:  summary.RefFrameSavingsBits,
	}
	if summary.ActiveWorstQChanged {
		row.ActiveWorstQChanged = 1
	}
	if summary.OvershootSeen {
		row.OvershootSeen = 1
	}
	if summary.UndershootSeen {
		row.UndershootSeen = 1
	}
	state := e.oracleTraceState()
	emitOracleTraceRow(state.writer, &row)
	// Task #218: emit per-MB rate snapshots for THIS iter from the still-live
	// mbBuffer. The buffer holds the just-completed iter's chosen-mode rate
	// per MB; the next iter's encodeInterFrameAttempt /
	// encodeKeyFrameAttempt resets it via resetOracleMBTraceBuffer. Without
	// this loop the per-MB rate is only visible for the FINAL accepted iter,
	// which can be at a different Q than the iter we want to compare on
	// (govpx exits at Q=9, libvpx at Q=7 for the 0bb41d74 frame-4 seed).
	for i := range state.mbBuffer {
		mb := &state.mbBuffer[i]
		iterRow := oracleTraceMBIterRateRow{
			Type:           "mb_iter_rate",
			FrameIndex:     mb.FrameIndex,
			Iter:           summary.Iter,
			Q:              summary.Q,
			MBRow:          mb.MBRow,
			MBCol:          mb.MBCol,
			Mode:           mb.Mode,
			RefFrame:       mb.RefFrame,
			MVRow:          mb.MVRow,
			MVCol:          mb.MVCol,
			Skip:           mb.Skip,
			MBRate:         mb.MBRate,
			AggregatedRate: mb.AggregatedRate,
		}
		emitOracleTraceRow(state.writer, &iterRow)
	}
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
	emitOracleTraceRow(e.oracleTraceState().writer, &row)
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
func (e *VP8Encoder) emitOracleRateAndRecodeTrace(frameType vp8common.FrameType, finalQuantizer int, sizeBytes int, projectedBits int, coefSavings int, refFrameSavings int) {
	if !e.oracleTraceEnabled() {
		return
	}
	state := e.oracleTraceState()
	keyFrame := frameType == vp8common.KeyFrame
	activeBest, activeWorst := e.rc.libvpxActiveQuantizerBounds(keyFrame, false)
	e.emitOracleRateTrace(oracleTraceRateSummary{
		FrameType:              frameType,
		QIndex:                 finalQuantizer,
		ActiveWorstQ:           activeWorst,
		ActiveBestQ:            activeBest,
		BufferLevelBits:        int64(e.rc.bufferLevelBits),
		TotalByteCount:         state.totalByteCount,
		ProjectedFrameSizeBits: projectedBits,
		ThisFrameTargetBits:    e.rc.frameTargetBits,
		KFOverspendBits:        e.rc.kfOverspendBits,
		GFOverspendBits:        e.rc.gfOverspendBits,
		CPISpeed:               e.libvpxCPUUsed(),
		AvgEncodeTime:          e.avgEncodeTime,
		AvgPickModeTime:        e.avgPickModeTime,
		ZbinOverQuant:          e.rc.currentZbinOverQuant,
		CoefSavingsBits:        coefSavings,
		RefFrameSavingsBits:    refFrameSavings,
	})
	if state.recodeLoopCount > 1 {
		reason := state.recodeReason
		if reason == "" {
			reason = "size_recode"
		}
		e.emitOracleRecodeTrace(oracleTraceRecodeSummary{
			LoopCount: state.recodeLoopCount,
			FinalQ:    finalQuantizer,
			Reason:    reason,
		})
	}
	// Mirror libvpx's "cpi->total_byte_count += projected_bytes" which runs
	// after pack_bitstream. The trace row already reflects the pre-frame
	// total so the next frame's rate row sees the same cumulative value
	// libvpx would.
	state.totalByteCount += int64(sizeBytes)
}

// oracleTracePretrellisUVDumpEnabled reports whether the encoder is
// configured to emit per-UV-block pre-trellis qcoeff rows on the accepted
// path. Used by the per-MB UV quantize loop to skip the duplicate-quantize
// + emit work when the harness is off.
func (e *VP8Encoder) oracleTracePretrellisUVDumpEnabled() bool {
	state := e.oracleTraceState()
	return state != nil && state.writer != nil && state.pretrellisUVDump
}

// oracleTraceChromaOptimizeBDumpEnabled reports whether the encoder is
// configured to emit per-UV-block POST-trellis qcoeff rows on the
// accepted path. Mirrors GOVPX_ORACLE_CHROMA_OPTIMIZE_B on the libvpx
// side; used to skip the per-block emit work when the harness is off.
func (e *VP8Encoder) oracleTraceChromaOptimizeBDumpEnabled() bool {
	state := e.oracleTraceState()
	return state != nil && state.writer != nil && state.chromaOptimizeBDump
}

// emitOracleChromaOptimizeBTrace writes a single
// {"type":"chroma_optimize_b",...} row for one UV block (16..23) on the
// accepted-path encode. The caller must pass the post-trellis qcoeff /
// dqcoeff snapshot taken immediately after
// optimizeQuantizedBlockWithRDConstants finishes for that block (matching
// the libvpx splice site, vp8_encode_inter16x16 right after optimize_mb).
// rdmult/rddiv mirror x->rdmult / x->rddiv at the optimize_b entry; intra
// is 1 when the MB picker picked an intra mode for this UV block. The
// row's primary purpose is to surface (block, scan_pos) where libvpx
// drops a ±1 qcoeff and govpx keeps it (or vice versa), localizing the
// task #314 chroma trellis divergence to specific Viterbi positions.
func (e *VP8Encoder) emitOracleChromaOptimizeBTrace(mbRow int, mbCol int, block int, coeff *[16]int16, qcoeff *[16]int16, dqcoeff *[16]int16, dequant *[16]int16, eob int, rdMult int, rdDiv int, intra bool) {
	if !e.oracleTraceChromaOptimizeBDumpEnabled() {
		return
	}
	if coeff == nil || qcoeff == nil || dqcoeff == nil || dequant == nil {
		return
	}
	intraFlag := 0
	if intra {
		intraFlag = 1
	}
	row := oracleTraceChromaOptimizeBRow{
		Type:       "chroma_optimize_b",
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Block:      block,
		EOB:        eob,
		RDMult:     rdMult,
		RDDiv:      rdDiv,
		Intra:      intraFlag,
		QCoeff:     *qcoeff,
		DQCoeff:    *dqcoeff,
		Dequant:    *dequant,
		Coeff:      *coeff,
	}
	emitOracleTraceRow(e.oracleTraceState().writer, &row)
}

// emitOraclePretrellisUVTrace writes a single
// {"type":"pretrellis_uv_qcoeff",...} row for one UV block (16..23) on
// the accepted-path encode. The caller is responsible for passing the
// pre-trellis qcoeff/dqcoeff snapshot taken between
// quantizeBlockWithZbinAndActivity and optimizeQuantizedBlockWithRDConstants
// (mirroring the libvpx call site between vp8_quantize_mb and optimize_mb
// inside vp8_encode_inter16x16). zbinExtra is the per-block zbin-extra used
// by the regular quantizer (it changes across MBs via vp8_update_zbin_extra
// when zbin_mode_boost_enabled is true, and via vp8cx_mb_init_quantizer on
// segment-id transitions); zbinOQ is x->zbin_over_quant (the per-frame zbin
// over-quant raised by the RC's zbin_over_quant adjustment). Both numerics
// surface alongside the qcoeff payload so a divergence in the zbin path
// shows up in the trace before the qcoeff diff is interpreted.
func (e *VP8Encoder) emitOraclePretrellisUVTrace(mbRow int, mbCol int, block int, coeff *[16]int16, qcoeff *[16]int16, dqcoeff *[16]int16, eob int, zbinExtra int, zbinOQ int) {
	if !e.oracleTracePretrellisUVDumpEnabled() {
		return
	}
	if coeff == nil || qcoeff == nil || dqcoeff == nil {
		return
	}
	row := oracleTracePretrellisUVRow{
		Type:       "pretrellis_uv_qcoeff",
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Block:      block,
		EOB:        eob,
		Coeff:      *coeff,
		QCoeff:     *qcoeff,
		DQCoeff:    *dqcoeff,
		ZbinExtra:  zbinExtra,
		ZbinOQ:     zbinOQ,
	}
	emitOracleTraceRow(e.oracleTraceState().writer, &row)
}
