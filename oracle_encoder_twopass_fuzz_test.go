//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// logFirstPassDivergenceSummary records the largest per-field
// difference between govpx and libvpx first-pass stats for each
// frame and the totals row. Failures are NOT raised here — the
// scoreboard data is intended for a future tightening pass.
func logFirstPassDivergenceSummary(t *testing.T, label string, govpx, libvpx []FirstPassFrameStats) {
	t.Helper()
	if len(govpx) != len(libvpx) {
		t.Logf("%s first-pass stats length: govpx=%d libvpx=%d", label, len(govpx), len(libvpx))
		return
	}
	maxAbsField := ""
	maxAbsValue := 0.0
	for i := range govpx {
		g, l := govpx[i], libvpx[i]
		checkField := func(name string, got, want float64) {
			d := math.Abs(got - want)
			if d > maxAbsValue {
				maxAbsValue = d
				maxAbsField = name
			}
		}
		checkField("IntraError", g.IntraError, l.IntraError)
		checkField("CodedError", g.CodedError, l.CodedError)
		checkField("SSIMWeightedPredErr", g.SSIMWeightedPredErr, l.SSIMWeightedPredErr)
		checkField("MVrAbs", g.MVrAbs, l.MVrAbs)
		checkField("MVcAbs", g.MVcAbs, l.MVcAbs)
		checkField("MVrv", g.MVrv, l.MVrv)
		checkField("MVcv", g.MVcv, l.MVcv)
	}
	t.Logf("%s first-pass stats max divergence: field=%s |Δ|=%.4g (logged-only)", label, maxAbsField, maxAbsValue)
}

// FuzzEncoderTwoPassByteParity closes plan-§3 F2 / G3: a fuzz-driven
// option grid drives both libvpx and govpx through a full two-pass
// VBR encode, asserting pass-2 keyframe byte parity strictly and
// logging first-pass stats divergences plus inter-frame pass-2
// mismatches per the §5 matched-prefix-length scoreboard convention.
//
// First-pass stats are tolerance-compared today by
// TestOracleFirstPassStatsCompare only for a tightly-pinned (cpu=0,
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
	// Each seed is (bitrateBucket, kfBucket, arnrBucket, threadsBucket,
	// cpuBucket, framesBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0},
		{1, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0},
		{0, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 0, 0},
		{2, 0, 0, 0, 1, 0},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		vpxenc := findVpxenc(t)
		vpxencOracle := findVpxencOracle(t)
		cfg := newTwoPassFuzzCase(data)
		opts := cfg.buildOpts()

		sum := sha256.Sum256(data)
		label := "fuzz-twopass-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d kbps=%d kf=%d arnr=%d/%d/%d threads=%d cpu=%d frames=%d",
			label, opts.Width, opts.Height, cfg.targetKbps, opts.KeyFrameInterval,
			opts.ARNRMaxFrames, opts.ARNRStrength, opts.ARNRType, opts.Threads, opts.CpuUsed, len(cfg.sources))

		dir := t.TempDir()
		yuvPath := filepath.Join(dir, label+".yuv")
		fpfPath := filepath.Join(dir, label+".fpf")
		ivf1Path := filepath.Join(dir, label+"-pass1.ivf")
		ivf2Path := filepath.Join(dir, label+"-pass2.ivf")

		writeEncoderValidationI420(t, yuvPath, cfg.sources)

		// First-pass stats: govpx (in-process) vs libvpx (vpxenc pass=1).
		govpxStats := captureGovpxFirstPassStats(t, opts, cfg.sources)
		runLibvpxPass1(t, vpxenc, yuvPath, ivf1Path, fpfPath, opts, cfg.targetKbps, len(cfg.sources))
		fpfData, err := os.ReadFile(fpfPath)
		if err != nil {
			t.Fatalf("read fpf: %v", err)
		}
		libvpxStats := parseLibvpxFirstPassStats(t, fpfData)
		logFirstPassDivergenceSummary(t, label, govpxStats, libvpxStats)

		// Second-pass: feed libvpx-derived stats to govpx and run
		// vpxenc-oracle pass=2 for the libvpx reference IVF.
		govpxOpts := opts
		govpxOpts.TwoPassStats = libvpxStats
		govpxFrames := encodeFramesWithGovpx(t, govpxOpts, cfg.sources)
		libvpxFrames := runLibvpxPass2BytesOnly(t, vpxencOracle, yuvPath, ivf2Path, fpfPath, opts, cfg.targetKbps, len(cfg.sources))

		// Keyframe byte parity on pass 2 is the achievable floor today;
		// inter-frame parity under arbitrary configs depends on
		// second-pass quantizer estimator convergence and isn't strict.
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 1)
	})
}

// runLibvpxPass2BytesOnly is the bytes-only variant of
// runLibvpxPass2WithTrace: it runs vpxenc-oracle in pass=2 mode and
// returns the per-frame VP8 packet payloads from the resulting IVF
// without writing a trace. Mirrors the args in runLibvpxPass2WithTrace
// minus the GOVPX_ORACLE_TRACE_OUT plumbing.
func runLibvpxPass2BytesOnly(t *testing.T, vpxencOracle string, yuvPath string, ivfPath string, fpfPath string, opts EncoderOptions, targetKbps int, count int) [][]byte {
	t.Helper()
	deadlineArg := libvpxDeadlineArg(opts.Deadline)
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--passes=2",
		"--pass=2",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--kf-max-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(count),
		"--output=" + ivfPath,
	}
	if opts.Threads > 0 {
		args = append(args, "--threads="+strconv.Itoa(opts.Threads))
	}
	if opts.ARNRMaxFrames > 0 {
		args = append(args,
			"--arnr-maxframes="+strconv.Itoa(opts.ARNRMaxFrames),
			"--arnr-strength="+strconv.Itoa(opts.ARNRStrength),
			"--arnr-type="+strconv.Itoa(opts.ARNRType))
	}
	args = append(args, yuvPath)
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		// Treat process failure as a hard error: the libvpx oracle
		// either accepts the config or this fuzzer needs to learn the
		// constraint as a generator filter.
		if errors.Is(err, exec.ErrNotFound) {
			t.Skipf("vpxenc-oracle not executable: %v", err)
		}
		t.Fatalf("vpxenc-oracle pass 2 failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read %s: %v", ivfPath, err)
	}
	return parseIVFFramePayloads(t, data)
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
	r := oracleRuntimeControlFuzzBytes{data: data}

	kbpsPool := [...]int{300, 500, 700}
	kfPool := [...]int{30, 60, 120}
	arnrMaxPool := [...]int{0, 3, 7}
	threadPool := [...]int{0, 2}
	cpuPool := [...]int{0, 4}
	framesPool := [...]int{6, 8, 12}

	c := twoPassFuzzCase{
		width:      32,
		height:     32,
		targetKbps: kbpsPool[r.pick(len(kbpsPool))],
		deadline:   DeadlineGoodQuality,
		cpuUsed:    cpuPool[r.pick(len(cpuPool))],
		kfInterval: kfPool[r.pick(len(kfPool))],
		arnrMax:    arnrMaxPool[r.pick(len(arnrMaxPool))],
		arnrStr:    3,
		arnrType:   3,
		threads:    threadPool[r.pick(len(threadPool))],
		frames:     framesPool[r.pick(len(framesPool))],
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
