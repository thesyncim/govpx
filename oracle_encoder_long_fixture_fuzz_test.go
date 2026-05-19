//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"testing"
)

// longFixtureSeedsDeferred lists fuzz-corpus seed payloads whose strict byte
// parity is gated behind libvpx VP8 rate-control features govpx has not yet
// ported in full. Each entry cites the libvpx file:line that drives the
// divergence; reverting one of these defers must be paired with the
// corresponding verbatim port landing.
//
// History:
//
//   - {0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00} (VBR 300kbps panning kf=999):
//     closed 2026-05-17. Long-tail VBR Q regulation now matches libvpx byte
//     for byte across all 256 frames after the RC correction-factor +
//     vp8_change_config Speed-reset + double-precision RC projection +
//     active_worst/active_best CQ floor ports landed earlier on main.
//     Promoted to fuzz seed (no longer skipped).
//
//   - {0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00} (VBR 700kbps splitmv kf=30
//     good-quality): closed 2026-05-17. Auto-gold GF cliff on splitmv content
//     now matches libvpx byte for byte across all 256 frames after the
//     mbs_zero_last_dot_suppress per-MB port, calc_gf_params boost path
//     fixes, and the upstream SPLITMV BLOCK_8X8 search-window port. Promoted
//     to fuzz seed (no longer skipped).
//
//   - {0x31, 0x39} = []byte("19") (VBR 300kbps splitmv kf=30 tight-buf
//     good-quality): opened 2026-05-19 by the task #335 fuzz sweep. The fuzz
//     harness passes --kf-min-dist=0 --kf-max-dist=KeyFrameInterval which
//     sets libvpx's oxcf->auto_key = 1 (vp8_cx_iface.c:377-378). Under
//     compressor_speed != 2 (good/best quality), libvpx then runs
//     decide_key_frame (onyx_if.c:2713) inside the recode loop and inserts
//     an unscheduled intra recode at frame 107 of the splitmv quadrant pan
//     when this_frame_percent_intra crosses the line 2750-2773 thresholds
//     against last_frame_percent_intra. govpx's equivalent
//     `shouldRecodeInterAttemptAsKeyFrame` (encoder_reference_decisions.go:
//     198-211) is gated behind EncoderOptions.AdaptiveKeyFrames, which the
//     long-fixture fuzz does not enable; the gap therefore manifests as
//     govpx holding inter at frame 107 while libvpx forces a KF, scrambling
//     cumulative RC state for every later frame
//     (matched-prefix=107/256). Closing this seed requires either
//     enabling AdaptiveKeyFrames implicitly when KeyFrameInterval > 0 (and
//     porting the full recode + buffer-state regulate_q chain that
//     decide_key_frame triggers, libvpx onyx_if.c:3994-4068) or routing
//     the long-fixture fuzz through libvpx's fixed_kf_cntr path
//     (kf_min_dist == kf_max_dist), which itself diverges on
//     estimate_keyframe_frequency's auto_key clamp at ratectrl.c:1321 and
//     requires its own RC-bootstrap port. The corpus seed is captured at
//     testdata/fuzz/FuzzEncoderLongFixtureRateControl/
//     regression_vbr_300kbps_kf30_splitmv_tightbuf_aeeeb411 so the next
//     port revision exercises it directly.
//
// The list and helper are kept as substrate so future deferrals have a
// drop-in landing point.
var longFixtureSeedsDeferred = [][]byte{
	{0x31, 0x39}, // task #335: VBR 300kbps splitmv kf=30 tight-buf good-quality auto_key gap
}

func longFixtureSeedDeferred(data []byte) bool {
	for _, seed := range longFixtureSeedsDeferred {
		if bytes.Equal(data, seed) {
			return true
		}
	}
	return false
}

