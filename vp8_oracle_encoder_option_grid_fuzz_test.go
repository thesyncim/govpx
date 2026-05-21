//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzEncoderProductionStreamByteParity runs an option-grid fuzz against the
// canonical vpxenc-oracle harness (the same one TestVP8OracleEncoderStreamByteParity
// uses). It closes plan-§3 F1: random resolution × deadline × cpu_used × rate
// control × feature-toggle combinations are exercised at every fuzz iteration,
// including production resolutions and Threads ≥ 2 — picking up bitstream-
// affecting changes that the hand-picked strict-gate matrix would miss.
//
// Every fuzz iteration asserts full byte-exact parity (matchLimit=0) across
// all frames at every resolution. The matchLimit=1 keyframe-only floor and
// matched-prefix scoreboard convention used during the §5 cascade are now
// retired — the autospeed determinism work (#361-#369), the libvpx-oracle
// reproducibility retry (#355/#369), and the matchLimit tightening sweep
// (#384) closed the residual production-resolution slack, so the gate is
// uniformly strict. Divergences land in
// testdata/fuzz/FuzzEncoderProductionStreamByteParity and replay as ordinary
// go test regressions.
func FuzzEncoderProductionStreamByteParity(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run option-grid byte-parity fuzz")
	}
	// Each seed is (resBucket, deadlineBucket, cpuBucket, rcBucket, featBucket,
	// tokenPartBucket, threadsBucket, sharpBucket, tuneBucket, arnrBucket,
	// errorResBucket). The errorResBucket byte was added by task #251 to
	// roll error_resilient independently of featBucket so the (error_res ×
	// token_partitions × threads) product is exercised every iteration.
	// 10-byte seeds remain valid; the cursor wraps to byte 0 for the 11th
	// pick (no errorResBucket toggle).
	seeds := [][]byte{
		// Mirrors of canonical strict-gate cases at the smallest sizes,
		// confirming the fuzzer's small-resolution path matches a known
		// pass before exploration begins.
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // realtime cpu0 16x16 CBR
		{1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // good cpu4 32x16 CBR
		{2, 2, 2, 0, 0, 0, 0, 0, 0, 0, 0}, // best cpu0 48x48 CBR
		{3, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0}, // realtime cpu-3 64x64 VBR
		{4, 0, 0, 0, 0, 2, 1, 0, 0, 0, 0}, // realtime cpu0 96x96 CBR token=4 threads=2
		{5, 1, 1, 0, 0, 0, 0, 1, 0, 0, 0}, // good cpu4 128x128 sharpness=4
		// Production-resolution seeds (keyframe floor + scoreboard).
		{7, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},  // realtime 320x240 threads=2
		{8, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0},  // realtime 640x360 threads=2
		{9, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0},  // realtime 854x480 threads=4
		{10, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0}, // realtime 1280x720 threads=4
		// task #251: error_resilient × token_partitions ∈ {1,2,4,8} ×
		// threads ∈ {1,2,4} on the WebRTC-shaped panning fixtures. Each
		// seed flips errorResBucket=1 so featBucket stays free to roll
		// orthogonal toggles. Resolution buckets stay small to bound
		// fuzz iteration time; the production-resolution coverage of
		// this combo comes from the corpus the fuzzer mints itself.
		{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, // RT cpu0 64x64 CBR token=1 thr=1 ER
		{3, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}, // RT cpu0 64x64 CBR token=2 thr=1 ER
		{3, 0, 0, 0, 0, 2, 0, 0, 0, 0, 1}, // RT cpu0 64x64 CBR token=4 thr=1 ER
		{3, 0, 0, 0, 0, 3, 0, 0, 0, 0, 1}, // RT cpu0 64x64 CBR token=8 thr=1 ER
		{3, 0, 0, 0, 0, 2, 1, 0, 0, 0, 1}, // RT cpu0 64x64 CBR token=4 thr=2 ER
		{3, 0, 0, 0, 0, 3, 2, 0, 0, 0, 1}, // RT cpu0 64x64 CBR token=8 thr=4 ER
		{4, 0, 0, 0, 0, 2, 1, 0, 0, 0, 1}, // RT cpu0 96x96 CBR token=4 thr=2 ER
		{4, 0, 0, 0, 0, 3, 2, 0, 0, 0, 1}, // RT cpu0 96x96 CBR token=8 thr=4 ER
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		vpxencOracle := coracletest.VpxencOracle(t)
		cfg := newOptionGridFuzzCase(data)
		opts, libvpxArgs := cfg.buildOpts()
		sources := cfg.buildSources()

		sum := sha256.Sum256(data)
		label := "fuzz-option-grid-" + cfg.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d deadline=%v cpu=%d threads=%d rc=%v sharp=%d tune=%v sc=%d er=%t token=%d arnr=%d/%d/%d frames=%d",
			label, opts.Width, opts.Height, opts.Deadline, opts.CpuUsed, opts.Threads,
			opts.RateControlMode, opts.Sharpness, opts.Tuning, opts.ScreenContentMode,
			opts.ErrorResilient, opts.TokenPartitions, opts.ARNRMaxFrames, opts.ARNRStrength, opts.ARNRType, len(sources))

		govpxFrames := encodeFramesWithGovpx(t, opts, sources)
		// Task #369: govpx is now deterministic across host load at
		// threads>=2 thanks to the inter-frame budget/3 wall-clock pin
		// (interFrameAutoSpeedTimingCompensation, vp8_encoder_config.go).
		// The libvpx oracle is still byte-flaky at threads>=2 for
		// several VP8 configs (notably the 1f411689 seed#7 cohort:
		// 640x360 RT cpu_used=0 threads=2 CBR, where libvpx cycles
		// through 3-4 distinct bitstreams across consecutive runs).
		// encodeVP8FramesWithLibvpxOracleMatchingGovpx retries the oracle
		// up to N times searching for a run whose bytes match govpx;
		// for serial (--threads<=1) callers it degrades to a single
		// pass-through. See vp8_oracle_threading_reproducibility_helpers_test.go
		// for the retry policy and failure diagnostics.
		libvpxFrames := encodeVP8FramesWithLibvpxOracleMatchingGovpx(t, vpxencOracle, label, opts, cfg.targetKbps, sources, libvpxArgs, govpxFrames)

		// Strict byte parity on every frame. Seeds that hit a documented
		// divergence (see byte-exactness tracker gaps C, D) are expected
		// to fail today until the relevant fix lands; a green run means
		// the matched prefix covers the full clip for that config.
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

// optionGridFuzzCase materialises a fuzz seed into an EncoderOptions plus the
// libvpx vpxenc-oracle command-line tail that mirrors those options. Each
// `bucket` consumes one byte from the seed; running out of bytes wraps the
// reader so even minimal seeds yield a fully-specified case.
type optionGridFuzzCase struct {
	name       string
	width      int
	height     int
	frames     int
	targetKbps int
	deadline   Deadline
	cpuUsed    int
	threads    int
	rcMode     RateControlMode
	rcModeSet  bool
	sharpness  int
	tuning     Tuning
	tuningSet  bool
	screenMode int
	errorRes   bool
	tokenParts int
	arnrMax    int
	arnrStr    int
	arnrType   int
	extraArgs  []string
}

func newOptionGridFuzzCase(data []byte) optionGridFuzzCase {
	r := testutil.NewByteCursor(data)

	resPool := [...]struct {
		w, h, frames int
	}{
		{16, 16, 8},
		{32, 16, 8},
		{48, 48, 8},
		{64, 64, 8},
		{96, 96, 6},
		{128, 128, 6},
		{160, 96, 6},
		{320, 240, 4},
		{640, 360, 3},
		{854, 480, 3},
		{1280, 720, 2},
	}
	deadlinePool := [...]Deadline{DeadlineRealtime, DeadlineGoodQuality, DeadlineBestQuality}
	cpuPool := [...]int{0, -3, -8, 4, 8}
	rcPool := [...]RateControlMode{RateControlCBR, RateControlVBR, RateControlCQ, RateControlQ}
	threadPool := [...]int{0, 2, 4}
	sharpnessPool := [...]int{0, 4, 7}

	res := resPool[r.Pick(len(resPool))]
	deadline := deadlinePool[r.Pick(len(deadlinePool))]
	cpuUsed := cpuPool[r.Pick(len(cpuPool))]
	rcBucket := r.Pick(len(rcPool))
	featBucket := r.Pick(8)
	tokenParts := r.Pick(4)
	threads := threadPool[r.Pick(len(threadPool))]
	sharpBucket := r.Pick(len(sharpnessPool) + 1) // bucket 0 = leave default
	tuneBucket := r.Pick(3)                       // 0=default, 1=PSNR, 2=SSIM
	arnrBucket := r.Pick(4)                       // bucket 0 = disabled
	// errorResBucket lets the fuzzer flip --error-resilient=1 independently
	// of the screen-content / arnr feature buckets so the
	// {token_partitions ∈ {1,2,4,8}} × {error_resilient on/off} ×
	// {threads ∈ {1,2,4}} product is reachable on a single fuzz iteration
	// — task #251 wire-image audit of vp8_pack_tokens_into_partitions
	// (libvpx vp8/encoder/bitstream.c:292-318).
	errorResBucket := r.Pick(2)

	c := optionGridFuzzCase{
		width:      res.w,
		height:     res.h,
		frames:     res.frames,
		targetKbps: 700,
		deadline:   deadline,
		cpuUsed:    strictByteParityCPUUsed(deadline, cpuUsed),
		threads:    threads,
		tokenParts: tokenParts,
	}

	// Rate control + bitrate + libvpx CLI mirroring.
	switch rcBucket {
	case 1:
		c.rcMode = RateControlVBR
		c.rcModeSet = true
		c.extraArgs = append(c.extraArgs, "--end-usage=vbr")
	case 2:
		c.rcMode = RateControlCQ
		c.rcModeSet = true
		c.extraArgs = append(c.extraArgs, "--end-usage=cq")
	case 3:
		c.rcMode = RateControlQ
		c.rcModeSet = true
		c.extraArgs = append(c.extraArgs, "--end-usage=q")
	default:
		c.rcMode = RateControlCBR
		c.extraArgs = append(c.extraArgs, "--end-usage=cbr")
	}

	// Feature bucket: pick a single mutually-exclusive group of toggles
	// per iteration to keep the option surface tractable. Choosing 8
	// disjoint slots biases each fuzz iter toward one explored axis.
	switch featBucket {
	case 1:
		c.screenMode = 1
		c.extraArgs = append(c.extraArgs, "--screen-content-mode=1")
	case 2:
		c.screenMode = 2
		c.extraArgs = append(c.extraArgs, "--screen-content-mode=2")
	case 3:
		c.errorRes = true
		c.extraArgs = append(c.extraArgs, "--error-resilient=1")
	}
	// errorResBucket toggles --error-resilient=1 independently of
	// featBucket so the (error_resilient × token_partitions × threads)
	// product is reachable on each fuzz iteration. featBucket==3 already
	// turns errorRes on; in that case the bucket is idempotent.
	if errorResBucket == 1 && !c.errorRes {
		c.errorRes = true
		c.extraArgs = append(c.extraArgs, "--error-resilient=1")
	}

	if tokenParts > 0 {
		c.extraArgs = append(c.extraArgs, "--token-parts="+strconv.Itoa(tokenParts))
	}
	if threads > 0 {
		c.extraArgs = append(c.extraArgs, "--threads="+strconv.Itoa(threads))
	}
	if sharpBucket > 0 {
		c.sharpness = sharpnessPool[sharpBucket-1]
		c.extraArgs = append(c.extraArgs, "--sharpness="+strconv.Itoa(c.sharpness))
	}
	switch tuneBucket {
	case 1:
		c.tuning = TunePSNR
		c.tuningSet = true
		c.extraArgs = append(c.extraArgs, "--tune=psnr")
	case 2:
		c.tuning = TuneSSIM
		c.tuningSet = true
		c.extraArgs = append(c.extraArgs, "--tune=ssim")
	}
	if arnrBucket > 0 {
		// Cap arnr to small values; large frames+strength explodes wall
		// time and is exercised by dedicated arnr strict-gate cases.
		c.arnrMax = arnrBucket
		c.arnrStr = r.Pick(4)
		c.arnrType = 1 + r.Pick(3)
		c.extraArgs = append(c.extraArgs,
			"--arnr-maxframes="+strconv.Itoa(c.arnrMax),
			"--arnr-strength="+strconv.Itoa(c.arnrStr),
			"--arnr-type="+strconv.Itoa(c.arnrType))
	}

	c.name = "w" + strconv.Itoa(res.w) + "h" + strconv.Itoa(res.h)
	return c
}

func (c *optionGridFuzzCase) buildOpts() (EncoderOptions, []string) {
	rcMode := c.rcMode
	if !c.rcModeSet {
		rcMode = RateControlCBR
	}
	tuning := TunePSNR
	if c.tuningSet {
		tuning = c.tuning
	}
	cqLevel := 0
	if rcMode == RateControlCQ || rcMode == RateControlQ {
		cqLevel = 32
		// CQ/Q libvpx reference also needs --cq-level mirrored.
		c.extraArgs = append(c.extraArgs, "--cq-level=32")
	}
	opts := EncoderOptions{
		Width:             c.width,
		Height:            c.height,
		FPS:               30,
		RateControlMode:   rcMode,
		TargetBitrateKbps: c.targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		CQLevel:           cqLevel,
		KeyFrameInterval:  999,
		Deadline:          c.deadline,
		CpuUsed:           c.cpuUsed,
		Tuning:            tuning,
		Sharpness:         c.sharpness,
		ScreenContentMode: c.screenMode,
		ErrorResilient:    c.errorRes,
		TokenPartitions:   c.tokenParts,
		Threads:           c.threads,
		ARNRMaxFrames:     c.arnrMax,
		ARNRStrength:      c.arnrStr,
		ARNRType:          c.arnrType,
	}
	return opts, libvpxEndUsageArgs(c.extraArgs)
}

func (c *optionGridFuzzCase) buildSources() []Image {
	sources := make([]Image, c.frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(c.width, c.height, i)
	}
	return sources
}
