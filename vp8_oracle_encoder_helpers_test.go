//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8corpus"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type copyReferenceChecksum struct {
	Frame    int
	Ref      string
	YAdler32 uint32
	UAdler32 uint32
	VAdler32 uint32
}

type copyReferenceCheck struct {
	ref  ReferenceFrame
	name string
}

type copyReferenceSet struct {
	ref          ReferenceFrame
	name         string
	panningIndex int
}

func copyReferenceCheckApply(label string, refs ...ReferenceFrame) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		dst := newTestImage(e.opts.Width, e.opts.Height)
		for _, ref := range refs {
			mustRuntime(t, label+" CopyReferenceFrame("+copyReferenceName(ref)+")", e.CopyReferenceFrame(ref, &dst))
		}
	}
}

func copyReferenceName(ref ReferenceFrame) string {
	switch ref {
	case ReferenceLast:
		return "last"
	case ReferenceGolden:
		return "golden"
	case ReferenceAltRef:
		return "altref"
	default:
		return strconv.Itoa(int(ref))
	}
}

func copyReferenceParityOptions(width, height int) EncoderOptions {
	return EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
}

func emptyCopyReferenceScript(frames int) []string {
	script := make([]string, frames)
	for i := range script {
		script[i] = "-"
	}
	return script
}

func captureLibvpxCopyReferenceChecksums(t *testing.T, driver, name string, opts EncoderOptions, sources []Image, flags []EncodeFlags, script []string) []copyReferenceChecksum {
	t.Helper()
	return captureLibvpxCopyReferenceChecksumsWithExtraArgs(t, driver, name, opts, sources, flags, script, nil)
}

func captureLibvpxCopyReferenceChecksumsWithExtraArgs(t *testing.T, driver, name string, opts EncoderOptions, sources []Image, flags []EncodeFlags, script []string, extraArgs []string) []copyReferenceChecksum {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), name+".log")
	args := []string{
		"--control-script=" + strings.Join(script, ","),
		"--copy-ref-log=" + logPath,
	}
	args = append(args, extraArgs...)
	_ = encodeFramesWithFrameFlagsDriver(t, driver, name, opts, opts.TargetBitrateKbps, sources, flags, args)
	return readCopyReferenceChecksumLog(t, logPath)
}

func captureGovpxCopyReferenceChecksums(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, sets map[int][]copyReferenceSet, checks map[int][]copyReferenceCheck) []copyReferenceChecksum {
	t.Helper()
	return captureGovpxCopyReferenceChecksumsWithApply(t, opts, sources, flags, sets, nil, checks)
}

