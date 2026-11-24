//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil"
	"testing"
)

// encodeWithMidStreamResize runs a single govpx encoder across two
// resolution segments. It encodes seg1 at the dimensions supplied in
// initOpts, drains via FlushInto, calls SetRealtimeTarget with the new
// (w2,h2), and encodes seg2. Returns the per-frame VP8 payloads of each
// segment.
func encodeWithMidStreamResize(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image) ([][]byte, [][]byte) {
	t.Helper()
	return encodeWithMidStreamResizeAndControlSplit(t, initOpts, w2, h2, seg1, seg2, nil)
}

func encodeWithMidStreamResizeAndControl(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, afterResize func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	out1, out2 := encodeWithMidStreamResizeAndControlSplit(t, initOpts, w2, h2, seg1, seg2, afterResize)
	return append(append([][]byte(nil), out1...), out2...)
}

func encodeWithMidStreamResizeGlobalControls(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, flags []EncodeFlags, apply map[int]func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	return encodeWithMidStreamResizeGlobalControlsAndResize(t, initOpts, w2, h2, seg1, seg2, flags, apply, nil)
}

func encodeWithMidStreamResizeGlobalControlsAndResize(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, flags []EncodeFlags, apply map[int]func(*testing.T, *VP8Encoder),
	resizeApply func(*testing.T, *VP8Encoder, int, int)) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(initOpts)
	if err != nil {
		t.Fatalf("NewVP8Encoder seg1 (%dx%d): %v", initOpts.Width, initOpts.Height, err)
	}
	defer enc.Close()
	buf := make([]byte, max(initOpts.Width*initOpts.Height, w2*h2)*6+4096)
	out := make([][]byte, 0, len(seg1)+len(seg2))
	encodeOne := func(global int, src Image) {
		t.Helper()
		if fn := apply[global]; fn != nil {
			fn(t, enc)
		}
		var f EncodeFlags
		if global < len(flags) {
			f = flags[global]
		}
		result, err := enc.EncodeInto(buf, src, uint64(global), 1, f)
		if errors.Is(err, ErrFrameNotReady) {
			return
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", global, err)
		}
		if result.Dropped {
			t.Fatalf("frame %d unexpectedly dropped", global)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for i, src := range seg1 {
		encodeOne(i, src)
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg1 FlushInto: %v", err)
		}
		if r.Dropped {
			t.Fatalf("seg1 flush packet unexpectedly dropped")
		}
		out = append(out, append([]byte(nil), r.Data...))
	}
	if resizeApply != nil {
		resizeApply(t, enc, w2, h2)
	} else if err := enc.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget(%dx%d): %v", w2, h2, err)
	}
	for i, src := range seg2 {
		encodeOne(len(seg1)+i, src)
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg2 FlushInto: %v", err)
		}
		if r.Dropped {
			t.Fatalf("seg2 flush packet unexpectedly dropped")
		}
		out = append(out, append([]byte(nil), r.Data...))
	}
	return out
}

func encodeWithMidStreamResizeAndControlSplit(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, afterResize func(*testing.T, *VP8Encoder)) ([][]byte, [][]byte) {
	t.Helper()
	enc, err := NewVP8Encoder(initOpts)
	if err != nil {
		t.Fatalf("NewVP8Encoder seg1 (%dx%d): %v", initOpts.Width, initOpts.Height, err)
	}
	defer enc.Close()
	// Scratch buffer sized for the larger of the two coded resolutions
	// plus generous slack for header overhead. Same shape as the
	// shared encodeFramesWithGovpx helper but stretched to cover both
	// segments without reallocating between them.
	buf := make([]byte, max(initOpts.Width*initOpts.Height, w2*h2)*6+4096)

	out1 := make([][]byte, 0, len(seg1))
	for i, src := range seg1 {
		r, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("seg1 EncodeInto %d: %v", i, err)
		}
		if r.Dropped {
			t.Fatalf("seg1 frame %d unexpectedly dropped", i)
		}
		out1 = append(out1, append([]byte(nil), r.Data...))
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg1 FlushInto: %v", err)
		}
		out1 = append(out1, append([]byte(nil), r.Data...))
	}

	if err := enc.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget(%dx%d): %v", w2, h2, err)
	}
	if afterResize != nil {
		afterResize(t, enc)
	}

	out2 := make([][]byte, 0, len(seg2))
	for i, src := range seg2 {
		// Continue the PTS clock past the segment-1 frames so the
		// timestamp is monotonic; libvpx's rate-controller key off the
		// PTS delta, and a non-monotonic PTS would skew the
		// post-resize state in ways unrelated to the resize itself.
		pts := uint64(len(seg1) + i)
		r, err := enc.EncodeInto(buf, src, pts, 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("seg2 EncodeInto %d: %v", i, err)
		}
		if r.Dropped {
			t.Fatalf("seg2 frame %d unexpectedly dropped", i)
		}
		if i == 0 && !r.KeyFrame {
			t.Fatalf("seg2 frame 0 KeyFrame=false, want true after resize")
		}
		out2 = append(out2, append([]byte(nil), r.Data...))
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg2 FlushInto: %v", err)
		}
		out2 = append(out2, append([]byte(nil), r.Data...))
	}
	return out1, out2
}

