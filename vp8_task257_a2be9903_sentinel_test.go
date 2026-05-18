//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"
)

// TestVP8Task257A2be9903Sentinel pins
// FuzzEncoderProductionStreamByteParity/fuzz-option-grid-w128h128-a2be9903
// as a closed sentinel.
//
// The internal fuzz label `fuzz-option-grid-w128h128-a2be9903` derives from
// the SHA-256 prefix of the fuzz seed bytes `1120000020`, persisted as the
// corpus file
// testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_option_grid_a4ba465f
// (the `a4ba465f` suffix is Go's fuzz framework's corpus-file content hash
// over the persisted seed line, distinct from the per-iteration SHA-256 of
// the raw seed bytes that yields the internal label).
//
// SEED PARAMETERS (decoded via newOptionGridFuzzCase):
//
//	w=128 h=128 deadline=GoodQuality cpu=0 threads=0 rc=VBR
//	tune=SSIM sc=0 er=true token_parts=0 arnr=0/0/0 frames=6
//
// HISTORICAL DIVERGENCE (pre-#201, captured by task #194's 90s round-2 sweep
// at commit e12867e8):
//
//	frame 0 (keyframe):  byte MATCH
//	frame 1:             byte MATCH
//	frame 2:             byte MATCH
//	frame 3:             first_token byte 89 diverges
//	                     (len + first_partition_size MATCH)
//	frames 4-5:          cascading mismatch
//
// Task #237 flagged the seed as failing on both pre- and post-#237
// origin/main (out of #237's scope) under the internal label
// `a2be9903`. Bisecting the post-#194 main history shows the cohort
// was actually closed by task #201 (commit 2accbaaa) — "vp8: rebuild
// SSIM activity_map per recode attempt (tasks #183/#201)" — which lands
// the same root cause already documented for the sibling
// regression_option_grid_75578e9f (a 160x96 SSIM seed) in
// TestVP8Byte58Frame2DivergenceAudit. The #237 agent saw the failure on
// their local main snapshot because the libvpx oracle binary in their
// build tree predated #201's fix carrying through to the
// vpxenc-oracle build script (cf. task #259's analogous stale-oracle
// note for regression_general_e5f453c6).
//
// ROOT CAUSE (closed by task #201 / commit 2accbaaa):
//
// libvpx vp8/encoder/encodeframe.c:721-732 rebuilds the per-MB
// activity_map inside every vp8_encode_frame call, and the recode
// loop at onyx_if.c:3962-3968 reruns vp8_encode_frame per attempt.
// Each recoded Q therefore observes a fresh activity_map (and fresh
// per-MB act_zbin_adj values that feed ZBIN_EXTRA_UV at
// vp8_quantize.c:281). govpx previously built the activity_map ONCE
// in encoder_frame.go before the recode loop, reusing the stale map
// across attempts; on the SSIM-tuned (good cpu0 TuneSSIM) cohorts that
// recode, the stale map shifted UV act_zbin_adj just enough to tip a
// single UV coefficient across the ZBIN boundary, cascading into the
// (b=2,band=6,ctx=2) UV coef-prob update slot and propagating into
// every subsequent frame's entropy state.
//
// Fix (encoder_attempts.go, commit 2accbaaa): invoke
// prepareTuningActivityMap at the top of each recode attempt
// (attempt > 0 && Tuning==TuneSSIM) in both
// encodeKeyFrameWithQuantizerFeedback and
// encodeInterFrameWithQuantizerFeedback. The pre-loop call in
// encoder_frame.go still seeds the first attempt; the new in-loop call
// covers all subsequent attempts, matching libvpx's per-
// vp8_encode_frame cadence.
//
// CURRENT STATE (post-#201, replayed in this worktree at task #257):
//
//	frame 0 (keyframe):  len=11797 first_part=922  byte MATCH
//	frame 1:             len=2027  first_part=291  byte MATCH
//	frame 2:             len=1314  first_part=280  byte MATCH
//	frame 3:             len=2341  first_part=279  byte MATCH
//	frame 4:             len=1295  first_part=271  byte MATCH
//	frame 5:             len=2445  first_part=290  byte MATCH
//
// All 6 frames byte-MATCH the libvpx oracle. The live byte-parity
// regression for this seed is the corpus file
// testdata/fuzz/FuzzEncoderProductionStreamByteParity/regression_option_grid_a4ba465f
// which runs under FuzzEncoderProductionStreamByteParity as an
// ordinary go-test subtest. This sentinel records the label/corpus
// mapping and the closure context so a future audit hitting the
// `a2be9903` label name knows where to look.
//
// References:
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:721-732   build_activity_map
//     gate (per vp8_encode_frame)
//   - libvpx v1.16.0 vp8/encoder/onyx_if.c:3962-3968      recode loop calling
//     vp8_encode_frame per attempt
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1105-1108  intra adjust_act_zbin
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1191-1194  inter adjust_act_zbin
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:276-289   ZBIN_EXTRA_* macros
//   - govpx encoder_attempts.go (attempt > 0 SSIM-recode activity_map
//     rebuild — closed by commit 2accbaaa)
//   - govpx encoder_tuning.go:47-97                       prepareTuningActivityMap
//   - task #183 (commit e12867e8)                         regression_option_grid_75578e9f
//     capture (sibling 160x96 SSIM seed sharing root cause)
//   - task #194 (commit e12867e8)                         a4ba465f seed capture
//   - task #201 (commit 2accbaaa)                         per-recode activity_map fix
//   - task #237 (commit 554eb64b)                         flagged a2be9903 as out-of-scope
//   - task #259 (commit a00f4949)                         analogous closed-sentinel pin
//     for regression_general_e5f453c6 (runtime-control transitions cohort)
//
// Test is documentation-only: the live byte-parity assertion is
// already covered by the FuzzEncoderProductionStreamByteParity corpus
// run on `regression_option_grid_a4ba465f`. Skipping here keeps CI
// cost zero while making the closure trail discoverable from a future
// `a2be9903`-flavoured grep.
func TestVP8Task257A2be9903Sentinel(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	t.Skip("documentation-only; live byte-parity regression covered by FuzzEncoderProductionStreamByteParity/regression_option_grid_a4ba465f (seed bytes \"1120000020\" → internal label a2be9903), closed by task #201 (commit 2accbaaa)")
}
