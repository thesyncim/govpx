package govpx

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestOracleFirstPassStatsCompare(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run first-pass oracle comparison")
	}
	vpxenc := findVpxenc(t)

	const (
		width      = 32
		height     = 32
		fps        = 30
		targetKbps = 400
	)
	cases := []struct {
		name   string
		frames []Image
	}{
		{name: "ramp", frames: firstPassOracleFrames(3, func(i int) Image {
			return firstPassOracleRampFrame(width, height, i)
		})},
		{name: "y4m-shaped", frames: firstPassOracleFrames(4, func(i int) Image {
			return firstPassOracleY4MFrame(width, height, i)
		})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  60,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           0,
			}
			govpxStats := captureGovpxFirstPassStats(t, opts, tc.frames)
			libvpxStats := captureLibvpxFirstPassStats(t, vpxenc, "firstpass-"+tc.name, opts, targetKbps, tc.frames)
			compareFirstPassStats(t, govpxStats, libvpxStats)
		})
	}
}

func firstPassOracleFrames(count int, fn func(int) Image) []Image {
	frames := make([]Image, count)
	for i := range frames {
		frames[i] = fn(i)
	}
	return frames
}

func firstPassOracleRampFrame(width int, height int, shift int) Image {
	img := testImage(width, height)
	for y := range height {
		for x := range width {
			v := min(max(32+(y+shift)*3+(x+shift)*2, 0), 235)
			img.Y[y*img.YStride+x] = byte(v)
		}
	}
	for i := range img.U {
		img.U[i] = 128
	}
	for i := range img.V {
		img.V[i] = 128
	}
	return img
}

func firstPassOracleY4MFrame(width int, height int, shift int) Image {
	img := testImage(width, height)
	for y := range height {
		for x := range width {
			v := min(max(64+(y+shift)*3+(x+shift)*2, 0), 235)
			img.Y[y*img.YStride+x] = byte(v)
		}
	}
	px := 4 + shift
	py := 4 + shift
	for dy := range 8 {
		for dx := range 8 {
			x := px + dx
			y := py + dy
			if x >= 0 && x < width && y >= 0 && y < height {
				img.Y[y*img.YStride+x] = 16
			}
		}
	}
	for i := range img.U {
		img.U[i] = 128
	}
	for i := range img.V {
		img.V[i] = 128
	}
	return img
}

func captureGovpxFirstPassStats(t *testing.T, opts EncoderOptions, frames []Image) []FirstPassFrameStats {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	stats := make([]FirstPassFrameStats, len(frames))
	for i, frame := range frames {
		stats[i], err = enc.CollectFirstPassStats(frame, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d] returned error: %v", i, err)
		}
	}
	return FinalizeFirstPassStats(stats)
}

func captureLibvpxFirstPassStats(t *testing.T, vpxenc string, name string, opts EncoderOptions, targetKbps int, frames []Image) []FirstPassFrameStats {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	fpfPath := filepath.Join(dir, name+".fpf")
	writeEncoderValidationI420(t, yuvPath, frames)
	deadlineArg := "--good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "--best"
	case DeadlineRealtime:
		deadlineArg = "--rt"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--passes=2",
		"--pass=1",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(frames)),
		"--output=" + ivfPath,
		yuvPath,
	}
	cmd := exec.Command(vpxenc, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc first pass failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(fpfPath)
	if err != nil {
		t.Fatalf("ReadFile %s returned error: %v", fpfPath, err)
	}
	return parseLibvpxFirstPassStats(t, data)
}

func parseLibvpxFirstPassStats(t *testing.T, data []byte) []FirstPassFrameStats {
	t.Helper()
	const fields = 18
	const packetSize = fields * 8
	if len(data) == 0 || len(data)%packetSize != 0 {
		t.Fatalf("libvpx first-pass stats size = %d, want non-zero multiple of %d", len(data), packetSize)
	}
	stats := make([]FirstPassFrameStats, len(data)/packetSize)
	for i := range stats {
		offset := i * packetSize
		read := func(field int) float64 {
			return math.Float64frombits(binary.LittleEndian.Uint64(data[offset+field*8 : offset+(field+1)*8]))
		}
		stats[i] = FirstPassFrameStats{
			Frame:               uint64(read(0)),
			IntraError:          read(1),
			CodedError:          read(2),
			SSIMWeightedPredErr: read(3),
			PcntInter:           read(4),
			PcntMotion:          read(5),
			PcntSecondRef:       read(6),
			PcntNeutral:         read(7),
			MVr:                 read(8),
			MVrAbs:              read(9),
			MVc:                 read(10),
			MVcAbs:              read(11),
			MVrv:                read(12),
			MVcv:                read(13),
			MVInOutCount:        read(14),
			NewMVCount:          read(15),
			Duration:            read(16),
			Count:               read(17),
			IsTotal:             i == len(stats)-1,
		}
	}
	return stats
}

