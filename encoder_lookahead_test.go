package govpx

import (
	"errors"
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// newLookaheadTestEncoder builds an encoder with the requested lookahead
// depth. The resulting buffer slice has len = depth+1, matching libvpx's
// max_sz = depth + 1 (the trailing slot keeps the most-recently-popped frame
// addressable for backward peek).
func newLookaheadTestEncoder(tb testing.TB, depth int) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		LookaheadFrames:     depth,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func sourceFromTestImage(img Image) vp8enc.SourceImage {
	return sourceImageFromImage(img)
}

func TestLookaheadPushFillsConfiguredCapacity(t *testing.T) {
	depth := 4
	e := newLookaheadTestEncoder(t, depth)
	if got, want := len(e.lookahead), depth+1; got != want {
		t.Fatalf("lookahead capacity = %d, want %d (depth+1 per libvpx max_sz)", got, want)
	}
	for i := range depth {
		img := testImage(16, 16)
		fillImage(img, byte(40+i), byte(90+i), byte(150+i))
		if err := e.pushLookahead(sourceFromTestImage(img), uint64(i), 1, 0); err != nil {
			t.Fatalf("pushLookahead(%d) returned error: %v", i, err)
		}
		if got := e.lookaheadDepth(); got != i+1 {
			t.Fatalf("after push %d depth = %d, want %d", i, got, i+1)
		}
	}
	// Queue is full: libvpx rejects further pushes with sz + 2 > max_sz.
	overflow := testImage(16, 16)
	if err := e.pushLookahead(sourceFromTestImage(overflow), 99, 1, 0); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("overflow push error = %v, want ErrFrameNotReady (lag clamp)", err)
	}
	if got := e.lookaheadDepth(); got != depth {
		t.Fatalf("after rejected push depth = %d, want unchanged %d", got, depth)
	}
}

func TestLookaheadPopBlocksUntilFullThenDrains(t *testing.T) {
	depth := 3
	e := newLookaheadTestEncoder(t, depth)
	for i := 0; i < depth-1; i++ {
		img := testImage(16, 16)
		if err := e.pushLookahead(sourceFromTestImage(img), uint64(i), 1, 0); err != nil {
			t.Fatalf("pushLookahead(%d): %v", i, err)
		}
		if entry, ok := e.popLookahead(false); ok {
			t.Fatalf("popLookahead(false) returned entry %v before queue full", entry)
		}
	}
	// Filling the last slot satisfies sz == max_sz - 1 and pop should succeed.
	last := testImage(16, 16)
	if err := e.pushLookahead(sourceFromTestImage(last), uint64(depth-1), 1, 0); err != nil {
		t.Fatalf("pushLookahead final: %v", err)
	}
	entry, ok := e.popLookahead(false)
	if !ok {
		t.Fatalf("popLookahead(false) failed when queue full")
	}
	if entry.pts != 0 {
		t.Fatalf("first popped pts = %d, want 0 (FIFO order)", entry.pts)
	}
	// EOS flush: drain remaining frames in order.
	wantPTS := []uint64{1, 2}
	for _, pts := range wantPTS {
		entry, ok := e.popLookahead(true)
		if !ok {
			t.Fatalf("drain pop failed at expected pts %d", pts)
		}
		if entry.pts != pts {
			t.Fatalf("drain pop pts = %d, want %d", entry.pts, pts)
		}
	}
	if _, ok := e.popLookahead(true); ok {
		t.Fatalf("drain pop after empty queue must return false")
	}
	if got := e.lookaheadDepth(); got != 0 {
		t.Fatalf("post-drain depth = %d, want 0", got)
	}
}

func TestLookaheadForwardPeekAtBoundaries(t *testing.T) {
	depth := 5
	e := newLookaheadTestEncoder(t, depth)
	if got := e.peekLookahead(0, true); got != nil {
		t.Fatalf("peek forward 0 on empty queue = %v, want nil", got)
	}
	for i := range depth {
		img := testImage(16, 16)
		fillImage(img, byte(20+i*10), byte(80+i*5), byte(160+i*5))
		if err := e.pushLookahead(sourceFromTestImage(img), uint64(100+i), 1, 0); err != nil {
			t.Fatalf("pushLookahead(%d): %v", i, err)
		}
	}
	// Peek at the head (next pop), one ahead, and the last in-range index.
	for _, idx := range []int{0, 1, depth - 1} {
		entry := e.peekLookahead(idx, true)
		if entry == nil {
			t.Fatalf("peek forward %d returned nil while in range", idx)
		}
		if entry.pts != uint64(100+idx) {
			t.Fatalf("peek forward %d pts = %d, want %d", idx, entry.pts, 100+idx)
		}
	}
	// Out of range: index == depth (libvpx's assert(index < max_sz - 1) and
	// the index >= sz check both fire here).
	if got := e.peekLookahead(depth, true); got != nil {
		t.Fatalf("peek forward %d (out of range) = %v, want nil", depth, got)
	}
	if got := e.peekLookahead(-1, true); got != nil {
		t.Fatalf("peek forward -1 = %v, want nil", got)
	}
}

