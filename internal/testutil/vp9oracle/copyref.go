//go:build govpx_oracle_trace

package vp9oracle

import (
	"bufio"
	"fmt"
	"hash/adler32"
	"image"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

type CopyReferenceChecksum struct {
	Frame    int
	Ref      string
	YAdler32 uint32
	UAdler32 uint32
	VAdler32 uint32
}

type CopyReferenceCheck struct {
	Ref  govpx.ReferenceFrame
	Name string
}

type CopyReferenceSet struct {
	Ref          govpx.ReferenceFrame
	Name         string
	PanningIndex int
}

func EmptyCopyReferenceScript(frames int) []string {
	script := make([]string, frames)
	for i := range script {
		script[i] = "-"
	}
	return script
}

func CaptureLibvpxCopyReferenceChecksums(t testing.TB, name string,
	sources []*image.YCbCr, flags []govpx.EncodeFlags, script []string,
	extraArgs []string,
) []CopyReferenceChecksum {
	t.Helper()
	if len(flags) > len(sources) {
		t.Fatalf("VP9 copy-reference flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	logPath := vp9test.VpxencFrameFlagCopyReferenceLog(t, name, sources,
		LibvpxFrameFlags(flags), script, extraArgs...)
	return ReadCopyReferenceChecksumLog(t, logPath)
}

func CaptureGovpxCopyReferenceChecksums(t testing.TB,
	opts govpx.VP9EncoderOptions, sources []*image.YCbCr,
	flags []govpx.EncodeFlags, sets map[int][]CopyReferenceSet,
	checks map[int][]CopyReferenceCheck,
) []CopyReferenceChecksum {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("empty VP9 copy-reference source")
	}
	if len(flags) > len(sources) {
		t.Fatalf("VP9 copy-reference flag count = %d, want <= %d",
			len(flags), len(sources))
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	opts.Width = width
	opts.Height = height
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	dstSize, err := EncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("EncodeBufferSize: %v", err)
	}
	buf := make([]byte, dstSize)
	out := make([]CopyReferenceChecksum, 0)
	for i, src := range sources {
		for _, set := range sets[i] {
			img := PanningImage(width, height, set.PanningIndex)
			if err := enc.SetReferenceFrame(set.Ref, img); err != nil {
				t.Fatalf("frame %d SetReferenceFrame(%s): %v",
					i, set.Name, err)
			}
		}
		for _, check := range checks[i] {
			dst := NewImage(width, height)
			if err := enc.CopyReferenceFrame(check.Ref, &dst); err != nil {
				t.Fatalf("frame %d CopyReferenceFrame(%s): %v",
					i, check.Name, err)
			}
			out = append(out, CopyReferenceImageChecksum(i, check.Name, dst))
		}
		var frameFlags govpx.EncodeFlags
		if i < len(flags) {
			frameFlags = flags[i]
		}
		if _, err := enc.EncodeIntoWithFlags(src, buf, frameFlags); err != nil {
			t.Fatalf("EncodeIntoWithFlags frame %d: %v", i, err)
		}
	}
	return out
}

func AssertCopyReferenceChecksumsEqual(t testing.TB,
	got []CopyReferenceChecksum, want []CopyReferenceChecksum,
) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CopyReferenceFrame checksum mismatch\n govpx: %s\nlibvpx: %s",
			FormatCopyReferenceChecksums(got),
			FormatCopyReferenceChecksums(want))
	}
}

func FormatCopyReferenceChecksums(checksums []CopyReferenceChecksum) string {
	parts := make([]string, len(checksums))
	for i, c := range checksums {
		parts[i] = fmt.Sprintf("frame=%d ref=%s y=%08x u=%08x v=%08x",
			c.Frame, c.Ref, c.YAdler32, c.UAdler32, c.VAdler32)
	}
	return strings.Join(parts, "; ")
}

func ReadCopyReferenceChecksumLog(t testing.TB, path string) []CopyReferenceChecksum {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open copy-reference log %s: %v", path, err)
	}
	defer file.Close()

	var out []CopyReferenceChecksum
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := parseCopyReferenceLogFields(t, line)
		out = append(out, CopyReferenceChecksum{
			Frame:    parseCopyReferenceLogInt(t, fields, "frame"),
			Ref:      fields["ref"],
			YAdler32: parseCopyReferenceLogUint32(t, fields, "y_adler32"),
			UAdler32: parseCopyReferenceLogUint32(t, fields, "u_adler32"),
			VAdler32: parseCopyReferenceLogUint32(t, fields, "v_adler32"),
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan copy-reference log %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("copy-reference log %s had no entries", path)
	}
	return out
}

func CopyReferenceImageChecksum(frame int, ref string, img govpx.Image) CopyReferenceChecksum {
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	return CopyReferenceChecksum{
		Frame:    frame,
		Ref:      ref,
		YAdler32: planeAdler32(img.Y, img.Width, img.Height, img.YStride),
		UAdler32: planeAdler32(img.U, uvWidth, uvHeight, img.UStride),
		VAdler32: planeAdler32(img.V, uvWidth, uvHeight, img.VStride),
	}
}

func PanningImage(width int, height int, index int) govpx.Image {
	img := NewImage(width, height)
	xoff := index * 2
	yoff := index
	for y := range height {
		for x := range width {
			srcX := x + xoff
			srcY := y + yoff
			img.Y[y*img.YStride+x] = byte(32 + ((srcY*7 + srcX*11 + (srcX/8)*(srcY/8)*13) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for y := range uvHeight {
		for x := range uvWidth {
			srcX := x + xoff/2
			srcY := y + yoff/2
			img.U[y*img.UStride+x] = byte(96 + ((srcX*5 + srcY*3) & 63))
			img.V[y*img.VStride+x] = byte(144 + ((srcX*2 + srcY*7) & 63))
		}
	}
	return img
}

func parseCopyReferenceLogFields(t testing.TB, line string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, field := range strings.Fields(line) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			t.Fatalf("invalid copy-reference log field %q in %q", field, line)
		}
		out[key] = value
	}
	for _, key := range []string{"frame", "ref", "y_adler32", "u_adler32", "v_adler32"} {
		if out[key] == "" {
			t.Fatalf("copy-reference log line %q missing %s", line, key)
		}
	}
	return out
}

func parseCopyReferenceLogInt(t testing.TB, fields map[string]string, key string) int {
	t.Helper()
	v, err := strconv.Atoi(fields[key])
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, fields[key], err)
	}
	return v
}

func parseCopyReferenceLogUint32(t testing.TB, fields map[string]string, key string) uint32 {
	t.Helper()
	v, err := strconv.ParseUint(fields[key], 10, 32)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, fields[key], err)
	}
	return uint32(v)
}

func planeAdler32(plane []byte, width int, height int, stride int) uint32 {
	if min(min(width, height), stride) <= 0 {
		return 0
	}
	h := adler32.New()
	for row := range height {
		start := row * stride
		end := start + width
		if end > len(plane) {
			break
		}
		_, _ = h.Write(plane[start:end])
	}
	return h.Sum32()
}