func captureGovpxCopyReferenceChecksumsWithApply(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, sets map[int][]copyReferenceSet, apply map[int]func(*testing.T, *VP8Encoder), checks map[int][]copyReferenceCheck) []copyReferenceChecksum {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()

	maxPixels := opts.Width * opts.Height
	for _, src := range sources {
		if pixels := src.Width * src.Height; pixels > maxPixels {
			maxPixels = pixels
		}
	}
	buf := make([]byte, maxPixels*4+4096)
	out := make([]copyReferenceChecksum, 0)
	for i, src := range sources {
		for _, set := range sets[i] {
			img := encoderValidationPanningFrame(enc.opts.Width, enc.opts.Height, set.panningIndex)
			if err := enc.SetReferenceFrame(set.ref, img); err != nil {
				t.Fatalf("frame %d SetReferenceFrame(%s): %v", i, set.name, err)
			}
		}
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		for _, check := range checks[i] {
			dst := testImage(enc.opts.Width, enc.opts.Height)
			if err := enc.CopyReferenceFrame(check.ref, &dst); err != nil {
				t.Fatalf("frame %d CopyReferenceFrame(%s): %v", i, check.name, err)
			}
			out = append(out, copyReferenceImageChecksum(i, check.name, dst))
		}
		var frameFlags EncodeFlags
		if i < len(flags) {
			frameFlags = flags[i]
		}
		if _, err := enc.EncodeInto(buf, src, uint64(i), 1, frameFlags); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	return out
}

func copyReferenceImageChecksum(frame int, ref string, img Image) copyReferenceChecksum {
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	return copyReferenceChecksum{
		Frame:    frame,
		Ref:      ref,
		YAdler32: planeAdler32(img.Y, img.Width, img.Height, img.YStride),
		UAdler32: planeAdler32(img.U, uvWidth, uvHeight, img.UStride),
		VAdler32: planeAdler32(img.V, uvWidth, uvHeight, img.VStride),
	}
}

func readCopyReferenceChecksumLog(t *testing.T, path string) []copyReferenceChecksum {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open copy-ref log %s: %v", path, err)
	}
	defer file.Close()

	var out []copyReferenceChecksum
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := parseCopyReferenceLogFields(t, line)
		out = append(out, copyReferenceChecksum{
			Frame:    parseCopyReferenceLogInt(t, fields, "frame"),
			Ref:      fields["ref"],
			YAdler32: parseCopyReferenceLogUint32(t, fields, "y_adler32"),
			UAdler32: parseCopyReferenceLogUint32(t, fields, "u_adler32"),
			VAdler32: parseCopyReferenceLogUint32(t, fields, "v_adler32"),
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan copy-ref log %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("copy-ref log %s had no entries", path)
	}
	return out
}

func parseCopyReferenceLogFields(t *testing.T, line string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, field := range strings.Fields(line) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			t.Fatalf("invalid copy-ref log field %q in %q", field, line)
		}
		out[key] = value
	}
	for _, key := range []string{"frame", "ref", "y_adler32", "u_adler32", "v_adler32"} {
		if out[key] == "" {
			t.Fatalf("copy-ref log line %q missing %s", line, key)
		}
	}
	return out
}

func parseCopyReferenceLogInt(t *testing.T, fields map[string]string, key string) int {
	t.Helper()
	v, err := strconv.Atoi(fields[key])
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, fields[key], err)
	}
	return v
}

func parseCopyReferenceLogUint32(t *testing.T, fields map[string]string, key string) uint32 {
	t.Helper()
	v, err := strconv.ParseUint(fields[key], 10, 32)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, fields[key], err)
	}
	return uint32(v)
}

func assertCopyReferenceChecksumsEqual(t *testing.T, got []copyReferenceChecksum, want []copyReferenceChecksum) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CopyReferenceFrame checksum mismatch\n govpx: %s\nlibvpx: %s", formatCopyReferenceChecksums(got), formatCopyReferenceChecksums(want))
	}
}

func formatCopyReferenceChecksums(checksums []copyReferenceChecksum) string {
	parts := make([]string, len(checksums))
	for i, c := range checksums {
		parts[i] = fmt.Sprintf("frame=%d ref=%s y=%08x u=%08x v=%08x", c.Frame, c.Ref, c.YAdler32, c.UAdler32, c.VAdler32)
	}
	return strings.Join(parts, "; ")
}

