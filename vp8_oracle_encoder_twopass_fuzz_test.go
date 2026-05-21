//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// firstPassLooseTolerances captures per-field |Δ| ceilings that the
// fuzz-driven first-pass comparator enforces. The ceilings start at
// values that just barely admit the original seed corpus and are documented
// as a temporary floor pending closure of the residual govpx vs libvpx
// MV-accumulator divergence. Each entry
// is "ceiling on |govpx - libvpx|" for that FIRSTPASS_STATS field,
// applied per-frame and on the IsTotal aggregate.
//
// Tighten these as the underlying accumulator bug closes; the goal is
// a uniform 1e-9 (effective bit-exactness) ceiling once the first-pass
// reconstruction matches libvpx byte-for-byte.
type firstPassLooseTolerances struct {
	IntraError          float64
	CodedError          float64
	SSIMWeightedPredErr float64
	PcntInter           float64
	PcntMotion          float64
	PcntSecondRef       float64
	PcntNeutral         float64
	MVr                 float64
	MVrAbs              float64
	MVc                 float64
	MVcAbs              float64
	MVrv                float64
	MVcv                float64
	MVInOutCount        float64
	NewMVCount          float64
}

// defaultFirstPassLooseTolerances is the cross-config ceiling consumed by the
// two-pass fuzzer. The first-pass MV-accumulator divergence is closed:
// first-pass MV-accumulator divergence (govpx's diamond search was
// computing MV-SAD costs with sad_per_bit derived from qIndex=26,
// while libvpx's vp8_first_pass leaves x->sadperbit16 at the calloc
// zero-init because vp8cx_initialize_me_consts is never called before
// the first-pass loop — see firstPassMode in vp8_encoder_motion_search.go).
// With that fixed, every FIRSTPASS_STATS field matches byte-for-byte
// across the current seed corpus, so the ceiling collapses to a thin
// floating-point-noise floor. Bumping the floor catches any future
// regression that re-introduces drift while still allowing fmadd /
// fused-multiply-add rounding differences across CPU dispatches.
var defaultFirstPassLooseTolerances = firstPassLooseTolerances{
	IntraError:          1e-9, // libvpx writes intra_error >> 8, govpx mirrors
	CodedError:          1e-9,
	SSIMWeightedPredErr: 1e-9,
	PcntInter:           1e-9,
	PcntMotion:          1e-9,
	PcntSecondRef:       1e-9,
	PcntNeutral:         1e-9,
	MVr:                 1e-9,
	MVrAbs:              1e-9,
	MVc:                 1e-9,
	MVcAbs:              1e-9,
	MVrv:                1e-9,
	MVcv:                1e-9,
	MVInOutCount:        1e-9,
	NewMVCount:          1e-9,
}

// compareFirstPassStatsLoose enforces a per-field |Δ| ceiling between
// govpx and libvpx FIRSTPASS_STATS across all frames and the totals
// row. Unlike compareFirstPassStats (which expects a pinned cpu=0,
// frames<=4 fixture and asserts at 1e-12), this comparator is meant
// for the fuzz-driven cross-config corpus that historically exercised
// the first-pass MV-accumulator divergence.
//
// Returns (maxField, maxAbsValue) for diagnostic reporting; callers
// inspect the returned values to log a scoreboard summary alongside
// the per-field t.Errorf failures.
func compareFirstPassStatsLoose(t *testing.T, label string, govpx, libvpx []FirstPassFrameStats, tols firstPassLooseTolerances) (string, float64) {
	t.Helper()
	if len(govpx) != len(libvpx) {
		t.Errorf("%s first-pass stats length: govpx=%d libvpx=%d", label, len(govpx), len(libvpx))
		return "", 0
	}
	maxAbsField := ""
	maxAbsValue := 0.0
	check := func(frameLabel, field string, got, want, tol float64) {
		d := math.Abs(got - want)
		if d > maxAbsValue {
			maxAbsValue = d
			maxAbsField = field
		}
		if d > tol {
			t.Errorf("%s %s %s |Δ|=%.6g > tol=%.6g (got=%.6g want=%.6g)",
				label, frameLabel, field, d, tol, got, want)
		}
	}
	for i := range govpx {
		g, l := govpx[i], libvpx[i]
		fl := "frame " + strconv.Itoa(i)
		if l.IsTotal {
			fl = "total"
		}
		check(fl, "IntraError", g.IntraError, l.IntraError, tols.IntraError)
		check(fl, "CodedError", g.CodedError, l.CodedError, tols.CodedError)
		check(fl, "SSIMWeightedPredErr", g.SSIMWeightedPredErr, l.SSIMWeightedPredErr, tols.SSIMWeightedPredErr)
		check(fl, "PcntInter", g.PcntInter, l.PcntInter, tols.PcntInter)
		check(fl, "PcntMotion", g.PcntMotion, l.PcntMotion, tols.PcntMotion)
		check(fl, "PcntSecondRef", g.PcntSecondRef, l.PcntSecondRef, tols.PcntSecondRef)
		check(fl, "PcntNeutral", g.PcntNeutral, l.PcntNeutral, tols.PcntNeutral)
		check(fl, "MVr", g.MVr, l.MVr, tols.MVr)
		check(fl, "MVrAbs", g.MVrAbs, l.MVrAbs, tols.MVrAbs)
		check(fl, "MVc", g.MVc, l.MVc, tols.MVc)
		check(fl, "MVcAbs", g.MVcAbs, l.MVcAbs, tols.MVcAbs)
		check(fl, "MVrv", g.MVrv, l.MVrv, tols.MVrv)
		check(fl, "MVcv", g.MVcv, l.MVcv, tols.MVcv)
		check(fl, "MVInOutCount", g.MVInOutCount, l.MVInOutCount, tols.MVInOutCount)
		check(fl, "NewMVCount", g.NewMVCount, l.NewMVCount, tols.NewMVCount)
	}
	return maxAbsField, maxAbsValue
}

