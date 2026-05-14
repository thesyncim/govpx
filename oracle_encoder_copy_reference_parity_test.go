//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestOracleEncoderCopyReferenceFrameParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder reference-copy parity gate")
	}
	driver := findVpxencFrameFlags(t)

	t.Run("refreshed-references", func(t *testing.T) {
		opts := copyReferenceParityOptions(16, 16)
		sources := makePanningSources(opts.Width, opts.Height, 6, 0)
		flags := []EncodeFlags{
			0,
			0,
			EncodeForceGoldenFrame,
			0,
			EncodeForceAltRefFrame,
			0,
		}
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden+copyref:altref"
		script[3] = "copyref:golden"
		script[5] = "copyref:altref"
		probes := map[int][]copyReferenceProbe{
			1: {
				{ref: ReferenceLast, name: "last"},
				{ref: ReferenceGolden, name: "golden"},
				{ref: ReferenceAltRef, name: "altref"},
			},
			3: {{ref: ReferenceGolden, name: "golden"}},
			5: {{ref: ReferenceAltRef, name: "altref"}},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-refresh", opts, sources, flags, script)
		got := captureGovpxCopyReferenceChecksums(t, opts, sources, flags, nil, probes)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("external-set-reference", func(t *testing.T) {
		opts := copyReferenceParityOptions(33, 17)
		sources := makePanningSources(opts.Width, opts.Height, 4, 0)
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "setref:last:panning:8+copyref:last"
		script[2] = "setref:golden:panning:9+copyref:golden"
		script[3] = "setref:altref:panning:10+copyref:altref"
		sets := map[int][]copyReferenceSet{
			1: {{ref: ReferenceLast, name: "last", panningIndex: 8}},
			2: {{ref: ReferenceGolden, name: "golden", panningIndex: 9}},
			3: {{ref: ReferenceAltRef, name: "altref", panningIndex: 10}},
		}
		probes := map[int][]copyReferenceProbe{
			1: {{ref: ReferenceLast, name: "last"}},
			2: {{ref: ReferenceGolden, name: "golden"}},
			3: {{ref: ReferenceAltRef, name: "altref"}},
		}

		want := captureLibvpxCopyReferenceChecksums(t, driver, "copyref-setref", opts, sources, nil, script)
		got := captureGovpxCopyReferenceChecksums(t, opts, sources, nil, sets, probes)
		assertCopyReferenceChecksumsEqual(t, got, want)
	})

	t.Run("copy-reference-probes-do-not-change-bytestream", func(t *testing.T) {
		opts := copyReferenceParityOptions(32, 32)
		sources := makePanningSources(opts.Width, opts.Height, 6, 0)
		flags := []EncodeFlags{
			0,
			0,
			EncodeForceGoldenFrame,
			0,
			EncodeForceAltRefFrame,
			0,
		}
		script := emptyCopyReferenceScript(len(sources))
		script[1] = "copyref:last+copyref:golden+copyref:altref"
		script[3] = "copyref:golden"
		script[5] = "copyref:altref"
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: copyReferenceProbeApply("frame1", ReferenceLast, ReferenceGolden, ReferenceAltRef),
			3: copyReferenceProbeApply("frame3", ReferenceGolden),
			5: copyReferenceProbeApply("frame5", ReferenceAltRef),
		}
		logPath := filepath.Join(t.TempDir(), "copyref-bytestream.log")

		want := encodeFramesWithFrameFlagsDriver(t, driver, "copyref-bytestream", opts, opts.TargetBitrateKbps, sources, flags, []string{
			"--control-script=" + strings.Join(script, ","),
			"--copy-ref-log=" + logPath,
		})
		got := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
		assertSegmentByteParity(t, "copyref-bytestream", got, want, 0)
	})
}

type copyReferenceChecksum struct {
	Frame    int
	Ref      string
	YAdler32 uint32
	UAdler32 uint32
	VAdler32 uint32
}

type copyReferenceProbe struct {
	ref  ReferenceFrame
	name string
}

type copyReferenceSet struct {
	ref          ReferenceFrame
	name         string
	panningIndex int
}

func copyReferenceProbeApply(label string, refs ...ReferenceFrame) func(*testing.T, *VP8Encoder) {
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
	logPath := filepath.Join(t.TempDir(), name+".log")
	extraArgs := []string{
		"--control-script=" + strings.Join(script, ","),
		"--copy-ref-log=" + logPath,
	}
	_ = encodeFramesWithFrameFlagsDriver(t, driver, name, opts, opts.TargetBitrateKbps, sources, flags, extraArgs)
	return readCopyReferenceChecksumLog(t, logPath)
}

func captureGovpxCopyReferenceChecksums(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, sets map[int][]copyReferenceSet, probes map[int][]copyReferenceProbe) []copyReferenceChecksum {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()

	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([]copyReferenceChecksum, 0)
	for i, src := range sources {
		for _, set := range sets[i] {
			img := encoderValidationPanningFrame(opts.Width, opts.Height, set.panningIndex)
			if err := enc.SetReferenceFrame(set.ref, img); err != nil {
				t.Fatalf("frame %d SetReferenceFrame(%s): %v", i, set.name, err)
			}
		}
		for _, probe := range probes[i] {
			dst := testImage(opts.Width, opts.Height)
			if err := enc.CopyReferenceFrame(probe.ref, &dst); err != nil {
				t.Fatalf("frame %d CopyReferenceFrame(%s): %v", i, probe.name, err)
			}
			out = append(out, copyReferenceImageChecksum(i, probe.name, dst))
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