func TestVP8OracleExternalEncoderTestDataValidation(t *testing.T) {
	vp8test.RequireOracle(t, "external encoder source tests")
	root, ok := vp8corpus.SourceRoot(t)
	if !ok {
		return
	}
	oracle := vp8test.NewChecksumOracle(t)
	vpxenc := vp8test.Vpxenc(t)
	paths := vp8corpus.FindSources(t, root)
	if len(paths) == 0 {
		t.Fatalf("no encoder source files found under %s", root)
	}
	vp8corpus.AssertSourceMinimum(t, paths)

	maxFrames := vp8corpus.SourceFrameLimit(t)
	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			clip, ok := vp8corpus.ReadSourceClip(t, path, maxFrames)
			if !ok {
				t.Skipf("%s is not a supported 8-bit 4:2:0 source clip", path)
			}
			frames := vp8SourceClipImages(clip)
			targetKbps := vp8corpus.SourceTargetKbps(clip.Width, clip.Height, clip.FPS)
			tc := encoderValidationCase{
				name:       clip.Name,
				width:      clip.Width,
				height:     clip.Height,
				frames:     len(frames),
				fps:        clip.FPS,
				targetKbps: targetKbps,
				opts: encoderValidationOptions(clip.Width, clip.Height, clip.FPS, targetKbps, func(opts *EncoderOptions) {
					opts.KeyFrameInterval = 120
				}),
				minPSNR:          20.0,
				minSSIM:          0.75,
				minFramePSNR:     18.0,
				minFrameSSIM:     0.65,
				maxPSNRGap:       2.0,
				maxSSIMGap:       0.01,
				maxFramePSNRGap:  4.0,
				maxFrameSSIMGap:  0.02,
				maxRateHigh:      250.0,
				maxRateLow:       100.0,
				checkInterFrames: len(frames) > 1,
			}

			got := encodeGopvxValidationCorpus(t, tc, frames)
			gotChecksums := decodeIVFChecksums(t, got.ivf)
			wantChecksums := oracle.Frames(t, got.ivf)
			assertFrameChecksumsEqual(t, "external govpx encode decoded by libvpx", gotChecksums, wantChecksums)
			assertGopvxEncoderValidationFeatures(t, got.ivf, tc)
			assertEncoderValidationQuality(t, "external govpx", got.quality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "external govpx", got.outputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)

			libvpxIVF := encodeLibvpxValidationCorpus(t, vpxenc, tc, frames)
			libvpxGotChecksums := decodeIVFChecksums(t, libvpxIVF)
			libvpxWantChecksums := oracle.Frames(t, libvpxIVF)
			assertFrameChecksumsEqual(t, "external libvpx encode decoded by govpx", libvpxGotChecksums, libvpxWantChecksums)
			libvpxQuality := qualityMetricsForIVF(t, libvpxIVF, frames)
			libvpxOutputKbps := encoderValidationOutputKbps(len(libvpxIVF)-testutil.IVFFileHeaderSize-len(frames)*testutil.IVFFrameHeaderSize, tc.fps, len(frames))
			logEncoderValidationQuality(t, got.quality, got.outputKbps, libvpxQuality, libvpxOutputKbps)
			assertEncoderValidationQuality(t, "external libvpx", libvpxQuality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "external libvpx", libvpxOutputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQualityGap(t, got.quality, libvpxQuality, tc)
		})
	}
}

func captureLibvpxFirstPassStats(t *testing.T, vpxenc string, opts EncoderOptions, targetKbps int, frames []Image) []FirstPassFrameStats {
	t.Helper()
	deadline := "good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadline = "best"
	case DeadlineRealtime:
		deadline = "rt"
	}
	data, diag, err := vp8test.VpxencVP8FirstPassStatsI420(
		encoderValidationI420Bytes(t, frames),
		vp8test.VpxencVP8Config{
			BinaryPath:        vpxenc,
			Width:             opts.Width,
			Height:            opts.Height,
			Frames:            len(frames),
			Deadline:          deadline,
			CPUUsed:           opts.CpuUsed,
			TargetBitrateKbps: targetKbps,
			MinQ:              opts.MinQuantizer,
			MaxQ:              opts.MaxQuantizer,
			Timebase:          "1/" + strconv.Itoa(opts.FPS),
			FPS:               strconv.Itoa(opts.FPS) + "/1",
			ExtraArgs:         []string{"--end-usage=vbr"},
		},
	)
	if err != nil {
		t.Fatalf("vpxenc first pass failed: %v\n%s", err, diag)
	}
	return parseLibvpxFirstPassStats(t, data)
}

