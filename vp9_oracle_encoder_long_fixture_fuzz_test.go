//go:build govpx_oracle_trace

package govpx_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strconv"
	"testing"
)

// vp9LongFixtureParityGapSeeds lists VP9 fuzz-corpus seed payloads whose strict
// byte parity is gated behind libvpx VP9 features govpx has not yet ported.
// Each entry cites the libvpx file:line that drives the divergence so the
// corresponding port has a concrete starting point.
//
// The CBR keyframe-target gap (vp9_calc_iframe_target_size_one_pass_cbr @
// vp9_ratectrl.c:2205-2231) was closed by d248324; with that port in place
// the seeds below now match frame 0 in seed#0 (CBR 300kbps kf=999 cpu=8),
// but frame 1 still diverges on every seed. The remaining gaps are
// structural encoder features (compressed-header writer, interp-filter
// selection, and the full-RD encode path that cpu_used!=8 selects —
// use_nonrd_pick_mode==0, see per-seed notes) rather than rate-control drift
// or speed-feature flag divergence (the cpu0/3/4 REALTIME SPEED_FEATURES are
// already ported verbatim; see TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim), so
// the matched-prefix>240/256 target requires substantial encoder work that
// no longer maps to the VP8 gap A+B "AWQ drift" pattern.
//
// Deferred seeds:
//
//   - {0,0,0,0,0} (+ {} and {0x30} aliases) — CBR 300kbps kf=999
//     realtime cpu8. CLOSED: now byte-exact for all 256 frames. The final
//     divergence at frame 86 block (2,4) — govpx picked
//     GOLDEN+NEARESTMV(36,264) where libvpx picked LAST+NEARESTMV(36,264)
//     — was an out-of-order pred_mv_sad[LAST] refresh in the nonrd
//     inter picker. libvpx (vp9_pickmode.c:2284-2293) recomputes
//     x->pred_mv_sad[LAST_FRAME] from the NEWMV-winner SAD BEFORE the
//     duplicate-NEARESTMV `continue` (vp9_pickmode.c:2296-2299); when the
//     LAST/NEWMV search resolves to the NEARESTMV MV the candidate is
//     pruned as a duplicate but the refreshed SAD is already committed.
//     govpx performed that update AFTER the duplicate-NEAREST continue, so
//     for this block the LAST/NEWMV candidate (mv 36,264 == NEARESTMV) was
//     pruned first and pred_mv_sad[LAST] stayed at the stale vp9_mv_pred
//     scan value (3211) instead of the NEWMV value (2821). The GOLDEN
//     reference_masking 2x gate (vp9_pickmode.c:2212, pred_mv_sad[GOLDEN]
//     5891 > pred_mv_sad[LAST] << 1) therefore evaluated 5891 > 6422 (false,
//     GOLDEN kept) instead of libvpx's 5891 > 5642 (true, GOLDEN pruned),
//     leaving GOLDEN NEAREST/NEAR/NEW in the candidate set where govpx's
//     GOLDEN NEARESTMV then won a model_rd near-tie (42241271 vs LAST
//     42275307). Fixed by moving the pred_mv_sad[LAST] refresh ahead of the
//     duplicate-NEARESTMV check in pickVP9InterReferenceModeNonRD
//     (vp9_pick_inter_mode_nonrd.go), matching libvpx's ordering verbatim.
//     The earlier border-extension / model_rd var/sse lead was a red
//     herring: every candidate BOTH encoders evaluate has byte-identical
//     var/sse — the divergence was purely which GOLDEN candidates survive
//     reference_masking. NOTE: shared nonrd-picker code; verified zero
//     regression on the VP9 encoder + decoder oracle suites and that
//     {1,0,0,0,0} stays 256/256.
//
//   - {0,1,1,0,1} — CBR 700kbps kf=30 realtime cpu4. speed 4 sets
//     partition_search_type = VAR_BASED_PARTITION but keeps use_nonrd_pick_mode
//     == 0, so the superblock mode/coefficient decision runs the full-RD path
//     (vp9_rd_pick_inter_mode_sb / vp9_rd_pick_intra_mode_sb). PROGRESS: the
//     keyframe (frame 0) is byte-exact
//     (TestVP9EncoderVpxencOracleFullRDCPU4KeyframeByteParity), and frame 1 (the
//     first inter frame) is now ALSO byte-exact on the deep full-RD use-partition
//     path — the first byte-exact full-RD inter frame — closing the seed's
//     matched-frame prefix to 31 (frames 0..30, including the second keyframe). Pinned by
//     TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1ByteParity through the
//     production-default VAR_BASED use-partition stack. The seed stays deferred
//     because the full clip still diverges after that prefix.
//
//   - {1,0,0,0,0} — VBR 300kbps kf=999 realtime cpu8. CLOSED: now byte-exact
//     for all 256 frames. The final divergence at frame 40 (the first
//     golden-refresh after current_video_frame > 30, refresh_frame_flags 0x3:
//     govpx BaseQindex=94/FilterLevel=12 vs libvpx 120/16) was a missing
//     one-pass-VBR per-golden-group recompute. govpx held af_ratio=10 /
//     baseline_gf_interval=10 / gfu_boost=DEFAULT_GF_BOOST(2000) for every
//     golden group, but libvpx vp9_set_gf_update_one_pass_vbr
//     (vp9_ratectrl.c:2077-2127) — run at every frame begin inside
//     vp9_rc_get_one_pass_vbr_params — damps gfu_boost and af_ratio once
//     current_video_frame > 30: with avg_frame_low_motion≈34, gfu_boost =
//     VPXMAX(500, 2000*(lm<<1)/(lm+100)) and af_ratio = VPXMIN(15, VPXMAX(5,
//     3*gfu_boost/400)), which drops the boosted golden target
//     (avg_frame_bandwidth*gi*af/(gi+af-1)) and lifts the regulated q. Ported
//     verbatim: setGFUpdateOnePassVBR recomputes the GF interval / af_ratio /
//     gfu_boost and re-seeds frames_till_gf_update_due at frame begin (with the
//     rolling_{target,actual}_bits EMA monitors, vp9_ratectrl.c:392-393,
//     1931-1934, feeding rate_err), the golden countdown decrement now lives in
//     update_golden_frame_stats (vp9_ratectrl.c:1759-1784), and the GF
//     active-best now indexes get_gf_active_quality by the live gfu_boost
//     (vp9_ratectrl.c:906-919) instead of the 2000 default.
//
//   - {1,1,1,1,0} — VBR 700kbps kf=30 good-quality cpu8. Same compressed-
//     header gap as the previous seed plus the GoodQuality speed-features
//     path. The interp_filter SWITCHABLE handshake matches libvpx now
//     (cpi->sf.default_interp_filter @ vp9_speed_features.c:1008), but the
//     compressed-header divergence still defers the seed.
//
//   - {0,2,0,0,2} — CBR 1200kbps kf=999 realtime cpu0. The cpu_used=0 REALTIME
//     speed-feature flags are pinned verbatim by
//     TestVP9SetRtSpeedFeaturesCPUUsed0Verbatim (none of the speed>=N cascades
//     fire; the SF is the best-quality defaults union the RT baseline at
//     vp9_speed_features.c:458-483), so the gap is NOT the speed-feature
//     configurator. At speed 0 use_nonrd_pick_mode == 0 and
//     partition_search_type == SEARCH_PARTITION, i.e. the full-RD square
//     partition + RD mode/coefficient decision path
//     (vp9_encodeframe.c rd_pick_partition, vp9_rd_pick_intra_mode_sb /
//     vp9_rd_pick_inter_mode_sb).
//
//     SCOUT 2026-06-04 (main 4df8dab8): the KEYFRAME (frame 0) is now
//     BYTE-EXACT for this seed's exact config (incl. --timebase=1/30):
//     govpx vs vpxenc-vp9 both 3345 bytes, FirstByteDiff == -1, with
//     BaseQindex=16, FirstPartitionSize=104, FilterLevel=12 all matching;
//     pinned by TestVP9EncoderVpxencOracleFullRDCPU0KeyframeByteParity. The
//     prior "frame 0 byte mismatch at offset 27" note is STALE — the full-RD
//     keyframe intra + tx + coef RD path was closed by the cost-primitive
//     sweep. The FIRST divergence is now FRAME 1, the first INTER frame:
//     frame 1 govpx q=145 fps(FirstPartitionSize)=5  FilterLevel=20
//     frame 1 libvpx q=145 fps(FirstPartitionSize)=31 FilterLevel=14
//     BaseQindex matches (rate control through frame-1 q-selection is correct);
//     the uncompressed-header TxMode=TX_MODE_SELECT(4), ReferenceMode=
//     SINGLE_REFERENCE(0), InterpFilter=SWITCHABLE(4) and allow_hp all match.
//     The first BYTE divergence is uncompressed-header byte 4 (the FilterLevel
//     field: govpx 0xf1 vs libvpx 0xf0), and the FirstPartitionSize bytes 8-9
//     also differ (govpx 00 14 vs libvpx 00 7c). Both FilterLevel and
//     FirstPartitionSize are DERIVED quantities: FilterLevel from
//     vp9_pick_filter_level over the reconstructed frame, FirstPartitionSize
//     from the compressed-header probability deltas (derived from per-block
//     mode/mv/coef counts). govpx's tiny FirstPartitionSize=5 vs libvpx's 31
//     means govpx codes frame 1 as almost all SKIP / zero-residual blocks
//     while libvpx codes real residual — i.e. the per-block full-RD INTER
//     mode/MV decisions at frame 1, SB0, block (0,0) diverge upstream.
//
//     This is NOT a keyframe issue and NOT a small self-contained fix. It is
//     the all-or-nothing-per-frame full-RD INTER engine
//     (vp9_rdopt.c vp9_rd_pick_inter_mode_sb + single_motion_search:2673 +
//     full_pixel_diamond @ vp9_mcomp.c:2487 + vp9_get_mvpred_var variance
//     re-scoring @ :1454, then filter/tx/coef RD accumulation feeding
//     get_interp_filter @ vp9_encodeframe.c:5846 and the prob deltas). See the
//     vp9-fullrd-inter-bisection memory note: the verified prerequisite fixes
//     are step_param=0 for full-RD (vp9_encoder.c:3728 not called on the
//     no-recode RT path) + the variance-rescoring diamond port; these must all
//     land together. Closing this seed requires porting the COMPLETE full-RD
//     inter pipeline to bit-exactness as one effort, beyond a single-fix agent.
//
// Reverting any entry here must be paired with the corresponding direct libvpx
// port.
var vp9LongFixtureParityGapSeeds = [][]byte{
	// {0,0,0,0,0} (CBR 300kbps kf=999 realtime cpu8), together with the
	// Go-fuzz built-in nil seed {} and the persisted corpus alias {0x30}
	// (regression_cbr_300kbps_kf999_panning_defbuf_rt_cpum3_582528dd) that
	// materialise the identical case through the ByteCursor bucket
	// selection, are now CLOSED (byte-exact for all 256 frames) by the
	// nonrd pred_mv_sad[LAST] ordering fix documented above; they are no
	// longer deferred.
	{0, 1, 1, 0, 1},
	{1, 1, 1, 1, 0},
	{0, 2, 0, 0, 2},
}

