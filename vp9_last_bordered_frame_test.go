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

func TestVP9WorkerPrepKeepsLastBorderedPrivate(t *testing.T) {
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
			if &worker.lastBordered.Pixels[0] == &src.lastBordered.Pixels[0] {
				t.Fatalf("worker aliases parent lastBordered buffer")
			}
		})
	}
}

func newVP9LastBorderedEncoderForTest(t *testing.T, w, h int) *VP9Encoder {
	t.Helper()
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
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.refFrames[vp9LastRefSlot].store(img)
	e.ensureLastBordered()
	if !e.lastBorderedValid {
		t.Fatalf("source lastBorderedValid=false after setup")
	}
	return &e
}