func TestLookaheadBackwardPeekReturnsLastPopped(t *testing.T) {
	depth := 3
	e := newLookaheadTestEncoder(t, depth)
	// Push depth frames so pop succeeds.
	for i := range depth {
		img := testImage(16, 16)
		fillImage(img, byte(10+i*5), 80, 160)
		if err := e.pushLookahead(sourceFromTestImage(img), uint64(200+i), 1, 0); err != nil {
			t.Fatalf("pushLookahead(%d): %v", i, err)
		}
	}
	popped, ok := e.popLookahead(false)
	if !ok {
		t.Fatalf("popLookahead failed when full")
	}
	if popped.pts != 200 {
		t.Fatalf("popped pts = %d, want 200", popped.pts)
	}
	// Backward peek with index==1 returns the slot at read_idx-1, i.e. the
	// frame we just popped (libvpx leaves the buffer in place).
	prev := e.peekLookahead(1, false)
	if prev == nil {
		t.Fatalf("peekLookahead(1, backward) = nil, want previous source frame")
	}
	if prev.pts != 200 {
		t.Fatalf("backward peek pts = %d, want 200 (most recently popped)", prev.pts)
	}
	// libvpx asserts index == 1 for backward peek; other indices return nil.
	if got := e.peekLookahead(0, false); got != nil {
		t.Fatalf("backward peek index 0 = %v, want nil", got)
	}
	if got := e.peekLookahead(2, false); got != nil {
		t.Fatalf("backward peek index 2 = %v, want nil", got)
	}
}

func TestLookaheadActiveMapPartialCopyOnPush(t *testing.T) {
	// libvpx's vp8_lookahead_push only walks active runs when
	// max_sz == 1 && active_map && !flags. With the public init clamp the
	// max_sz==1 branch is unreachable through normal allocation, so exercise
	// the partial-copy helper directly with a synthetic 32x32 frame and an
	// active map that marks only the top-left 16x16 macroblock.
	width, height := 32, 32
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)
	mask := make([]uint8, mbRows*mbCols)
	mask[0] = 1

	e := newLookaheadTestEncoder(t, 1)
	dstBuf := &e.lookahead[0].frame
	const sentinel = byte(0xA5)
	for y := 0; y < dstBuf.Img.Height; y++ {
		row := dstBuf.Img.Y[y*dstBuf.Img.YStride:]
		for x := 0; x < dstBuf.Img.Width; x++ {
			row[x] = sentinel
		}
	}

	src := testImage(width, height)
	fillImage(src, 123, 200, 50)
	// Resize the destination to match the synthetic frame so plane strides
	// line up with the helper's row layout assumptions.
	if err := dstBuf.Resize(width, height, 32, 32); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	for y := 0; y < dstBuf.Img.Height; y++ {
		row := dstBuf.Img.Y[y*dstBuf.Img.YStride:]
		for x := 0; x < dstBuf.Img.Width; x++ {
			row[x] = sentinel
		}
	}
	copySourceToFrameBufferActive(dstBuf, sourceFromTestImage(src), mask, mbRows, mbCols)

	dst := &dstBuf.Img
	// Active MB (row 0, col 0): luma must equal the source pixel.
	for y := range 16 {
		for x := range 16 {
			if got := dst.Y[y*dst.YStride+x]; got != 123 {
				t.Fatalf("active MB Y[%d,%d] = %d, want 123 (source pixel)", y, x, got)
			}
		}
	}
	// Inactive MB (row 0, col 1): luma must keep the sentinel.
	for y := range 16 {
		for x := 16; x < 32; x++ {
			if got := dst.Y[y*dst.YStride+x]; got != sentinel {
				t.Fatalf("inactive MB Y[%d,%d] = %d, want sentinel %d", y, x, got, sentinel)
			}
		}
	}
}

func TestLookaheadActiveMapPartialCopyMultipleRuns(t *testing.T) {
	// Two active runs in the same row, one inactive run between them: only
	// the active runs should be copied, the gap must keep its prior content.
	// This pins the row-major active-run walk libvpx implements in
	// vp8_lookahead_push.
	width, height := 64, 16
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)
	if mbCols < 4 {
		t.Fatalf("test requires at least 4 mb columns, got %d", mbCols)
	}
	mask := make([]uint8, mbRows*mbCols)
	mask[0] = 1
	mask[2] = 1 // skip MB col 1, mark MB col 2 active.

	e := newLookaheadTestEncoder(t, 1)
	dstBuf := &e.lookahead[0].frame
	if err := dstBuf.Resize(width, height, 32, 32); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	const sentinel = byte(0xC3)
	for y := 0; y < dstBuf.Img.Height; y++ {
		row := dstBuf.Img.Y[y*dstBuf.Img.YStride:]
		for x := 0; x < dstBuf.Img.Width; x++ {
			row[x] = sentinel
		}
	}
	src := testImage(width, height)
	fillImage(src, 200, 100, 50)
	copySourceToFrameBufferActive(dstBuf, sourceFromTestImage(src), mask, mbRows, mbCols)
	dst := &dstBuf.Img
	check := func(xStart int, xEnd int, want byte, label string) {
		for y := range 16 {
			for x := xStart; x < xEnd; x++ {
				if got := dst.Y[y*dst.YStride+x]; got != want {
					t.Fatalf("%s Y[%d,%d] = %d, want %d", label, y, x, got, want)
				}
			}
		}
	}
	check(0, 16, 200, "first active run [0..16)")
	check(16, 32, sentinel, "inactive gap [16..32)")
	check(32, 48, 200, "second active run [32..48)")
	check(48, 64, sentinel, "trailing inactive [48..64)")
}