func vp9LongFixtureParityGapSeed(data []byte) bool {
	for _, seed := range vp9LongFixtureParityGapSeeds {
		if bytes.Equal(data, seed) {
			return true
		}
	}
	return false
}

// FuzzVP9EncoderLongFixtureRateControl mirrors FuzzEncoderLongFixtureRateControl
// for VP9: a long synthetic clip (≥ 256 frames) is encoded under fuzz-driven
// CBR / VBR configurations and the per-frame matched-prefix length is tallied.
// Strict byte parity is asserted; seeds that hit a cumulative VP9 RC drift gap
// fail visibly here and land as testdata/fuzz seeds for parity work.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary.
func FuzzVP9EncoderLongFixtureRateControl(f *testing.F) {
	vp9test.RequireOracle(f, "VP9 long-fixture RC fuzz")
	vp9test.RequireVpxenc(f)
	// Each seed is (rcBucket, bitrateBucket, kfBucket, deadlineBucket, cpuBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0}, // CBR 300kbps kf=999 realtime cpu8
		{0, 1, 1, 0, 1}, // CBR 700kbps kf=30
		{1, 0, 0, 0, 0}, // VBR 300kbps kf=999
		{1, 1, 1, 1, 0}, // VBR 700kbps kf=30 good
		{0, 2, 0, 0, 2}, // CBR 1200kbps cpu0
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if vp9LongFixtureParityGapSeed(data) {
			t.Skip("seed tracks a known VP9 parity gap; see vp9LongFixtureParityGapSeeds")
		}
		cfg := newVP9LongFixtureFuzzCase(data)
		opts := cfg.buildOpts()
		sources := cfg.buildSources()

		sum := sha256.Sum256(data)
		label := "fuzz-vp9-long-rc-" + hex.EncodeToString(sum[:4])
		t.Logf("%s rc=%v kbps=%d kf=%d cpu=%d frames=%d",
			label, opts.RateControlMode, cfg.targetKbps, cfg.kfInterval, cfg.cpuUsed, len(sources))

		govpxFrames := vp9oracle.EncodeFramesWithGovpx(t, opts, sources, nil)
		libvpxFrames := vp9test.VpxencPackets(t, sources, cfg.extraArgs...)

		prefix := testutil.MatchedFramePrefixLength(govpxFrames, libvpxFrames)
		t.Logf("%s matched-prefix=%d/%d frames", label, prefix, min(len(govpxFrames), len(libvpxFrames)))
		vp9test.AssertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9LongFixtureFuzzCase struct {
	width      int
	height     int
	frames     int
	targetKbps int
	kfInterval int
	rcMode     govpx.RateControlMode
	deadline   govpx.Deadline
	cpuUsed    int
	extraArgs  []string
}

func newVP9LongFixtureFuzzCase(data []byte) vp9LongFixtureFuzzCase {
	r := testutil.NewByteCursor(data)
	rcPool := [...]govpx.RateControlMode{govpx.RateControlCBR, govpx.RateControlVBR}
	kbpsPool := [...]int{300, 700, 1200}
	kfPool := [...]int{999, 30, 60}
	deadlinePool := [...]govpx.Deadline{govpx.DeadlineRealtime, govpx.DeadlineGoodQuality}
	cpuPool := [...]int{8, 4, 0}

	c := vp9LongFixtureFuzzCase{
		width:      64,
		height:     64,
		frames:     256,
		rcMode:     rcPool[r.Pick(len(rcPool))],
		targetKbps: kbpsPool[r.Pick(len(kbpsPool))],
		kfInterval: kfPool[r.Pick(len(kfPool))],
		deadline:   deadlinePool[r.Pick(len(deadlinePool))],
		cpuUsed:    cpuPool[r.Pick(len(cpuPool))],
	}
	endUsage := "cbr"
	if c.rcMode == govpx.RateControlVBR {
		endUsage = "vbr"
	}
	// Align oracle buffer + drop-frame knobs with the govpx-side opts
	// (BufferSizeMs 600 / 400 / 500, drop-frame disabled). vpxenc-vp9
	// defaults to 6000 / 4000 / 5000 ms which feeds a divergent
	// active_worst_quality through calc_active_worst_quality_one_pass_cbr
	// already at the very first keyframe, so frame 0 diverges before any
	// drift can accumulate. Match the working
	// TestVP9EncoderVpxencOracleCBRKeyframeByteParity argument set.
	c.extraArgs = []string{
		"--end-usage=" + endUsage,
		"--target-bitrate=" + strconv.Itoa(c.targetKbps),
		"--cpu-used=" + strconv.Itoa(c.cpuUsed),
		"--kf-min-dist=0",
		"--kf-max-dist=" + strconv.Itoa(c.kfInterval),
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		// Pin vpxenc-vp9's encoder timebase to 1/30 so libvpx derives
		// cpi->framerate == 30 exactly. Without this the binary keeps its
		// default 1/1000 (millisecond) output timebase: --fps=30/1 then
		// quantizes the per-frame duration to 33 ms, so vp9_new_framerate
		// (vp9_encoder.c:5774, 10000000.0/this_duration) sees framerate
		// 1000/33 = 30.303 and vp9_rc_update_framerate (vp9_ratectrl.c:2655,
		// round(target_bandwidth/framerate)) rounds avg_frame_bandwidth to
		// 9900 instead of 10000. govpx (FPS=30) correctly uses 10000, so the
		// 1-bit-per-frame target gap (e.g. CBR 300 kbps: libvpx 8663 vs govpx
		// 8750) accumulates through the CBR quantizer feedback and first
		// flips a base_qindex at frame 12 of seed {0,0,0,0,0}. The shared
		// vp9oracle.CBRArgs helper already pins this via --exact-fps-timebase
		// for the dedicated frame-flags driver; the long-fixture fuzz drives
		// stock vpxenc-vp9, whose equivalent knob is --timebase.
		"--timebase=1/30",
	}
	if c.deadline == govpx.DeadlineGoodQuality {
		// vpxenc-vp9 defaults to --rt; override only for good-quality.
		c.extraArgs = append(c.extraArgs, "--good")
	}
	return c
}

func (c *vp9LongFixtureFuzzCase) buildOpts() govpx.VP9EncoderOptions {
	return govpx.VP9EncoderOptions{
		Width:               c.width,
		Height:              c.height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     c.rcMode,
		TargetBitrateKbps:   c.targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: c.kfInterval,
		Deadline:            c.deadline,
		CpuUsed:             int8(c.cpuUsed),
	}
}

func (c *vp9LongFixtureFuzzCase) buildSources() []*image.YCbCr {
	return vp9test.NewPanningSources(c.width, c.height, c.frames)
}
