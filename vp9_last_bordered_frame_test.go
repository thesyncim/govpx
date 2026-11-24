package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestEnsureLastBorderedInvalidWhenNoLastRef checks the early-return path:
// when e.refFrames[vp9LastRefSlot].valid is false (encoder freshly
// constructed, no frame yet reconstructed), ensureLastBordered must
// leave lastBorderedValid = false.
func TestEnsureLastBorderedInvalidWhenNoLastRef(t *testing.T) {
	var e VP9Encoder
	e.lastBorderedValid = true // sentinel — must be cleared.
	e.ensureLastBordered()
	if e.lastBorderedValid {
		t.Fatalf("ensureLastBordered with !refFrames[LAST].valid: lastBorderedValid=true, want false")
	}
}

// TestEnsureLastBorderedReplicatesEdge stores a synthetic LAST_FRAME image
// in the encoder and asserts ensureLastBordered builds a libvpx-shaped
// border with edge replication. Mirrors the per-frame lifecycle the
// refreshVP9EncoderRefs hook drives.
func TestEnsureLastBorderedReplicatesEdge(t *testing.T) {
	const w, h = 8, 8
	yPlane := make([]uint8, w*h)
	uPlane := make([]uint8, (w/2)*(h/2))
	vPlane := make([]uint8, (w/2)*(h/2))
	for y := range h {
		for x := range w {
			yPlane[y*w+x] = uint8(y*16 + x + 1)
		}
	}
	img := Image{
		Width:   w,
		Height:  h,
		Y:       yPlane,
		U:       uPlane,
		V:       vPlane,
		YStride: w,
		UStride: w / 2,
		VStride: w / 2,
	}

	var e VP9Encoder
	e.refFrames[vp9LastRefSlot].store(img)
	e.ensureLastBordered()

	if !e.lastBorderedValid {
		t.Fatalf("ensureLastBordered: lastBorderedValid=false after store, want true")
	}
	if e.lastBordered.W != w || e.lastBordered.H != h {
		t.Fatalf("lastBordered dims: got (%d,%d), want (%d,%d)",
			e.lastBordered.W, e.lastBordered.H, w, h)
	}
	if e.lastBordered.Border != common.VP9EncBorderInPixels {
		t.Fatalf("lastBordered.Border = %d, want %d",
			e.lastBordered.Border, common.VP9EncBorderInPixels)
	}
	stride := e.lastBordered.Stride
	originX := e.lastBordered.OriginX()
	originY := e.lastBordered.OriginY()
	// Visible body must match the input plane verbatim.
	for y := range h {
		for x := range w {
			got := e.lastBordered.Pixels[(originY+y)*stride+(originX+x)]
			want := yPlane[y*w+x]
			if got != want {
				t.Fatalf("visible(%d,%d): got %d want %d", x, y, got, want)
			}
		}
	}
	// Top-border row must replicate the first visible row.
	for x := range w {
		got := e.lastBordered.Pixels[(originY-1)*stride+(originX+x)]
		want := yPlane[0*w+x]
		if got != want {
			t.Fatalf("top-border(%d): got %d want %d", x, got, want)
		}
	}
	// Left-border column must replicate column 0.
	for y := range h {
		got := e.lastBordered.Pixels[(originY+y)*stride+(originX-1)]
		want := yPlane[y*w+0]
		if got != want {
			t.Fatalf("left-border(%d): got %d want %d", y, got, want)
		}
	}
}

// TestEnsureLastBorderedReusesAcrossCalls confirms the buffer is reused
// (backing slice retained) when ensureLastBordered fires for successive
// frames of the same dimensions.
func TestEnsureLastBorderedReusesAcrossCalls(t *testing.T) {
	const w, h = 16, 16
	yPlane := make([]uint8, w*h)
	uPlane := make([]uint8, (w/2)*(h/2))
	vPlane := make([]uint8, (w/2)*(h/2))
	for i := range yPlane {
		yPlane[i] = uint8(i)
	}
	img := Image{
		Width:   w,
		Height:  h,
		Y:       yPlane,
		U:       uPlane,
		V:       vPlane,
		YStride: w,
		UStride: w / 2,
		VStride: w / 2,
	}

	var e VP9Encoder
	e.refFrames[vp9LastRefSlot].store(img)
	e.ensureLastBordered()
	first := e.lastBordered.Pixels
	if len(first) == 0 {
		t.Fatalf("first build: empty buffer")
	}
	e.ensureLastBordered()
	second := e.lastBordered.Pixels
	if cap(first) != cap(second) {
		t.Fatalf("buffer reuse: cap mismatch %d vs %d", cap(first), cap(second))
	}
	// Mutate the first slice; the second must see the change because
	// they alias the same backing array.
	first[0] = 0xAB
	if second[0] != 0xAB {
		t.Fatalf("buffer reuse: backing array detached on reuse")
	}
}

