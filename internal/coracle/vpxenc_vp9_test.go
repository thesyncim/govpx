package coracle

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestVpxencVP9EncodeI420RejectsInvalidInputBeforePathLookup(t *testing.T) {
	if _, _, err := VpxencVP9EncodeI420(nil, 16, 16, 1); err == nil {
		t.Fatalf("VpxencVP9EncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencVP9NotBuilt) {
		t.Fatalf("VpxencVP9EncodeI420 looked up vpxenc before validating input")
	}
	if _, _, err := VpxencVP9EncodeI420(make([]byte, 384), 0, 16, 1); err == nil {
		t.Fatalf("VpxencVP9EncodeI420 accepted zero width")
	}
}

func TestVpxencVP9EncodeI420ProducesProfile0IVF(t *testing.T) {
	if _, err := VpxencVP9Path(); err != nil {
		if errors.Is(err, ErrVpxencVP9NotBuilt) {
			t.Skip("vpxenc-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxencVP9Path: %v", err)
	}

	const width, height, frames = 32, 32, 2
	raw := makeGeneratedVP9I420(width, height, frames)
	ivf, diag, err := VpxencVP9EncodeI420(raw, width, height, frames)
	if err != nil {
		t.Fatalf("VpxencVP9EncodeI420 failed: %v\n%s", err, diag)
	}
	h, err := testutil.ParseIVFHeader(ivf)
	if err != nil {
		t.Fatalf("ParseIVFHeader: %v", err)
	}
	if h.FourCC != [4]byte{'V', 'P', '9', '0'} {
		t.Fatalf("FourCC = %q, want VP90", h.FourCC)
	}
	if h.Width != width || h.Height != height || h.FrameCount != frames {
		t.Fatalf("header = %dx%d frames=%d, want %dx%d frames=%d",
			h.Width, h.Height, h.FrameCount, width, height, frames)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != frames {
		t.Fatalf("IVF frame count = %d, want %d", count, frames)
	}
}

func TestVpxencVP9FrameFlagsEncodeI420RejectsInvalidInputBeforePathLookup(t *testing.T) {
	if _, _, err := VpxencVP9FrameFlagsEncodeI420(nil, 16, 16, 1, nil); err == nil {
		t.Fatalf("VpxencVP9FrameFlagsEncodeI420 accepted empty input")
	} else if errors.Is(err, ErrVpxencVP9FrameFlagsNotBuilt) {
		t.Fatalf("VpxencVP9FrameFlagsEncodeI420 looked up helper before validating input")
	}
	if _, _, err := VpxencVP9FrameFlagsEncodeI420(make([]byte, 384), 16, 16, 1, []uint32{0, 0}); err == nil {
		t.Fatalf("VpxencVP9FrameFlagsEncodeI420 accepted too many frame flags")
	}
}

func TestVpxencVP9FrameFlagsEncodeI420MatchesVpxencWithoutFlags(t *testing.T) {
	if _, err := VpxencVP9Path(); err != nil {
		if errors.Is(err, ErrVpxencVP9NotBuilt) {
			t.Skip("vpxenc-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxencVP9Path: %v", err)
	}
	if _, err := VpxencVP9FrameFlagsPath(); err != nil {
		if errors.Is(err, ErrVpxencVP9FrameFlagsNotBuilt) {
			t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
		}
		t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	}

	const width, height, frames = 32, 32, 2
	raw := makeGeneratedVP9I420(width, height, frames)
	want, diag, err := VpxencVP9EncodeI420(raw, width, height, frames)
	if err != nil {
		t.Fatalf("VpxencVP9EncodeI420 failed: %v\n%s", err, diag)
	}
	got, diag, err := VpxencVP9FrameFlagsEncodeI420(raw, width, height, frames, nil)
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsEncodeI420 failed: %v\n%s", err, diag)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame-flags IVF diverged from vpxenc without flags\ngot  % x\nwant % x", got, want)
	}
}

func TestVpxencVP9FrameFlagsTraceI420EmitsRows(t *testing.T) {
	if _, err := VpxencVP9FrameFlagsPath(); err != nil {
		if errors.Is(err, ErrVpxencVP9FrameFlagsNotBuilt) {
			t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
		}
		t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	}

	const width, height, frames = 32, 32, 2
	raw := makeGeneratedVP9I420(width, height, frames)
	ivf, trace, diag, err := VpxencVP9FrameFlagsTraceI420(raw, width, height,
		frames, nil)
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420 failed: %v\n%s", err, diag)
	}
	if len(ivf) == 0 {
		t.Fatal("VpxencVP9FrameFlagsTraceI420 returned empty IVF")
	}
	if got := bytes.Count(trace, []byte("\n")); got != frames {
		t.Fatalf("trace rows = %d, want %d\n%s", got, frames, trace)
	}
	for _, want := range [][]byte{
		[]byte(`"row":"vp9_frame"`),
		[]byte(`"base_qindex"`),
		[]byte(`"size_bits"`),
		[]byte(`"buffer_level_bits"`),
		[]byte(`"recode_loop_count":0`),
	} {
		if !bytes.Contains(trace, want) {
			t.Fatalf("trace missing %s:\n%s", want, trace)
		}
	}
}

func TestVpxencVP9FrameFlagsTraceI420EmitsTemporalMetadata(t *testing.T) {
	if _, err := VpxencVP9FrameFlagsPath(); err != nil {
		if errors.Is(err, ErrVpxencVP9FrameFlagsNotBuilt) {
			t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
		}
		t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	}

	const width, height, frames = 32, 32, 4
	raw := makeGeneratedVP9I420(width, height, frames)
	_, trace, diag, err := VpxencVP9FrameFlagsTraceI420(raw, width, height,
		frames, nil,
		"--end-usage=cbr",
		"--target-bitrate=300",
		"--temporal-layers=2",
		"--temporal-bitrates=180,300",
		"--temporal-decimators=2,1",
		"--temporal-periodicity=2",
		"--temporal-layer-ids=0,1")
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420 failed: %v\n%s", err, diag)
	}
	for _, want := range [][]byte{
		[]byte(`"temporal_layer_id":0`),
		[]byte(`"temporal_layer_id":1`),
		[]byte(`"temporal_layer_count":2`),
		[]byte(`"tl0_pic_idx"`),
		[]byte(`"temporal_layer_sync"`),
	} {
		if !bytes.Contains(trace, want) {
			t.Fatalf("temporal trace missing %s:\n%s", want, trace)
		}
	}
}