func TestVP8OracleFirstPassStatsCompare(t *testing.T) {
	vp8test.RequireOracle(t, "first-pass oracle comparison")
	vpxenc := vp8test.Vpxenc(t)

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
			libvpxStats := captureLibvpxFirstPassStats(t, vpxenc, opts, targetKbps, tc.frames)
			compareFirstPassStats(t, govpxStats, libvpxStats)
		})
	}
}

// FuzzEncoderFrameFlags drives randomized per-frame [EncodeFlags] schedules
// through both govpx and the libvpx-side `vpxenc-frameflags` companion driver
// and asserts byte-exact stream parity at every emitted frame.
//
// The shape mirrors FuzzVP8OracleEncoderRuntimeControlTransitions: the fuzz []byte
// derives a (width, height, cpu_used, frames) config plus a per-frame flag-set
// sequence drawn from a fixed pool, and govpx writes failing inputs to
// testdata/fuzz/FuzzEncoderFrameFlags for replay as plain regression tests
// under ordinary go test runs.
//
// Note: under 16-way parallel fuzz workers this harness has been observed to
// report intermittent parity divergences that do not reproduce in
// single-worker regression mode (the minimized seed always replays as PASS
// when fed back through go test -run=). That intermittent gap is not yet
// understood and the corresponding minimized seeds are intentionally NOT
// committed under testdata/fuzz/FuzzEncoderFrameFlags so the regression gate
// stays deterministic. When a truly reproducible divergence is found, follow
// the existing TestVP8OracleEncoderStreamByteParityFrameFlags pattern (pin it
// with a `limit:` case in vp8_oracle_encoder_stream_parity_frame_flags_test.go)
// rather than relying on a fuzz seed alone.
func FuzzEncoderFrameFlags(f *testing.F) {
	vp8test.RequireOracleF(f, "frame-flags fuzz parity")

	seeds := [][]byte{
		// Force-KF on frame 1 (smallest scenario that produces both a real key
		// and a real inter packet).
		{0, 0, 0, 0, 0, 1},
		// Hidden frame on frame 1, then force-KF on frame 3.
		{0, 0, 0, 0, 0, 8, 0, 1},
		// Run of NO_UPD_LAST inter frames — exercises the libvpx upd-mask path.
		{0, 0, 0, 0, 0, 0, 4, 4, 4, 4, 4, 4, 4},
		// Mixed: ForceGF, ForceARF, NoUpdateEntropy.
		{0, 0, 0, 0, 0, 0, 7, 0, 6, 0, 5},
		// All-byte saturation seed; r.pick cycles through the flag pool.
		{0xff, 0xaa, 0x55, 0x33, 0x11, 0x77, 0x88, 0x99, 0x44, 0x22, 0x10},
		// Long zero seed (all default flags) — ensures the parity infrastructure
		// still trips a recordable failure if the no-op driver call regresses.
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := vp8test.VpxencFrameFlags(t)
		tc := encoderFrameFlagsFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-frame-flags-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d cpu=%d frames=%d flags=%s", label, tc.opts.Width, tc.opts.Height, tc.opts.CpuUsed, len(tc.sources), encoderFrameFlagsLogString(tc.flags))

		govpxFrames := encodeFramesWithGovpxFrameFlags(t, tc.opts, tc.sources, tc.flags)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, nil)
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type encoderFrameFlagsFuzzCase struct {
	name       string
	opts       EncoderOptions
	targetKbps int
	sources    []Image
	flags      []EncodeFlags
}

// encoderFrameFlagsFuzzFlagPool is the closed set of per-frame EncodeFlags
// values the fuzz body can pick from. EncodeInvisibleFrame is excluded
// because the frame-flags driver routes it through a separate
// `--invisible-frames` argv (see encodeFramesWithFrameFlagsDriver) which we
// keep off the fuzz menu to keep the driver invocation deterministic with
// the per-frame `--frame-flags` schedule.
var encoderFrameFlagsFuzzFlagPool = []EncodeFlags{
	0,
	EncodeForceKeyFrame,
	EncodeNoUpdateEntropy,
	EncodeNoUpdateLast,
	EncodeNoUpdateGolden,
	EncodeNoUpdateAltRef,
	EncodeForceGoldenFrame,
	EncodeForceAltRefFrame,
	EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef,
	EncodeNoUpdateLast | EncodeNoUpdateEntropy,
	EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
}

func encoderFrameFlagsFuzzCaseFromBytes(data []byte) encoderFrameFlagsFuzzCase {
	r := testutil.NewByteCursor(data)

	dims := [...]struct {
		w int
		h int
	}{
		{16, 16},
		{32, 16},
		{32, 32},
	}
	speeds := [...]int{0, -3, -8}
	targets := [...]int{300, 700, 1200}

	dim := dims[r.Pick(len(dims))]
	cpuUsed := speeds[r.Pick(len(speeds))]
	targetKbps := targets[r.Pick(len(targets))]
	frames := 4 + r.Pick(5) // 4..8 frames

	// Threads is fixed at the single-threaded default. Threads>=2 on the VP8
	// encoder produces non-deterministic byte output (goroutine scheduling
	// affects token-partition packing), so it cannot participate in a
	// byte-parity fuzz harness. A separate threaded oracle parity test
	// (TestVP8OracleEncoderStreamByteParity with `threads:2`) drives the threaded
	// path with fixed inputs.
	opts := vp8OracleRuntimeBaseFuzzOptions(dim.w, dim.h, targetKbps, cpuUsed)
	opts.TargetBitrateKbps = targetKbps
	sources := vp8OracleRuntimeFuzzSources(dim.w, dim.h, frames, r.Pick(2))

	flags := make([]EncodeFlags, frames)
	// Frame 0 is always the initial keyframe; libvpx forces it regardless of
	// flags. Leave flags[0]=0 to match the existing parity harness and avoid
	// duplicate force-kf bits.
	for i := 1; i < frames; i++ {
		flags[i] = encoderFrameFlagsFuzzFlagPool[r.Pick(len(encoderFrameFlagsFuzzFlagPool))]
	}

	return encoderFrameFlagsFuzzCase{
		name:       "general",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    sources,
		flags:      flags,
	}
}

func encoderFrameFlagsLogString(flags []EncodeFlags) string {
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = strconv.FormatUint(uint64(f), 16)
	}
	return strings.Join(parts, ",")
}

