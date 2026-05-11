//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestOracleEncoderStreamByteParity is the strictest possible parity
// gate: it runs govpx and the patched libvpx vpxenc-oracle on the same
// I420 fixture under matching options and asserts the encoded frame
// payloads (skipping the IVF container/frame-headers) are SHA-256
// identical.
//
// Each subtest pins one (resolution × deadline × cpu-used × fixture)
// triple. A subtest that fails here means the encoder has diverged from
// libvpx in a way that affects the bitstream — quantization decisions,
// mode decisions, loop-filter level, token writing order, or anything
// downstream of those — and is the immediate signal that the plan.md
// "100% byte parity" target has regressed for that config.
func TestOracleEncoderStreamByteParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 4
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	splitmv64 := fixture{name: "splitmv-64x64", w: 64, h: 64, source: encoderValidationSplitMVQuadrantFrame}

	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fx       fixture
	}{
		{name: "realtime-cbr-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64},
		{name: "realtime-cbr-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning64},
		{name: "realtime-cbr-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64},
		{name: "good-quality-cbr-cpu5", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: panning64},
		{name: "best-quality-cbr-cpu0-splitmv", deadline: DeadlineBestQuality, cpuUsed: 0, fx: splitmv64},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, nil)

			if len(govpxFrames) != len(libvpxFrames) {
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			for i := range govpxFrames {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				if gHash != lHash {
					firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
					gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
					lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
					t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
						i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
						gFP, lFP, gIsKey, lIsKey,
						hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
				}
			}
		})
	}
}

// encodeFramesWithGovpx returns the raw per-frame VP8 packet payloads
// produced by govpx for the supplied sources.
func encodeFramesWithGovpx(t *testing.T, opts EncoderOptions, sources []Image) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto frame %d dropped, want full stream", i)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

// encodeFramesWithLibvpxOracle runs vpxenc-oracle on the supplied I420
// fixture and returns the per-frame VP8 packet payloads extracted from
// the resulting IVF file.
func encodeFramesWithLibvpxOracle(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) [][]byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	writeEncoderValidationI420(t, yuvPath, sources)
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
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=4",
		"--max-q=56",
		"--end-usage=cbr",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
	}
	args = append(args, extraArgs...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read %s: %v", ivfPath, err)
	}
	return parseIVFFramePayloads(t, data)
}

// firstByteDiff returns the byte offset of the first divergence between
// a and b, or -1 if the prefixes match up to min(len(a), len(b)).
func firstByteDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

// parseVP8FramePartitionSizes returns the first-partition byte length
// declared in the VP8 frame header plus whether the frame is marked as
// a keyframe. Returns (0, false) when the payload is too short.
func parseVP8FramePartitionSizes(p []byte) (firstPart int, isKeyframe bool) {
	if len(p) < 3 {
		return 0, false
	}
	tag := uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
	isKeyframe = (tag & 1) == 0
	firstPart = int((tag >> 5) & 0x7FFFF)
	return firstPart, isKeyframe
}

// parseIVFFramePayloads strips the 32-byte IVF header and the 12-byte
// per-frame headers, returning the raw VP8 frame payload bytes.
func parseIVFFramePayloads(t *testing.T, data []byte) [][]byte {
	t.Helper()
	if len(data) < 32 || string(data[:4]) != "DKIF" {
		t.Fatalf("ivf: missing DKIF magic (have %d bytes, prefix=%q)", len(data), data[:min(len(data), 4)])
	}
	pos := 32
	var out [][]byte
	for pos+12 <= len(data) {
		size := binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 12
		end := pos + int(size)
		if end > len(data) {
			t.Fatalf("ivf: frame size %d at pos %d overflows %d-byte buffer", size, pos-12, len(data))
		}
		out = append(out, bytes.Clone(data[pos:end]))
		pos = end
	}
	return out
}
