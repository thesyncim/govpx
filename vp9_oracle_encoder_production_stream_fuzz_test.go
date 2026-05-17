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

// FuzzVP9EncoderProductionStreamByteParity mirrors
// FuzzEncoderProductionStreamByteParity for VP9: option-grid fuzz against the
// vpxenc-vp9 oracle at small + production resolutions, asserting strict per-
// frame byte parity. Seeds where govpx VP9 has documented divergences from
// libvpx VP9 fail visibly here and land as testdata/fuzz seeds for follow-up.
//
// Gated by GOVPX_WITH_ORACLE=1 and a built vpxenc-vp9 binary.
func FuzzVP9EncoderProductionStreamByteParity(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 option-grid byte-parity fuzz")
	}
	requireVP9VpxencOracleFuzz(f)
	// Each seed is (resBucket, deadlineBucket, cpuBucket, rcBucket, featBucket,
	// threadsBucket, tileBucket, qBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0}, // realtime cpu8 64x64 fixed-q
		{1, 0, 0, 0, 0, 0, 0, 0}, // realtime cpu8 128x128 fixed-q
		{2, 0, 0, 0, 0, 1, 0, 0}, // realtime 320x180 threads=2
		{3, 0, 0, 0, 0, 1, 1, 0}, // realtime 640x360 threads=2 tile-col=1
		{4, 0, 0, 0, 0, 2, 1, 0}, // realtime 854x480 threads=4 tile-col=1
		{5, 0, 0, 0, 0, 2, 2, 0}, // realtime 1280x720 threads=4 tile-col=2
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg := newVP9OptionGridFuzzCase(data)
		opts := cfg.buildOpts()
		sources := cfg.buildSources()

		sum := sha256.Sum256(data)
		label := "fuzz-vp9-option-grid-" + cfg.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d cpu=%d threads=%d tile-cols=%d frames=%d",
			label, opts.Width, opts.Height, opts.CpuUsed, opts.Threads,
			cfg.tileCols, len(sources))

		govpxFrames := encodeVP9FramesWithGovpx(t, opts, sources, nil)
		libvpxFrames := encodeVP9FramesWithLibvpxOracle(t, sources, cfg.extraArgs)
		assertVP9SegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type vp9OptionGridFuzzCase struct {
	width     int
	height    int
	frames    int
	cpuUsed   int8
	threads   int
	tileCols  int
	cqLevel   int
	minQ      int
	maxQ      int
	name      string
	extraArgs []string
}

func newVP9OptionGridFuzzCase(data []byte) vp9OptionGridFuzzCase {
	r := vp9FuzzByteCursor{data: data}
	resPool := [...]struct {
		w, h, frames int
	}{
		{64, 64, 4},
		{128, 128, 4},
		{320, 180, 3},
		{640, 360, 2},
		{854, 480, 2},
		{1280, 720, 2},
	}
	cpuPool := [...]int{8, 6, 4, 2, 0}
	threadPool := [...]int{0, 2, 4}
	tilePool := [...]int{0, 1, 2}
	qPool := [...]int{32, 20, 48, 4, 56}

	// Skip deadlineBucket / rcBucket / featBucket consumption (1, 2, 3, 4) so
	// the seeds still land on the configuration they document.
	res := resPool[r.pick(len(resPool))]
	_ = r.pick(3)
	cpu := cpuPool[r.pick(len(cpuPool))]
	_ = r.pick(2)
	_ = r.pick(2)
	threads := threadPool[r.pick(len(threadPool))]
	tileCols := tilePool[r.pick(len(tilePool))]
	cqLevel := qPool[r.pick(len(qPool))]

	c := vp9OptionGridFuzzCase{
		width:    res.w,
		height:   res.h,
		frames:   res.frames,
		cpuUsed:  int8(cpu),
		threads:  threads,
		tileCols: tileCols,
		cqLevel:  cqLevel,
		minQ:     4,
		maxQ:     56,
	}
	c.name = "w" + strconv.Itoa(c.width) + "h" + strconv.Itoa(c.height)
	c.extraArgs = []string{
		"--cpu-used=" + strconv.Itoa(int(c.cpuUsed)),
		"--cq-level=" + strconv.Itoa(c.cqLevel),
		"--min-q=" + strconv.Itoa(c.minQ),
		"--max-q=" + strconv.Itoa(c.maxQ),
	}
	if c.threads > 0 {
		c.extraArgs = append(c.extraArgs, "--threads="+strconv.Itoa(c.threads))
	}
	if c.tileCols > 0 {
		c.extraArgs = append(c.extraArgs, "--tile-columns="+strconv.Itoa(c.tileCols))
	}
	return c
}

func (c *vp9OptionGridFuzzCase) buildOpts() VP9EncoderOptions {
	opts := VP9EncoderOptions{
		Width:               c.width,
		Height:              c.height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        c.minQ,
		MaxQuantizer:        c.maxQ,
		CQLevel:             c.cqLevel,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		Deadline:            DeadlineRealtime,
		CpuUsed:             c.cpuUsed,
		Threads:             c.threads,
		MaxKeyframeInterval: 128,
	}
	// Tile-column count is not part of VP9EncoderOptions on the govpx side
	// (the encoder derives it from Threads + Log2TileRows); only Log2TileRows
	// is exposed. We mirror tileCols on the libvpx CLI side and let the govpx
	// encoder pick the matching configuration via its standard derivation.
	return opts
}

func (c *vp9OptionGridFuzzCase) buildSources() []*image.YCbCr {
	return newVP9YCbCrFuzzSources(c.width, c.height, c.frames)
}