// TestVP8OracleVpxencFrameFlagsWritesTraceAndRuntimeControls is the infrastructure smoke
// test for the combined VP8 reference driver produced by
// internal/coracle/build_vpxenc_frameflags_oracle.sh. It encodes a
// short 64x64 panning fixture through `vpxenc-frameflags-oracle`
// with BOTH:
//
//   - a non-trivial --control-script that flips the libvpx bitrate
//     mid-stream (so we can prove the per-frame runtime-control path
//     is wired), and
//   - the oracle trace side channel enabled through the coracle runner
//     (so we can prove the per-MB JSONL trace path is wired in the same
//     encode pass).
//
// The test asserts that the resulting IVF has at least one frame,
// that the JSONL trace contains both `"type":"frame"` and
// `"type":"mb"` rows, and that no run of the combined surface drops
// either capability silently. It is intentionally cheap: 4 frames,
// 64x64, single-threaded — so it can run on every govpx_oracle_trace
// build without slowing down the parity gate.
func TestVP8OracleVpxencFrameFlagsWritesTraceAndRuntimeControls(t *testing.T) {
	driver := vp8test.VpxencFrameFlagsOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		frames     = 4
		targetKbps = 200
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}

	// Per-frame control-script: frame 0 keeps the starting bitrate
	// (which is itself --target-bitrate=200), frames 1..3 raise it
	// mid-stream. Empty entries ("-") are a no-op in vpxenc_frameflags.c
	// (see for_each_control_token), so the encode stays well-defined
	// even on the no-op frames.
	scriptEntries := []string{
		"-",
		"bitrate:" + strconv.Itoa(targetKbps*2),
		"-",
		"-",
	}

	ivfData, traceData, diag, err := vp8test.VpxencVP8FrameFlagsEncodeTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxencVP8FrameFlagsConfig{
			BinaryPath:        driver,
			Width:             width,
			Height:            height,
			Frames:            frames,
			FPSNum:            fps,
			FPSDen:            1,
			TargetBitrateKbps: targetKbps,
			MinQ:              4,
			MaxQ:              56,
			KeyFrameMinDist:   999,
			KeyFrameMaxDist:   999,
			Deadline:          "good",
			CPUUsed:           0,
			EndUsage:          "cbr",
			AutoAltRef:        false,
			TokenPartitions:   0,
			ExtraArgs:         []string{"--control-script=" + strings.Join(scriptEntries, ",")},
		},
	)
	if err != nil {
		t.Fatalf("vpxenc-frameflags-oracle failed: %v\n%s", err, diag)
	}

	// (a) IVF produced and non-empty.
	payloads, err := testutil.IVFFramePayloads(ivfData)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatalf("vpxenc-frameflags-oracle produced no frames (ivf=%d bytes)", len(ivfData))
	}

	// (b) Trace file produced and non-empty.
	if len(traceData) == 0 {
		t.Fatalf("trace file is empty (combined binary may be missing oracle_trace.c TU)")
	}

	// (c) Trace contains both per-frame and per-MB rows.
	var frameRows, mbRows int
	scanner := bufio.NewScanner(bytes.NewReader(traceData))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "frame":
			frameRows++
		case "mb":
			mbRows++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	if frameRows == 0 {
		t.Fatalf("no per-frame trace rows in returned trace (control-script path may have suppressed the oracle emit hook)")
	}
	if mbRows == 0 {
		t.Fatalf("no per-MB trace rows in returned trace (oracle_trace.c TU likely not linked into libvpx.a)")
	}
	t.Logf("vpxenc-frameflags-oracle trace-control coverage ok: frames=%d ivf=%d trace_frame_rows=%d trace_mb_rows=%d",
		len(payloads), len(ivfData), frameRows, mbRows)
}