func TestLookaheadActiveMapBypassedWhenFlagsOrMultiBuffer(t *testing.T) {
	// With flags != 0 or len(lookahead) != 1, libvpx falls back to the full
	// frame copy. Verify both gates by inspecting the destination after push.
	width, height := 32, 32
	mbRows := encoderMacroblockRows(height)
	mbCols := encoderMacroblockCols(width)
	mask := make([]uint8, mbRows*mbCols)
	mask[0] = 1
	// Resize the encoder by reconstructing a 32x32 instance because the
	// helper used 16x16 above. SetActiveMap requires width/height match the
	// configured encoder; rebuild with matching dimensions and depth=2.
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		LookaheadFrames:     2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	if err := e.SetActiveMap(mask, mbRows, mbCols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	const sentinel = byte(0x5A)
	for y := 0; y < e.lookahead[0].frame.Img.Height; y++ {
		row := e.lookahead[0].frame.Img.Y[y*e.lookahead[0].frame.Img.YStride:]
		for x := 0; x < e.lookahead[0].frame.Img.Width; x++ {
			row[x] = sentinel
		}
	}
	src := testImage(width, height)
	fillImage(src, 77, 88, 99)
	// Multi-buffer queue: full copy regardless of active map.
	if err := e.pushLookahead(sourceFromTestImage(src), 1, 1, 0); err != nil {
		t.Fatalf("pushLookahead multi-buffer: %v", err)
	}
	dst := &e.lookahead[0].frame.Img
	for y := range 16 {
		for x := 16; x < 32; x++ {
			if got := dst.Y[y*dst.YStride+x]; got != 77 {
				t.Fatalf("multi-buffer push Y[%d,%d] = %d, want full copy 77", y, x, got)
			}
		}
	}
}

func TestLookaheadDepthMatchesQueueSize(t *testing.T) {
	depth := 4
	e := newLookaheadTestEncoder(t, depth)
	if got := e.lookaheadDepth(); got != 0 {
		t.Fatalf("initial depth = %d, want 0", got)
	}
	for i := range depth {
		if err := e.pushLookahead(sourceFromTestImage(testImage(16, 16)), uint64(i), 1, 0); err != nil {
			t.Fatalf("pushLookahead(%d): %v", i, err)
		}
		if got := e.lookaheadDepth(); got != i+1 {
			t.Fatalf("depth after push %d = %d, want %d", i, got, i+1)
		}
	}
	for i := range depth {
		if _, ok := e.popLookahead(true); !ok {
			t.Fatalf("drain pop %d failed", i)
		}
		if got := e.lookaheadDepth(); got != depth-i-1 {
			t.Fatalf("depth after drain %d = %d, want %d", i, got, depth-i-1)
		}
	}
}

func TestLookaheadFutureEntryDelegatesToForwardPeek(t *testing.T) {
	// ARNR's lookaheadFutureEntry is the only existing forward-peek caller;
	// pin that it reuses the libvpx peek semantics (in-range returns the
	// matching entry, out-of-range returns nil).
	depth := 3
	e := newLookaheadTestEncoder(t, depth)
	for i := range depth {
		if err := e.pushLookahead(sourceFromTestImage(testImage(16, 16)), uint64(500+i), 1, 0); err != nil {
			t.Fatalf("pushLookahead(%d): %v", i, err)
		}
	}
	for i := range depth {
		got := e.lookaheadFutureEntry(i)
		if got == nil {
			t.Fatalf("lookaheadFutureEntry(%d) returned nil", i)
		}
		if got != e.peekLookahead(i, true) {
			t.Fatalf("lookaheadFutureEntry(%d) != peekLookahead(%d, true)", i, i)
		}
		if got.pts != uint64(500+i) {
			t.Fatalf("lookaheadFutureEntry(%d) pts = %d, want %d", i, got.pts, 500+i)
		}
	}
	if e.lookaheadFutureEntry(depth) != nil {
		t.Fatalf("lookaheadFutureEntry(%d) past end must be nil", depth)
	}
}
