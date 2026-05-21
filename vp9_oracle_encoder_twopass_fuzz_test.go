//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzVP9EncoderTwoPassByteParity exercises the libvpx vpxenc-vp9 two-pass
// CLI path over a fuzz-driven option grid and verifies the IVF shape. It grows
// into a govpx second-pass comparison once this target constructs finalized
// VP9FirstPassFrameStats for cfg.opts.
//
// Gated by GOVPX_WITH_ORACLE=1 plus a built vpxenc-vp9 binary.
func FuzzVP9EncoderTwoPassByteParity(f *testing.F) {
	coracletest.SkipWithoutOracle(f, "VP9 two-pass byte-parity fuzz")
	coracletest.VpxencVP9(f)
	// Each seed is (bitrateBucket, kfBucket, threadsBucket, cpuBucket, framesBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0},
		{1, 0, 0, 0, 0},
		{0, 1, 0, 0, 0},
		{0, 0, 1, 0, 0},
		{2, 0, 0, 1, 0},
		{1, 1, 1, 0, 1},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg := newVP9TwoPassFuzzCase(data)
		sources := cfg.buildSources()
		opts := cfg.buildOpts()

		sum := sha256.Sum256(data)
		label := "fuzz-vp9-twopass-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d kbps=%d kf=%d threads=%d cpu=%d frames=%d",
			label, opts.Width, opts.Height, cfg.targetKbps, cfg.kfInterval,
			cfg.threads, cfg.cpuUsed, len(sources))

		// Libvpx VP9 two-pass reference. This keeps the CLI bridge and IVF
		// parser covered while the govpx stats-to-second-pass comparison is
		// staged.
		var raw []byte
		for _, src := range sources {
			raw = vp9test.AppendI420(raw, src)
		}
		ivf, diag, err := coracle.VpxencVP9TwoPassEncodeI420(raw, cfg.width, cfg.height,
			len(sources), cfg.extraArgs...)
		if err != nil {
			t.Fatalf("vpxenc-vp9 two-pass encode failed: %v\n%s", err, diag)
		}
		libvpxFrames := parseVP9IVFFrames(t, ivf)
		if len(libvpxFrames) == 0 {
			t.Fatalf("%s: vpxenc-vp9 two-pass produced no frames", label)
		}
		if len(libvpxFrames) != len(sources) {
			t.Errorf("%s: libvpx VP9 two-pass IVF frame count = %d, want %d",
				label, len(libvpxFrames), len(sources))
		}
		// TODO(vp9-twopass-fuzz): construct finalized first-pass stats for
		// sources, set cfg.opts.TwoPassStats, and compare govpx/libvpx packets.
	})
}

type vp9TwoPassFuzzCase struct {
	width      int
	height     int
	targetKbps int
	kfInterval int
	threads    int
	cpuUsed    int
	frames     int
	extraArgs  []string
}

func newVP9TwoPassFuzzCase(data []byte) vp9TwoPassFuzzCase {
	r := testutil.NewByteCursor(data)
	kbpsPool := [...]int{300, 500, 700}
	kfPool := [...]int{30, 60, 120}
	threadPool := [...]int{0, 2}
	cpuPool := [...]int{0, 4}
	framesPool := [...]int{4, 6, 8}

	c := vp9TwoPassFuzzCase{
		width:      32,
		height:     32,
		targetKbps: kbpsPool[r.Pick(len(kbpsPool))],
		kfInterval: kfPool[r.Pick(len(kfPool))],
		threads:    threadPool[r.Pick(len(threadPool))],
		cpuUsed:    cpuPool[r.Pick(len(cpuPool))],
		frames:     framesPool[r.Pick(len(framesPool))],
	}
	c.extraArgs = []string{
		"--cpu-used=" + strconv.Itoa(c.cpuUsed),
		"--kf-min-dist=" + strconv.Itoa(c.kfInterval),
		"--kf-max-dist=" + strconv.Itoa(c.kfInterval),
		"--target-bitrate=" + strconv.Itoa(c.targetKbps),
	}
	if c.threads > 0 {
		c.extraArgs = append(c.extraArgs, "--threads="+strconv.Itoa(c.threads))
	}
	return c
}

func (c *vp9TwoPassFuzzCase) buildSources() []*image.YCbCr {
	return newVP9YCbCrFuzzSources(c.width, c.height, c.frames)
}

func (c *vp9TwoPassFuzzCase) buildOpts() VP9EncoderOptions {
	return VP9EncoderOptions{
		Width:               c.width,
		Height:              c.height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlVBR,
		TargetBitrateKbps:   c.targetKbps,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: c.kfInterval,
		Deadline:            DeadlineGoodQuality,
		CpuUsed:             int8(c.cpuUsed),
		Threads:             c.threads,
	}
}