func requireOracleTraceBuild(t *testing.T) {
	t.Helper()
	if !oracleTraceBuild {
		t.Skip("oracle tracing is compiled out; run with -tags govpx_oracle_trace")
	}
}

func splitNonEmptyLines(b []byte) [][]byte {
	var out [][]byte
	for line := range bytes.SplitSeq(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}

func customQuadrantROIMap(width, height int) *ROIMap {
	roi := quadrantROIMap(width, height)
	roi.DeltaQuantizer = [4]int{0, -10, 8, -20}
	roi.DeltaLoopFilter = [4]int{0, -3, 2, 5}
	roi.StaticThreshold = [4]int{0, 500, 0, 1200}
	return roi
}

func simpleCheckerROIMap(width, height int) *ROIMap {
	roi := roiMapPattern(width, height, "checker")
	roi.DeltaQuantizer = [4]int{0, -10, 0, 0}
	roi.DeltaLoopFilter = [4]int{}
	roi.StaticThreshold = [4]int{}
	return roi
}

// oneAtIndex returns a length-total flag slice with index `at` set to
// f and every other entry zero. Useful for "single-frame-only"
// schedules that exercise the upd-mask transition on both edges.
func oneAtIndex(total, at int, f EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, total)
	if at >= 0 && at < total {
		out[at] = f
	}
	return out
}

// alternateFlags returns a length-total flag slice with index 0 set
// to 0 (initial keyframe) and the remaining slots alternating between
// a (odd indices) and b (even indices).
func alternateFlags(total int, a, b EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, total)
	for i := 1; i < total; i++ {
		if i%2 == 1 {
			out[i] = a
		} else {
			out[i] = b
		}
	}
	return out
}