// FuzzEncoderTwoPassByteParity drives both libvpx and govpx through a full
// two-pass VBR encode, asserting pass-2 keyframe byte parity strictly and
// logging first-pass stats divergences plus inter-frame pass-2 mismatches
// under the matched-prefix-length scoreboard convention.
//
// First-pass stats are tolerance-compared today by
// TestVP8OracleFirstPassStatsCompare only for a tightly-pinned (cpu=0,
// frames≤4, no threads/ARNR) config. Under arbitrary fuzz parameters
// the per-field tolerances would trip on every iteration; the fuzzer
// records the divergence summary instead so a future tightening pass
// has the data, and feeds the libvpx-derived stats to govpx pass 2
// so the second-pass byte parity is exercised against a known-good
// stats blob even when govpx's own first pass disagrees.
//
// Each fuzz iteration spawns vpxenc twice (pass 1 and pass 2) plus
// the govpx encodes; expect roughly half a second per iteration
// on the seed corpus.
func FuzzEncoderTwoPassByteParity(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run two-pass byte-parity fuzz")
	}
	// Each seed is consumed in newTwoPassFuzzCase order:
	// byte0→targetKbps, byte1→cpuUsed, byte2→kfInterval, byte3→arnrMax,
	// byte4→threads, byte5→frames. Each byte is mod len(pool).
	//
	// The seeds exercise the cross-product of (bitrate, kf-interval,
	// arnr, threads, cpu, frames) on the firstPassOracleRampFrame
	// fixture. Mid-stream KF-force coverage (kfPool index 3 → kf=4 on
	// 6/8/12-frame clips, exercising libvpx vp8_second_pass's
	// `frames_to_key == 0` branch at firstpass.c line 2237) was
	// previously deferred while govpx's prepareKFGroup computed
	// kfGroupErr over `len(stats) - frame` rather than the libvpx
	// `frames_to_key` returned by find_next_key_frame. The libvpx walk is
	// now ported verbatim (firstpass.c lines 2533-2596), so the mid-stream
	// KF re-seed integrates over the same span libvpx uses.
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0},
		{1, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0},
		{0, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 0, 0},
		{2, 0, 0, 0, 1, 0},
		// kf=4 mid-stream KF-force seeds (kfPool index 3): exercise
		// libvpx vp8_second_pass's `frames_to_key == 0` branch at
		// firstpass.c line 2237 on 6/8/12-frame clips.
		{0, 0, 3, 0, 0, 0},
		{0, 0, 3, 0, 0, 1},
		{0, 0, 3, 0, 0, 2},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		vpxenc := coracletest.Vpxenc(t)
		vpxencOracle := coracletest.VpxencOracle(t)
		cfg := newTwoPassFuzzCase(data)
		opts := cfg.buildOpts()

		sum := sha256.Sum256(data)
		label := "fuzz-twopass-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d kbps=%d kf=%d arnr=%d/%d/%d threads=%d cpu=%d frames=%d",
			label, opts.Width, opts.Height, cfg.targetKbps, opts.KeyFrameInterval,
			opts.ARNRMaxFrames, opts.ARNRStrength, opts.ARNRType, opts.Threads, opts.CpuUsed, len(cfg.sources))

		// First-pass stats: govpx (in-process) vs libvpx (vpxenc pass=1).
		govpxStats := captureGovpxFirstPassStats(t, opts, cfg.sources)

		fpfData, libvpxIVF, diag, err := coracle.VpxencVP8TwoPassEncodeI420(
			encoderValidationI420Bytes(t, cfg.sources),
			coracle.VpxencVP8TwoPassConfig{
				FirstPassBinaryPath:  vpxenc,
				SecondPassBinaryPath: vpxencOracle,
				Common:               vp8TwoPassFuzzVpxencConfig(opts, cfg.targetKbps, len(cfg.sources)),
				SecondPassExtraArgs:  vp8TwoPassFuzzSecondPassArgs(opts),
			},
		)
		if err != nil {
			t.Fatalf("vpxenc two-pass encode failed: %v\n%s", err, diag)
		}
		libvpxStats := parseLibvpxFirstPassStats(t, fpfData)
		maxField, maxAbs := compareFirstPassStatsLoose(t, label, govpxStats, libvpxStats, defaultFirstPassLooseTolerances)
		t.Logf("%s first-pass stats max divergence (post-tolerance): field=%s |Δ|=%.4g", label, maxField, maxAbs)

		// Second-pass: feed libvpx-derived stats to govpx and run
		// vpxenc-oracle pass=2 for the libvpx reference IVF.
		govpxOpts := opts
		govpxOpts.TwoPassStats = libvpxStats
		govpxFrames := encodeFramesWithGovpx(t, govpxOpts, cfg.sources)
		libvpxFrames, err := testutil.IVFFramePayloads(libvpxIVF)
		if err != nil {
			t.Fatalf("IVFFramePayloads: %v", err)
		}

		// Strict byte parity on pass 2 output. Seeds where govpx pass 2
		// (driven by libvpx-derived stats) diverges from libvpx pass 2
		// fail visibly here; that's the signal for pass-2 RC fixes.
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