func TestSubpelReferenceBorderedRebuildsAfterSameBufferStore(t *testing.T) {
	const w, h = 16, 16
	var e VP9Encoder
	ref := &e.refFrames[vp9LastRefSlot]
	ref.store(vp9BorderedTestImage(w, h, 23))
	e.ensureLastBordered()
	if !e.lastBorderedValid {
		t.Fatal("initial LAST border is invalid")
	}
	oldGeneration := ref.generation
	oldBacking := &ref.y[0]

	second := vp9BorderedTestImage(w, h, 91)
	ref.store(second)
	if &ref.y[0] != oldBacking {
		t.Fatal("reference store did not reuse backing; test needs same-buffer replacement")
	}
	if ref.generation == oldGeneration {
		t.Fatal("reference generation did not advance")
	}

	pixels, stride, originX, originY, _, _, ok :=
		e.vp9SubpelReferencePlane(vp9dec.LastFrame, ref)
	if !ok {
		t.Fatal("vp9SubpelReferencePlane returned !ok")
	}
	if got, want := pixels[originY*stride+originX], second.Y[0]; got != want {
		t.Fatalf("rebuilt visible origin = %d, want %d", got, want)
	}
	if e.lastBorderedGeneration != ref.generation {
		t.Fatalf("border generation = %d, want %d",
			e.lastBorderedGeneration, ref.generation)
	}
}

func TestSubpelReferenceBorderedCachesPerReferenceSlot(t *testing.T) {
	const w, h = 16, 16
	var e VP9Encoder
	golden := vp9BorderedTestImage(w, h, 23)
	altRef := vp9BorderedTestImage(w, h, 91)
	e.refFrames[vp9GoldenRefSlot].store(golden)
	e.refFrames[vp9AltRefSlot].store(altRef)

	goldenPixels, _, goldenOriginX, goldenOriginY, _, _, ok :=
		e.vp9SubpelReferencePlane(vp9dec.GoldenFrame,
			&e.refFrames[vp9GoldenRefSlot])
	if !ok || len(goldenPixels) == 0 ||
		!e.subpelRefBorderedValid[vp9GoldenRefSlot] ||
		e.subpelRefBorderedValid[vp9AltRefSlot] {
		t.Fatalf("golden cache state ok=%t valid=%v",
			ok, e.subpelRefBorderedValid)
	}
	goldenBuf := &goldenPixels[0]
	goldenStride := e.subpelRefBordered[vp9GoldenRefSlot].Stride
	if got := goldenPixels[goldenOriginY*goldenStride+goldenOriginX]; got != 23 {
		t.Fatalf("golden visible sample = %d, want 23", got)
	}

	altPixels, _, altOriginX, altOriginY, _, _, ok :=
		e.vp9SubpelReferencePlane(vp9dec.AltrefFrame,
			&e.refFrames[vp9AltRefSlot])
	if !ok || len(altPixels) == 0 ||
		!e.subpelRefBorderedValid[vp9GoldenRefSlot] ||
		!e.subpelRefBorderedValid[vp9AltRefSlot] {
		t.Fatalf("altref cache state ok=%t valid=%v",
			ok, e.subpelRefBorderedValid)
	}
	altBuf := &altPixels[0]
	if goldenBuf == altBuf {
		t.Fatal("golden and altref subpel bordered caches share backing storage")
	}
	altStride := e.subpelRefBordered[vp9AltRefSlot].Stride
	if got := altPixels[altOriginY*altStride+altOriginX]; got != 91 {
		t.Fatalf("altref visible sample = %d, want 91", got)
	}

	goldenAgain, _, _, _, _, _, ok := e.vp9SubpelReferencePlane(
		vp9dec.GoldenFrame, &e.refFrames[vp9GoldenRefSlot])
	if !ok || len(goldenAgain) == 0 || &goldenAgain[0] != goldenBuf {
		t.Fatal("golden subpel bordered cache was rebuilt after altref lookup")
	}
}