func vp8SourceClipImages(clip vp8corpus.SourceClip) []Image {
	frames := make([]Image, len(clip.Frames))
	for i, frame := range clip.Frames {
		frames[i] = Image{
			Width:   clip.Width,
			Height:  clip.Height,
			Y:       frame.Y,
			U:       frame.Cb,
			V:       frame.Cr,
			YStride: frame.YStride,
			UStride: frame.CStride,
			VStride: frame.CStride,
		}
	}
	return frames
}

var vp8OracleRuntimeFullPermutationSeed = []byte{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

const (
	vp8OracleRuntimeFuzzConfigPhase uint8 = iota
	vp8OracleRuntimeFuzzCodecPhase
)

func vp8OracleRuntimeFullPermutationActionReader(kind int) testutil.ByteCursor {
	switch kind {
	case 3, 4:
		return testutil.NewByteCursor([]byte{2})
	case 8:
		return testutil.NewByteCursor([]byte{})
	case 17, 19:
		return testutil.NewByteCursor([]byte{1})
	default:
		return testutil.NewByteCursor([]byte{0})
	}
}

func vp8OracleRuntimeCurrentDropConfig(e *VP8Encoder) (bool, int) {
	if e == nil || !e.opts.DropFrameAllowed {
		return false, 0
	}
	return true, e.opts.DropFrameWaterMark
}

func vp8OracleRuntimeCurrentRateControlConfig(e *VP8Encoder) RateControlConfig {
	if e == nil {
		return RateControlConfig{}
	}
	return RateControlConfig{
		Mode:                e.rc.mode,
		TargetBitrateKbps:   e.rc.targetBitrateKbps,
		MinBitrateKbps:      e.rc.minBitrateKbps,
		MaxBitrateKbps:      e.rc.maxBitrateKbps,
		MinQuantizer:        vp8common.QIndexToPublicQuantizer(e.rc.minQuantizer),
		MaxQuantizer:        vp8common.QIndexToPublicQuantizer(e.rc.maxQuantizer),
		CQLevel:             e.opts.CQLevel,
		UndershootPct:       e.rc.undershootPct,
		OvershootPct:        e.rc.overshootPct,
		BufferSizeMs:        e.rc.bufferSizeMs,
		BufferInitialSizeMs: e.rc.bufferInitialSizeMs,
		BufferOptimalSizeMs: e.rc.bufferOptimalSizeMs,
		DropFrameAllowed:    e.rc.dropFramesWaterMark > 0,
		DropFrameWaterMark:  e.rc.dropFramesWaterMark,
		MaxIntraBitratePct:  e.rc.maxIntraBitratePct,
		GFCBRBoostPct:       e.rc.gfCBRBoostPct,
	}
}

// FuzzVP8OracleEncoderProductionRuntimeControls drives the same per-frame
// runtime-control schedule machinery as FuzzVP8OracleEncoderRuntimeControlTransitions
// but at production resolutions (640×360, 854×480, 1280×720) with an explicit
// Threads axis (0/1/2/4). This closes G1 (no strict gate above ~160×96) and G2
// (multi-threaded encode parity is weak in the strict gate) under
// fuzz-generated control schedules. It lives in a separate fuzzer so its
// regression corpus and the smaller-resolution one stay independent, and so a
// future "long mode" can run only this one.
//
// Per-iteration cost is meaningfully higher than the small-resolution fuzzer,
// so the case generator caps frames at 2–4 on the heaviest resolutions and
// reuses the same per-frame action pool.
func FuzzVP8OracleEncoderProductionRuntimeControls(f *testing.F) {
	vp8test.RequireOracleF(f, "production runtime-control fuzz parity")
	seeds := [][]byte{
		// (resolution-bucket, threads-bucket, frames, cpu, kind, then actions)
		{0, 0, 0, 0, 0, 0, 1, 2, 3},
		{0, 1, 1, 0, 0, 7, 7, 5},
		{1, 1, 0, 1, 0, 5, 8, 6},
		{1, 2, 1, 0, 0, 0, 9, 13},
		{2, 1, 0, 0, 0, 11, 6, 1},
		{2, 2, 0, 1, 1, 0, 5, 8, 4},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := vp8test.VpxencFrameFlags(t)
		tc := vp8OracleProductionRuntimeControlFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-prod-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d threads=%d frames=%d script=%s",
			label, tc.opts.Width, tc.opts.Height, tc.opts.Threads, len(tc.sources), strings.Join(tc.script, ","))

		govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, tc.sources, tc.flags, tc.apply)
		extraArgs := append([]string(nil), tc.extraArgs...)
		if tc.copyRefLog {
			extraArgs = append(extraArgs, "--copy-ref-log="+filepath.Join(t.TempDir(), "copy-reference.log"))
		}
		extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)
		// Strict byte parity at every production resolution. Seeds that
		// hit the documented runtime-control state-propagation gap
		// (gap D) fail here until the relevant fix lands. Failure logs
		// pinpoint the exact frame index and the byte delta size.
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