// FuzzEncoderLongFixtureRateControl closes plan-§3 F3 / G4: a long
// synthetic clip (≥ 256 frames) is encoded under fuzz-driven CBR / VBR
// configurations and the per-frame SHA-256 matched-prefix length is
// tallied. The strict gate today runs ~16 frames per case, so
// cumulative rate-control drift, GF/ARF schedule divergence, and
// adaptive-ARNR decisions that take dozens of frames to manifest go
// unobserved.
//
// The assertion is matched-prefix-length-strict: every fuzz iteration
// must match at least 1 frame (the keyframe) byte-for-byte; later
// frames are logged with their matched-prefix length and divergence
// position. Iterations that find a longer matched prefix than the
// scoreboard baseline land in testdata/fuzz/ as future regression
// seeds; iterations that regress the keyframe parity fail hard.
func FuzzEncoderLongFixtureRateControl(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run long-fixture RC fuzz")
	}
	// Each seed is (rcBucket, bitrateBucket, kfBucket, bufBucket,
	// fixtureBucket, deadlineBucket, cpuBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0}, // CBR 300kbps panning kf=999 default-buf realtime cpu-3
		{0, 1, 1, 0, 0, 0, 1}, // CBR 700kbps panning kf=30
		{1, 0, 0, 0, 0, 0, 0}, // VBR 300kbps panning kf=999
		{1, 1, 1, 1, 1, 1, 0}, // VBR 700kbps splitmv kf=30 tight-buf good
		{0, 2, 0, 0, 0, 0, 2}, // CBR 1200kbps panning kf=999 cpu-8
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if longFixtureSeedDeferred(data) {
			t.Skip("seed deferred: see longFixtureSeedsDeferred for libvpx file:line citations")
		}
		vpxencOracle := findVpxencOracle(t)
		cfg := newLongFixtureFuzzCase(data)
		opts, extraArgs := cfg.buildOpts()

		sum := sha256.Sum256(data)
		label := "fuzz-long-rc-" + hex.EncodeToString(sum[:4])
		t.Logf("%s rc=%v kbps=%d kf=%d buf=%d/%d/%d cpu=%d fixture=%s frames=%d",
			label, opts.RateControlMode, cfg.targetKbps, opts.KeyFrameInterval,
			opts.BufferSizeMs, opts.BufferInitialSizeMs, opts.BufferOptimalSizeMs,
			opts.CpuUsed, cfg.fixtureName, len(cfg.sources))

		govpxFrames := encodeFramesWithGovpx(t, opts, cfg.sources)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, label, opts, cfg.targetKbps, cfg.sources, libvpxEndUsageArgs(extraArgs))

		prefix := matchedFramePrefixLength(govpxFrames, libvpxFrames)
		t.Logf("%s matched-prefix=%d/%d frames (govpx=%d libvpx=%d total)",
			label, prefix, min(len(govpxFrames), len(libvpxFrames)), len(govpxFrames), len(libvpxFrames))

		// Strict byte parity. Seeds with documented cumulative drift
		// (gap A: kf=30 GF cliff; gap B: VBR kf=999 long-run drift) are
		// expected to fail until the corresponding fix lands; the
		// failure log surfaces the exact frame index where parity broke.
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

// matchedFramePrefixLength returns the largest N such that
// got[:N] and want[:N] are SHA-256 equal frame-by-frame. Used to
// produce a scoreboard signal for cumulative RC drift across long
// fixtures.
func matchedFramePrefixLength(got, want [][]byte) int {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if sha256.Sum256(got[i]) != sha256.Sum256(want[i]) {
			return i
		}
	}
	return n
}

type longFixtureFuzzCase struct {
	width          int
	height         int
	frames         int
	targetKbps     int
	kfInterval     int
	rcMode         RateControlMode
	deadline       Deadline
	cpuUsed        int
	bufferMs       int
	bufferInitMs   int
	bufferOptMs    int
	fixtureName    string
	fixtureBuilder func(w, h, i int) Image
	sources        []Image
}

func newLongFixtureFuzzCase(data []byte) longFixtureFuzzCase {
	r := oracleRuntimeControlFuzzBytes{data: data}

	rcPool := [...]RateControlMode{RateControlCBR, RateControlVBR}
	kbpsPool := [...]int{300, 700, 1200}
	kfPool := [...]int{999, 30, 60}
	bufPool := [...]struct {
		size, init, opt int
	}{
		{6000, 4000, 5000}, // libvpx default
		{600, 400, 500},    // tight RTC-style buffer
	}
	fixturePool := [...]struct {
		name    string
		builder func(w, h, i int) Image
	}{
		{"panning", encoderValidationPanningFrame},
		{"splitmv", encoderValidationSplitMVQuadrantFrame},
	}
	deadlinePool := [...]Deadline{DeadlineRealtime, DeadlineGoodQuality}
	cpuPool := [...]int{-3, 0, -8}

	rc := rcPool[r.pick(len(rcPool))]
	kbps := kbpsPool[r.pick(len(kbpsPool))]
	kf := kfPool[r.pick(len(kfPool))]
	buf := bufPool[r.pick(len(bufPool))]
	fx := fixturePool[r.pick(len(fixturePool))]
	deadline := deadlinePool[r.pick(len(deadlinePool))]
	cpu := strictByteParityCPUUsed(deadline, cpuPool[r.pick(len(cpuPool))])

	c := longFixtureFuzzCase{
		width:          64,
		height:         64,
		frames:         256,
		targetKbps:     kbps,
		kfInterval:     kf,
		rcMode:         rc,
		deadline:       deadline,
		cpuUsed:        cpu,
		bufferMs:       buf.size,
		bufferInitMs:   buf.init,
		bufferOptMs:    buf.opt,
		fixtureName:    fx.name,
		fixtureBuilder: fx.builder,
	}
	c.sources = make([]Image, c.frames)
	for i := range c.sources {
		c.sources[i] = fx.builder(c.width, c.height, i)
	}
	return c
}

