//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8corpus"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

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