func vp8TwoPassFuzzVpxencConfig(opts EncoderOptions, targetKbps int, frames int) coracle.VpxencVP8Config {
	return coracle.VpxencVP8Config{
		Width:             opts.Width,
		Height:            opts.Height,
		Frames:            frames,
		Deadline:          libvpxOracleDeadline(opts.Deadline),
		CPUUsed:           opts.CpuUsed,
		TargetBitrateKbps: targetKbps,
		MinQ:              opts.MinQuantizer,
		MaxQ:              opts.MaxQuantizer,
		Timebase:          "1/" + strconv.Itoa(opts.FPS),
		FPS:               strconv.Itoa(opts.FPS) + "/1",
		KeyFrameDistSet:   true,
		KeyFrameMinDist:   opts.KeyFrameInterval,
		KeyFrameMaxDist:   opts.KeyFrameInterval,
		ExtraArgs:         []string{"--end-usage=vbr"},
	}
}

func vp8TwoPassFuzzSecondPassArgs(opts EncoderOptions) []string {
	args := []string{}
	if opts.Threads > 0 {
		args = append(args, "--threads="+strconv.Itoa(opts.Threads))
	}
	if opts.ARNRMaxFrames > 0 {
		args = append(args,
			"--arnr-maxframes="+strconv.Itoa(opts.ARNRMaxFrames),
			"--arnr-strength="+strconv.Itoa(opts.ARNRStrength),
			"--arnr-type="+strconv.Itoa(opts.ARNRType))
	}
	return args
}

type twoPassFuzzCase struct {
	width      int
	height     int
	frames     int
	targetKbps int
	deadline   Deadline
	cpuUsed    int
	kfInterval int
	threads    int
	arnrMax    int
	arnrStr    int
	arnrType   int
	sources    []Image
}

func newTwoPassFuzzCase(data []byte) twoPassFuzzCase {
	r := testutil.NewByteCursor(data)

	kbpsPool := [...]int{300, 500, 700}
	// kfPool: indices 0-2 retain the long-interval coverage (no mid-stream
	// kf force in frames<=12); index 3 (kf=4) drives a mid-stream kf
	// force at frame 4 of an 8/12-frame clip — exercising libvpx
	// vp8_second_pass's `frames_to_key == 0` branch at firstpass.c line
	// 2237 along with `find_next_key_frame` re-seed under two pass.
	kfPool := [...]int{30, 60, 120, 4}
	arnrMaxPool := [...]int{0, 3, 7}
	threadPool := [...]int{0, 2}
	cpuPool := [...]int{0, 4}
	framesPool := [...]int{6, 8, 12}

	c := twoPassFuzzCase{
		width:      32,
		height:     32,
		targetKbps: kbpsPool[r.Pick(len(kbpsPool))],
		deadline:   DeadlineGoodQuality,
		cpuUsed:    cpuPool[r.Pick(len(cpuPool))],
		kfInterval: kfPool[r.Pick(len(kfPool))],
		arnrMax:    arnrMaxPool[r.Pick(len(arnrMaxPool))],
		arnrStr:    3,
		arnrType:   3,
		threads:    threadPool[r.Pick(len(threadPool))],
		frames:     framesPool[r.Pick(len(framesPool))],
	}
	c.sources = make([]Image, c.frames)
	for i := range c.sources {
		c.sources[i] = firstPassOracleRampFrame(c.width, c.height, i)
	}
	return c
}

func (c *twoPassFuzzCase) buildOpts() EncoderOptions {
	return EncoderOptions{
		Width:             c.width,
		Height:            c.height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: c.targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  c.kfInterval,
		Deadline:          c.deadline,
		CpuUsed:           c.cpuUsed,
		Tuning:            TunePSNR,
		ARNRMaxFrames:     c.arnrMax,
		ARNRStrength:      c.arnrStr,
		ARNRType:          c.arnrType,
		Threads:           c.threads,
	}
}