func indexedResizeFlags(frames int, updates map[int]EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for frame, flag := range updates {
		if frame >= 0 && frame < frames {
			flags[frame] = flag
		}
	}
	return flags
}

// assertSegmentByteParity compares per-frame VP8 payloads between two
// captures (typically govpx vs libvpx). matchLimit caps how many
// leading frames are asserted strictly: 0 requires the full length,
// a positive value requires only the first matchLimit frames, and a
// negative value logs mismatches without asserting a byte-match prefix.
func assertSegmentByteParity(t *testing.T, label string, got, want [][]byte, matchLimit int) {
	t.Helper()
	if len(got) != len(want) {
		if matchLimit < 0 || (matchLimit > 0 && matchLimit <= len(got) && matchLimit <= len(want)) {
			t.Logf("%s: frame count mismatch (logged only, matchLimit=%d): got=%d want=%d",
				label, matchLimit, len(got), len(want))
		} else {
			t.Errorf("%s: frame count mismatch: got=%d want=%d", label, len(got), len(want))
			return
		}
	}
	limit := len(got)
	if matchLimit < 0 {
		limit = 0
	} else if matchLimit > 0 && matchLimit < limit {
		limit = matchLimit
	}
	common := len(got)
	if len(want) < common {
		common = len(want)
	}
	for i := 0; i < common; i++ {
		gHash := sha256.Sum256(got[i])
		lHash := sha256.Sum256(want[i])
		gFP, gIsKey := parseVP8FramePartitionSizes(got[i])
		lFP, lIsKey := parseVP8FramePartitionSizes(want[i])
		if gHash == lHash {
			t.Logf("%s frame %d byte MATCH: len=%d first_part=%d keyframe=%t",
				label, i, len(got[i]), gFP, gIsKey)
			continue
		}
		firstDiff := testutil.FirstByteDiff(got[i], want[i])
		if i >= limit {
			t.Logf("%s frame %d byte mismatch (not asserted, limit=%d): got_len=%d want_len=%d first_diff=%d got_first_part=%d want_first_part=%d got_keyframe=%t want_keyframe=%t got_sha=%s want_sha=%s",
				label, i, limit, len(got[i]), len(want[i]), firstDiff,
				gFP, lFP, gIsKey, lIsKey,
				hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			continue
		}
		t.Errorf("%s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d got_first_part=%d want_first_part=%d got_keyframe=%t want_keyframe=%t got_sha=%s want_sha=%s",
			label, i, len(got[i]), len(want[i]), firstDiff,
			gFP, lFP, gIsKey, lIsKey,
			hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
	}
}

func assertFirstFrameByteParity(t *testing.T, label string, got, want [][]byte) {
	t.Helper()
	if len(got) == 0 || len(want) == 0 {
		t.Fatalf("%s: missing first frame: got=%d want=%d", label, len(got), len(want))
	}
	assertSegmentByteParity(t, label, got[:1], want[:1], 0)
}

// assertStrictGateKnownGapMatchedPrefix is the migration target for
// strict-gate cases that opt into known-divergence behaviour with
// tc.limit < 0. It computes the matched-prefix length of got vs
// want and asserts it is at least `floor`. Per plan §5 this catches
// silent regression in the matched prefix that the prior
// log-and-return pattern would have masked. Empty common range
// (one side produced zero frames) is logged-only — the floor only
// binds when at least one common frame exists.
func assertStrictGateKnownGapMatchedPrefix(t *testing.T, label string, got, want [][]byte, floor int) {
	t.Helper()
	common := min(len(got), len(want))
	if common == 0 {
		t.Logf("%s known-gap: no common frames (got=%d want=%d)", label, len(got), len(want))
		return
	}
	matched := 0
	for i := 0; i < common; i++ {
		if sha256.Sum256(got[i]) == sha256.Sum256(want[i]) {
			matched++
		} else {
			break
		}
	}
	t.Logf("%s known-gap matched-prefix=%d (floor=%d)", label, matched, floor)
	if matched < floor {
		t.Errorf("%s matched-prefix=%d below floor=%d (regression in matched prefix)", label, matched, floor)
	}
}