// vp8OracleProductionRuntimeControlFuzzCaseFromBytes maps a fuzz seed onto a
// production-resolution runtime-control case. The first five bytes pick the
// scenario shape (resolution, threads, frames, cpu, source-kind); subsequent
// bytes feed the per-frame action selector through the shared
// vp8OracleRuntimeRandomFuzzAction infrastructure.
func vp8OracleProductionRuntimeControlFuzzCaseFromBytes(data []byte) vp8OracleRuntimeControlFuzzCase {
	r := testutil.NewByteCursor(data)
	dims := [...]struct {
		w int
		h int
	}{
		{640, 360},
		{854, 480},
		{1280, 720},
	}
	threadPool := [...]int{0, 1, 2, 4}
	speeds := [...]int{0, -3, -8}
	targets := [...]int{300, 700, 1200}

	dim := dims[r.Pick(len(dims))]
	threads := threadPool[r.Pick(len(threadPool))]
	framesBucket := r.Pick(3)
	frames := 2 + framesBucket
	if dim.w >= 1280 && frames > 3 {
		frames = 3
	}
	cpuUsed := speeds[r.Pick(len(speeds))]
	kind := r.Pick(2)
	targetKbps := targets[r.Pick(len(targets))]

	opts := vp8OracleRuntimeBaseFuzzOptions(dim.w, dim.h, targetKbps, cpuUsed)
	opts.Threads = threads
	sources := vp8OracleRuntimeFuzzSources(dim.w, dim.h, frames, kind)
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	copyRefLog := false

	for frame := 1; frame < frames; frame++ {
		actionCount := 1 + r.Pick(3)
		actions := make([]vp8OracleRuntimeFuzzAction, 0, actionCount)
		haveConfig := false
		for range actionCount {
			action, flag, usesCopyRef := vp8OracleRuntimeRandomFuzzAction(&r, targets[:])
			if flag != 0 {
				flags[frame] = flag
				continue
			}
			if action.token == "" {
				continue
			}
			if action.phase == vp8OracleRuntimeFuzzConfigPhase {
				if haveConfig {
					continue
				}
				haveConfig = true
			}
			copyRefLog = copyRefLog || usesCopyRef
			actions = append(actions, action)
		}
		vp8OracleRuntimeShuffleActions(&r, actions)
		vp8OracleRuntimeInstallFuzzActions(script, apply, frame, actions)
	}

	return vp8OracleRuntimeControlFuzzCase{
		name:       "prod-general",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    sources,
		flags:      flags,
		script:     script,
		apply:      apply,
		copyRefLog: copyRefLog,
	}
}