func TestSubpelReferenceBorderedInvalidatesOnlyRefreshedSlots(t *testing.T) {
	var e VP9Encoder
	for slot := range e.subpelRefBorderedValid {
		e.subpelRefBorderedValid[slot] = true
		e.subpelRefBorderedShared[slot] = true
	}

	e.invalidateVP9SubpelRefBordered(1 << vp9LastRefSlot)
	if e.subpelRefBorderedValid[vp9LastRefSlot] ||
		e.subpelRefBorderedShared[vp9LastRefSlot] {
		t.Fatal("refreshed LAST cache remained valid or shared")
	}
	for _, slot := range []int{vp9GoldenRefSlot, vp9AltRefSlot, 7} {
		if !e.subpelRefBorderedValid[slot] ||
			!e.subpelRefBorderedShared[slot] {
			t.Fatalf("unchanged slot %d was invalidated", slot)
		}
	}

	e.invalidateVP9SubpelRefBordered((1 << vp9GoldenRefSlot) | (1 << 7))
	for _, slot := range []int{vp9GoldenRefSlot, 7} {
		if e.subpelRefBorderedValid[slot] || e.subpelRefBorderedShared[slot] {
			t.Fatalf("refreshed slot %d remained valid or shared", slot)
		}
	}
	if !e.subpelRefBorderedValid[vp9AltRefSlot] ||
		!e.subpelRefBorderedShared[vp9AltRefSlot] {
		t.Fatal("unchanged ALTREF cache was invalidated")
	}
}

// Tile and count workers run synchronously within one frame epoch, and the
// reference pixels they mirror are immutable refcounted pool buffers, so
// their lastBordered is an intentional read-only alias of the parent's
// buffer (lastBorderedShared). A rebuild on a worker must detach to a
// private allocation before writing. Frame-parallel helpers run
// concurrently with the parent across frames and must stay fully private.
func TestVP9WorkerPrepSharesLastBorderedReadOnly(t *testing.T) {
	tests := []struct {
		name    string
		shared  bool
		prepare func(worker, src *VP9Encoder)
	}{
		{
			name:   "count",
			shared: true,
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9CountWorker(src, 16, 16, 2, 2)
			},
		},
		{
			name:   "tile-encode",
			shared: true,
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9TileEncodeWorker(src, 2, 2)
			},
		},
		{
			name:   "frame-parallel",
			shared: false,
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9FrameParallelWorker(src, 2, 2, 16, 16)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := newVP9LastBorderedEncoderForTest(t, 16, 16)
			var worker VP9Encoder
			vp9dec.SetupBlockPlanes(&worker.planes, 1, 1)

			tc.prepare(&worker, src)

			if !worker.lastBorderedValid {
				t.Fatalf("worker lastBorderedValid=false after prep")
			}
			if len(worker.lastBordered.Pixels) == 0 ||
				len(src.lastBordered.Pixels) == 0 {
				t.Fatalf("lastBordered buffers unexpectedly empty")
			}
			aliases := &worker.lastBordered.Pixels[0] == &src.lastBordered.Pixels[0]
			if aliases != tc.shared {
				t.Fatalf("worker lastBordered aliasing = %t, want %t",
					aliases, tc.shared)
			}
			if worker.lastBorderedShared != tc.shared {
				t.Fatalf("worker lastBorderedShared = %t, want %t",
					worker.lastBorderedShared, tc.shared)
			}
			if !tc.shared {
				return
			}
			// A forced rebuild on the worker must detach from the shared
			// buffer instead of writing through the parent's pixels.
			worker.lastBorderedValid = false
			worker.ensureLastBordered()
			if !worker.lastBorderedValid {
				t.Fatalf("worker lastBorderedValid=false after rebuild")
			}
			if worker.lastBorderedShared {
				t.Fatalf("worker still marked shared after rebuild")
			}
			if &worker.lastBordered.Pixels[0] == &src.lastBordered.Pixels[0] {
				t.Fatalf("worker rebuild wrote through the parent's buffer")
			}
		})
	}
}

