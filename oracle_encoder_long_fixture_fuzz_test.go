//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

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

		// Keyframe parity is the strict floor: every fuzz config must
		// produce a matching frame 0. matchLimit=1 in
		// assertSegmentByteParity logs everything else as scoreboard.
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 1)
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
	extra := []string{
		"--end-usage=" + endUsage,
	}
	return opts, extra
}
