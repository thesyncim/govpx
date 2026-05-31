package govpx_test

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

const (
	vp9DefaultBaseQIndexForTest      = 37
	vp9DefaultInterBaseQIndexForTest = 128
	vp9SteadyStateAllocRunsForTest   = 25
)

func vp9EncodedKeyframeForTest(t testing.TB, width int, height int, y byte) []byte {
	t.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: vp9DefaultBaseQIndexForTest,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder %dx%d: %v", width, height, err)
	}
	defer e.Close()
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, y, 128, 128))
	if err != nil {
		t.Fatalf("Encode %dx%d: %v", width, height, err)
	}
	if len(packet) == 0 {
		t.Fatalf("Encode %dx%d returned empty packet", width, height)
	}
	return packet
}

func vp9DecodeLastVisibleFrameForTest(t testing.TB, packets ...[]byte) govpx.Image {
	t.Helper()
	return vp9DecodeLastVisibleFrameWithOptionsForTest(t, govpx.VP9DecoderOptions{},
		packets...)
}

func vp9DecodeLastVisibleFrameWithOptionsForTest(t testing.TB,
	opts govpx.VP9DecoderOptions, packets ...[]byte,
) govpx.Image {
	t.Helper()
	d, err := govpx.NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	var last govpx.Image
	ok := false
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, frameOK := d.NextFrame(); frameOK {
			last = frame
			ok = true
		}
	}
	if !ok {
		t.Fatal("packet sequence did not publish a visible frame")
	}
	return last
}

func newVP9TestImageForTest(width int, height int) govpx.Image {
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	return govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func fillVP9PublicImageForTest(img *govpx.Image, value byte) {
	for i := range img.Y {
		img.Y[i] = value
	}
	for i := range img.U {
		img.U[i] = value
	}
	for i := range img.V {
		img.V[i] = value
	}
}

func appendVP9I420ForTest(out []byte, img govpx.Image) []byte {
	return testutil.AppendI420Planes(out, img.Width, img.Height,
		img.Y, img.YStride,
		img.U, img.UStride,
		img.V, img.VStride)
}

func assertVP9NeutralFrameForTest(t testing.TB, got govpx.Image, width int, height int) {
	t.Helper()
	assertVP9FilledFrameForTest(t, got, width, height, 128, 128, 128)
}

func assertVP9ImagesEqualForTest(t testing.TB, want govpx.Image, got govpx.Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, want.Width, want.Height)
	}
	if !vp9VisiblePlanesEqualForTest(want.Y, want.YStride, got.Y, got.YStride,
		want.Width, want.Height) {
		t.Fatal("Y plane differs")
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(want.Width, want.Height)
	if !vp9VisiblePlanesEqualForTest(want.U, want.UStride, got.U, got.UStride,
		uvWidth, uvHeight) {
		t.Fatal("U plane differs")
	}
	if !vp9VisiblePlanesEqualForTest(want.V, want.VStride, got.V, got.VStride,
		uvWidth, uvHeight) {
		t.Fatal("V plane differs")
	}
}

func assertVP9FilledFrameForTest(t testing.TB, got govpx.Image, width int, height int,
	yValue byte, uValue byte, vValue byte,
) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	assertVP9PlaneFilledForTest(t, "Y", got.Y, got.YStride, width, height, yValue)
	assertVP9PlaneFilledForTest(t, "U", got.U, got.UStride, uvWidth, uvHeight, uValue)
	assertVP9PlaneFilledForTest(t, "V", got.V, got.VStride, uvWidth, uvHeight, vValue)
}

func assertVP9PlaneFilledForTest(t testing.TB, name string, plane []byte,
	stride int, width int, height int, want byte,
) {
	t.Helper()
	if stride < width {
		t.Fatalf("%s stride = %d, want at least %d", name, stride, width)
	}
	wantLen := buffers.PlaneLen(stride, height, width)
	if len(plane) < wantLen {
		t.Fatalf("%s plane len = %d, want at least %d",
			name, len(plane), wantLen)
	}
	for row := range height {
		for col := range width {
			if got := plane[row*stride+col]; got != want {
				t.Fatalf("%s[%d,%d] = %d, want %d",
					name, row, col, got, want)
			}
		}
	}
}

func assertVP9PlaneAlignedForTest(t testing.TB, name string, plane []byte,
	alignment int,
) {
	t.Helper()
	if len(plane) == 0 {
		t.Fatalf("%s plane is empty", name)
	}
	ptr := uintptr(unsafe.Pointer(&plane[0]))
	if ptr%uintptr(alignment) != 0 {
		t.Fatalf("%s plane pointer %#x is not %d-byte aligned",
			name, ptr, alignment)
	}
}

func vp9VisiblePlanesEqualForTest(a []byte, aStride int, b []byte, bStride int,
	width int, height int,
) bool {
	for row := range height {
		aStart := row * aStride
		bStart := row * bStride
		if !bytes.Equal(a[aStart:aStart+width], b[bStart:bStart+width]) {
			return false
		}
	}
	return true
}