func TestVP9TileWorkerPrepSharesSubpelRefBorderedReadOnly(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(worker, src *VP9Encoder)
	}{
		{
			name: "count",
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9CountWorker(src, 16, 16, 2, 2)
			},
		},
		{
			name: "tile-encode",
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9TileEncodeWorker(src, 2, 2)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := newVP9LastBorderedEncoderForTest(t, 16, 16)
			src.refFrames[vp9GoldenRefSlot].store(vp9BorderedTestImage(16, 16, 33))
			golden, _, _, _, _, _, ok := src.vp9SubpelReferencePlane(
				vp9dec.GoldenFrame, &src.refFrames[vp9GoldenRefSlot])
			if !ok || len(golden) == 0 ||
				!src.subpelRefBorderedValid[vp9GoldenRefSlot] {
				t.Fatalf("source golden subpel cache not ready: ok=%t valid=%v",
					ok, src.subpelRefBorderedValid)
			}
			srcBuf := &golden[0]

			var worker VP9Encoder
			vp9dec.SetupBlockPlanes(&worker.planes, 1, 1)

			tc.prepare(&worker, src)

			if !worker.subpelRefBorderedValid[vp9GoldenRefSlot] {
				t.Fatalf("worker golden subpel cache invalid after prep")
			}
			if !worker.subpelRefBorderedShared[vp9GoldenRefSlot] {
				t.Fatalf("worker golden subpel cache not marked shared")
			}
			if len(worker.subpelRefBordered[vp9GoldenRefSlot].Pixels) == 0 ||
				&worker.subpelRefBordered[vp9GoldenRefSlot].Pixels[0] != srcBuf {
				t.Fatalf("worker golden subpel cache did not alias parent")
			}

			worker.subpelRefBordered[vp9GoldenRefSlot].W = 0
			rebuilt, _, _, _, _, _, ok := worker.vp9SubpelReferencePlane(
				vp9dec.GoldenFrame, &worker.refFrames[vp9GoldenRefSlot])
			if !ok || len(rebuilt) == 0 {
				t.Fatalf("worker golden subpel rebuild failed: ok=%t len=%d",
					ok, len(rebuilt))
			}
			if worker.subpelRefBorderedShared[vp9GoldenRefSlot] {
				t.Fatalf("worker golden subpel cache still marked shared after rebuild")
			}
			if &rebuilt[0] == srcBuf {
				t.Fatalf("worker golden subpel rebuild wrote through parent cache")
			}
		})
	}
}

func TestVP9WorkerPrepKeepsMLPartitionPaddedBuffersPrivate(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(worker, src *VP9Encoder)
	}{
		{
			name: "count",
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9CountWorker(src, 16, 16, 2, 2)
			},
		},
		{
			name: "tile-encode",
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9TileEncodeWorker(src, 2, 2)
			},
		},
		{
			name: "frame-parallel",
			prepare: func(worker, src *VP9Encoder) {
				worker.prepareVP9FrameParallelWorker(src, 2, 2, 16, 16)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := newVP9LastBorderedEncoderForTest(t, 16, 16)
			src.mlPartitionPaddedLast.pixels = []uint8{1, 2, 3}
			src.mlPartitionPaddedSrc.pixels = []uint8{4, 5, 6}

			var worker VP9Encoder
			vp9dec.SetupBlockPlanes(&worker.planes, 1, 1)
			worker.mlPartitionPaddedLast.pixels = []uint8{7, 8, 9}
			worker.mlPartitionPaddedSrc.pixels = []uint8{10, 11, 12}

			tc.prepare(&worker, src)

			if len(worker.mlPartitionPaddedLast.pixels) == 0 ||
				len(worker.mlPartitionPaddedSrc.pixels) == 0 {
				t.Fatalf("worker ML padded buffers unexpectedly empty")
			}
			if &worker.mlPartitionPaddedLast.pixels[0] ==
				&src.mlPartitionPaddedLast.pixels[0] {
				t.Fatalf("worker aliases parent ML padded LAST buffer")
			}
			if &worker.mlPartitionPaddedSrc.pixels[0] ==
				&src.mlPartitionPaddedSrc.pixels[0] {
				t.Fatalf("worker aliases parent ML padded source buffer")
			}
		})
	}
}

func newVP9LastBorderedEncoderForTest(t *testing.T, w, h int) *VP9Encoder {
	t.Helper()
	img := vp9BorderedTestImage(w, h, 0)

	var e VP9Encoder
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.refFrames[vp9LastRefSlot].store(img)
	e.ensureLastBordered()
	if !e.lastBorderedValid {
		t.Fatalf("source lastBorderedValid=false after setup")
	}
	return &e
}

func vp9BorderedTestImage(w, h int, base uint8) Image {
	yPlane := make([]uint8, w*h)
	uPlane := make([]uint8, (w/2)*(h/2))
	vPlane := make([]uint8, (w/2)*(h/2))
	for i := range yPlane {
		yPlane[i] = base + uint8(i)
	}
	return Image{
		Width:   w,
		Height:  h,
		Y:       yPlane,
		U:       uPlane,
		V:       vPlane,
		YStride: w,
		UStride: w / 2,
		VStride: w / 2,
	}
}