func TestVpxencVP9FrameFlagsTraceI420AppliesBufferSchedules(t *testing.T) {
	if _, err := VpxencVP9FrameFlagsPath(); err != nil {
		if errors.Is(err, ErrVpxencVP9FrameFlagsNotBuilt) {
			t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
		}
		t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	}

	const width, height, frames = 32, 32, 3
	raw := makeGeneratedVP9I420(width, height, frames)
	_, trace, diag, err := VpxencVP9FrameFlagsTraceI420(raw, width, height,
		frames, nil,
		"--end-usage=cbr",
		"--target-bitrate=300",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--buf-sz-schedule=1:200",
		"--buf-initial-sz-schedule=1:100",
		"--buf-optimal-sz-schedule=1:150")
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420 failed: %v\n%s", err, diag)
	}
	if !bytes.Contains(trace, []byte(`"buffer_optimal_bits":45000`)) {
		t.Fatalf("buffer schedule trace missing 45k optimal buffer:\n%s", trace)
	}
}

func TestVpxencVP9FrameFlagsTraceI420WithFrameSizesAppliesResize(t *testing.T) {
	if _, err := VpxencVP9FrameFlagsPath(); err != nil {
		if errors.Is(err, ErrVpxencVP9FrameFlagsNotBuilt) {
			t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
		}
		t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	}

	sizes := []VpxencVP9FrameSize{
		{Width: 32, Height: 32},
		{Width: 48, Height: 40},
		{Width: 48, Height: 40},
	}
	var raw []byte
	for _, size := range sizes {
		raw = append(raw, makeGeneratedVP9I420(size.Width, size.Height, 1)...)
	}
	ivf, trace, diag, err := VpxencVP9FrameFlagsTraceI420WithFrameSizes(raw,
		sizes, nil, nil)
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420WithFrameSizes failed: %v\n%s", err, diag)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != len(sizes) {
		t.Fatalf("IVF frame count = %d, want %d", count, len(sizes))
	}
	if !bytes.Contains(trace, []byte(`"coded_width":48`)) ||
		!bytes.Contains(trace, []byte(`"coded_height":40`)) {
		t.Fatalf("resize trace missing resized dimensions:\n%s", trace)
	}
}

func TestVpxencVP9FrameFlagsTraceI420WithFrameSizesAppliesInvisibleSchedule(t *testing.T) {
	if _, err := VpxencVP9FrameFlagsPath(); err != nil {
		if errors.Is(err, ErrVpxencVP9FrameFlagsNotBuilt) {
			t.Skip("vpxenc-vp9-frameflags not built; run internal/coracle/build_vpxenc_vp9_frameflags.sh")
		}
		t.Fatalf("VpxencVP9FrameFlagsPath: %v", err)
	}

	const width, height, frames = 32, 32, 1
	raw := makeGeneratedVP9I420(width, height, frames)
	ivf, trace, diag, err := VpxencVP9FrameFlagsTraceI420WithFrameSizes(raw,
		[]VpxencVP9FrameSize{
			{Width: width, Height: height},
		},
		nil, []bool{true})
	if err != nil {
		t.Fatalf("VpxencVP9FrameFlagsTraceI420WithFrameSizes failed: %v\n%s", err, diag)
	}
	count, err := testutil.CountIVFFrames(ivf)
	if err != nil {
		t.Fatalf("CountIVFFrames: %v", err)
	}
	if count != frames {
		t.Fatalf("IVF frame count = %d, want %d", count, frames)
	}
	if !bytes.Contains(trace, []byte(`"show_frame":false`)) {
		t.Fatalf("invisible schedule trace missing hidden row:\n%s", trace)
	}
}

func makeGeneratedVP9I420(width int, height int, frames int) []byte {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	frameSize := width*height + 2*uvWidth*uvHeight
	raw := make([]byte, 0, frameSize*frames)
	for frame := 0; frame < frames; frame++ {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				raw = append(raw, byte(24+(x*7+y*11+frame*13)%208))
			}
		}
		for y := 0; y < uvHeight; y++ {
			for x := 0; x < uvWidth; x++ {
				raw = append(raw, byte(80+(x*5+y*3+frame*9)%96))
			}
		}
		for y := 0; y < uvHeight; y++ {
			for x := 0; x < uvWidth; x++ {
				raw = append(raw, byte(96+(x*3+y*7+frame*5)%96))
			}
		}
	}
	return raw
}