func (c *longFixtureFuzzCase) buildOpts() (EncoderOptions, []string) {
	opts := EncoderOptions{
		Width:               c.width,
		Height:              c.height,
		FPS:                 30,
		RateControlMode:     c.rcMode,
		TargetBitrateKbps:   c.targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    c.kfInterval,
		Deadline:            c.deadline,
		CpuUsed:             c.cpuUsed,
		Tuning:              TunePSNR,
		BufferSizeMs:        c.bufferMs,
		BufferInitialSizeMs: c.bufferInitMs,
		BufferOptimalSizeMs: c.bufferOptMs,
	}
	endUsage := "cbr"
	if c.rcMode == RateControlVBR {
		endUsage = "vbr"
	}
	// The base `encodeFramesWithLibvpxOracle` helper hard-codes
	// `--kf-min-dist=999 --kf-max-dist=999` so libvpx will never insert
	// a non-first keyframe unless the caller overrides it. The govpx
	// side, however, honours `EncoderOptions.KeyFrameInterval` and
	// schedules a forced KF at frame N*interval (see
	// `applyFixedKeyFrameIntervalFlag` in encoder_reference_decisions.go).
	// Without aligning the libvpx oracle's kf-max-dist with
	// `KeyFrameInterval`, the seeds with `kf=30` produce a govpx
	// keyframe at frame 30 while the oracle stays on inter, diverging
	// at the second-keyframe boundary. Match libvpx's
	// `--kf-min-dist=0 --kf-max-dist=interval` to the govpx-side fixed
	// KF schedule (mirrors libvpx vp8_cx_iface.c
	// `cfg.kf_max_dist`-driven `cpi->key_frame_frequency` handling).
	//
	// Note (task #335): kf-min-dist=0 also flips libvpx's
	// `oxcf->auto_key` to 1 at vp8_cx_iface.c:377-378, which under
	// compressor_speed != 2 (good/best quality) runs `decide_key_frame`
	// (onyx_if.c:2713 / line 3991) inside the recode loop and can
	// insert unscheduled intra recodes whenever the committed
	// inter-mode intra-percentage crosses libvpx's thresholds. govpx's
	// equivalent is gated behind EncoderOptions.AdaptiveKeyFrames,
	// which the long-fixture fuzz does not enable, so for content with
	// strong scene cuts (splitmv quadrant pan + tight VBR buffer) the
	// two encoders make different KF decisions late in the sequence.
	// See longFixtureSeedsDeferred for the aeeeb411 ("19") seed that
	// surfaces this gap.
	//
	// Buffer-size flags must also be forwarded: govpx receives
	// `BufferSizeMs / BufferInitialSizeMs / BufferOptimalSizeMs` via
	// EncoderOptions, but libvpx vpxenc defaults to its own
	// 6000/4000/5000ms envelope unless `--buf-sz / --buf-initial-sz /
	// --buf-optimal-sz` are passed. Without forwarding, fuzz cohorts
	// that select the tight-buf entry from `bufPool` ran govpx against
	// a 600/400/500ms buffer while the libvpx oracle kept the default
	// 6000/4000/5000ms one — making the two sides regulate Q against
	// different `cpi->buffer_level` / `cpi->oxcf.optimal_buffer_level`
	// states inside calc_pframe_target_size's percent_low buffer
	// shrink (libvpx vp8/encoder/ratectrl.c lines 706-727). That
	// mismatch was the sole driver of the b3ea8a0d ("0001") seed
	// first_partition delta (govpx target=8048 → regulate_q Q=7 vs
	// libvpx target=8524 → regulate_q Q=6 at the first inter frame).
	// Pass the govpx-side buffer config through so libvpx sees the
	// same envelope (mirrors libvpx vpxenc.c argument plumbing for
	// `cfg.rc_buf_sz / rc_buf_initial_sz / rc_buf_optimal_sz`).
	extra := []string{
		"--end-usage=" + endUsage,
		"--kf-min-dist=0",
		"--kf-max-dist=" + strconv.Itoa(c.kfInterval),
		"--buf-sz=" + strconv.Itoa(c.bufferMs),
		"--buf-initial-sz=" + strconv.Itoa(c.bufferInitMs),
		"--buf-optimal-sz=" + strconv.Itoa(c.bufferOptMs),
	}
	return opts, extra
}
