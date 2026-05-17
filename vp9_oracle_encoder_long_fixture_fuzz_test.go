//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"os"
	"strconv"
	"testing"
)

// FuzzVP9EncoderLongFixtureRateControl mirrors FuzzEncoderLongFixtureRateControl
// for VP9: a long synthetic clip (≥ 256 frames) is encoded under fuzz-driven
// CBR / VBR configurations and the per-frame matched-prefix length is tallied.
// Strict byte parity is asserted; seeds that hit a cumulative VP9 RC drift gap
// fail visibly here and land as testdata/fuzz seeds for follow-up.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary.
func FuzzVP9EncoderLongFixtureRateControl(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 long-fixture RC fuzz")
	}
	requireVP9VpxencOracleFuzz(f)
	// Each seed is (rcBucket, bitrateBucket, kfBucket, deadlineBucket, cpuBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0}, // CBR 300kbps kf=999 realtime cpu8
		{0, 1, 1, 0, 1}, // CBR 700kbps kf=30
		{1, 0, 0, 0, 0}, // VBR 300kbps kf=999
		{1, 1, 1, 1, 0}, // VBR 700kbps kf=30 good
		{0, 2, 0, 0, 2}, // CBR 1200kbps cpu4
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg := newVP9LongFixtureFuzzCase(data)
		opts := cfg.buildOpts()
		sources := cfg.buildSources()

		sum := sha256.Sum256(data)
		label := "fuzz-vp9-long-rc-" + hex.EncodeToString(sum[:4])
		t.Logf("%s rc=%v kbps=%d kf=%d cpu=%d frames=%d",
			label, opts.RateControlMode, cfg.targetKbps, cfg.kfInterval, cfg.cpuUsed, len(sources))

		govpxFrames := encodeVP9FramesWithGovpx(t, opts, sources, nil)
		libvpxFrames := encodeVP9FramesWithLibvpxOracle(t, sources, cfg.extraArgs)

		prefix := matchedVP9FramePrefixLength(govpxFrames, libvpxFrames)
		t.Logf("%s matched-prefix=%d/%d frames", label, prefix, min(len(govpxFrames), len(libvpxFrames)))
		assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

// matchedVP9FramePrefixLength returns the largest N such that the SHA-256s of
// got[:N] and want[:N] match frame-by-frame.
func matchedVP9FramePrefixLength(got, want [][]byte) int {
	n := min(len(got), len(want))
	for i := 0; i < n; i++ {
		if sha256.Sum256(got[i]) != sha256.Sum256(want[i]) {
			return i
		}
	}
	return n
}

type vp9LongFixtureFuzzCase struct {
	width      int
	height     int
	frames     int
	targetKbps int
	kfInterval int
	rcMode     RateControlMode
	deadline   Deadline
	cpuUsed    int
	extraArgs  []string
}

func newVP9LongFixtureFuzzCase(data []byte) vp9LongFixtureFuzzCase {
	r := vp9FuzzByteCursor{data: data}
	rcPool := [...]RateControlMode{RateControlCBR, RateControlVBR}
	kbpsPool := [...]int{300, 700, 1200}
	kfPool := [...]int{999, 30, 60}
	deadlinePool := [...]Deadline{DeadlineRealtime, DeadlineGoodQuality}
	cpuPool := [...]int{8, 4, 0}

	c := vp9LongFixtureFuzzCase{
		width:      64,
		height:     64,
		frames:     256,
		rcMode:     rcPool[r.pick(len(rcPool))],
		targetKbps: kbpsPool[r.pick(len(kbpsPool))],
		kfInterval: kfPool[r.pick(len(kfPool))],
		deadline:   deadlinePool[r.pick(len(deadlinePool))],
		cpuUsed:    cpuPool[r.pick(len(cpuPool))],
	}
	endUsage := "cbr"
	if c.rcMode == RateControlVBR {
		endUsage = "vbr"
	}
	c.extraArgs = []string{
		"--end-usage=" + endUsage,
		"--target-bitrate=" + strconv.Itoa(c.targetKbps),
		"--cpu-used=" + strconv.Itoa(c.cpuUsed),
		"--kf-min-dist=0",
		"--kf-max-dist=" + strconv.Itoa(c.kfInterval),
	}
	if c.deadline == DeadlineGoodQuality {
		// vpxenc-vp9 defaults to --rt; override only for good-quality.
		c.extraArgs = append(c.extraArgs, "--good")
	}
	return c
}

func (c *vp9LongFixtureFuzzCase) buildOpts() VP9EncoderOptions {
	return VP9EncoderOptions{
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
	return newVP9YCbCrFuzzSources(c.width, c.height, c.frames)
}