func compareFirstPassStats(t *testing.T, govpxStats []FirstPassFrameStats, libvpxStats []FirstPassFrameStats) {
	t.Helper()
	if len(govpxStats) != len(libvpxStats) {
		t.Fatalf("first-pass stats length = %d, want %d", len(govpxStats), len(libvpxStats))
	}
	for i := range govpxStats {
		g := govpxStats[i]
		l := libvpxStats[i]
		label := "frame " + strconv.Itoa(i)
		if l.IsTotal {
			label = "total"
		}
		if g.IsTotal != l.IsTotal {
			t.Fatalf("%s IsTotal = %v, want %v", label, g.IsTotal, l.IsTotal)
		}
		if g.Frame != l.Frame {
			t.Errorf("%s Frame = %d, want %d", label, g.Frame, l.Frame)
		}
		// The first-pass predictor/reconstruction path is quality-gated, not
		// universally bit-exact. Keep integer-derived errors within a tiny
		// post-shift tolerance while requiring the mode/MV percentages below
		// to match exactly on this deterministic corpus.
		assertFirstPassClose(t, label, "IntraError", g.IntraError, l.IntraError, 2, 0)
		assertFirstPassClose(t, label, "CodedError", g.CodedError, l.CodedError, 2, 0)
		assertFirstPassClose(t, label, "SSIMWeightedPredErr", g.SSIMWeightedPredErr, l.SSIMWeightedPredErr, 3, 1e-3)
		assertFirstPassClose(t, label, "PcntInter", g.PcntInter, l.PcntInter, 1e-12, 0)
		assertFirstPassClose(t, label, "PcntMotion", g.PcntMotion, l.PcntMotion, 1e-12, 0)
		assertFirstPassClose(t, label, "PcntSecondRef", g.PcntSecondRef, l.PcntSecondRef, 1e-12, 0)
		assertFirstPassClose(t, label, "PcntNeutral", g.PcntNeutral, l.PcntNeutral, 1e-12, 0)
		assertFirstPassClose(t, label, "MVr", g.MVr, l.MVr, 1e-12, 0)
		assertFirstPassClose(t, label, "MVrAbs", g.MVrAbs, l.MVrAbs, 1e-12, 0)
		assertFirstPassClose(t, label, "MVc", g.MVc, l.MVc, 1e-12, 0)
		assertFirstPassClose(t, label, "MVcAbs", g.MVcAbs, l.MVcAbs, 1e-12, 0)
		assertFirstPassClose(t, label, "MVrv", g.MVrv, l.MVrv, 1e-12, 0)
		assertFirstPassClose(t, label, "MVcv", g.MVcv, l.MVcv, 1e-12, 0)
		assertFirstPassClose(t, label, "MVInOutCount", g.MVInOutCount, l.MVInOutCount, 1e-12, 0)
		assertFirstPassClose(t, label, "NewMVCount", g.NewMVCount, l.NewMVCount, 1e-12, 0)
		assertFirstPassClose(t, label, "Count", g.Count, l.Count, 0, 0)
		if l.Duration <= 0 {
			t.Errorf("%s libvpx Duration = %v, want positive duration", label, l.Duration)
		}
	}
}

func assertFirstPassClose(t *testing.T, label string, field string, got float64, want float64, absTol float64, relTol float64) {
	t.Helper()
	limit := absTol
	if relTol > 0 {
		limit = math.Max(limit, math.Abs(want)*relTol)
	}
	if math.Abs(got-want) > limit {
		t.Errorf("%s %s = %.15g, want %.15g", label, field, got, want)
	}
}
