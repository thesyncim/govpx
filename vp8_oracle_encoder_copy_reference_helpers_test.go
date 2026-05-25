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
